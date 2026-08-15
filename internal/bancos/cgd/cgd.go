// Package cgd é o simulador de crédito à habitação da Caixa Geral de Depósitos.
//
// O mais simples dos dez: HTTP puro, same-origin, **sem autenticação nenhuma** —
// sem chave, sem cookies, sem CSRF, sem reCAPTCHA — e sem pedir um único dado
// pessoal. Dois pedidos: o /limits (elegibilidade) e o /calculate (a simulação);
// o HTML da página (de onde saem os períodos de taxa fixa) só volta a ser pedido
// quando não há catálogo guardado (KAN-36).
//
// O que este pacote sabe sobre a CGD foi medido a 2026-07-26 contra o simulador
// a sério, e está gravado em `capturas/`. Onde um comentário diz "medido", há
// uma captura por trás.
package cgd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/transporte"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

const (
	BancoID   = "cgd"
	BancoNome = "CGD"

	// Base é o simulador público. Não há aqui endpoint de lead nenhum, e não se
	// chama nenhum: um simulador é para simular.
	Base = "https://simuladorch.cgd.pt"

	// ProdutoPacks é a bonificação de spread dos packs de vinculação.
	ProdutoPacks = "cgd:packs"

	// EuriborImposta é o único indexante que a CGD comercializa. O dropdown
	// #IndexRate da página tem uma opção e uma só — "Euribor 6 meses".
	EuriborImposta = dominio.Euribor6M

	// limiteDeCorpo é o tecto de leitura de uma resposta. A homepage anda pelos
	// 77 KB e o /calculate pelos 2 KB; 4 MB é folgado para os dois e impede que
	// uma resposta desgovernada nos coma a memória do varrimento.
	limiteDeCorpo = 4 << 20
)

// Catalogos guarda o que a CGD publica e nós temos de saber antes de lhe montar
// o pedido — aqui, os períodos de taxa fixa (KAN-36).
//
// ⚠️ **Declarada outra vez aqui, e não importada do `bancos`.** É a mesma razão
// que faz o `Novo` receber o transporte em vez do `bancos.Transportes`: se este
// pacote importasse o `bancos`, o registo — que vive lá — não podia importar
// este. As interfaces em Go são estruturais, portanto a `bancos.Catalogos`
// satisfaz esta sem que nenhum dos dois pacotes conheça o outro.
//
// ⚠️ **Nula desliga**, e é o que os testes usam: vai-se à página de cada vez, que
// é o que este banco fazia antes de a tabela existir.
type Catalogos interface {
	Ler(ctx context.Context, bancoID, nome string) (valor []byte, achou bool)
	Guardar(ctx context.Context, bancoID, nome string, valor []byte)
}

// catalogoPeriodos é o nome desta entrada dentro do banco. Um banco pode vir a
// ter mais do que um catálogo, e a chave é (banco_id, nome).
const catalogoPeriodos = "periodos"

// Banco é a CGD.
//
// O transporte é injectado e nunca instanciado aqui — é o que permite correr
// este banco inteiro contra um transporte.Falso, sem rede.
type Banco struct {
	http      transporte.HTTPSimples
	catalogos Catalogos
}

// Novo constrói a CGD sobre o transporte que a infra montou.
//
// ⚠️ Recebe o transporte e não o bancos.Transportes, e devolve *Banco e não
// bancos.Banco, para este pacote não importar o `bancos`. Se o importasse, o
// registo — que vive lá — não podia importar este, e o ciclo fechava-se. Quem
// faz a ponte entre as duas formas é o registo.Predefinido, numa linha.
//
// ⚠️ `catalogos` nulo é válido: a página é pedida a cada simulação com fase fixa,
// que era o comportamento até 2026-08-14.
func Novo(http transporte.HTTPSimples, catalogos Catalogos) *Banco {
	return &Banco{http: http, catalogos: catalogos}
}

func (b *Banco) ID() string   { return BancoID }
func (b *Banco) Nome() string { return BancoNome }

// periodosFixosConhecidos é a lista de recurso para quando o HTML não se deixa
// ler. Serve os Requisitos() — que a app precisa de ter sempre — e não a
// simulação: essa lê o HTML e avisa quando não consegue.
//
// ⚠️ São os da **mista**, medidos a 2026-07-26. A fixa vende todos os anos de 5
// a 40, e declarar aqui os 36 daria à app uma lista que só vale para metade dos
// produtos. Ver a nota em periodosDoHTML.
var periodosFixosConhecidos = []int{5, 10, 15, 20, 25, 30, 35}

