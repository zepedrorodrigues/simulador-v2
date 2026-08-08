package dominio_test

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

func inteiro(n int) *int { return &n }

func racio(t *testing.T, s string) dominio.Racio {
	t.Helper()
	r, err := dominio.RacioDeTexto(s)
	if err != nil {
		t.Fatalf("rácio de teste inválido %q: %v", s, err)
	}
	return r
}

func TestEncaixarPeriodoFixo(t *testing.T) {
	validos := []int{2, 5, 10}

	casos := []struct {
		nome        string
		validos     []int
		pedido      *int
		tecto       *int
		queroAnos   int
		queroAjuste bool
		queroErro   error
	}{
		{
			nome:    "pedido válido aplica-se tal e qual",
			validos: validos, pedido: inteiro(5),
			queroAnos: 5,
		},
		{
			nome:    "pedido acima do maior desce para o maior",
			validos: validos, pedido: inteiro(20),
			queroAnos: 10, queroAjuste: true,
		},
		{
			nome:    "pedido abaixo do menor sobe para o menor",
			validos: validos, pedido: inteiro(1),
			queroAnos: 2, queroAjuste: true,
		},
		{
			nome:    "pedido entre dois desce para o maior abaixo",
			validos: validos, pedido: inteiro(7),
			queroAnos: 5, queroAjuste: true,
		},
		{
			nome:    "sem pedido aplica o mais longo",
			validos: validos, pedido: nil,
			queroAnos: 10,
		},
		{
			nome:    "lista vazia é defeito de quem chama",
			validos: nil, pedido: inteiro(5),
			queroErro: dominio.ErrSemPeriodosValidos,
		},
		{
			nome:    "tecto que filtra tudo volta à lista inteira",
			validos: validos, pedido: inteiro(5), tecto: inteiro(1),
			queroAnos: 5,
		},
		{
			nome:    "tecto que filtra alguns limita a escolha",
			validos: validos, pedido: inteiro(10), tecto: inteiro(5),
			queroAnos: 5, queroAjuste: true,
		},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			anos, ajuste, err := dominio.EncaixarPeriodoFixo(c.validos, c.pedido, c.tecto)

			if c.queroErro != nil {
				if !errors.Is(err, c.queroErro) {
					t.Fatalf("erro = %v, queria %v", err, c.queroErro)
				}
				return
			}
			if err != nil {
				t.Fatalf("erro inesperado: %v", err)
			}
			if anos != c.queroAnos {
				t.Errorf("período fixo = %d anos, queria %d", anos, c.queroAnos)
			}

			if !c.queroAjuste {
				if ajuste != nil {
					t.Errorf("houve ajuste e não devia: %+v", ajuste)
				}
				return
			}
			if ajuste == nil {
				t.Fatal("não houve ajuste, e o valor aplicado difere do pedido — a nota é obrigatória")
			}
			if ajuste.Campo() != dominio.AjustadoPeriodoFixo {
				t.Errorf("campo ajustado = %q, queria %q", ajuste.Campo(), dominio.AjustadoPeriodoFixo)
			}
			if ajuste.De() != *c.pedido {
				t.Errorf("ajuste.De = %v, queria %d — sem o valor pedido o desvio não é verificável", ajuste.De(), *c.pedido)
			}
			if ajuste.Para() != c.queroAnos {
				t.Errorf("ajuste.Para = %v, queria %d", ajuste.Para(), c.queroAnos)
			}
			for _, n := range []int{*c.pedido, c.queroAnos} {
				if !strings.Contains(ajuste.Nota(), strconv.Itoa(n)) {
					t.Errorf("a nota não nomeia o número %d: %q", n, ajuste.Nota())
				}
			}
		})
	}
}

// requisitosDeTeste é um banco genérico: 5 a 40 anos, contrato a terminar até
// aos 75.
func requisitosDeTeste() dominio.Requisitos {
	return dominio.Requisitos{
		BancoID: "banco", BancoNome: "Banco de Teste", Custo: dominio.CustoBarato,
		PeriodosFixosModo: dominio.ModoLista,
		PrazoMin:          5,
		PrazoMax:          40,
		IdadeMaximaFim:    75,
	}
}

func TestEncaixarPrazo(t *testing.T) {
	casos := []struct {
		nome        string
		pedido      int
		idade       int
		queroAnos   int
		queroAjuste bool
		queroCodigo dominio.CodigoErro
	}{
		{nome: "cabe nos limites e na idade", pedido: 30, idade: 30, queroAnos: 30},
		{nome: "acima do máximo do banco desce ao máximo", pedido: 45, idade: 30, queroAnos: 40, queroAjuste: true},
		{nome: "a idade aperta mais do que o banco", pedido: 40, idade: 45, queroAnos: 30, queroAjuste: true},
		{nome: "abaixo do mínimo do banco sobe ao mínimo", pedido: 2, idade: 30, queroAnos: 5, queroAjuste: true},
		{nome: "nem o mínimo cabe na idade", pedido: 30, idade: 71, queroCodigo: dominio.ErroPrazoImpossivel},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			anos, ajuste, err := dominio.EncaixarPrazo(c.pedido, requisitosDeTeste(), c.idade)

			if c.queroCodigo != "" {
				var e *dominio.ErroOferta
				if !errors.As(err, &e) {
					t.Fatalf("erro = %v, queria um *ErroOferta", err)
				}
				if e.Codigo != c.queroCodigo {
					t.Errorf("código = %q, queria %q", e.Codigo, c.queroCodigo)
				}
				if e.Mensagem == "" {
					t.Error("a falha não traz mensagem para a pessoa ler")
				}
				return
			}
			if err != nil {
				t.Fatalf("erro inesperado: %v", err)
			}
			if anos != c.queroAnos {
				t.Errorf("prazo = %d anos, queria %d", anos, c.queroAnos)
			}
			if c.queroAjuste && ajuste == nil {
				t.Fatal("não houve ajuste, e o prazo aplicado difere do pedido")
			}
			if !c.queroAjuste && ajuste != nil {
				t.Errorf("houve ajuste e não devia: %+v", ajuste)
			}
		})
	}
}

