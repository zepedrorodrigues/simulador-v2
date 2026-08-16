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

**Inconclusivo.** Não há evidência de que o BPI tem uma API JSON, mas também não há evidência de que não tem.

**Para confirmar, é necessário:**
1. Abrir o site do BPI no browser
2. Abrir DevTools → Network
3. Fazer uma simulação completa
4. Procurar por chamadas XHR/fetch que retornem JSON (em vez de HTML/postback)

Se existirem essas chamadas, o scraper pode ser reescrito em HTTP puro (~2 s). Se não existirem, o caminho é browser com selectores estruturados (nunca esperas por tempo nem recorte de texto).

## Próximos passos

1. **Captura manual de tráfego** — alguém precisa de fazer uma simulação no browser e gravar o que a rede envia/recebe
2. **Se JSON encontrado:** identificar endpoints, parâmetros e respostas; seguir CONTRATO-BANCO.md §3
3. **Se apenas postbacks:** documentar a conclusão e avançar com browser + selectores estruturados

---

*Este documento é uma investigação, não uma conclusão. A conclusão depende de dados que só um browser pode fornecer.*