// Requisitos declara o que a CGD usa, ignora e impõe.
//
// ⚠️ Ela ignora dados pessoais por completo: não pede idade, rendimento,
// profissão nem número de titulares. Isso não é uma omissão nossa a declarar
// com valor neutro — é o simulador que não os tem, e por isso vão a `Usa:
// false`. A app esbate-os, e a distinção importa: o v1 misturava "a API ignora"
// com "o simulador não pede" debaixo do mesmo nome.
func (b *Banco) Requisitos() dominio.Requisitos {
	usa := func(c dominio.CampoCanonico) dominio.Input {
		return dominio.Input{Campo: c, Usa: true}
	}
	naoUsa := func(c dominio.CampoCanonico, nota string) dominio.Input {
		return dominio.Input{Campo: c, Usa: false, Nota: nota}
	}

	return dominio.Requisitos{
		BancoID:   BancoID,
		BancoNome: BancoNome,
		Custo:     dominio.CustoBarato,

		Inputs: []dominio.Input{
			usa(dominio.CampoValorImovel),
			usa(dominio.CampoMontante),
			usa(dominio.CampoPrazoAnos),
			usa(dominio.CampoTipoTaxa),
			usa(dominio.CampoPeriodoFixo),
			usa(dominio.CampoFinalidade),
			usa(dominio.CampoGarantiaPublica),
			naoUsa(dominio.CampoIndexante, "A CGD só comercializa Euribor a 6 meses e ignora a escolha."),
			naoUsa(dominio.CampoDataNascimento, "O simulador da CGD não pergunta a idade. Ela só entra no prazo máximo, que a CGD limita a terminar aos 70 anos."),
			naoUsa(dominio.CampoRendimentoMensal, "O simulador da CGD não pergunta o rendimento."),
			naoUsa(dominio.CampoProfissao, "O simulador da CGD não pergunta a profissão."),
			naoUsa(dominio.CampoSegundoTitular, "O preço da CGD não depende do número de titulares."),
			naoUsa(dominio.CampoLocalizacao, "O simulador da CGD não pergunta onde fica o imóvel."),
			naoUsa(dominio.CampoTipologia, "O simulador da CGD não pergunta a tipologia."),
			naoUsa(dominio.CampoJaCliente, "O preçário simulado é o mesmo para clientes e não clientes; o desconto vem dos packs."),
		},

		PeriodosFixos:     periodosFixosConhecidos,
		PeriodosFixosModo: dominio.ModoDoHTML,

		// Vazio de propósito: a CGD impõe o seu tenor. Ver EuriborImposta.
		EuriborOpcoes:  nil,
		EuriborImposto: EuriborImposta,

		PrazoMin: 1,
		PrazoMax: 40,

		// ⚠️ 70 é o da habitação própria, que é o caso comum e o mais
		// apertado. A CGD publica 75 na secundária e no arrendamento — o
		// contrato só transporta um número, e a simulação usa o número vivo do
		// /limits, que sabe a finalidade. Declarar aqui o 75 dava à app um
		// prazo que a CGD recusa na própria.
		IdadeMaximaFim: 70,

		Produtos: []dominio.Produto{{
			ID:     ProdutoPacks,
			Rotulo: "Packs CGD (redução de spread)",
			Descricao: "Vinculação, Ligação e Proteção. Descontam spread — medido a " +
				"2026-07-26: 1,350 p.p. passam a 0,650. Exigem deter os produtos.",
			PorOmissao: false,
		}},

		Notas: []string{
			"TAEG e MTIC vêm do próprio simulador, sem lead e sem dados pessoais.",
			"LTV até 90 % na habitação própria e 80 % na secundária e no arrendamento. Com a Garantia Pública Jovens vai até 100 %, mas exige pelo menos 85 %.",
			"Prazo até 40 anos na habitação própria e 30 na secundária e no arrendamento.",
			"A taxa fixa da CGD é ao prazo todo: o período de taxa fixa e o prazo são o mesmo número.",
		},
	}
}

