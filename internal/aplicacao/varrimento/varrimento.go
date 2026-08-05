// Package varrimento corre os bancos sobre a grelha de preço, em paralelo, e
// devolve as observações.
//
// É o único caminho por onde este repositório fala com um banco
// (ARQUITETURA.md §1) e corre por subcomando, em hora morta — nunca no caminho
// de um cliente. Uma comparação responde-se por consulta à grelha e cálculo
// local, e não chega aqui.
//
// O que vive neste pacote é mecânica de concorrência e mais nada: que pontos se
// varrem decide-se em KAN-16, quem escreve as observações em Postgres é a
// infra, e o resíduo de cada varrimento (§7.4) é medição sobre o resultado.
package varrimento

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/bancos"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// Ponto é um ponto da grelha: o pedido que se faz ao banco e a chave do cenário
// que ele representa.
//
// ⚠️ O Cenario é opaco aqui. É a chave estruturada de catalogo_taxas (§4) —
// banda de LTV, tipo de taxa, período fixo, finalidade — e o formato exacto
// fixa-se ao escrever a CGD. O orquestrador não a lê nem a constrói: transporta
// -a do ponto para a observação, que é o que a torna identificável do outro
// lado.
type Ponto struct {
	Cenario string
	Pedido  dominio.Pedido
}

// Observacao é o que saiu de um banco num ponto da grelha, com o ponto
// agarrado. Corresponde a uma linha de catalogo_taxas — mas escrevê-la é da
// infra; aqui só se produz.
type Observacao struct {
	Ponto  Ponto
	Oferta dominio.Oferta

	// Degrau, quando não é nulo, diz que esta linha é um degrau da escala de
	// LTV: o spread dela vale sobre o intervalo medido, e não só no ponto.
	// Nulo é uma observação num ponto, que não afirma intervalo nenhum.
	//
	// ⚠️ É a distinção que a §4 fixa em «O que estas três colunas descrevem». A
	// observação continua a ser uma observação a sério — a que traz o spread
	// servido do degrau —, e é por isso que isto é um campo a mais e não um
	// tipo à parte: a linha de catalogo_taxas é a mesma.
	Degrau *dominio.DegrauLTV
}

// Sucesso deriva da oferta: uma observação de falha traz o erro estruturado em
// Oferta.Erro.
func (o Observacao) Sucesso() bool { return o.Oferta.Sucesso() }

// Salto é um banco que não se varreu, e porquê.
//
// São duas coisas diferentes e distinguem-se por errors.Is: ErrBancoTravado é o
// travão a funcionar — outro varrimento tem o banco, e não há nada a corrigir —
// e qualquer outro motivo é falha nossa ao tomar o travão (a base em baixo, por
// exemplo). ⚠️ Nenhuma das duas vira observação de falha: dar uma avaria nossa
// como banco_indisponivel escreveria no catálogo que o banco está em baixo
// quando quem está em baixo somos nós.
type Salto struct {
	BancoID string
	Motivo  error
}

// Resultado é o que uma corrida do varrimento produziu.
//
// As observações vêm pela ordem dos bancos e, dentro de cada banco, pela ordem
// dos pontos — não pela ordem em que chegaram. Uma corrida é um Resultado, e é
// isso que agrupa as linhas do mesmo varrimento_id; quem persiste atribui o id.
type Resultado struct {
	Observacoes []Observacao
	Saltados    []Salto

	// EscalasNaoMedidas são os bancos cuja escala de LTV não se conseguiu medir.
	//
	// ⚠️ Não é um Salto e não se confunde com um: o banco FOI varrido, e as
	// observações dos pontos dele estão no Resultado. O que falta é a dimensão
	// do LTV. Calá-lo deixava passar uma grelha sem degraus com ar de grelha
	// completa, e quem a lesse servia o spread do ponto de referência a toda a
	// gente — que é a banda única que a KAN-35 matou.
	EscalasNaoMedidas []Salto
}

// ErrBancoTravado diz que outro varrimento tem este banco. Não é falha.
var ErrBancoTravado = errors.New("banco já está a ser varrido")

// ErrPanico marca um pânico do nosso código, recuperado dentro da goroutine do
// ponto.
//
// ⚠️ Hoje sai como banco_indisponivel, que culpa o banco por um erro nosso. É
// conhecido e tem issue própria (KAN-30): o recover é daqui, o código de erro
// decide-se lá. O sentinela existe para que essa decisão tenha por onde pegar.
var ErrPanico = errors.New("pânico ao simular")

