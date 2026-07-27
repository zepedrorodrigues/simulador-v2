package varrimento_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/varrimento"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos"
)

func TestAGuardaNaoDeixaCorrerEmCimaDoVarrimentoAnterior(t *testing.T) {
	t.Parallel()

	// O defeito que ela existe para impedir tem número: o v1 estragou a
	// primeira medição com cinco corridas em 14 minutos, que não são cinco dias
	// de dados. A guarda pergunta ANTES de ir aos bancos — perguntar depois
	// poupava a escrita e não poupava a carga no simulador alheio.
	agora := time.Date(2026, time.July, 27, 5, 0, 0, 0, time.UTC)
	banco := &bancoFalso{id: "cgd"}
	v := varredor(t, varrimento.Config{
		Bancos: []bancos.Banco{banco},
		Agora:  func() time.Time { return agora },
	})

	cat := &catalogoFalso{ultimo: agora.Add(-2 * time.Hour), houve: true}

	lote, err := v.VarrerEGravar(t.Context(), cat, varrimento.MesmosPontos(pontos("variavel/0/propria")), 6*time.Hour)
	if !errors.Is(err, varrimento.ErrVarrimentoRecente) {
		t.Fatalf("erro = %v, esperava ErrVarrimentoRecente", err)
	}
	if !lote.Saltado {
		t.Error("o lote não ficou marcado como saltado")
	}
	if n := banco.chamadas.Load(); n != 0 {
		t.Errorf("foi ao banco %d vezes com a guarda a travar — a carga é o que ela existe para poupar", n)
	}
	if cat.lotes != 0 {
		t.Errorf("gravou %d lotes com a guarda a travar", cat.lotes)
	}
}

func TestAGuardaDeixaPassarOQueJaEVelhoEOQueNuncaCorreu(t *testing.T) {
	t.Parallel()

	agora := time.Date(2026, time.July, 27, 5, 0, 0, 0, time.UTC)

	for _, caso := range []struct {
		nome     string
		catalogo *catalogoFalso
	}{
		{
			// ⚠️ Nunca ter varrido não é «varreu-se há muito tempo» por acaso:
			// é o caso em que a guarda TEM de deixar passar, e escrevê-lo assim
			// evita comparar um instante zero com um relógio.
			nome:     "nunca se varreu nada",
			catalogo: &catalogoFalso{houve: false},
		},
		{
			nome:     "o último foi há mais do que a guarda pede",
			catalogo: &catalogoFalso{ultimo: agora.Add(-7 * time.Hour), houve: true},
		},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			v := varredor(t, varrimento.Config{
				Bancos: []bancos.Banco{&bancoFalso{id: "cgd"}},
				Agora:  func() time.Time { return agora },
			})

			lote, err := v.VarrerEGravar(t.Context(), caso.catalogo, varrimento.MesmosPontos(pontos("variavel/0/propria")), 6*time.Hour)
			if err != nil {
				t.Fatalf("VarrerEGravar: %v", err)
			}
			if lote.Saltado {
				t.Error("a guarda travou, e não devia")
			}
			if caso.catalogo.lotes != 1 {
				t.Errorf("gravou %d lotes, esperava 1", caso.catalogo.lotes)
			}
			if lote.ID == "" {
				t.Error("o lote gravado ficou sem varrimento_id")
			}
		})
	}
}

func TestGuardaAZeroNaoPerguntaNada(t *testing.T) {
	t.Parallel()

	// Desligar a guarda é uma escolha explícita de quem chama. O que não pode
	// acontecer é a guarda ficar ligada por omissão a zero e travar sempre — ou
	// desligada sem ninguém o ter pedido.
	agora := time.Now()
	cat := &catalogoFalso{ultimo: agora, houve: true}
	v := varredor(t, varrimento.Config{Bancos: []bancos.Banco{&bancoFalso{id: "cgd"}}})

	lote, err := v.VarrerEGravar(t.Context(), cat, varrimento.MesmosPontos(pontos("variavel/0/propria")), 0)
	if err != nil {
		t.Fatalf("VarrerEGravar: %v", err)
	}
	if lote.Saltado {
		t.Fatal("a guarda travou com o prazo a zero")
	}
	if cat.perguntas != 0 {
		t.Errorf("perguntou %d vezes pelo último varrimento com a guarda desligada", cat.perguntas)
	}
}

func TestUmaCorridaSemObservacoesNaoAbreLote(t *testing.T) {
	t.Parallel()

	// ⚠️ Um varrimento_id sem linhas nenhumas ficaria na série a dizer que se
	// varreu, e a guarda seguinte contaria com ele para não voltar a correr —
	// que é a guarda a proteger o vazio.
	cat := &catalogoFalso{}
	v := varredor(t, varrimento.Config{Bancos: []bancos.Banco{&bancoFalso{id: "cgd"}}})

	lote, err := v.VarrerEGravar(t.Context(), cat, varrimento.MesmosPontos(nil), 0)
	if err != nil {
		t.Fatalf("VarrerEGravar: %v", err)
	}
	if lote.ID != "" || cat.lotes != 0 {
		t.Errorf("abriu o lote %q com %d gravações, e não havia observações", lote.ID, cat.lotes)
	}
}

