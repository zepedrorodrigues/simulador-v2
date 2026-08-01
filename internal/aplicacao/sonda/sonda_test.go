package sonda_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/sonda"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// A sonda, afirmada sem rede nenhuma.
//
// ⚠️ **Zero I/O**, como o `comparar`: o que se mede aqui é a decisão — onde a
// sonda se põe, quanto tolera, e o que conclui —, e essa decisão não precisa de
// um banco para ser exercida. O banco entra por uma função, e nos testes é uma
// tabela.
//
// A escala de prova é a da CGD medida na KAN-35, com a fronteira em LTV não
// inteiro que motivou a escala medida: (0,30 ; 0,665] a 2,000 e
// (0,665 ; 0,6775] a 2,050.

func TestASondaPoeSeLogoAbaixoDoTopoDoDegrau(t *testing.T) {
	pontos := pontosDe(t, escalaDeProva(t), dominio.Racio{})

	if len(pontos) != 2 {
		t.Fatalf("dois degraus, e vieram %d pontos", len(pontos))
	}

	// ⚠️ Esta é a afirmação que paga a assimetria da §7: no TOPO e não no meio.
	// O meio do primeiro degrau seria 0,4825 — se algum dia isto passar a
	// devolver esse número, a sonda deixou de apanhar fronteiras que descem.
	verRacio(t, "sonda do 1.º degrau", pontos[0].LTV, "0.664")
	verRacio(t, "sonda do 2.º degrau", pontos[1].LTV, "0.6765")

	// E continua dentro do degrau que diz confirmar.
	for i, p := range pontos {
		if p.LTV.Cmp(p.Degrau.De) <= 0 || p.LTV.Cmp(p.Degrau.Ate) > 0 {
			t.Errorf("a sonda %d caiu em %s, fora do degrau (%s ; %s]",
				i+1, p.LTV, p.Degrau.De, p.Degrau.Ate)
		}
	}
}

// TestUmaFronteiraQueDesceEApanhadaEUmaQueSobeNao é a assimetria inteira, e é a
// razão de a sonda se pôr onde se põe.
//
// ⚠️ O segundo caso **passa de propósito**: é o erro aceite, e está escrito na
// §7. Se algum dia ele começar a falhar, ou a sonda mudou de sítio ou alguém
// resolveu o problema — e nos dois casos esta afirmação tem de ser reescrita, e
// não apagada.
func TestUmaFronteiraQueDesceEApanhadaEUmaQueSobeNao(t *testing.T) {
	pontos := pontosDe(t, escalaDeProva(t), dominio.Racio{})

	t.Run("a fronteira desce: a sonda cai no degrau de cima e vê outro preço", func(t *testing.T) {
		// A fronteira mudou de 0,665 para 0,66: a sonda do 1.º degrau, em
		// 0,664, passa a estar no território do 2.º e mede 2,050.
		r := correr(t, pontos, banco(map[string]string{
			"0.664":  "2.050", // era 2,000
			"0.6765": "2.050",
		}))
		if r.Divergentes != 1 {
			t.Fatalf("esperava 1 divergência, houve %d", r.Divergentes)
		}
		if !r.Leituras[0].Divergiu {
			t.Error("a sonda do 1.º degrau não deu pela fronteira a descer")
		}
		if r.Confirmada() {
			t.Error("a grelha deu-se por confirmada com a fronteira mudada")
		}
	})

	t.Run("a fronteira sobe: a sonda não vê, e é o erro aceite", func(t *testing.T) {
		// A fronteira mudou de 0,665 para 0,67: quem está entre 0,665 e 0,67
		// passou a pagar 2,000, e nós continuamos a servir-lhe 2,050. A sonda
		// do 1.º degrau (0,664) continua a medir 2,000 e não estranha.
		r := correr(t, pontos, banco(map[string]string{
			"0.664":  "2.000",
			"0.6765": "2.050",
		}))
		if r.Divergentes != 0 {
			t.Fatalf("esperava não ver nada, e viu %d divergência(s)", r.Divergentes)
		}
		if !r.Confirmada() {
			t.Error("deu-se por não confirmada sem ter medido diferença nenhuma")
		}
		// ⚠️ E o erro que fica é servir o preço MAIS ALTO — a direcção que a
		// MCD manda presumir. Se a asserção acima se inverter um dia, isto é o
		// que tem de continuar verdadeiro.
	})
}

// TestATolerenciaSaiDoDegrauENaoDeUmaConstante: num degrau resolvido é zero;
// num degrau por resolver é a largura da incerteza já medida.
func TestATolerenciaSaiDoDegrauENaoDeUmaConstante(t *testing.T) {
	minimo := taxa(t, "1.800")
	porResolver, err := dominio.NovaEscalaDeLTV([]dominio.DegrauLTV{
		{De: racio(t, "0.30"), Ate: racio(t, "0.80"), Spread: taxa(t, "2.000"), SpreadMinimo: &minimo},
	})
	if err != nil {
		t.Fatalf("escala por resolver: %v", err)
	}

	resolvidos := pontosDe(t, escalaDeProva(t), dominio.Racio{})
	if got := resolvidos[0].Tolerancia; !got.Decimal().IsZero() {
		t.Errorf("degrau resolvido tolera %s, e devia tolerar zero", got)
	}

	naoResolvidos := pontosDe(t, porResolver, dominio.Racio{})
	if got := naoResolvidos[0].Tolerancia.String(); got != "0.2" {
		t.Errorf("degrau por resolver tolera %s, e a incerteza medida são 2,000 - 1,800 = 0,2", got)
	}

	// ⚠️ Um desvio IGUAL à largura da incerteza não diverge: é o outro lado do
	// degrau, que já sabíamos ser possível ali.
	r := correr(t, naoResolvidos, banco(map[string]string{"0.799": "1.800"}))
	if r.Divergentes != 0 {
		t.Errorf("o outro lado de um degrau por resolver contou como divergência")
	}

	// E um cabelo acima já diverge.
	r = correr(t, naoResolvidos, banco(map[string]string{"0.799": "1.799"}))
	if r.Divergentes != 1 {
		t.Errorf("um desvio maior do que a incerteza medida passou como aceitável")
	}
}

