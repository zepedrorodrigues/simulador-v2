// Package varrer monta o varrimento para o subcomando `simulador varrer`.
//
// ⚠️ Existe porque o binário não pode montar isto sozinho, e é de propósito: o
// depguard nega ao `cmd` os imports de `dominio`, `bancos` e `aplicacao`
// (ARQUITETURA.md §3, «cmd → infra»). O binário monta o que a infra construir.
// Aqui é onde o registo dos bancos, os transportes, a grelha, o travão e o
// catálogo se encontram — e é a única função deste pacote.
//
// O varrimento corre por subcomando e não por rota HTTP (§7.1): uma rota teria
// de ser protegida, e autenticação foi o que o v1 ganhou sem decidir e teve de
// apagar em três migrações.
package varrer

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/grelha"
	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/varrimento"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/transporte"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
	"github.com/zepedrorodrigues/simulador-v2/internal/infra/catalogo"
	"github.com/zepedrorodrigues/simulador-v2/internal/infra/travao"
)

// SeAntigoOmissao é a guarda de idempotência por omissão.
//
// ⚠️ Seis horas, e não zero. A §7.1 quer o varrimento cíclico várias vezes ao
// dia em horas mortas; seis horas deixam quatro corridas por dia e travam a
// segunda de quem disparar duas à mão seguidas — que é o defeito medido do v1.
// Quem quiser correr sem guarda escreve `--se-antigo=0`, e escrevê-lo é a
// escolha.
const SeAntigoOmissao = 6 * time.Hour

// Opcoes é o que o subcomando deixa escolher.
type Opcoes struct {
	// SeAntigo é a guarda: só corre se o último varrimento for mais velho do
	// que isto. Zero desliga-a.
	SeAntigo time.Duration

	// Bancos limita a corrida a alguns ids. Vazio corre os registados todos.
	Bancos []string
}

// BancoSaltado é um banco que não se varreu, e porquê.
type BancoSaltado struct {
	ID     string
	Motivo string
}

// Relatorio é o que a corrida tem para dizer.
//
// ⚠️ Só tipos primitivos. É o que o binário imprime, e o binário não conhece o
// domínio — se este tipo trouxesse uma dominio.Oferta, a §3 caía por aqui.
type Relatorio struct {
	VarrimentoID string
	Saltado      bool
	Motivo       string

	Bancos      []string
	Pontos      int
	Observacoes int
	Falhas      int

	BancosSaltados []BancoSaltado

	// EscalasNaoMedidas são os bancos varridos a que faltou a escala de LTV.
	// Distinto de BancosSaltados: aqui houve observações gravadas.
	EscalasNaoMedidas []BancoSaltado

	// Residuos é o resíduo da §7.4 resumido por banco, pela ordem dos ids.
	Residuos []ResiduoDeBanco
}

// ResiduoDeBanco resume o resíduo de um banco numa corrida.
//
// ⚠️ Existe para o número APARECER a quem corre o subcomando, e não só ficar na
// coluna. A §7.4 quer que uma mudança do lado do banco «apareça como número, não
// como silêncio» — e uma medição que só existe na base é silêncio para quem
// acabou de varrer.
//
// Os valores são strings em euros com duas casas, e não float: são para
// imprimir, e converter um decimal para float64 no caminho da impressão é
// reintroduzir na saída o ruído que a §4 proíbe na tabela.
type ResiduoDeBanco struct {
	ID string

	// Medidos é em quantas observações houve o que comparar. Menor do que as
	// observações do banco quando alguma falhou ou veio sem plano de fases.
	Medidos int

	// Maior é o maior resíduo em valor absoluto, com o sinal preservado, e
	// Mediano é a mediana dos valores absolutos. ⚠️ Dois números e não um: o
	// maior sozinho é uma observação estranha, a mediana sozinha esconde-a.
	Maior   string
	Mediano string
}