// Simular interroga a CGD.
//
// A ordem é a que o simulador impõe: os períodos (só quando o pedido tem fase
// fixa), depois os limites, e só então o cálculo.
func (b *Banco) Simular(ctx context.Context, p dominio.Pedido) (dominio.Oferta, error) {
	cat, avisoDoHTML, err := b.periodos(ctx, p)
	if err != nil {
		return dominio.Oferta{}, err
	}

	lim, err := b.limites(ctx, p)
	if err != nil {
		return dominio.Oferta{}, err
	}

	valores, ajustes, err := construirPayload(p, lim, cat, b.idadeMaisVelho(p))
	if err != nil {
		return dominio.Oferta{}, err
	}

	corpo, err := b.publicar(ctx, "/calculate", valores.Encode())
	if err != nil {
		return dominio.Oferta{}, err
	}

	// ⚠️ As duas colunas de preço vêm na mesma resposta e a escolha é de leitura,
	// não de pedido: a CGD não tem campo nenhum no /calculate por onde se ligarem
	// os packs. Isso torna-a o banco mais barato de varrer nas duas variantes —
	// um pedido dá as duas linhas.
	oferta, err := lerResposta(corpo, p.TemProduto(ProdutoPacks))
	if err != nil {
		return dominio.Oferta{}, err
	}

	oferta.BancoID, oferta.BancoNome = BancoID, BancoNome
	for _, a := range ajustes {
		oferta.Acrescentar(a)
	}
	if avisoDoHTML != "" {
		oferta.Anotar(avisoDoHTML)
	}
	return oferta, nil
}

// periodos lê os catálogos de períodos do HTML da página.
//
// ⚠️ Devolve um aviso, e não silêncio, quando o HTML não se deixa ler. O v1
// caía na lista estática sem uma palavra: se a CGD mudasse o widget, o
// simulador continuava a responder com os períodos do ano passado e ninguém
// sabia. Aqui a oferta sai com uma nota que o diz.
//
// Só se pede a página quando o pedido tem fase fixa. Numa taxa variável não há
// código nenhum a escolher, e são 77 KB que não se pedem ao banco por nada.
//
// ⚠️ **E com fase fixa também já não se pedem a cada simulação** (KAN-36,
// 2026-08-14). Medido: a página são 75 291 dos 82 333 bytes de um pedido com
// fase fixa — **91,4 %** — para extrair catorze pares `ano → código`. Enquanto
// houve varrimento isto diluía-se por dezenas de pontos da grelha; ao vivo é por
// pessoa que pergunta. O catálogo passa pelos `Catalogos`, que é um parâmetro
// guardado e não uma resposta em cache (`ARQUITETURA.md` §4).
func (b *Banco) periodos(ctx context.Context, p dominio.Pedido) (catalogo, string, error) {
	if !p.TipoTaxa.TemPeriodoFixo() {
		return catalogo{}, "", nil
	}

	if cat, ok := b.periodosGuardados(ctx); ok {
		return cat, "", nil
	}

	recurso := catalogo{
		fixa:      codigosDe(periodosFixosConhecidos),
		mista:     codigosDe(periodosFixosConhecidos),
		deRecurso: true,
	}

	html, err := b.obter(ctx, "/")
	if err != nil {
		return recurso, avisoDeRecurso(err), nil
	}
	fixa, mista, err := periodosDoHTML(html)
	if err != nil {
		return recurso, avisoDeRecurso(err), nil
	}

	cat := catalogo{fixa: fixa, mista: mista}
	b.guardarPeriodos(ctx, cat)
	return cat, "", nil
}

// periodosGuardados lê o catálogo guardado, se houver um válido.
//
// ⚠️ **Um catálogo de recurso nunca se guarda nem se lê daqui.** O que se guarda
// é o que se leu da CGD; guardar a lista conhecida transformava um soluço da
// página em «os períodos do ano passado» durante a validade inteira, e calava o
// aviso que existe precisamente para o dizer.
func (b *Banco) periodosGuardados(ctx context.Context) (catalogo, bool) {
	if b.catalogos == nil {
		return catalogo{}, false
	}
	bruto, achou := b.catalogos.Ler(ctx, BancoID, catalogoPeriodos)
	if !achou {
		return catalogo{}, false
	}
	var guardado periodosEmJSON
	if err := json.Unmarshal(bruto, &guardado); err != nil {
		return catalogo{}, false // ilegível é o mesmo que não haver: vai-se à página
	}
	if len(guardado.Fixa) == 0 || len(guardado.Mista) == 0 {
		return catalogo{}, false
	}
	return catalogo{fixa: guardado.Fixa, mista: guardado.Mista}, true
}

func (b *Banco) guardarPeriodos(ctx context.Context, cat catalogo) {
	if b.catalogos == nil {
		return
	}
	bruto, err := json.Marshal(periodosEmJSON{Fixa: cat.fixa, Mista: cat.mista})
	if err != nil {
		return
	}
	b.catalogos.Guardar(ctx, BancoID, catalogoPeriodos, bruto)
}

