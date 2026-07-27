package montepio

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"

	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// A resposta do Montepio é a da plataforma ITSCredit: um envelope
// `{Status, Error, Result}` e, lá dentro, o plano por fases, os encargos e os
// números oficiais.
//
// ⚠️ Os números vêm como números de JSON e lêem-se para json.Number, não para
// float64 — a regra do dominio/dinheiro.go. json.Number guarda o texto tal como
// veio e TaxaDeTexto/DinheiroDeTexto lêem-no exacto.
type resposta struct {
	Status string     `json:"Status"`
	Error  *erroBanco `json:"Error"`
	Result *resultado `json:"Result"`
}

// erroBanco é a recusa. ⚠️ Ao contrário do Novo Banco, **não tem código**:
// medido a 2026-07-27, o `Code` veio nulo nos quatro modos de falha capturados.
// O que sobra é uma mensagem em português — e ela mente sobre a causa (ver
// lerErro).
type erroBanco struct {
	VisibleToHuman bool    `json:"VisibleToHuman"`
	Code           *string `json:"Code"`
	Message        string  `json:"Message"`
}

type resultado struct {
	Ammount            json.Number `json:"Ammount"`
	Term               int         `json:"Term"`
	ProductCode        string      `json:"ProductCode"`
	ProductDescription string      `json:"ProductDescription"`
	TAEG               json.Number `json:"TAEG"`
	MTIC               json.Number `json:"MTIC"`
	Spread             json.Number `json:"Spread"`
	SpreadBase         json.Number `json:"SpreadBase"`
	TotalInterest      json.Number `json:"TotalInterest"`
	PeriodInstallment  []faseBruta `json:"PeriodInstallment"`

	// HasIS e IsISInstallmentOut dizem se o Imposto do Selo sobre os juros está
	// **dentro** da prestação. ⚠️ Não é detalhe fiscal: no arrendamento vem
	// dentro, e é isso que faz a mesma TAN dar uma prestação diferente (ver
	// anotarImpostoDoSelo).
	HasIS              bool `json:"HasIS"`
	IsISInstallmentOut bool `json:"IsISInstallmentOut"`
}

// faseBruta é um troço do plano.
//
// ⚠️ Duration é ponteiro porque a **última fase da mista vem sem duração**
// (`null`) — medido nas capturas mista_5a e mista_25a. Ela vai até ao fim do
// prazo, e é assim que se lê.
type faseBruta struct {
	Position    int         `json:"Position"`
	Duration    *int        `json:"Duration"`
	Installment json.Number `json:"Installment"`
	TAN         json.Number `json:"TAN"`
	Rate        taxaDaFase  `json:"Rate"`
}

type taxaDaFase struct {
	BaseRateCode        string      `json:"BaseRateCode"`
	BaseRateDescription string      `json:"BaseRateDescription"`
	BaseRateValue       json.Number `json:"BaseRateValue"`
	Spread              json.Number `json:"Spread"`
	SpreadBase          json.Number `json:"SpreadBase"`
}

// indexanteDoCodigo traduz o código da taxa base da fase no tenor da Euribor.
//
// ⚠️ É por aqui que se sabe o indexante, e não pelo que se pediu — é o que a
// §5 do CONTRATO-BANCO.md manda. E a numeração do banco não é a óbvia: a Euribor
// a 12 meses é **EH2**, não "EH12" — medido a 2026-07-27 nas três capturas de
// taxa variável. Um mapa nosso a partir da família pedida daria 12M onde o banco
// diz 12M por acaso, e não haveria como ver uma renumeração do lado dele.
//
// Os códigos SWAP são taxas de referência da fase fixa, e não Euribor nenhuma:
// para essas devolve-se vazio de propósito.
func indexanteDoCodigo(codigo string) dominio.Indexante {
	switch strings.ToUpper(strings.TrimSpace(codigo)) {
	case "EH3":
		return dominio.Euribor3M
	case "EH6":
		return dominio.Euribor6M
	case "EH2":
		return dominio.Euribor12M
	default:
		return ""
	}
}

// ehIndexada diz se a fase é a indexada à Euribor.
func (f faseBruta) ehIndexada() bool { return indexanteDoCodigo(f.Rate.BaseRateCode) != "" }

// --- o arranque -----------------------------------------------------------------