func TestIdadeMaisVelho(t *testing.T) {
	nasceu := func(s string) dominio.Titular {
		t.Helper()
		d, err := dominio.DataDeTexto(s)
		if err != nil {
			t.Fatalf("data de teste inválida %q: %v", s, err)
		}
		return dominio.Titular{DataNascimento: d}
	}
	hoje := dominio.Data{Ano: 2026, Mes: time.July, Dia: 25}

	t.Run("manda o mais velho dos dois", func(t *testing.T) {
		idade, err := dominio.IdadeMaisVelho(
			[]dominio.Titular{nasceu("1990-04-12"), nasceu("1978-01-03")}, hoje)
		if err != nil {
			t.Fatalf("erro inesperado: %v", err)
		}
		if idade != 48 {
			t.Errorf("idade = %d, queria 48", idade)
		}
	})

	t.Run("faz anos amanhã, ainda não os fez", func(t *testing.T) {
		idade, err := dominio.IdadeMaisVelho([]dominio.Titular{nasceu("1990-07-26")}, hoje)
		if err != nil {
			t.Fatalf("erro inesperado: %v", err)
		}
		if idade != 35 {
			t.Errorf("idade = %d, queria 35 — contar só o ano dava 36", idade)
		}
	})

	t.Run("fez anos hoje, já os fez", func(t *testing.T) {
		idade, err := dominio.IdadeMaisVelho([]dominio.Titular{nasceu("1990-07-25")}, hoje)
		if err != nil {
			t.Fatalf("erro inesperado: %v", err)
		}
		if idade != 36 {
			t.Errorf("idade = %d, queria 36", idade)
		}
	})

	t.Run("sem titulares não há idade", func(t *testing.T) {
		if _, err := dominio.IdadeMaisVelho(nil, hoje); !errors.Is(err, dominio.ErrSemTitulares) {
			t.Fatalf("erro = %v, queria ErrSemTitulares", err)
		}
	})
}

func TestLTV(t *testing.T) {
	t.Run("duzentos mil sobre duzentos e cinquenta mil são oitenta por cento", func(t *testing.T) {
		l, err := dominio.LTV(dominio.DinheiroDeInteiro(200000), dominio.DinheiroDeInteiro(250000))
		if err != nil {
			t.Fatalf("erro inesperado: %v", err)
		}
		if !l.Equal(racio(t, "0.8")) {
			t.Errorf("LTV = %s, queria 0.8", l)
		}
	})

	t.Run("valor do imóvel a zero é recusado, não é pânico", func(t *testing.T) {
		// Sem esta guarda o decimal entra em pânico ("decimal division by 0"),
		// o recover do orquestrador apanha-o, e a app vê "este banco avariou"
		// quando o defeito é um input nosso.
		if _, err := dominio.LTV(dominio.DinheiroDeInteiro(200000), dominio.Dinheiro{}); !errors.Is(err, dominio.ErrValorImovelZero) {
			t.Fatalf("erro = %v, queria ErrValorImovelZero", err)
		}
	})
}

// ⚠️ O TestBandaLTV e o TestMesmaBanda estavam aqui e saíram com as funções
// que testavam, a 2026-07-26 (KAN-35). O que eles afirmavam — bandas de 5 %
// fechadas em cima — deixou de ser verdade sobre o preço: as fronteiras são
// medidas por banco. O que os substitui está no escala_ltv_test.go.

// FuzzEncaixarPeriodoFixo afirma o invariante que nenhuma tabela consegue
// esgotar: o que sai está sempre na lista do banco. Um período fixo que o banco
// não pratica é um número inventado.
//
// O corpus de sementes corre no `go test` normal, sem -fuzz, por isso isto
// entra no portão a custo zero.
func FuzzEncaixarPeriodoFixo(f *testing.F) {
	f.Add(uint8(2), uint8(5), uint8(10), 7, true)
	f.Add(uint8(1), uint8(1), uint8(1), 0, false)
	f.Add(uint8(30), uint8(3), uint8(15), -4, true)

	f.Fuzz(func(t *testing.T, a, b, c uint8, pedido int, temPedido bool) {
		validos := []int{int(a) + 1, int(b) + 1, int(c) + 1}
		var p *int
		if temPedido {
			p = &pedido
		}
		anos, _, err := dominio.EncaixarPeriodoFixo(validos, p, nil)
		if err != nil {
			t.Fatalf("lista não vazia e mesmo assim erro: %v", err)
		}
		if !slices.Contains(validos, anos) {
			t.Fatalf("escolheu %d, que não está em %v", anos, validos)
		}
	})
}

// ⚠️ **Havia aqui a `FuzzEscalaDeLTV`**, e sai com a `EscalaDeLTV`
// (2026-08-08). Afirmava que qualquer rácio dentro do domínio preçado cai em
// exactamente um degrau e recebe um spread **medido** e nunca interpolado — um
// invariante bom sobre uma tabela que deixou de existir: ao vivo o spread é o
// que o simulador do banco devolveu, e não há escala nossa onde procurá-lo.
//
// ⚠️ **O facto medido que ela guardava não morre com ela** (KAN-35): na CGD o
// spread **não é monótono** no rácio — o 2,050 % é um patamar isolado entre dois
// mais baixos. Era por isso que este fuzz recusava afirmar monotonia, e quem um
// dia voltar a modelar uma escala de LTV parte deste facto e não do palpite.