// Travao garante a regra da §7.2: nunca dois varrimentos do mesmo banco ao
// mesmo tempo.
//
// ⚠️ A implementação vive na base — advisory lock em Postgres — e não no
// processo. Duas instâncias de um travão em memória não se travam uma à outra;
// duas sessões de Postgres travam-se. O que o v1 fez está na §7.5.
//
// A porta declara-se aqui, do lado de quem a usa, e implementa-se na infra: é o
// que mantém este pacote sem SQL (§3).
type Travao interface {
	// Tomar toma o travão do banco. Devolve ErrBancoTravado — e não uma falha —
	// quando outro varrimento já o tem.
	Tomar(ctx context.Context, bancoID string) (Largar, error)
}

// Largar devolve o travão.
//
// ⚠️ Não recebe ctx de propósito: chama-se num defer, e nessa altura o ctx do
// varrimento pode já estar cancelado. Medido a 2026-07-25: um ctx derivado de
// um pai que termina fica `context canceled` no mesmo instante — largar com ele
// nunca chegaria à base, e o banco ficaria travado até a sessão morrer. Quem
// implementa larga com um ctx destacado (context.WithoutCancel) e prazo
// próprio.
type Largar func()

// Prazos é o tecto de tempo de um pedido, por custo declarado do banco.
//
// ⚠️ Dois e não um. O Requisitos().Custo já traz `barato` (1-2 s medidos no v1)
// e `caro` (30-80 s), e um tecto único de 90 s deixaria um banco de HTTP
// avariado a segurar um lugar durante 90 s por nada.
type Prazos struct {
	Barato time.Duration
	Caro   time.Duration
}

// Os prazos de omissão saem das medições do v1 (2026-07-22, contentor contra
// Postgres 17): HTTP puro custa 1-2 s, browser 30-80 s. Não são esses números —
// são tectos com folga por cima deles, que é o que um prazo é. Revêem-se com o
// primeiro banco a sério que os exercite.
const (
	PrazoBaratoOmissao = 10 * time.Second
	PrazoCaroOmissao   = 150 * time.Second
)

// PorBancoOmissao é quantos pontos da grelha se pedem ao mesmo banco ao mesmo
// tempo.
//
// ⚠️ Era 1 enquanto não havia número medido. Há: contra a CGD a sério
// (2026-07-26, 8 pontos), o tempo por ponto foi de 1,086 s com um pedido de
// cada vez, 603 ms com dois e 367 ms com quatro, **sem uma única falha em
// nenhum dos três**. Está no TestE2ETectoDeConcorrenciaDaCGD.
//
// É 2 e não 4, e a diferença entre os dois não é técnica. O varrimento corre em
// hora morta e ninguém está à espera dele: comprar mais 40 % de velocidade ao
// preço de quadruplicar a carga que pomos num simulador público alheio não é
// uma troca que se faça por poder fazer. Dois está medido, chega, e deixa
// margem para o banco. Quem tiver razão para subir sobe o campo — e mede o que
// isso lhe faz.
const PorBancoOmissao = 2

// prazo devolve o tecto do custo. O custo inválido não chega aqui — Novo
// recusa-o à construção —, e se chegasse valia o de browser: cortar um banco
// lento por não saber o que ele é seria inventar uma falha.
func (p Prazos) prazo(c dominio.Custo) time.Duration {
	if c == dominio.CustoBarato {
		return p.Barato
	}
	return p.Caro
}

// Config é o que o varredor precisa. Os bancos e o travão chegam por injecção —
// este pacote nunca importa o registo, e é isso que o torna testável com bancos
// falsos (§3).
type Config struct {
	Bancos []bancos.Banco
	Travao Travao

	// Prazos a zero valem os de omissão, campo a campo.
	Prazos Prazos

	// PorBanco a zero vale PorBancoOmissao.
	PorBanco int

	// Degraus mede a escala de LTV de cada banco. Nulo não descobre escala
	// nenhuma — e é o que serve quem varre só pontos, incluindo os testes que
	// existiam antes desta dimensão.
	Degraus DegrausDe

	// Agora é o relógio. Nulo vale time.Now. O domínio não tem relógio; quem
	// carimba a hora da captura é esta camada (ver dominio.Oferta.CapturadoEm).
	Agora func() time.Time
}

// Varredor corre um varrimento.
type Varredor struct {
	bancos   []bancos.Banco
	travao   Travao
	prazos   Prazos
	porBanco int
	degraus  DegrausDe
	agora    func() time.Time
}

