# Investigação: API/Screen Services do BPI

**Data:** 2026-08-16
**Issue:** KAN-21
**Objetivo:** Determinar se o BPI expõe uma API JSON ou OutSystems screen services que permitam simular em HTTP puro (~2 s) em vez de browser (~52 s).

---

## O que se fez

1. **Fetch do HTML** da página `https://www.bancobpi.pt/particulares/credito/credito-habitacao/simulador-credito-habitacao`
2. **Análise do `_osjs.js`** (runtime OutSystems 11.34.1.45035)
3. **Revisão do scraper v1** (`simulador-credito-habitacao/src/chmonitor/scrapers/bpi.py`)

## O que se encontrou

### Estrutura da página

A página é um formulário OutSystems Traditional Web (`Simulacao.aspx`):

- **Viewstate criptografado:** `__OSVSTATE` (não `__VIEWSTATE` ASP.NET, mas conceito equivalente)
- **Postbacks:** todas as interações são AJAX postbacks para `Simulacao.aspx` via `OsAjax()`
- **Form action:** `Simulacao.aspx` (self-posting)
- **Mecanismo:** `__doPostBack()` → `OsAjax()` → `OsExecuteCallToServer()` → submit do form com `__OSVSTATE`

### Runtime OutSystems (`_osjs.js`)

- jQuery 1.8 + plugin `outsystems.internal` (postback, validação, formatação)
- Funções principais: `OsAjax()`, `OsExecuteCallToServer()`, `OsRefreshElement()`, `OsJSONUpdate()`
- **Nenhuma referência a screen services ou endpoints REST** no runtime genérico
- O runtime é o mesmo para todas as aplicações OutSystems — não contém endpoints específicos da aplicação BPI

### Scraper v1

O scraper v1 (`bpi.py`) usa Playwright para:
1. Navegar à página
2. Preencher campos (valor imóvel, rendimento, prazo, etc.)
3. Clicar em botões (radio buttons, botões de cálculo)
4. Esperar por elementos na página
5. Ler o texto da página via `innerText` e extrair valores com regex

**Não há chamadas HTTP diretas** — tudo passa pelo browser.

## O que NÃO se conseguiu fazer

1. **Captura de tráfego de rede** com DevTools do browser durante uma simulação real — isto é essencial para ver se existem chamadas XHR/fetch para endpoints REST que retornam JSON
2. **Teste de endpoints especulativos** (ex: `/BPICreditoHabitacao/rest/...`) — sem evidência de que existem, testar à sorte não é produtivo

## Análise

O OutSystems pode expor "screen services" que são a mesma lógica da tela acessível via REST/JSON. Estes ficariam tipicamente em URLs como:
- `/AppName/rest/ScreenName/ActionName`
- `/AppName/rest/Simulacao/Calcular`

**Mas não há evidência destes endpoints no HTML ou no JavaScript da página.** Os screen services, se existirem, seriam descobertos através de:
1. Captura de tráfego de rede durante uma simulação real
2. Documentação interna do OutSystems (que não temos)
3. Prospecção de URLs comuns (arriscado e não produtivo)

### Comparação com outros bancos

| Banco | Transporte | Tempo | Nota |
|-------|-----------|-------|------|
| Banco CTT | HTTP puro (screen services) | ~1 s | OutSystems, mas com API |
| Crédito Agrícola | HTTP puro (screen services) | ~2.5 s | OutSystems, mas com API |
| BPI | Browser (form postbacks) | ~52 s | OutSystems, sem API conhecida |

**A presença de OutSystems não garante a existência de screen services.** Dois bancos têm, um pode não ter.

## Conclusão

**Não há API JSON.** Captura de tráfego de 2026-08-16 confirma:

### Os 3 tipos de pedido observados

1. **`/PerformanceProbe/rest/BeaconInternal/WebScreenClientExecutedEvent`** — telemetria OutSystems (rastreio de eventos client-side). Não é simulação.

2. **`Simulacao.aspx` AJAX postbacks** — form submissions com `__OSVSTATE` (viewstate criptografado). Resposta: JavaScript com `OsJSONUpdate()` que contém **fragmentos HTML**, não JSON. Content-Type: `text/html`.

3. **`ResultadoSimulacao.aspx` AJAX postbacks** — mesma mecânica: `__OSVSTATE` in, fragmento HTML out. Content-Type: `text/html`.

### O que o BPI retorna

O "JSON" nas respostas é na verdade código JavaScript que chama `OsJSONUpdate({outers: {...}, hidden: {...}, js: [...]})`. Isto é o mecanismo AJAX do OutSystems a actualizar o DOM com **novos fragmentos HTML** — não é uma API REST a retornar dados estruturados.

Exemplo real da resposta:
```javascript
OsJSONUpdate({
    "outers": {
        "BPIWeb_Theme_..._wtctnResultado": {
            "inner": "<div class=\"credito-habitacao\">...HTML com valores...</div>",
            "attributes": { "id": "..." }
        }
    },
    "hidden": { "__OSVSTATE": "..." }
})
```

Os valores (prestação, TAEG, MTIC) vêm **embebidos no HTML**, não como campos JSON separados.

### Caminho para o v2

O scraper do BPI terá de ser **browser-based** (Playwright/Puppeteer), como o v1. A optimização passa por:
- Selectores CSS estruturados (nunca `innerText` + regex)
- Esperas por elemento (nunca `sleep` fixo)
- Extrair valores dos spans com classes específicas (ex: `.value-container-value`, `.account-name`)

---

*Conclusão baseada em captura de tráfego de rede de 2026-08-16.*
