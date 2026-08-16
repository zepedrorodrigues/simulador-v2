package transporte

import (
	"context"
	"fmt"
	"time"

	"github.com/mxschmitt/playwright-go"
)

// BrowserPlaywright implementa BrowserComoCliente com playwright-go.
//
// ⚠️ Cada chamada a Executar arranca um browser novo e fecha-o no fim. A
// optimização — reutilizar o browser entre simulações — é da infra, não do
// banco, e fica para quando houver mais do que um banco de browser.
type BrowserPlaywright struct {
	pw *playwright.Playwright
}

// NovoBrowserPlaywright arranca o Playwright e devolve o transporte.
//
// ⚠️ O Playwright tem de estar instalado: `playwright install chromium`.
func NovoBrowserPlaywright() (*BrowserPlaywright, error) {
	pw, err := playwright.Run()
	if err != nil {
		return nil, fmt.Errorf("playwright: %w", err)
	}
	return &BrowserPlaywright{pw: pw}, nil
}

// Parar fecha o Playwright e liberta os recursos.
func (b *BrowserPlaywright) Parar() {
	if b.pw != nil {
		_ = b.pw.Stop()
	}
}

// Executar abre um browser Chromium, navega para url, executa o callback, e
// devolve o HTML da página no fim.
func (b *BrowserPlaywright) Executar(
	ctx context.Context, url, userAgent string,
	callback func(ctx context.Context, page interface{}) error,
) (string, error) {
	browser, err := b.pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(true),
	})
	if err != nil {
		return "", fmt.Errorf("browser: %w", err)
	}
	defer func() { _ = browser.Close() }()

	page, err := browser.NewPage()
	if err != nil {
		return "", fmt.Errorf("page: %w", err)
	}

	// Executar o callback com o contexto
	if err := callback(ctx, page); err != nil {
		return "", err
	}

	// Extrair o HTML da página
	html, err := page.Evaluate("() => document.body.innerHTML")
	if err != nil {
		return "", fmt.Errorf("evaluar innerHTML: %w", err)
	}

	htmlStr, ok := html.(string)
	if !ok {
		return "", fmt.Errorf("innerHTML não é string")
	}

	return htmlStr, nil
}

// EsperarPorRedeIdle espera que a rede fique inativa.
func EsperarPorRedeIdle(page playwright.Page, timeout time.Duration) {
	_ = page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
		State: playwright.LoadStateNetworkidle,
	})
	time.Sleep(300 * time.Millisecond)
}
