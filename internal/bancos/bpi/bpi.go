package bpi

import (
	"context"
	"fmt"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/transporte"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

const (
	// URL é o endereço do simulador BPI.
	URL = "https://www.bancobpi.pt/particulares/credito/credito-habitacao/simulador-credito-habitacao"

	// agente é o que o simulador espera ver.
	agente = "Mozilla/5.0 (Windows NT 10.0; Win64; x64)"
)

// Banco é o Banco BPI. O transporte é injectado e nunca instanciado aqui.
type Banco struct {
	browser transporte.BrowserComoCliente

	// agora é o relógio, e existe para os testes o poderem congelar.
	agora func() time.Time
}

// Novo constrói o Banco BPI sobre o transporte que a infra montou.
func Novo(browser transporte.BrowserComoCliente) *Banco {
	return &Banco{browser: browser}
}

func (b *Banco) ID() string   { return IDBanco }
func (b *Banco) Nome() string { return NomeBanco }

// Requisitos declara o que o Banco BPI usa, ignora e impõe.
func (b *Banco) Requisitos() dominio.Requisitos {
	usa := func(c dominio.CampoCanonico) dominio.Input {
		return dominio.Input{Campo: c, Usa: true}
	}
	usaComNota := func(c dominio.CampoCanonico, nota string) dominio.Input {
		return dominio.Input{Campo: c, Usa: true, Nota: nota}
	}
	naoUsa := func(c dominio.CampoCanonico, nota string) dominio.Input {
		return dominio.Input{Campo: c, Usa: false, Nota: nota}
	}

	return dominio.Requisitos{
		BancoID:   IDBanco,
		BancoNome: NomeBanco,
		Custo:     dominio.CustoCaro,

		Inputs: []dominio.Input{
			usa(dominio.CampoMontante),
			usa(dominio.CampoPrazoAnos),
			usa(dominio.CampoTipoTaxa),
			usa(dominio.CampoPeriodoFixo),
			usaComNota(dominio.CampoDataNascimento,
				"Vai no formulário e decide o prazo máximo — o contrato tem de terminar até aos 70 anos."),
			usaComNota(dominio.CampoSegundoTitular,
				"Vai no formulário. Manda a idade do titular mais velho, que é quem aperta o prazo máximo."),
			naoUsa(dominio.CampoValorImovel,
				"O BPI não pede o valor do imóvel — só montante + prazo. LTV não aplicável."),
			naoUsa(dominio.CampoIndexante,
				"⚠️ Não é escolhível: a Euribor é sempre a 6 meses (média aritmética simples)."),
			naoUsa(dominio.CampoFinalidade,
				"O BPI não distingue arrendamento (simulado como habitação própria)."),
			naoUsa(dominio.CampoLocalizacao,
				"O BPI usa distrito (default LISBOA) — não a região Continente/Ilhas."),
			naoUsa(dominio.CampoRendimentoMensal,
				"O simulador não o pede para calcular o preço."),
			naoUsa(dominio.CampoProfissao,
				"O simulador não pergunta pela profissão nem pelo vínculo."),
			naoUsa(dominio.CampoTipologia,
				"O simulador não pergunta pela tipologia."),
			naoUsa(dominio.CampoJaCliente,
				"Ser cliente não é opção do simulador. O que desconta são as vendas associadas."),
		},

		PeriodosFixos:     PeriodosFixosMista,
		PeriodosFixosModo: dominio.ModoLista,

		EuriborOpcoes:  nil,
		EuriborImposto: dominio.Euribor6M,

		PrazoMin: PrazoMinAnos,
		PrazoMax: PrazoMaxAnos,

		IdadeMaximaFim: IdadeMaximaFim,

		Produtos: []dominio.Produto{
			{
				ID:     ProdutoVendasAssociadas,
				Rotulo: "Vendas associadas (seguros)",
				Descricao: "Usa a prestação/TAEG 'com vendas associadas' (seguros) " +
					"em vez do valor base. O BPI só oferece o pacote, não produtos individuais.",
				PorOmissao: false,
			},
		},

		Notas: []string{
			"Não pede o valor do imóvel (LTV não aplicável).",
			"Usa o distrito (default LISBOA) — não a região Continente/Ilhas.",
			"TAN/spread/MTIC/indexante vêm do painel 'Detalhe da Simulação' " +
				"(o scraper seleciona uma solução para o abrir), com e sem " +
				"vendas associadas; a prestação e a TAEG vêm do topo.",
			"O prazo máximo depende da idade (fim do prazo ≈ ≤70 anos): o " +
				"simulador valida e o scraper ajusta ao máximo indicado.",
		},
	}
}

// Simular interroga o Banco BPI. É um pedido ao browser.
func (b *Banco) Simular(ctx context.Context, p dominio.Pedido) (dominio.Oferta, error) {
	requisitos := b.Requisitos()

	idade, err := dominio.IdadeMaisVelho(p.Titulares, b.hoje())
	if err != nil {
		return dominio.Oferta{}, &dominio.ErroOferta{
			Codigo:   dominio.ErroProdutoIndisponivel,
			Mensagem: "O Banco BPI precisa da data de nascimento de pelo menos um titular.",
		}
	}

	// ⚠️ O prazo encaixa-se ANTES de ir à rede.
	prazo, _, err := dominio.EncaixarPrazo(p.PrazoAnos, requisitos, idade)
	if err != nil {
		return dominio.Oferta{}, err
	}

	// ⚠️ O período fixo só existe na taxa mista.
	if p.TipoTaxa == dominio.TaxaMista {
		if _, _, err := dominio.EncaixarPeriodoFixo(PeriodosFixosMista, p.PeriodoFixoAnos, &prazo); err != nil {
			return dominio.Oferta{}, &dominio.ErroOferta{
				Codigo:   dominio.ErroProdutoIndisponivel,
				Mensagem: fmt.Sprintf("Não há período de taxa fixa do Banco BPI que caiba neste prazo: %v", err),
			}
		}
	}

	html, err := b.browser.Executar(ctx, URL, agente, func(ctx context.Context, page interface{}) error {
		// TODO: implementar a condução do formulário com Playwright
		// Por agora, devolver erro — a implementação real precisa de capturas
		return fmt.Errorf("BPI: scraper ainda não implementado — necessário capturar HTML primeiro")
	})
	if err != nil {
		return dominio.Oferta{}, &dominio.ErroOferta{
			Codigo:   dominio.ErroBancoIndisponivel,
			Mensagem: fmt.Sprintf("O Banco BPI não respondeu: %v", err),
		}
	}

	return lerResposta(html, p)
}

func (b *Banco) hoje() dominio.Data {
	agora := b.agora
	if agora == nil {
		agora = time.Now
	}
	return dominio.DataDeInstante(agora())
}