// Novo constrói o varredor e recusa o que não é corrível.
//
// ⚠️ Recusa o travão nulo. Um travão ausente não é "sem travão por agora": é o
// varrimento a bater no mesmo banco a partir de dois sítios, que é a regra da
// §7.2 ao contrário. E recusa o banco que não declara um custo válido, porque é
// do custo que sai o prazo — um banco sem custo não tem prazo derivável, e
// escolher-lhe um por omissão era esconder um erro de declaração.
func Novo(c Config) (*Varredor, error) {
	if c.Travao == nil {
		return nil, errors.New("varrimento sem travão — a §7.2 exige um, e nenhum é o defeito que o v1 avisava no arranque")
	}
	for _, b := range c.Bancos {
		if b == nil {
			return nil, errors.New("varrimento com um banco nulo na lista")
		}
		if custo := b.Requisitos().Custo; !custo.Valido() {
			return nil, fmt.Errorf("banco %q declara o custo %q, e é do custo que sai o prazo", b.ID(), custo)
		}
	}

	v := &Varredor{
		bancos:   c.Bancos,
		travao:   c.Travao,
		prazos:   c.Prazos,
		porBanco: c.PorBanco,
		degraus:  c.Degraus,
		agora:    c.Agora,
	}
	if v.prazos.Barato <= 0 {
		v.prazos.Barato = PrazoBaratoOmissao
	}
	if v.prazos.Caro <= 0 {
		v.prazos.Caro = PrazoCaroOmissao
	}
	if v.porBanco <= 0 {
		v.porBanco = PorBancoOmissao
	}
	if v.agora == nil {
		v.agora = time.Now
	}
	return v, nil
}

// saidaDeBanco é o que um banco produziu: ou as observações dos seus pontos, ou
// a razão por que não se varreu.
type saidaDeBanco struct {
	observacoes []Observacao
	salto       *Salto

	// escalaFalhada diz que os pontos correram e a escala de LTV não. São
	// coisas independentes: uma não anula a outra, e por isso viaja ao lado das
	// observações em vez de as substituir.
	escalaFalhada *Salto
}

// PontosDe diz que pontos se pedem a cada banco.
//
// ⚠️ É função do banco e não uma lista só, e isso não é generalidade
// especulativa: a grelha é DERIVADA dos Requisitos de cada banco (§4) — os
// períodos fixos, os tenores da Euribor e os produtos são dele, e não do
// projecto. A CGD tem 7 períodos e impõe o tenor; o Novo Banco tem 9 e aceita
// três. Uma lista só para todos pedia a cada banco pontos que ele não pratica,
// e gastava um pedido por cada um para receber uma recusa.
type PontosDe func(b bancos.Banco) []Ponto

// MesmosPontos é a PontosDe de quem quer a mesma lista para todos os bancos.
// Serve os testes e quem varre um ponto de referência transversal.
func MesmosPontos(pontos []Ponto) PontosDe {
	return func(bancos.Banco) []Ponto { return pontos }
}

// DegrausDe mede a escala de LTV de um banco e devolve-a como observações de
// degrau — as linhas que trazem ltv_min/ltv_max.
//
// ⚠️ Declara-se como porta, e não se chama aqui o grelha.DescobrirBanco, porque
// é o grelha que importa este pacote e não ao contrário (§3). Quem a preenche é
// a infra, ao montar o varredor.
//
// ⚠️ E corre DENTRO do travão do banco, a seguir aos pontos dele: são ~86
// pedidos ao mesmo simulador, e deixá-los fora do travão era pôr no banco
// exactamente a carga concorrente que a §7.2 existe para impedir.
type DegrausDe func(ctx context.Context, b bancos.Banco) ([]Observacao, error)

// Varrer corre todos os bancos sobre a mesma lista de pontos.
func (v *Varredor) Varrer(ctx context.Context, pontos []Ponto) Resultado {
	return v.VarrerCada(ctx, MesmosPontos(pontos))
}