// Correr monta tudo e varre.
//
// Abre e fecha o seu próprio pool: o subcomando é um processo que corre e sai,
// e um pool que sobrevivesse a ele não teria quem o fechasse.
func Correr(ctx context.Context, url string, o Opcoes) (Relatorio, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return Relatorio{}, fmt.Errorf("abrir o pool de ligações: %w", err)
	}
	defer pool.Close()

	escolhidos, err := escolher(o.Bancos)
	if err != nil {
		return Relatorio{}, err
	}

	// ⚠️ O "hoje" sai do fuso de Lisboa e não de UTC. É dele que sai a data de
	// nascimento do titular neutro, e entre a meia-noite e a uma da manhã no
	// horário de Verão o "hoje" em UTC ainda é ontem — o que muda a idade de
	// quem faz anos nesse dia, e com ela o prazo máximo. É a mesma razão que o
	// dominio.Data já regista.
	hoje := dominio.DataDeInstante(time.Now().In(lisboa()))

	// ⚠️ A escala de LTV entra por aqui e corre dentro do travão de cada banco.
	// São ~86 pedidos por banco além dos pontos — o orçamento da
	// ANALISE-KAN-35.md §6 —, e é o que faz uma corrida passar de segundos a
	// cerca de um minuto por banco. Em hora morta, que é quando isto corre.
	v, err := varrimento.Novo(varrimento.Config{
		Bancos:  escolhidos,
		Travao:  travao.NovoPostgres(pool),
		Degraus: grelha.DegrausPorBanco(grelha.Referencia{}, hoje, grelha.Config{}, time.Now),
	})
	if err != nil {
		return Relatorio{}, fmt.Errorf("montar o varredor: %w", err)
	}

	pontos := grelhaPorBanco(hoje)
	rel := Relatorio{Bancos: ids(escolhidos)}
	for _, b := range escolhidos {
		rel.Pontos += len(pontos(b))
	}

	lote, err := v.VarrerEGravar(ctx, catalogo.NovoPostgres(pool), pontos, o.SeAntigo)
	if errors.Is(err, varrimento.ErrVarrimentoRecente) {
		// Não é falha: é a guarda a fazer o que existe para fazer. Quem chama
		// sai com 0.
		return Relatorio{Bancos: rel.Bancos, Pontos: rel.Pontos, Saltado: true, Motivo: err.Error()}, nil
	}
	if err != nil {
		return rel, err
	}

	rel.VarrimentoID = lote.ID
	rel.Observacoes = len(lote.Resultado.Observacoes)
	for _, obs := range lote.Resultado.Observacoes {
		if !obs.Sucesso() {
			rel.Falhas++
		}
	}
	for _, s := range lote.Resultado.Saltados {
		rel.BancosSaltados = append(rel.BancosSaltados, BancoSaltado{ID: s.BancoID, Motivo: s.Motivo.Error()})
	}
	// ⚠️ Uma escala não medida sai no relatório, e não é o mesmo que um banco
	// saltado: os pontos dele estão gravados. Sem esta linha, uma corrida em que
	// nenhum banco deu escala nenhuma era indistinguível de uma corrida boa.
	for _, s := range lote.Resultado.EscalasNaoMedidas {
		rel.EscalasNaoMedidas = append(rel.EscalasNaoMedidas, BancoSaltado{ID: s.BancoID, Motivo: s.Motivo.Error()})
	}
	rel.Residuos = residuosPorBanco(rel.Bancos, lote.Resultado.Observacoes)
	return rel, nil
}

// residuosPorBanco resume o resíduo de cada banco da corrida.
//
// ⚠️ A ordem é a dos bancos escolhidos e não a de um mapa. Uma saída que muda de
// ordem entre corridas não se compara com a da véspera, que é precisamente o que
// se faz com um resíduo.
//
// ⚠️ Um banco sem uma única medição não aparece. Uma linha «cgd: 0,00 €» quando
// o que houve foram três falhas seria o número mais enganador da saída toda.
func residuosPorBanco(ids []string, obs []varrimento.Observacao) []ResiduoDeBanco {
	porBanco := map[string][]decimal.Decimal{}
	for _, o := range obs {
		r, ok := varrimento.Residuo(o)
		if !ok {
			continue
		}
		porBanco[o.Oferta.BancoID] = append(porBanco[o.Oferta.BancoID], r.Decimal())
	}

	var saida []ResiduoDeBanco
	for _, id := range ids {
		medidos := porBanco[id]
		if len(medidos) == 0 {
			continue
		}
		saida = append(saida, ResiduoDeBanco{
			ID:      id,
			Medidos: len(medidos),
			Maior:   maiorEmModulo(medidos).StringFixed(2),
			Mediano: medianaDosModulos(medidos).StringFixed(2),
		})
	}
	return saida
}