// hashRequest apanha o campo escondido que o gateway exige na query string.
var hashRequest = regexp.MustCompile(`id="HashRequest"[^>]*value="([^"]*)"`)

// LerHashRequest tira o HashRequest do HTML do arranque.
//
// ⚠️ Esta leitura vive aqui, no pacote do banco, e não no transporte: o
// HashRequest é conhecimento do Montepio, não do HTTP. É a correcção registada
// na §4 do CONTRATO-BANCO.md, e é o que a torna uma função pura, testável contra
// a captura `arranque.html` sem tocar na rede.
func LerHashRequest(htmlDoArranque []byte) (string, error) {
	achado := hashRequest.FindSubmatch(htmlDoArranque)
	if achado == nil {
		return "", fmt.Errorf("não há campo HashRequest no HTML do simulador")
	}
	valor := strings.TrimSpace(string(achado[1]))
	if valor == "" {
		return "", fmt.Errorf("o campo HashRequest do simulador está vazio")
	}
	return valor, nil
}

// configuracoes apanha o JSON de configuração que o simulador embebe na página.
var configuracoes = regexp.MustCompile(`id="model\.Configs"[^>]*value="([^"]*)"`)

// EscalaoDePrazo é o prazo máximo, em anos, para uma faixa de idades.
type EscalaoDePrazo struct {
	IdadeMin, IdadeMax int
	PrazoMaxAnos       int
}

// escaloesPorOmissao são os que valem quando o HTML do arranque não os traz.
//
// Medidos ao vivo a 2026-07-27 nas fronteiras (36 anos aceita 35 e recusa 36) e
// iguais ao que o v1 tinha apurado por busca binária. ⚠️ Só servem de rede de
// segurança: o caminho normal é lê-los do arranque, porque uma tabela nossa
// envelhece em silêncio e a do banco não.
var escaloesPorOmissao = []EscalaoDePrazo{
	{IdadeMin: 0, IdadeMax: 30, PrazoMaxAnos: 40},
	{IdadeMin: 31, IdadeMax: 35, PrazoMaxAnos: 37},
	{IdadeMin: 36, IdadeMax: 99, PrazoMaxAnos: 35},
}

// LerEscaloesDePrazo lê a tabela de prazo máximo por idade do HTML do arranque.
//
// O simulador publica-a no `model.Configs`, na chave `maxmortgageterm`, com a
// forma `00-30:40;31-35:37;36-99:35;` — faixa de idades e prazo máximo em anos.
//
// ⚠️ É o melhor que este banco dá, e é o padrão que o Novo Banco tornou
// preferível: perguntar ao banco em vez de guardar uma tabela nossa que
// envelhece sem avisar. Quando a chave falta ou não se lê, devolve-se a medida —
// e o segundo valor diz qual dos dois caminhos foi.
func LerEscaloesDePrazo(htmlDoArranque []byte) ([]EscalaoDePrazo, bool) {
	achado := configuracoes.FindSubmatch(htmlDoArranque)
	if achado == nil {
		return escaloesPorOmissao, false
	}

	var cfg struct {
		MaxMortgageTerm string `json:"maxmortgageterm"`
	}
	if err := json.Unmarshal([]byte(html.UnescapeString(string(achado[1]))), &cfg); err != nil {
		return escaloesPorOmissao, false
	}

	escaloes := make([]EscalaoDePrazo, 0, 3)
	for _, troco := range strings.Split(cfg.MaxMortgageTerm, ";") {
		faixa, prazo, temPrazo := strings.Cut(strings.TrimSpace(troco), ":")
		if !temPrazo {
			continue
		}
		minimo, maximo, temTraco := strings.Cut(faixa, "-")
		if !temTraco {
			continue
		}
		de, err1 := strconv.Atoi(strings.TrimSpace(minimo))
		ate, err2 := strconv.Atoi(strings.TrimSpace(maximo))
		anos, err3 := strconv.Atoi(strings.TrimSpace(prazo))
		if err1 != nil || err2 != nil || err3 != nil || anos < 1 || ate < de {
			continue
		}
		escaloes = append(escaloes, EscalaoDePrazo{IdadeMin: de, IdadeMax: ate, PrazoMaxAnos: anos})
	}
	if len(escaloes) == 0 {
		return escaloesPorOmissao, false
	}
	return escaloes, true
}

