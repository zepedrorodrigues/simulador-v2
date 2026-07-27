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

	lote, err := v.VarrerEGravar(t.Context(), cat, pontos("variavel/0/propria"), 6*time.Hour)
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

			lote, err := v.VarrerEGravar(t.Context(), caso.catalogo, pontos("variavel/0/propria"), 6*time.Hour)
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

	lote, err := v.VarrerEGravar(t.Context(), cat, pontos("variavel/0/propria"), 0)
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

	lote, err := v.VarrerEGravar(t.Context(), cat, nil, 0)
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
	if _, err := v.VarrerEGravar(t.Context(), nil, pontos("variavel/0/propria"), 0); err == nil {
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
