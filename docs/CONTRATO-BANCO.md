# Contrato dos bancos

Como se acrescenta um banco, e a disciplina que o torna testável. O conhecimento técnico já apurado sobre cada banco está em `DOSSIE-BANCOS.md`; este documento é o *como*, não o *o quê*.

## 1. A interface

```go
package bancos

// Banco é o que um banco sabe fazer. Nada mais entra aqui: se um banco
// precisar de um método que só ele tem, isso é um detalhe do seu pacote.
type Banco interface {
    ID() string
    Nome() string

    // Requisitos declara o que este banco usa, aceita e impõe: que inputs lê,
    // que períodos fixos são válidos, se deixa escolher o indexante Euribor,
    // que produtos expõe, e os limites de prazo. A app monta o formulário
    // adaptativo a partir disto — é a única fonte dessa informação.
    Requisitos() dominio.Requisitos

    // Simular interroga o banco. Devolve erro apenas quando o banco não
    // conseguiu responder de todo; um pedido que o banco ajustou devolve uma
    // Oferta com Aplicado e Notas preenchidos, e erro nil.
    Simular(ctx context.Context, p dominio.Pedido) (dominio.Oferta, error)
}
```

⚠️ `Simular` respeita o `ctx`. O orquestrador impõe um prazo por banco; um banco que ignore o cancelamento segura a comparação inteira. No v1 o BPI consumia 52 s de um pedido de 52 s — em Go isso é um `context.WithTimeout` e um banco que não o honre é um defeito.

## 2. Os três ficheiros

```
internal/bancos/cgd/
  cgd.go         monta as peças, declara Requisitos(), implementa Simular()
  pedido.go      dominio.Pedido  →  o payload que o banco espera     (PURO)
  resposta.go    o payload do banco →  dominio.Oferta                (PURO)
  capturas/      pares pedido/resposta reais, versionados
  cgd_test.go    os testes: parsing contra capturas, e mapeamento de pedido
```

**Puro** quer dizer: sem rede, sem relógio, sem base de dados, sem variáveis de ambiente. Uma função de dados para dados.

```go
// pedido.go
func construirPayload(p dominio.Pedido, cfg configBanco) (url.Values, error)

// resposta.go
func lerResposta(corpo []byte, p dominio.Pedido) (dominio.Oferta, error)
```

Toda a inteligência difícil — que campo significa o quê, que valor é o spread da fase indexada e não o da fase fixa, que erro do banco traz o prazo máximo lá dentro — vive nestas duas funções. E ambas são testáveis sem tocar na rede.

⚠️ **É esta a diferença estrutural face ao v1.** Lá, os seis bancos de HTTP puro eram testáveis (trocando a classe do cliente HTTP globalmente) e os quatro de browser não eram testáveis de todo. Aqui a testabilidade não depende do transporte, porque o parsing não conhece o transporte.

## 3. Captura primeiro

**Não se escreve um banco a partir da documentação nem a partir de suposições. Escreve-se a partir de uma resposta real, gravada.**

A ordem é sempre esta:

1. **Capturar.** Fazer uma simulação real no site do banco, com o browser, e gravar o pedido e a resposta. Guardar em `capturas/` com um nome que diga o cenário: `mista_5a_ltv80.json`, `variavel_ltv90_2titulares.json`.
2. **Escrever **`resposta.go` até o teste contra essa captura passar.
3. **Escrever **`pedido.go` até o payload gerado bater com o payload capturado.
4. **Só então** ligar o transporte e correr ao vivo.
5. **Confirmar ao vivo** que o número que sai é o mesmo que o site do banco mostra. Registar essa confirmação no teste, com a data.

⚠️ **Uma captura por comportamento, não uma por banco.** O que interessa capturar são os casos que a lógica distingue: cada tipo de taxa, com e sem produtos, um e dois titulares, e **cada modo de falha conhecido**. Um banco com uma só captura tem um parser que só se sabe funcionar num caminho.

### O que **não** vai para uma captura

⚠️ As capturas são versionadas e o repositório é público. Antes de gravar:

- **Nada de dados pessoais reais.** Datas de nascimento e rendimentos das capturas são de um titular fictício. As da grelha de referência já o são por construção.
- **Nada de cookies de sessão, tokens ou cabeçalhos de autenticação** no ficheiro gravado. Removem-se na captura, e há uma verificação no portão que procura padrões óbvios (`Authorization`, `Set-Cookie`, `Bearer`).
- **Nada de endpoints de *****lead*****.** Um simulador público é para simular. Um endpoint que regista um contacto no CRM do banco não se chama, nem em captura nem em teste. O v1 mantinha uma lista destes caminhos; ela migra para cá.

### Quando o banco muda o formato