// maiorEmModulo devolve o resíduo mais afastado de zero, com o sinal que tinha.
func maiorEmModulo(ds []decimal.Decimal) decimal.Decimal {
	maior := ds[0]
	for _, d := range ds[1:] {
		if d.Abs().GreaterThan(maior.Abs()) {
			maior = d
		}
	}
	return maior
}

// medianaDosModulos é a mediana dos valores absolutos: a pergunta é «de que
// tamanho é o resíduo típico», e num conjunto com desvios para os dois lados a
// mediana com sinal responderia perto de zero por compensação.
func medianaDosModulos(ds []decimal.Decimal) decimal.Decimal {
	modulos := make([]decimal.Decimal, len(ds))
	for i, d := range ds {
		modulos[i] = d.Abs()
	}
	slices.SortFunc(modulos, func(a, b decimal.Decimal) int { return a.Cmp(b) })

	meio := len(modulos) / 2
	if len(modulos)%2 == 1 {
		return modulos[meio]
	}
	return modulos[meio-1].Add(modulos[meio]).Div(decimal.NewFromInt(2))
}

// grelhaPorBanco devolve os pontos de cada banco, derivados dos Requisitos dele.
//
// ⚠️ Um banco cujos Requisitos não sejam servíveis não faz cair a corrida: fica
// sem pontos, e o varredor salta-o. Derrubar o varrimento inteiro por causa de
// um banco é exactamente o que a §5 proíbe.
func grelhaPorBanco(hoje dominio.Data) varrimento.PontosDe {
	cache := map[string][]varrimento.Ponto{}
	return func(b bancos.Banco) []varrimento.Ponto {
		if p, jaLa := cache[b.ID()]; jaLa {
			return p
		}
		p, err := grelha.Pontos(b.Requisitos(), grelha.Referencia{}, hoje)
		if err != nil {
			p = nil
		}
		cache[b.ID()] = p
		return p
	}
}

// escolher constrói os bancos pedidos, ou todos.
func escolher(pedidos []string) ([]bancos.Banco, error) {
	registo := bancos.Predefinido()

	// ⚠️ Um cliente HTTP por corrida, e não um por banco: o subcomando é um
	// processo curto, e os transportes são partilháveis por construção.
	comSessao, err := transporte.NovoClienteComSessao(nil)
	if err != nil {
		return nil, fmt.Errorf("montar o transporte com sessão: %w", err)
	}
	ts := bancos.Transportes{
		HTTP:      transporte.NovoCliente(nil),
		ComSessao: comSessao,
	}

	if len(pedidos) == 0 {
		todos, err := registo.Todos(ts)
		if err != nil {
			return nil, fmt.Errorf("construir os bancos: %w", err)
		}
		return todos, nil
	}

	escolhidos := make([]bancos.Banco, 0, len(pedidos))
	for _, id := range pedidos {
		b, err := registo.Construir(id, ts)
		if err != nil {
			return nil, fmt.Errorf("%w (registados: %v)", err, registo.IDs())
		}
		escolhidos = append(escolhidos, b)
	}
	return escolhidos, nil
}

func ids(bs []bancos.Banco) []string {
	saida := make([]string, 0, len(bs))
	for _, b := range bs {
		saida = append(saida, b.ID())
	}
	slices.Sort(saida)
	return saida
}

// lisboa devolve o fuso do país. Falhando a base de dados de fusos — que num
// contentor `scratch` pode não existir —, cai-se em UTC em vez de rebentar: um
// varrimento com a data um dia trocada é mau, e não correr é pior.
func lisboa() *time.Location {
	l, err := time.LoadLocation("Europe/Lisbon")
	if err != nil {
		return time.UTC
	}
	return l
}