// PrazoMaximoDoEscalao devolve o prazo máximo, em anos, para uma idade.
//
// Uma idade acima de todas as faixas fica com a do escalão mais alto — que é o
// mais apertado, e é o lado seguro por onde errar.
func PrazoMaximoDoEscalao(escaloes []EscalaoDePrazo, idade int) int {
	maximo := 0
	for _, e := range escaloes {
		if idade >= e.IdadeMin && idade <= e.IdadeMax {
			return e.PrazoMaxAnos
		}
		if e.PrazoMaxAnos > maximo {
			maximo = e.PrazoMaxAnos
		}
	}
	if len(escaloes) == 0 {
		return 0
	}
	// Fora de todas as faixas: fica o mais apertado dos declarados.
	apertado := escaloes[0].PrazoMaxAnos
	for _, e := range escaloes[1:] {
		if e.PrazoMaxAnos < apertado {
			apertado = e.PrazoMaxAnos
		}
	}
	return apertado
}

// --- a recusa ---------------------------------------------------------------

// lerErro traduz uma recusa do banco.
//
// Devolve nil quando a resposta não é uma recusa.
//
// ⚠️ **A mensagem do Montepio não classifica a causa, e chega a nomear a
// errada.** Medido a 2026-07-27, com um titular de 36 anos e prazo de 20:
// pedir 30 anos de período fixo devolve "Não existem condições disponíveis para
// a idade dos proponentes." — a idade não tinha nada que ver com isso. A mesma
// frase sai de um prazo abaixo do mínimo (3 e 4 anos) e de um prazo acima do
// máximo por idade. E um período fixo que não existe (M3, M12, M20) devolve
// `Message` **vazia** e `Code` nulo.
//
// Consequência de desenho, e é a razão de o pedido.go impor tudo antes de ir à
// rede: o que o banco responde não serve para decidir o que correr mal, e por
// isso não se lhe pergunta — pergunta-se antes.
func lerErro(corpo []byte) *dominio.ErroOferta {
	var r resposta
	if err := json.Unmarshal(corpo, &r); err != nil {
		return &dominio.ErroOferta{
			Codigo:   dominio.ErroRespostaIlegivel,
			Mensagem: fmt.Sprintf("O Banco Montepio respondeu qualquer coisa que não é JSON: %v", err),
		}
	}
	if strings.EqualFold(r.Status, "Ok") && r.Result != nil {
		return nil
	}

	mensagem := ""
	if r.Error != nil {
		mensagem = strings.TrimSpace(r.Error.Message)
	}
	if mensagem == "" {
		return &dominio.ErroOferta{
			Codigo: dominio.ErroProdutoIndisponivel,
			Mensagem: "O Banco Montepio recusou a simulação sem dizer porquê — é o que responde a um " +
				"período de taxa fixa que não pratica.",
		}
	}
	return &dominio.ErroOferta{
		Codigo: dominio.ErroProdutoIndisponivel,
		Mensagem: fmt.Sprintf(
			"O Banco Montepio recusou a simulação: %s ⚠️ A razão é a que o banco deu, e ele dá esta mesma para "+
				"causas que nada têm que ver com a idade.", mensagem),
	}
}

// --- a oferta ---------------------------------------------------------------