Não se corrige o parser contra a produção. Captura-se de novo, vê-se o teste **falhar** contra a captura nova, e só então se corrige. É a regra de casa aplicada aqui: um teste que nunca se viu falhar não prova nada.

## 4. As quatro estratégias de transporte

O transporte é injectado, nunca instanciado dentro do banco. São quatro porque o v1 provou que são precisas quatro — não é generalização antecipada.

```go
// internal/bancos/transporte/transporte.go
type HTTPSimples interface {
    Fazer(ctx context.Context, req *http.Request) (*http.Response, error)
}

// Sessao é o que o arranque deixou para o pedido real.
type Sessao struct {
    HTML []byte
}

type HTTPComSessao interface {
    HTTPSimples

    // Arrancar faz o GET inicial: fixa os cookies no jar e devolve o corpo.
    Arrancar(ctx context.Context, url string) (Sessao, error)
}

type Browser interface {
    Abrir(ctx context.Context, url string, opts OpcoesBrowser) (Pagina, error)
}
```

⚠️ **Duas correcções a este documento, feitas ao implementar (KAN-6) e registadas aqui antes do código.**

**`HTTPComSessao` estende `HTTPSimples`.** A redacção anterior dava-lhe só o `Arrancar`, e então o `POST` do Montepio teria de sair de um `HTTPSimples` à parte — que é outro cliente, com outro jar, e portanto sem os cookies que o arranque acabou de fixar. Dito de outra maneira: as duas interfaces separadas obrigavam a partilhar um jar entre elas, que é a mesma exigência escrita de forma que se pode esquecer. Estendendo, o pedido real sai por construção do cliente que tem a sessão. Há um teste que o mede — dois clientes distintos não vêem os cookies um do outro.

**`Sessao` leva o HTML em bruto e não os valores já extraídos.** «Mais o que for extraído do HTML» punha a leitura dentro do transporte, e o que dela se extrai é conhecimento do banco (o `HashRequest` é do Montepio, não do HTTP). Devolvendo o corpo, a extracção fica onde a §2 manda — numa função pura do pacote do banco, testável contra uma captura e sem rede. Os cookies não passam pela `Sessao` de propósito: vivem no jar do cliente, que é quem faz o pedido a seguir.

**O transporte falso.** `transporte.Falso` cumpre as duas interfaces, grava o que lhe pediram — com o corpo já lido, e sem o gastar para quem o receba a seguir — e responde o que lhe programaram. O campo `Atraso` é o que torna mensurável um banco que ignore o `ctx`: o falso desiste com o `ctx`, e um banco que não o passe fica lá dentro o atraso inteiro.

| estratégia | bancos | fase |
| --- | --- | --- |
| `HTTPSimples` | CGD, Novo Banco, Banco CTT, Crédito Agrícola, Santander | 1 e 2 |
| `HTTPComSessao` | Montepio | 1 |
| `BrowserParaCredencial` | ActivoBank, Millennium BCP | 3 |
| `BrowserComoCliente` | Bankinter, BPI | 3 |

⚠️ **As duas de browser são risco por avaliar.** Ver `ARQUITETURA.md` §5.

## 5. Requisitos: o formulário adaptativo

`Requisitos()` é a única fonte do que a app mostra. Se um banco não pede o rendimento, a app tem de o saber por aqui — não por uma lista escrita à mão do lado da app.

```go
type Requisitos struct {
    BancoID, BancoNome string
    Custo              string    // "barato" | "caro" — expectativa honesta de tempo
    Inputs             []Input   // cada input canónico: usa? com que nota?
    PeriodosFixos      []int     // anos válidos para taxa mista/fixa
    PeriodosFixosModo  string    // "lista" | "da-api" | "do-html"
    EuriborOpcoes      []string  // vazio = o banco impõe o seu
    EuriborImposto     string
    PrazoMin, PrazoMax int
    IdadeMaximaFim     int       // idade do titular no fim do contrato
    Produtos           []Produto
    Notas              []string
}
```

⚠️ `EuriborOpcoes` vazio não é «não sei». Quer dizer que o banco impõe o seu indexante e ignora a escolha. O v1 aprendeu isto à custa do Banco CTT: a API aceitava outros identificadores de indexante, mas o bundle do próprio simulador forçava um — e o preço dos outros era preço que o banco não comercializa. **Uma API aceitar não é o banco vender.**

⚠️ `Custo` não é decoração nem é opcional: o `openapi.yaml` exige-o em `Banco`, e sem ele o `GET /api/v1/bancos` não é servível. Com bancos caros seleccionados a comparação demora dezenas de segundos, e esconder isso parece avaria.

