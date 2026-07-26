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
// processo. Não é preferência de estilo: o v1 tinha o gate por banco dentro do
// processo e avisava no arranque que, com mais do que um worker, o mesmo banco
// levava N scrapes em paralelo — precisamente o que o gate existia para evitar.
// Duas instâncias de um travão em memória não se travam uma à outra; duas
// sessões de Postgres travam-se.
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
// ⚠️ É 1 porque não há número medido. O varrimento não é um pedido de cliente:
// são dezenas de pontos contra o mesmo simulador público, a partir do nosso IP,
// e quantos aguenta em paralelo mede-se contra o banco, não se escolhe por
// instinto. Um de cada vez não precisa de justificação; qualquer valor acima
// precisa, e é para isso que o campo existe. O primeiro sítio onde a medição é
// possível é a CGD (KAN-9).
const PorBancoOmissao = 1

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
}

// Varrer corre todos os bancos sobre todos os pontos e devolve o que saiu.
//
// Não devolve erro: as falhas são por banco e por ponto, e cada uma tem o seu
// lugar no Resultado. Um banco avariado nunca derruba o varrimento (§5).
func (v *Varredor) Varrer(ctx context.Context, pontos []Ponto) Resultado {
	if len(pontos) == 0 || len(v.bancos) == 0 {
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
			saidas[i] = v.varrerBanco(ctx, b, pontos)
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
	}
	return r
}

// varrerBanco toma o travão do banco e corre os seus pontos, no máximo
// porBanco de cada vez.
func (v *Varredor) varrerBanco(ctx context.Context, b bancos.Banco, pontos []Ponto) saidaDeBanco {
	id := b.ID()

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

	return saidaDeBanco{observacoes: observacoes[:entregues]}
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
