package bpi

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mxschmitt/playwright-go"

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
		return b.conduzirFormulario(ctx, page, p, prazo, idade)
	})
	if err != nil {
		return dominio.Oferta{}, &dominio.ErroOferta{
			Codigo:   dominio.ErroBancoIndisponivel,
			Mensagem: fmt.Sprintf("O Banco BPI não respondeu: %v", err),
		}
	}

	return lerResposta(html, p)
}

// conduzirFormulario preenche e submete o formulário do simulador BPI.
//
// O page é uma interface{} porque o transporte define-a como tal para desacoplar
// o banco da biblioteca de browser. Aqui fazemos type assert para playwright.Page.
func (b *Banco) conduzirFormulario(ctx context.Context, page interface{}, p dominio.Pedido, prazo, idade int) error {
	pg, ok := page.(playwright.Page)
	if !ok {
		return fmt.Errorf("page não é playwright.Page")
	}

	// Navegar para o simulador
	if _, err := pg.Goto(URL); err != nil {
		return fmt.Errorf("goto: %w", err)
	}

	// Esperar que a página carregue — timeout é ignorado, como no v1
	_ = pg.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
		State: playwright.LoadStateNetworkidle,
	})

	// Aceitar cookies se aparecerem
	for _, nome := range []string{"Concordar com todos", "Aceitar"} {
		btn := pg.GetByRole("button", playwright.PageGetByRoleOptions{Name: nome})
		if count, err := btn.Count(); err == nil && count > 0 {
			if err := btn.First().Click(); err == nil {
				break
			}
		}
	}

	// Esperar pela estabilização da página
	time.Sleep(5 * time.Second)

	// Verificar se o botão de cookies ainda está visível e clicar novamente se necessário
	for _, nome := range []string{"Concordar com todos", "Aceitar"} {
		btn := pg.GetByRole("button", playwright.PageGetByRoleOptions{Name: nome})
		if count, err := btn.Count(); err == nil && count > 0 {
			if err := btn.First().Click(); err == nil {
				time.Sleep(2 * time.Second)
				break
			}
		}
	}

	// Helper para preencher input
	fillInput := func(suffix, value string) error {
		el := pg.Locator(fmt.Sprintf(`input[id$="%s"]`, suffix)).First()
		if err := el.Click(); err != nil {
			if err := el.Focus(); err != nil {
				return fmt.Errorf("focus em %s: %w", suffix, err)
			}
		}
		if err := el.Fill(""); err != nil {
			return fmt.Errorf("fill vazio em %s: %w", suffix, err)
		}
		if err := el.PressSequentially(value, playwright.LocatorPressSequentiallyOptions{
			Delay: playwright.Float(25),
		}); err != nil {
			return fmt.Errorf("type em %s: %w", suffix, err)
		}
		if err := el.Press("Tab"); err != nil {
			return fmt.Errorf("tab em %s: %w", suffix, err)
		}
		time.Sleep(1 * time.Second)
		return nil
	}

	// Helper para selecionar opção
	selectOption := func(suffix, label string) error {
		sel := pg.Locator(fmt.Sprintf(`select[id$="%s"]`, suffix)).First()
		if _, err := sel.SelectOption(playwright.SelectOptionValues{
			Labels: &[]string{label},
		}); err != nil {
			return fmt.Errorf("select %s: %w", suffix, err)
		}
		time.Sleep(1 * time.Second)
		return nil
	}

	// Helper para esperar por AJAX idle
	waitForAjaxIdle := func() {
		overlay := pg.Locator(`[class*="Feedback_AjaxWait"]`).First()
		if count, err := overlay.Count(); err == nil && count > 0 {
			_ = overlay.WaitFor(playwright.LocatorWaitForOptions{
				State: playwright.WaitForSelectorStateHidden,
			})
		}
		time.Sleep(300 * time.Millisecond)
	}

	// 1. Clicar em "Comprar Casa" (se disponível)
	comprarBtn := pg.GetByRole("button", playwright.PageGetByRoleOptions{Name: "Comprar Casa"})
	if count, err := comprarBtn.Count(); err == nil && count > 0 {
		if err := comprarBtn.First().Click(); err == nil {
			waitForAjaxIdle()
		}
	}

	// 2. Montante
	montante := p.Montante.Decimal().IntPart() // Converter para euros
	if err := fillInput("wtInputValorEuros", fmt.Sprintf("%d", montante)); err != nil {
		return fmt.Errorf("montante: %w", err)
	}

	// 3. Prazo
	if err := fillInput("wtInputPrazoAnos", fmt.Sprintf("%d", prazo)); err != nil {
		return fmt.Errorf("prazo: %w", err)
	}

	// 4. Modalidade
	modalidade := p.TipoTaxa.BPI()
	if err := selectOption("wtinputModalidade", modalidade); err != nil {
		return fmt.Errorf("modalidade: %w", err)
	}

	// 5. Proponentes (1 ou 2)
	numProponentes := 1
	if len(p.Titulares) > 1 {
		numProponentes = 2
	}
	if _, err := pg.Evaluate(fmt.Sprintf(`() => {
		const radio = document.querySelector('input[id*="Proponentes"][value="%d"]');
		if (radio) {
			radio.checked = true;
			radio.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
			return true;
		}
		return false;
	}`, numProponentes)); err != nil {
		return fmt.Errorf("proponentes: %w", err)
	}
	waitForAjaxIdle()

	// 6. Data de nascimento (titular mais velho)
	dataNascimento := time.Now().AddDate(-idade, 0, 0).Format("02/01/2006")
	if err := fillInput("wtinputDataNascimento1", dataNascimento); err != nil {
		return fmt.Errorf("data nascimento: %w", err)
	}

	// 7. Tipo de propriedade
	if err := selectOption("wtinputTipoPropriedade", "Habitação Própria Permanente"); err != nil {
		return fmt.Errorf("tipo propriedade: %w", err)
	}

	// 8. Distrito
	if err := selectOption("wtinputDistrito", "LISBOA"); err != nil {
		return fmt.Errorf("distrito: %w", err)
	}

	waitForAjaxIdle()

	// Clicar em Simular
	simularBtn := pg.Locator(`input[id$="wtbtnSimular"]`).First()
	if err := simularBtn.Click(); err != nil {
		return fmt.Errorf("simular: %w", err)
	}

	// Esperar pelos resultados
	for i := 0; i < 60; i++ { // max 30s
		texto, _ := pg.Evaluate("() => document.body ? document.body.innerText : ''")
		if textoStr, ok := texto.(string); ok {
			if strings.Contains(textoStr, "TAEG") && strings.Contains(textoStr, "/mês") {
				break
			}
		}

		voltar, err := pg.Locator(`a[id$="wtbtnVoltar"], input[id$="wtbtnVoltar"]`).Count()
		if err == nil && voltar > 0 {
			break
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			time.Sleep(500 * time.Millisecond)
		}
	}

	time.Sleep(2 * time.Second) // estabilização extra

	return nil
}

func (b *Banco) hoje() dominio.Data {
	agora := b.agora
	if agora == nil {
		agora = time.Now
	}
	return dominio.DataDeInstante(agora())
}