// VarrerCada corre cada banco sobre os pontos que lhe pertencem e devolve o que
// saiu.
//
// Não devolve erro: as falhas são por banco e por ponto, e cada uma tem o seu
// lugar no Resultado. Um banco avariado nunca derruba o varrimento (§5).
func (v *Varredor) VarrerCada(ctx context.Context, pontos PontosDe) Resultado {
	if pontos == nil || len(v.bancos) == 0 {
		return Resultado{}
	}

	saidas := make([]saidaDeBanco, len(v.bancos))

	var wg sync.WaitGroup
	for i, b := range v.bancos {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Cada goroutine escreve o seu índice e mais nenhum: não há partilha
			// para proteger, e a saída sai pela ordem dos bancos em vez da ordem
			// em que chegaram.
			saidas[i] = v.varrerBanco(ctx, b, pontos(b))
		}()
	}
	wg.Wait()

	var r Resultado
	for _, s := range saidas {
		if s.salto != nil {
			r.Saltados = append(r.Saltados, *s.salto)
			continue
		}
		r.Observacoes = append(r.Observacoes, s.observacoes...)
		if s.escalaFalhada != nil {
			r.EscalasNaoMedidas = append(r.EscalasNaoMedidas, *s.escalaFalhada)
		}
	}
	return r
}

// varrerBanco toma o travão do banco e corre os seus pontos, no máximo
// porBanco de cada vez.
func (v *Varredor) varrerBanco(ctx context.Context, b bancos.Banco, pontos []Ponto) saidaDeBanco {
	id := b.ID()

	// ⚠️ Sem pontos não se toma o travão. Tomá-lo para não fazer nada seria
	// impedir, durante esse instante, um varrimento a sério do mesmo banco —
	// e é precisamente o que o travão existe para arbitrar.
	if len(pontos) == 0 {
		return saidaDeBanco{}
	}

	largar, err := v.travao.Tomar(ctx, id)
	if err != nil {
		return saidaDeBanco{salto: &Salto{BancoID: id, Motivo: err}}
	}
	defer largar()

	prazo := v.prazos.prazo(b.Requisitos().Custo)
	observacoes := make([]Observacao, len(pontos))

	// Os trabalhadores são o tecto de concorrência contra este banco. Puxam
	// índices de um canal em vez de haver uma goroutine por ponto: assim o
	// número de goroutines é o tecto, e não o tamanho da grelha.
	indices := make(chan int)
	var wg sync.WaitGroup
	for range min(v.porBanco, len(pontos)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range indices {
				observacoes[i] = v.simular(ctx, b, pontos[i], prazo)
			}
		}()
	}

	// Cancelar o varrimento pára de alimentar. Como os índices se entregam por
	// ordem, o que ficou para trás é a cauda — e devolve-se só o que se chegou a
	// tentar, em vez de linhas por preencher com ar de observações.
	entregues := 0
alimentar:
	for i := range pontos {
		select {
		case indices <- i:
			entregues = i + 1
		case <-ctx.Done():
			break alimentar
		}
	}
	close(indices)
	wg.Wait()

	saida := saidaDeBanco{observacoes: observacoes[:entregues]}

	// A escala de LTV, ainda dentro do travão deste banco.
	//
	// ⚠️ Só se o varrimento dos pontos não tiver sido interrompido. Depois de um
	// cancelamento, ir buscar mais ~86 pontos ao banco é bater em quem já
	// desistimos de ouvir; e a descoberta devolveria de qualquer forma uma
	// escala truncada, que é o que o grelha.Descobrir recusa fazer.
	if v.degraus != nil && ctx.Err() == nil {
		degraus, err := v.degraus(ctx, b)
		if err != nil {
			saida.escalaFalhada = &Salto{BancoID: id, Motivo: err}
		} else {
			saida.observacoes = append(saida.observacoes, degraus...)
		}
	}
	return saida
}

// simular corre um ponto contra um banco, com prazo próprio, e devolve sempre
// uma observação — de sucesso ou de falha.
//
// ⚠️ A chamada ao banco corre numa goroutine e espera-se por ela em select com
// o prazo. É a diferença entre o prazo ser um pedido e ser um facto: um banco
// que ignore o ctx não segura o varrimento, porque quem desiste é este lado. A
// goroutine abandonada não fica pendurada — o canal tem espaço para uma
// resposta, portanto ela acaba sozinha quando o banco finalmente voltar — mas
// sobrevive a esta função, e é por isso que respeitar o ctx continua a ser
// obrigação de cada banco, afirmada um a um com prova.RespeitaPrazo.
func (v *Varredor) simular(ctx context.Context, b bancos.Banco, p Ponto, prazo time.Duration) Observacao {
	ctx, cancelar := context.WithTimeout(ctx, prazo)
	defer cancelar()

	type resposta struct {
		oferta dominio.Oferta
		err    error
	}
	// Espaço para uma: sem ele, um banco surdo que voltasse depois de nós
	// desistirmos ficava bloqueado para sempre a escrever num canal sem leitor —
	// e aí a fuga já era nossa.
	resp := make(chan resposta, 1)

	go func() {
		oferta, err := correr(ctx, b, p.Pedido)
		resp <- resposta{oferta: oferta, err: err}
	}()

	var oferta dominio.Oferta
	select {
	case r := <-resp:
		if r.err != nil {
			oferta = dominio.Falhar(b.ID(), b.Nome(), traduzir(r.err, b.Nome()))
			break
		}
		oferta = r.oferta
	case <-ctx.Done():
		oferta = dominio.Falhar(b.ID(), b.Nome(), &dominio.ErroOferta{
			Codigo: dominio.ErroBancoIndisponivel,
			Mensagem: fmt.Sprintf(
				"O %s não respondeu dentro do prazo de %s e não desistiu quando lho pedimos.",
				b.Nome(), prazo),
		})
	}

	// Quem sabe a quem perguntou é este lado, e a linha do catálogo tem de o
	// dizer mesmo que o banco se esqueça de se identificar.
	oferta.BancoID, oferta.BancoNome = b.ID(), b.Nome()
	oferta.CapturadoEm = v.agora()
	return Observacao{Ponto: p, Oferta: oferta}
}

