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

	v, err := varrimento.Novo(varrimento.Config{
		Bancos: escolhidos,
		Travao: travao.NovoPostgres(pool),
	})
	if err != nil {
		return Relatorio{}, fmt.Errorf("montar o varredor: %w", err)
	}

	// ⚠️ O "hoje" sai do fuso de Lisboa e não de UTC. É dele que sai a data de
	// nascimento do titular neutro, e entre a meia-noite e a uma da manhã no
	// horário de Verão o "hoje" em UTC ainda é ontem — o que muda a idade de
	// quem faz anos nesse dia, e com ela o prazo máximo. É a mesma razão que o
	// dominio.Data já regista.
	hoje := dominio.DataDeInstante(time.Now().In(lisboa()))

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
	return rel, nil
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