func TestSemCatalogoNaoSeVarre(t *testing.T) {
	t.Parallel()

	// Correr sem ter onde gravar é bater nos bancos para deitar fora o
	// resultado. Recusa-se à cabeça, como o Novo recusa o travão nulo.
	v := varredor(t, varrimento.Config{Bancos: []bancos.Banco{&bancoFalso{id: "cgd"}}})
	if _, err := v.VarrerEGravar(t.Context(), nil, varrimento.MesmosPontos(pontos("variavel/0/propria")), 0); err == nil {
		t.Fatal("varreu sem catálogo")
	}
}

// catalogoFalso conta o que lhe pediram. ⚠️ Não substitui o de Postgres: o
// critério de pronto da KAN-16 — dois varrimentos seguidos dão UM lote — mede-se
// contra uma base a sério, em internal/infra/catalogo.
type catalogoFalso struct {
	ultimo    time.Time
	houve     bool
	err       error
	perguntas int
	lotes     int
}

func (c *catalogoFalso) UltimoVarrimentoEm(context.Context) (time.Time, bool, error) {
	c.perguntas++
	return c.ultimo, c.houve, c.err
}

func (c *catalogoFalso) GravarLote(_ context.Context, obs []varrimento.Observacao) (string, error) {
	if len(obs) == 0 {
		return "", errors.New("lote sem observações")
	}
	c.lotes++
	c.ultimo, c.houve = time.Now(), true
	return "00000000-0000-4000-8000-00000000000" + string(rune('0'+c.lotes)), nil
}

func TestCadaBancoRecebeAGrelhaDele(t *testing.T) {
	t.Parallel()

	// ⚠️ A grelha é DERIVADA dos Requisitos de cada banco (§4): os períodos
	// fixos, os tenores e os produtos são dele. A CGD tem 7 períodos e impõe o
	// tenor; o Novo Banco tem 9 e aceita três. Servir a lista de um a todos
	// pedia a cada banco pontos que ele não pratica — e gastava um pedido por
	// cada um para receber uma recusa.
	//
	// Este teste existe porque a reversão que trocava `pontos(b)` por
	// `pontos(primeiro)` passou em tudo o resto: era o caminho que nenhum teste
	// percorria.
	cgd := &bancoFalso{id: "cgd"}
	novobanco := &bancoFalso{id: "novobanco"}
	v := varredor(t, varrimento.Config{Bancos: []bancos.Banco{cgd, novobanco}})

	porBanco := map[string][]varrimento.Ponto{
		"cgd":       pontos("fixa/5/propria", "fixa/10/propria"),
		"novobanco": pontos("fixa/2/propria"),
	}

	r := v.VarrerCada(t.Context(), func(b bancos.Banco) []varrimento.Ponto {
		return porBanco[b.ID()]
	})

	if n := cgd.chamadas.Load(); n != 2 {
		t.Errorf("a CGD levou %d pedidos, e a grelha dela tem 2", n)
	}
	if n := novobanco.chamadas.Load(); n != 1 {
		t.Errorf("o Novo Banco levou %d pedidos, e a grelha dele tem 1", n)
	}

	vistos := map[string]string{}
	for _, o := range r.Observacoes {
		vistos[o.Ponto.Cenario] = o.Oferta.BancoID
	}
	if dono := vistos["fixa/10/propria"]; dono != "cgd" {
		t.Errorf("o ponto fixa/10/propria foi para %q, e é da grelha da CGD", dono)
	}
	if dono := vistos["fixa/2/propria"]; dono != "novobanco" {
		t.Errorf("o ponto fixa/2/propria foi para %q, e é da grelha do Novo Banco", dono)
	}
}

func TestUmBancoSemPontosNaoTomaOTravao(t *testing.T) {
	t.Parallel()

	// Tomar o travão para não fazer nada seria impedir, durante esse instante,
	// um varrimento a sério do mesmo banco — que é precisamente o que o travão
	// existe para arbitrar.
	travao := novoTravaoFalso()
	v := varredor(t, varrimento.Config{
		Bancos: []bancos.Banco{&bancoFalso{id: "cgd"}, &bancoFalso{id: "novobanco"}},
		Travao: travao,
	})

	v.VarrerCada(t.Context(), func(b bancos.Banco) []varrimento.Ponto {
		if b.ID() == "cgd" {
			return pontos("variavel/0/propria")
		}
		return nil
	})

	if tomados := travao.tomados; len(tomados) != 1 || tomados[0] != "cgd" {
		t.Errorf("travões tomados = %v, esperava só o da CGD", tomados)
	}
}