// correr chama o banco com o pânico recuperado.
//
// ⚠️ É aqui que fica a captura genérica, e num sítio só. No v1 ela obrigava a um
// except Exception dentro de cada scraper; aqui, dentro de um banco, os erros
// devolvem-se e não se engolem (§5).
func correr(ctx context.Context, b bancos.Banco, p dominio.Pedido) (oferta dominio.Oferta, err error) {
	defer func() {
		if r := recover(); r != nil {
			oferta, err = dominio.Oferta{}, fmt.Errorf("%w: %v", ErrPanico, r)
		}
	}()
	return b.Simular(ctx, p)
}

// traduzir converte o erro de um banco no erro estruturado que a observação
// guarda.
func traduzir(err error, bancoNome string) *dominio.ErroOferta {
	// O banco que já se explicou em dominio.ErroOferta sabe melhor do que nós o
	// que lhe aconteceu: produto indisponível, prazo impossível, resposta
	// ilegível. Não se sobrepõe.
	var estruturado *dominio.ErroOferta
	if errors.As(err, &estruturado) {
		return estruturado
	}

	if errors.Is(err, ErrPanico) {
		// ⚠️ Culpa o banco por um erro nosso. Conhecido, e é o objecto da KAN-30.
		return &dominio.ErroOferta{
			Codigo:   dominio.ErroBancoIndisponivel,
			Mensagem: fmt.Sprintf("Erro nosso ao simular o %s: %v", bancoNome, err),
		}
	}

	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return &dominio.ErroOferta{
			Codigo:   dominio.ErroBancoIndisponivel,
			Mensagem: fmt.Sprintf("O %s não respondeu dentro do prazo.", bancoNome),
		}
	}

	// O contrato do banco diz que só se devolve erro quando não se conseguiu
	// responder de todo — logo, o que sobra é indisponibilidade.
	return &dominio.ErroOferta{
		Codigo:   dominio.ErroBancoIndisponivel,
		Mensagem: fmt.Sprintf("O %s não respondeu: %v", bancoNome, err),
	}
}

// PrazoAplicado devolve o prazo que o banco praticou nesta observação, em meses.
//
// ⚠️ Existe porque a resposta precisa do prazo e o plano de fases **não se
// grava**: a §4 não lhe deu coluna, e uma observação relida de `catalogo_taxas`
// chega sem ele. Sem isto, o `comparar` funcionava sobre observações em memória
// e partia-se sobre observações lidas da base — e o defeito só apareceria com a
// base montada, que é o pior sítio para o descobrir.
//
// A ordem das fontes é a da fiabilidade, e não a da conveniência:
//
//  1. o **plano de fases**, quando existe. É o que o banco descreveu;
//  2. o **ajuste ao prazo**, quando houve um. É o que ele declarou ter aplicado;
//  3. o prazo do **pedido**. É o que se lhe perguntou, e sem 1 nem 2 é a melhor
//     coisa que há — porque não ter ajuste quer dizer que ele não mexeu.
func PrazoAplicado(o Observacao) int {
	if n := len(o.Oferta.Fases); n > 0 {
		return o.Oferta.Fases[n-1].AteMes
	}
	if anos, ok := o.Oferta.Aplicado()[string(dominio.AjustadoPrazoAnos)].(int); ok && anos > 0 {
		return anos * 12
	}
	return o.Ponto.Pedido.PrazoAnos * 12
}