// periodosEmJSON é a forma com que este catálogo vai para a tabela.
//
// ⚠️ A forma é **do banco** e a tabela guarda-a opaca: a coluna é `jsonb` e quem
// a lê e escreve é só este pacote. Pôr aqui um tipo partilhado obrigaria a
// camada que guarda a conhecer os catálogos de todos os bancos.
type periodosEmJSON struct {
	Fixa  codigos `json:"fixa"`
	Mista codigos `json:"mista"`
}

func avisoDeRecurso(err error) string {
	return fmt.Sprintf(
		"Não se conseguiu ler da página da CGD os períodos de taxa fixa que ela pratica hoje (%v); "+
			"usaram-se os últimos conhecidos, de 2026-07-26. O período aplicado pode não ser o que a CGD vende agora.",
		err)
}

// codigosDe monta o catálogo de recurso. Os códigos são os que a CGD usava a
// 2026-07-26 para a mista — os únicos que se podem afirmar sem ler a página.
func codigosDe(anos []int) codigos {
	conhecidos := map[int]string{5: "4", 10: "5", 15: "6", 20: "7", 25: "8", 30: "9", 35: "18"}
	c := codigos{}
	for _, a := range anos {
		if v, ok := conhecidos[a]; ok {
			c[a] = v
		}
	}
	return c
}

func (b *Banco) limites(ctx context.Context, p dominio.Pedido) (limites, error) {
	corpo, err := b.publicar(ctx, "/limits", strings.Join([]string{
		"SimulationSubOriginID=1",
		"Code=",
		"IsMedidaJovem=" + booleano(p.GarantiaPublica),
		"Purpose=" + purposeAquisicao,
		"ProductPurpose=" + destino(p.Finalidade),
	}, "&"))
	if err != nil {
		return limites{}, err
	}
	return lerLimites(corpo)
}

// idadeMaisVelho devolve a idade que manda no prazo. Sem titulares devolve
// zero, que é o que desliga o limite por idade — e é o caso do varrimento, que
// corre com titular neutro sobre um banco que não pergunta idades.
//
// ⚠️ É o único sítio deste pacote onde entra um relógio, e entra aqui e não no
// pedido.go porque esse é puro. Não fica atrás de um campo injectável: nada o
// preencheria com outra coisa, e o teste da idade escreve-se sem ele — com uma
// data de nascimento contada a partir de hoje.
func (*Banco) idadeMaisVelho(p dominio.Pedido) int {
	hoje := dominio.DataDeInstante(time.Now())
	idade, err := dominio.IdadeMaisVelho(p.Titulares, hoje)
	if err != nil {
		return 0
	}
	return idade
}

// --- transporte --------------------------------------------------------------

func (b *Banco) obter(ctx context.Context, caminho string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, Base+caminho, nil)
	if err != nil {
		return nil, indisponivel(err)
	}
	req.Header.Set("Accept", "text/html")
	req.Header.Set("User-Agent", agente)
	return b.fazer(req)
}

func (b *Banco) publicar(ctx context.Context, caminho, corpo string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, Base+caminho, strings.NewReader(corpo))
	if err != nil {
		return nil, indisponivel(err)
	}
	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("Origin", Base)
	req.Header.Set("Referer", Base+"/")
	req.Header.Set("User-Agent", agente)
	// Sem este cabeçalho o simulador responde a página em vez do JSON.
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	return b.fazer(req)
}

// agente é o que o simulador espera ver. Não é disfarce: é um cliente de
// browser a falar com um endpoint de browser, e a CGD serve HTML a quem não o
// pareça.
const agente = "Mozilla/5.0 (Windows NT 10.0; Win64; x64)"

func (b *Banco) fazer(req *http.Request) ([]byte, error) {
	resp, err := b.http.Fazer(req.Context(), req)
	if err != nil {
		return nil, indisponivel(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, indisponivel(fmt.Errorf("%s devolveu %s", req.URL.Path, resp.Status))
	}

	corpo, err := io.ReadAll(io.LimitReader(resp.Body, limiteDeCorpo))
	if err != nil {
		return nil, indisponivel(fmt.Errorf("ler o corpo de %s: %w", req.URL.Path, err))
	}
	return corpo, nil
}

func indisponivel(err error) error {
	return &dominio.ErroOferta{
		Codigo:   dominio.ErroBancoIndisponivel,
		Mensagem: fmt.Sprintf("A CGD não respondeu: %v", err),
	}
}