⚠️ **Um input que o banco pede e que nós preenchemos com um valor neutro declara-se com **`usa: true`** e uma nota que o diga.** Um campo preenchido por nós é um pressuposto: se não muda o preço, basta estar declarado; se muda, tem de aparecer na oferta como nota, pela mesma regra da §6. O v1 misturou duas perguntas diferentes debaixo do mesmo nome — declarava `requires_occupation=False` em nove bancos e ao mesmo tempo enviava `profissao`, `habilitacoes`, `situacaoProfissional` e `vinculoLaboral` fixos no Novo Banco, e `occupationType: "PERMANENT"` no Santander. «A API ignora» e «o simulador não pede» não são a mesma afirmação, e só a segunda dispensa a nota.

⚠️ **`Produtos` declara o que existe; quem escolhe é o **`Pedido`** (KAN-33).** Cada `Produto.ID` vai prefixado pelo banco (`cgd:packs`, `novobanco:protecao`) — não é convenção de leitura, é o que reparte a selecção de um pedido que vai a vários bancos, e o `Requisitos.Validar` recusa um id sem prefixo. `PorOmissao` é conselho à app sobre o que pré-seleccionar, **e mais nada**: um `Pedido` sem produtos pede o preço sem produtos. Um banco que ligue por si os seus descontos põe-se ao lado dos outros com uma vantagem que não tem — medido, e inverteu a ordem entre a CGD e o Novo Banco (ver `ARQUITETURA.md` §4).

⚠️ **E o caminho da selecção mede-se, banco a banco: ela pode ir no pedido ou sair na leitura.** Na CGD as duas colunas de preço vêm no mesmo `/calculate` (`BaseResult` e `DiscountedResult`), logo a escolha é de **leitura** e um pedido dá as duas linhas do varrimento. No Novo Banco é o campo `bonificacoes` do **pedido** que decide, e por isso são precisas duas capturas — é o que a §3 quer dizer com «com e sem produtos».

⚠️ **Ler da resposta o que o banco aplicou, em vez de confiar no nosso mapa.** O mesmo caso: o scraper do v1 passou a ler o tenor real da resposta em vez de assumir o que tinha pedido. Uma renumeração silenciosa do lado do banco passa a ser visível em vez de produzir números errados com ar de certos.

## 6. Ajuste, nota, falha — qual é qual

Três resultados possíveis, e a distinção importa:

| situação | resultado |
| --- | --- |
| O banco aceitou o pedido tal como foi | `Oferta` com `Aplicado` vazio |
| O banco não aceita esse valor exacto e há um próximo válido | `Oferta`, `Aplicado` diz o que foi usado, `Notas` explica em português |
| O banco não oferece este produto de todo | `Oferta` de falha, com uma razão que nomeia a coisa |
| O banco não respondeu, ou respondeu lixo | `Oferta` de falha, com o erro técnico |

Exemplos reais do v1, todos do primeiro e segundo tipo:

- Pedido de 5 anos de período fixo ao Santander → aplica 4, anota.
- Prazo acima do máximo por idade → simula no máximo, anota qual e porquê.
- Montepio em taxa fixa → o banco não tem taxa fixa pura; aproxima com mista do prazo todo, e anota que é uma aproximação.
- Crédito Agrícola com spread promocional mais imóvel do banco → são exclusivos; cede o promocional e anota.

⚠️ **Uma nota é para pessoas, e é obrigatória sempre que **`Aplicado` não está vazio. Devolver números diferentes dos pedidos sem o dizer, numa comparação de crédito, é enganador.

## 7. Como acrescentar um banco: a lista

1. Ler a entrada do banco em `DOSSIE-BANCOS.md`.
2. Capturar as respostas reais (§3) e limpá-las de segredos e dados pessoais.
3. Levantar, **na captura**, o que o simulador pede sobre profissão, vínculo, habilitações e situação profissional — e, separadamente, o que a API usa de facto. São duas perguntas, e o v1 respondeu-lhes com um campo só (ver §5).
4. `resposta.go` + testes contra as capturas. **Ver os testes falhar primeiro.**
5. `pedido.go` + teste que compara com o payload capturado.
6. `Requisitos()` — o que o banco usa, ignora e impõe, e com que valor neutro.
7. Ligar o transporte; um teste ao vivo, por trás de `//go:build rede`, que não corre no portão. E `prova.RespeitaPrazo(t, banco, pedido, prazo)` com um `transporte.Falso` lento — a afirmação de que o `Simular` volta dentro do prazo do `ctx` é de cada banco, não do contrato em geral.
8. Confrontar com o site do banco e registar a data da confirmação.
9. Registar no `registo.go` e acrescentar ao cenário de referência do varrimento.
10. Actualizar a tabela de bancos no `README.md`.

**Critério de pronto de um banco:** os testes de captura passam offline, o número foi confrontado com o site do banco, o `Simular` volta dentro do prazo do `ctx` contra um transporte lento, e cada ajuste que o banco faz ao pedido está coberto por um teste que se viu falhar.