// lerResposta traduz o corpo do banco numa Oferta.
//
// É pura: dados para dados, sem rede e sem relógio. Recebe o pedido tal como foi
// enviado porque o que se pediu decide o que se pode afirmar do que veio.
func lerResposta(corpo []byte, enviado payload) (dominio.Oferta, error) {
	var r resposta
	if err := json.Unmarshal(corpo, &r); err != nil {
		return dominio.Oferta{}, ilegivel("o corpo", err)
	}
	if r.Result == nil {
		return dominio.Oferta{}, &dominio.ErroOferta{
			Codigo:   dominio.ErroRespostaIlegivel,
			Mensagem: "O Banco Montepio respondeu sem resultado nenhum.",
		}
	}
	res := r.Result
	if len(res.PeriodInstallment) == 0 {
		return dominio.Oferta{}, &dominio.ErroOferta{
			Codigo:   dominio.ErroRespostaIlegivel,
			Mensagem: "O Banco Montepio respondeu sem plano de prestações.",
		}
	}

	// ⚠️ O prazo vem da resposta e não do que se pediu (CONTRATO-BANCO.md §5):
	// uma mudança do lado do banco fica visível em vez de produzir números certos
	// para um contrato que ninguém pediu.
	if res.Term < 1 {
		return dominio.Oferta{}, ilegivel("o prazo", fmt.Errorf("o banco devolveu %d meses", res.Term))
	}

	var o dominio.Oferta
	var err error

	primeira := res.PeriodInstallment[0]
	if o.TAN, err = taxa(primeira.TAN); err != nil {
		return o, ilegivel("a TAN", err)
	}
	if o.Prestacao, err = dinheiro(primeira.Installment); err != nil {
		return o, ilegivel("a prestação", err)
	}
	if o.TAEG, err = taxa(res.TAEG); err != nil {
		return o, ilegivel("a TAEG", err)
	}
	if o.Spread, err = taxa(res.Spread); err != nil {
		return o, ilegivel("o spread", err)
	}
	if o.MTIC, err = dinheiro(res.MTIC); err != nil {
		return o, ilegivel("o MTIC", err)
	}

	if indexada, achou := faseIndexada(res.PeriodInstallment); achou {
		o.Indexante = indexanteDoCodigo(indexada.Rate.BaseRateCode)
		if o.EuriborValor, err = taxa(indexada.Rate.BaseRateValue); err != nil {
			return o, ilegivel("o valor da Euribor", err)
		}
	}

	if o.Fases, err = lerFases(res); err != nil {
		return o, err
	}
	if len(o.Fases) > 0 {
		if err := dominio.ValidarFases(o.Fases, res.Term/12); err != nil {
			return o, ilegivel("o plano de fases", err)
		}
	}

	o.ProdutosAplicados = enviado.produtosAplicados()
	anotarCaudaIndexada(&o, res)
	anotarImpostoDoSelo(&o, res)
	anotarContrapartidas(&o, res, enviado.Counterparts)
	return o, nil
}

// faseIndexada devolve a fase ligada à Euribor, se existir. Na variável é a
// única; na mista é a segunda; numa fixa ao prazo todo não há nenhuma.
func faseIndexada(fases []faseBruta) (faseBruta, bool) {
	for _, f := range fases {
		if f.ehIndexada() {
			return f, true
		}
	}
	return faseBruta{}, false
}

// lerFases monta o plano — e só o monta quando ele é verdade.
//
// ⚠️ **Com duas fases não sai plano nenhum, e isso é uma afirmação, não uma
// omissão.** Medido a 2026-07-27 na captura `mista_5a`: a fase indexada vem com
// `Duration: null`, e com a TAN e a prestação **repetidas da fase fixa** — 4,350
// e 995,62 — apesar de a taxa que ela própria declara ser Euribor 3M 2,339 mais
// 1,500 de spread, ou seja 3,839. Os dois números não podem estar os dois certos.
// A aritmética diz qual é qual: 995,62 × 300 − 181 898,17 = 116 788, que é o
// `TotalInterest` dessa fase (116 790,53); à taxa de 3,839 seriam 101 320. Logo o
// banco **projecta a fase indexada à taxa do período fixo**, e é dessa projecção
// que saem o MTIC e a TAEG dele.
//
// Publicar essa fase com a taxa que ela declara e a prestação que veio seria
// juntar dois números que se contradizem; publicá-la com a TAN da fase fixa seria
// afirmar uma taxa que o contrato não tem depois do período fixo. Fica sem plano,
// e a nota do anotarCaudaIndexada diz em português o que o banco declarou para
// essa fase. É a mesma decisão que o Novo Banco tomou na mista, pela mesma razão.
func lerFases(res *resultado) ([]dominio.Fase, error) {
	if len(res.PeriodInstallment) != 1 {
		return nil, nil
	}
	unica := res.PeriodInstallment[0]

	tan, err := taxa(unica.TAN)
	if err != nil || tan == nil {
		return nil, ilegivel("a TAN da fase", err)
	}
	prestacao, err := dinheiro(unica.Installment)
	if err != nil || prestacao == nil {
		return nil, ilegivel("a prestação da fase", err)
	}

	fases, err := dominio.FasesDeDuracoes([]dominio.FaseDuracao{
		{Meses: res.Term, Taxa: *tan, Prestacao: *prestacao},
	})
	if err != nil {
		return nil, ilegivel("o plano de fases", err)
	}
	return fases, nil
}

