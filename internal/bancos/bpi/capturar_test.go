//go:build rede

package bpi_test

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
)

const (
	bpiURL = "https://www.bancobpi.pt/particulares/credito/credito-habitacao/simulador-credito-habitacao"
)

func TestCapturarBPI(t *testing.T) {
	if testing.Short() {
		t.Skip("captura de BPI requer rede e browser")
	}

	pw, err := playwright.Run()
	if err != nil {
		t.Fatalf("playwright: %v", err)
	}
	defer func() { _ = pw.Stop() }()

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(true),
	})
	if err != nil {
		t.Fatalf("browser: %v", err)
	}
	defer func() { _ = browser.Close() }()

	page, err := browser.NewPage()
	if err != nil {
		t.Fatalf("page: %v", err)
	}

	// Navegar para o simulador
	t.Log("a navegar para o simulador BPI...")
	if _, err := page.Goto(bpiURL); err != nil {
		t.Fatalf("goto: %v", err)
	}

	// Esperar que a página carregue
	if err := page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
		State: playwright.LoadStateNetworkidle,
	}); err != nil {
		t.Log("aviso: network idle timeout, a continuar...")
	}

	// Aceitar cookies se aparecerem
	t.Log("a procurar banner de cookies...")
	for _, nome := range []string{"Concordar com todos", "Aceitar"} {
		btn := page.GetByRole("button", playwright.PageGetByRoleOptions{
			Name: nome,
		})
		count, err := btn.Count()
		if err != nil {
			t.Logf("aviso: count para %q falhou: %v", nome, err)
			continue
		}
		if count > 0 {
			if err := btn.First().Click(); err != nil {
				t.Logf("aviso: clique em %q falhou: %v", nome, err)
			} else {
				t.Log("cookies aceites")
				break
			}
		}
	}

	// Esperar 5 segundos para a página estabilizar (como no v1)
	time.Sleep(5 * time.Second)

	// Função para preencher input
	fillInput := func(suffix, value string) error {
		el := page.Locator(fmt.Sprintf(`input[id$="%s"]`, suffix)).First()
		if err := el.Click(); err != nil {
			// Fallback: focar via teclado (como no v1)
			t.Logf("aviso: clique em %s falhou, a focar via teclado: %v", suffix, err)
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
		// Esperar pelo postback (como no v1)
		time.Sleep(1 * time.Second)
		return nil
	}

	// Função para selecionar opção
	selectOption := func(suffix, label string) error {
		sel := page.Locator(fmt.Sprintf(`select[id$="%s"]`, suffix)).First()
		if _, err := sel.SelectOption(playwright.SelectOptionValues{
			Labels: &[]string{label},
		}); err != nil {
			return fmt.Errorf("select %s: %w", suffix, err)
		}
		// Esperar pelo postback (como no v1)
		time.Sleep(1 * time.Second)
		return nil
	}

	// Função para esperar pelo overlay (como no v1)
	waitForAjaxIdle := func() {
		overlay := page.Locator(`[class*="Feedback_AjaxWait"]`).First()
		count, err := overlay.Count()
		if err == nil && count > 0 {
			_ = overlay.WaitFor(playwright.LocatorWaitForOptions{
				State: playwright.WaitForSelectorStateHidden,
			})
		}
		time.Sleep(300 * time.Millisecond)
	}

	// Preencher o formulário com dados fictícios
	t.Log("a preencher o formulário...")

	// 1. Clicar em "Comprar Casa" (opcional, mas faz parte do fluxo)
	t.Log("a clicar em Comprar Casa...")
	comprarBtn := page.GetByRole("button", playwright.PageGetByRoleOptions{
		Name: "Comprar Casa",
	})
	if count, err := comprarBtn.Count(); err == nil && count > 0 {
		if err := comprarBtn.First().Click(); err != nil {
			t.Logf("aviso: clique em Comprar Casa falhou: %v", err)
		} else {
			waitForAjaxIdle()
		}
	}

	// 2. Montante: 200.000 EUR
	if err := fillInput("wtInputValorEuros", "200000"); err != nil {
		t.Fatalf("montante: %v", err)
	}

	// 3. Prazo: 30 anos
	if err := fillInput("wtInputPrazoAnos", "30"); err != nil {
		t.Fatalf("prazo: %v", err)
	}

	// 4. Modalidade: Taxa Variável
	if err := selectOption("wtinputModalidade", "Taxa Variável"); err != nil {
		t.Fatalf("modalidade: %v", err)
	}

	// 5. Selecionar 1 proponente (importante: o padrão é 2)
	// Usar JavaScript para disparar um MouseEvent no radio (simula clique real)
	t.Log("a selecionar 1 proponente...")
	if _, err := page.Evaluate(`() => {
		const radio = document.querySelector('input[id*="Proponentes"][value="1"]');
		if (radio) {
			radio.checked = true;
			// Disparar evento de clique real para o OutSystems detectar
			radio.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
			return true;
		}
		return false;
	}`); err != nil {
		t.Logf("aviso: JS click no radio 1 proponente falhou: %v", err)
	} else {
		waitForAjaxIdle()
	}

	// 6. Data de nascimento: 01/01/1990
	if err := fillInput("wtinputDataNascimento1", "01/01/1990"); err != nil {
		t.Fatalf("data nascimento: %v", err)
	}

	// 7. Tipo de propriedade: Habitação Própria Permanente
	if err := selectOption("wtinputTipoPropriedade", "Habitação Própria Permanente"); err != nil {
		t.Fatalf("tipo propriedade: %v", err)
	}

	// 8. Distrito: LISBOA
	if err := selectOption("wtinputDistrito", "LISBOA"); err != nil {
		t.Fatalf("distrito: %v", err)
	}

	// Esperar pelo fim de qualquer postback
	waitForAjaxIdle()

	// Capturar HTML antes de submeter
	htmlAntes, err := page.Evaluate("() => document.body.innerHTML")
	if err == nil {
		if htmlStr, ok := htmlAntes.(string); ok {
			if err := os.WriteFile("capturas/variavel_30a_antes.html", []byte(htmlStr), 0o644); err != nil {
				t.Logf("aviso: WriteFile HTML antes falhou: %v", err)
			}
		}
	}

	// Clicar em Simular
	t.Log("a clicar em Simular...")
	simularBtn := page.Locator(`input[id$="wtbtnSimular"]`).First()
	if err := simularBtn.Click(); err != nil {
		t.Fatalf("simular: %v", err)
	}

	// Esperar até que o botão "Voltar" apareça (indicador de resultados)
	t.Log("a aguardar resultados...")
	for i := 0; i < 60; i++ { // max 30s
		// Verificar texto da página
		texto, _ := page.Evaluate("() => document.body ? document.body.innerText : ''")
		if textoStr, ok := texto.(string); ok {
			if strings.Contains(textoStr, "Prazo do Empréstimo") && strings.Contains(textoStr, "EUR") {
				t.Logf("iteração %d: formulário ainda visível (página não mudou)", i)
			}
			if strings.Contains(textoStr, "Resultado") || strings.Contains(textoStr, "TAEG") || strings.Contains(textoStr, "MTIC") {
				t.Logf("iteração %d: resultados detectados no texto", i)
				break
			}
			if strings.Contains(textoStr, "Carregando") || strings.Contains(textoStr, "aguarde") {
				t.Logf("iteração %d: ainda a carregar...", i)
			}
		}
		voltar, err := page.Locator(`a[id$="wtbtnVoltar"], input[id$="wtbtnVoltar"]`).Count()
		if err == nil && voltar > 0 {
			t.Logf("iteração %d: botão Voltar encontrado", i)
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	time.Sleep(2 * time.Second) // estabilização extra

	// Capturar HTML depois de clicar em Simular
	htmlDepois, err := page.Evaluate("() => document.body.innerHTML")
	if err == nil {
		if htmlStr, ok := htmlDepois.(string); ok {
			if err := os.WriteFile("capturas/variavel_30a_depois.html", []byte(htmlStr), 0o644); err != nil {
				t.Logf("aviso: WriteFile HTML depois falhou: %v", err)
			}
		}
	}

	// Capturar texto depois de clicar em Simular
	body, err := page.Evaluate("() => document.body ? document.body.innerText : ''")
	if err == nil {
		if bodyStr, ok := body.(string); ok {
			if err := os.WriteFile("capturas/variavel_30a_depois.txt", []byte(bodyStr), 0644); err != nil {
				t.Logf("aviso: WriteFile TXT depois falhou: %v", err)
			}
		}
	}

	// Verificar se há resultados
	t.Log("a verificar resultados...")
	body, err = page.Evaluate("() => document.body ? document.body.innerText : ''")
	if err != nil {
		t.Fatalf("innerText: %v", err)
	}

	bodyStr, ok := body.(string)
	if !ok {
		t.Fatal("innerText não é string")
	}

	// Guardar resultados para debug
	resultado := map[string]interface{}{
		"banco":        "bpi",
		"modalidade":   "Taxa Variável",
		"prazo_anos":   30,
		"montante":     200000,
		"data_captura": time.Now().Format(time.RFC3339),
		"texto":        bodyStr,
	}

	jsonBytes, err := json.MarshalIndent(resultado, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if err := os.WriteFile("capturas/variavel_30a.json", jsonBytes, 0644); err != nil {
		t.Fatalf("WriteFile JSON: %v", err)
	}
	t.Log("JSON guardado em capturas/variavel_30a.json")

	// Verificar se há resultados
	if strings.Contains(bodyStr, "TAEG") && strings.Contains(bodyStr, "/mês") {
		t.Log("resultados encontrados!")
		// Guardar HTML e texto dos resultados
		if err := os.WriteFile("capturas/variavel_30a.html", []byte(htmlDepois.(string)), 0644); err != nil {
			t.Fatalf("WriteFile HTML: %v", err)
		}
		if err := os.WriteFile("capturas/variavel_30a.txt", []byte(bodyStr), 0644); err != nil {
			t.Fatalf("WriteFile TXT: %v", err)
		}
	} else {
		t.Log("resultados não encontrados no texto")
		// Verificar se há erros
		if strings.Contains(bodyStr, "Prazo não pode ser superior a") {
			t.Logf("BPI rejeitou o prazo: %s", bodyStr)
		}
		if strings.Contains(bodyStr, "Preencha todos os campos obrigatórios") {
			t.Log("AVISO: formulário com campos obrigatórios por preencher")
		}
	}
}