// TestUmaSondaCegaNaoConfirma: o silêncio não é concordância.
//
// ⚠️ É o modo de falha mais perigoso de um detector — a tranquilidade sem a
// medição. Um banco em baixo daria «confirmada» a uma grelha de que ninguém
// sabe nada.
func TestUmaSondaCegaNaoConfirma(t *testing.T) {
	pontos := pontosDe(t, escalaDeProva(t), dominio.Racio{})

	r := correr(t, pontos, func(_ context.Context, ltv dominio.Racio) (dominio.Taxa, error) {
		if ltv.String() == "0.664" {
			return dominio.Taxa{}, errors.New("o banco não respondeu")
		}
		return taxa(t, "2.050"), nil
	})

	if r.Cegos != 1 {
		t.Fatalf("esperava 1 sonda cega, houve %d", r.Cegos)
	}
	if r.Divergentes != 0 {
		t.Errorf("uma sonda cega contou como divergência: são coisas diferentes")
	}
	if r.Confirmada() {
		t.Error("a grelha deu-se por CONFIRMADA com uma sonda que não mediu nada")
	}
}

// TestOVeredictoVemAcompanhadoDoQueNaoViu: «confirmada» sem a cobertura engana.
func TestOVeredictoVemAcompanhadoDoQueNaoViu(t *testing.T) {
	pontos := pontosDe(t, escalaDeProva(t), dominio.Racio{})
	r := correr(t, pontos, banco(map[string]string{"0.664": "2.000", "0.6765": "2.050"}))

	if !r.Confirmada() {
		t.Fatal("esperava confirmada")
	}
	cobertura := r.Cobertura()
	for _, exigido := range []string{"fronteira que suba", "patamar novo"} {
		if !contem(cobertura, exigido) {
			t.Errorf("a cobertura não diz o que a sonda não vê (%q): %q", exigido, cobertura)
		}
	}
}

// TestUmDegrauMaisEstreitoQueORecuoESondadoAoMeioENaoSaltado.
func TestUmDegrauMaisEstreitoQueORecuoESondadoAoMeioENaoSaltado(t *testing.T) {
	estreita, err := dominio.NovaEscalaDeLTV([]dominio.DegrauLTV{
		{De: racio(t, "0.30"), Ate: racio(t, "0.3005"), Spread: taxa(t, "2.000")},
	})
	if err != nil {
		t.Fatalf("escala estreita: %v", err)
	}

	pontos := pontosDe(t, estreita, dominio.Racio{}) // recuo 0,001 > largura 0,0005
	if len(pontos) != 1 {
		t.Fatalf("o degrau estreito foi saltado: %d pontos", len(pontos))
	}
	verRacio(t, "sonda do degrau estreito", pontos[0].LTV, "0.30025")
}

// --- ajudantes -------------------------------------------------------------------

// escalaDeProva é a da CGD medida na KAN-35, com a fronteira em LTV não inteiro.
func escalaDeProva(t *testing.T) dominio.EscalaDeLTV {
	t.Helper()
	e, err := dominio.NovaEscalaDeLTV([]dominio.DegrauLTV{
		{De: racio(t, "0.30"), Ate: racio(t, "0.665"), Spread: taxa(t, "2.000")},
		{De: racio(t, "0.665"), Ate: racio(t, "0.6775"), Spread: taxa(t, "2.050")},
	})
	if err != nil {
		t.Fatalf("escala de prova: %v", err)
	}
	return e
}

func pontosDe(t *testing.T, e dominio.EscalaDeLTV, recuo dominio.Racio) []sonda.Ponto {
	t.Helper()
	p, err := sonda.PontosDe(e, recuo)
	if err != nil {
		t.Fatalf("PontosDe: %v", err)
	}
	return p
}

func correr(t *testing.T, pontos []sonda.Ponto, medir sonda.Medir) sonda.Relatorio {
	t.Helper()
	r, err := sonda.Correr(context.Background(), "provabank", pontos, medir)
	if err != nil {
		t.Fatalf("Correr: %v", err)
	}
	return r
}

// banco devolve um Medir que responde de uma tabela `LTV → spread`.
func banco(tabela map[string]string) sonda.Medir {
	return func(_ context.Context, ltv dominio.Racio) (dominio.Taxa, error) {
		s, tem := tabela[ltv.String()]
		if !tem {
			return dominio.Taxa{}, errors.New("a tabela de prova não tem " + ltv.String())
		}
		return dominio.TaxaDeTexto(s)
	}
}

func racio(t *testing.T, s string) dominio.Racio {
	t.Helper()
	r, err := dominio.RacioDeTexto(s)
	if err != nil {
		t.Fatalf("racio %q: %v", s, err)
	}
	return r
}

func taxa(t *testing.T, s string) dominio.Taxa {
	t.Helper()
	x, err := dominio.TaxaDeTexto(s)
	if err != nil {
		t.Fatalf("taxa %q: %v", s, err)
	}
	return x
}

func verRacio(t *testing.T, nome string, obtido dominio.Racio, esperado string) {
	t.Helper()
	if obtido.String() != esperado {
		t.Errorf("%s = %s, esperava %s", nome, obtido, esperado)
	}
}

func contem(s, pedaco string) bool { return strings.Contains(s, pedaco) }