// anotarCaudaIndexada diz, em português, o que o banco declarou para depois do
// período de taxa fixa — e que a prestação publicada é a do período fixo.
func anotarCaudaIndexada(o *dominio.Oferta, res *resultado) {
	if len(res.PeriodInstallment) < 2 {
		return
	}
	fixa := res.PeriodInstallment[0]
	indexada, achou := faseIndexada(res.PeriodInstallment)
	if !achou {
		return
	}

	anos := 0
	if fixa.Duration != nil {
		anos = *fixa.Duration / 12
	}
	o.Anotar(fmt.Sprintf(
		"A TAN e a prestação são as dos primeiros %d anos, de taxa fixa. Depois deles o Banco Montepio declara "+
			"%s (%s %%) mais %s de spread.",
		anos, indexada.Rate.BaseRateDescription, indexada.Rate.BaseRateValue, indexada.Rate.Spread))
	o.Anotar(
		"⚠️ O MTIC e a TAEG do Banco Montepio para esta simulação são calculados projectando a taxa do período " +
			"fixo até ao fim do contrato, e não o indexante do período seguinte — medido a 2026-07-27. " +
			"São mais altos do que a Euribor de hoje daria.")
}

// anotarImpostoDoSelo avisa quando a prestação traz o imposto lá dentro.
//
// ⚠️ Medido a 2026-07-27: no arrendamento o banco devolve `HasIS: true` e
// `IsISInstallmentOut: false`, e a prestação sobe de 936,36 para 953,97 **com a
// mesma TAN de 3,839 %** — são exactamente 4 % dos juros, o Imposto do Selo sobre
// a utilização do crédito. Sem esta nota, o Montepio aparecia ao lado dos outros
// bancos com uma prestação 1,9 % mais cara e uma TAN igual, e ninguém saberia
// porquê: a comparação era entre coisas diferentes.
func anotarImpostoDoSelo(o *dominio.Oferta, res *resultado) {
	if !res.HasIS || res.IsISInstallmentOut {
		return
	}
	o.Anotar(
		"⚠️ Nesta finalidade a prestação do Banco Montepio já inclui o Imposto do Selo sobre os juros (4 %), " +
			"que os outros bancos cobram por fora. Medido a 2026-07-27: é o que faz a mesma TAN dar uma " +
			"prestação mais alta.")
}

// anotarContrapartidas diz o que a escolha valeu, ou que não está neste preço.
//
// ⚠️ A nota de quem não escolheu não leva números vindos desta resposta, porque
// ela não os tem: com `Counterparts: 0` o banco devolve o spread base e mais
// nada. Os valores medidos vivem nos Requisitos, com data.
func anotarContrapartidas(o *dominio.Oferta, res *resultado, escolhidas int) {
	if escolhidas <= 0 {
		o.Anotar("Este preço não inclui as contrapartidas do Banco Montepio. " +
			"Escolhê-las desconta spread, e exigem deter pelo menos dois dos produtos elegíveis.")
		return
	}
	com, err := taxa(res.Spread)
	if err != nil || com == nil {
		return
	}
	sem, err := taxa(res.SpreadBase)
	if err != nil || sem == nil || com.Equal(*sem) {
		return
	}
	o.Anotar(fmt.Sprintf(
		"O preço inclui %d contrapartidas do Banco Montepio, que descontam %s p.p. de spread (%s em vez de %s). "+
			"Exigem deter esses produtos no banco.",
		escolhidas, sem.Sub(*com), com, sem))
}

// --- números ------------------------------------------------------------------

// taxa e dinheiro lêem um número de JSON sem passar por float64. Um campo
// ausente vem como json.Number vazia, e isso é "o banco não o deu" — devolve-se
// nil, que é o que a Oferta usa para omitir em vez de inventar.
func taxa(n json.Number) (*dominio.Taxa, error) {
	if n.String() == "" {
		return nil, nil
	}
	t, err := dominio.TaxaDeTexto(n.String())
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func dinheiro(n json.Number) (*dominio.Dinheiro, error) {
	if n.String() == "" {
		return nil, nil
	}
	d, err := dominio.DinheiroDeTexto(n.String())
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func ilegivel(oQue string, err error) error {
	return &dominio.ErroOferta{
		Codigo:   dominio.ErroRespostaIlegivel,
		Mensagem: fmt.Sprintf("Não se conseguiu ler %s da resposta do Banco Montepio: %v", oQue, err),
	}
}
