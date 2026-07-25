package dominio_test

import (
	"errors"
	"testing"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

var hojeDeTeste = dominio.Data{Ano: 2026, Mes: time.July, Dia: 25}

func pedidoValido(t *testing.T) dominio.Pedido {
	t.Helper()
	nascimento, err := dominio.DataDeTexto("1990-04-12")
	if err != nil {
		t.Fatalf("data inválida: %v", err)
	}
	return dominio.Pedido{
		ValorImovel: dominio.DinheiroDeInteiro(250000),
		Montante:    dominio.DinheiroDeInteiro(200000),
		PrazoAnos:   30,
		TipoTaxa:    dominio.TaxaMista,
		Indexante:   dominio.Euribor6M,
		Titulares: []dominio.Titular{
			{DataNascimento: nascimento, RendimentoMensal: dominio.DinheiroDeInteiro(2200)},
		},
		Finalidade:  dominio.FinalidadePropria,
		Localizacao: dominio.LocalizacaoContinente,
	}
}

func TestPedidoValidar(t *testing.T) {
	casos := []struct {
		nome       string
		muda       func(p *dominio.Pedido)
		queroCampo string
	}{
		{nome: "o pedido de referência passa"},
		{
			nome:       "valor do imóvel a zero",
			muda:       func(p *dominio.Pedido) { p.ValorImovel = dominio.Dinheiro{} },
			queroCampo: "valor_imovel",
		},
		{
			nome:       "montante a zero",
			muda:       func(p *dominio.Pedido) { p.Montante = dominio.Dinheiro{} },
			queroCampo: "montante",
		},
		{
			nome:       "montante acima do valor do imóvel",
			muda:       func(p *dominio.Pedido) { p.Montante = dominio.DinheiroDeInteiro(300000) },
			queroCampo: "montante",
		},
		{
			nome:       "prazo a zero",
			muda:       func(p *dominio.Pedido) { p.PrazoAnos = 0 },
			queroCampo: "prazo_anos",
		},
		{
			nome:       "prazo acima do tecto do domínio",
			muda:       func(p *dominio.Pedido) { p.PrazoAnos = dominio.PrazoMaximoAnos + 1 },
			queroCampo: "prazo_anos",
		},
		{
			nome:       "tipo de taxa por preencher",
			muda:       func(p *dominio.Pedido) { p.TipoTaxa = "" },
			queroCampo: "rate_type",
		},
		{
			nome: "período fixo numa taxa variável",
			muda: func(p *dominio.Pedido) {
				p.TipoTaxa = dominio.TaxaVariavel
				p.PeriodoFixoAnos = inteiro(5)
			},
			queroCampo: "fixed_period_years",
		},
		{
			nome:       "período fixo de zero anos",
			muda:       func(p *dominio.Pedido) { p.PeriodoFixoAnos = inteiro(0) },
			queroCampo: "fixed_period_years",
		},
		{
			nome:       "indexante que não existe",
			muda:       func(p *dominio.Pedido) { p.Indexante = "1m" },
			queroCampo: "euribor_indexante",
		},
		{
			nome:       "finalidade por preencher",
			muda:       func(p *dominio.Pedido) { p.Finalidade = "" },
			queroCampo: "finalidade",
		},
		{
			nome:       "localização por preencher",
			muda:       func(p *dominio.Pedido) { p.Localizacao = "" },
			queroCampo: "localizacao",
		},
		{
			nome:       "sem titulares",
			muda:       func(p *dominio.Pedido) { p.Titulares = nil },
			queroCampo: "titulares",
		},
		{
			nome: "três titulares",
			muda: func(p *dominio.Pedido) {
				p.Titulares = append(p.Titulares, p.Titulares[0], p.Titulares[0])
			},
			queroCampo: "titulares",
		},
		{
			nome:       "titular sem data de nascimento",
			muda:       func(p *dominio.Pedido) { p.Titulares[0].DataNascimento = dominio.Data{} },
			queroCampo: "data_nascimento",
		},
		{
			nome: "titular nascido no futuro",
			muda: func(p *dominio.Pedido) {
				p.Titulares[0].DataNascimento = dominio.Data{Ano: 2027, Mes: time.January, Dia: 1}
			},
			queroCampo: "data_nascimento",
		},
		{
			nome: "titular menor de idade",
			muda: func(p *dominio.Pedido) {
				p.Titulares[0].DataNascimento = dominio.Data{Ano: 2010, Mes: time.January, Dia: 1}
			},
			queroCampo: "data_nascimento",
		},
		{
			nome: "rendimento negativo",
			muda: func(p *dominio.Pedido) {
				d, err := dominio.DinheiroDeTexto("-1")
				if err != nil {
					t.Fatalf("montante inválido: %v", err)
				}
				p.Titulares[0].RendimentoMensal = d
			},
			queroCampo: "rendimento_mensal",
		},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			p := pedidoValido(t)
			if c.muda != nil {
				c.muda(&p)
			}

			err := p.Validar(hojeDeTeste)

			if c.queroCampo == "" {
				if err != nil {
					t.Fatalf("erro inesperado: %v", err)
				}
				return
			}
			var e *dominio.ErroValidacao
			if !errors.As(err, &e) {
				t.Fatalf("erro = %v, queria um *ErroValidacao", err)
			}
			if e.Campo != c.queroCampo {
				t.Errorf("campo = %q, queria %q", e.Campo, c.queroCampo)
			}
			if e.Mensagem == "" {
				t.Error("a recusa não traz mensagem")
			}
		})
	}
}

func TestTipoTaxaTemPeriodoFixo(t *testing.T) {
	if dominio.TaxaVariavel.TemPeriodoFixo() {
		t.Error("a taxa variável não tem período fixo")
	}
	if !dominio.TaxaFixa.TemPeriodoFixo() || !dominio.TaxaMista.TemPeriodoFixo() {
		t.Error("a fixa e a mista têm período fixo")
	}
}
