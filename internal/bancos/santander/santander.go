// Package santander é o simulador de crédito à habitação do Santander.
//
// É o primeiro banco escrito que **não sabe onde fica a sua própria API**: o
// `client_id` e o `bff_url` vêm de um `config.json` do SPA, lido em runtime. Os
// quatro anteriores tinham o endereço no código.
//
// Quatro pedidos por simulação, encadeados: a configuração, os limites, o
// catálogo de taxas, e a simulação. Nenhum deles regista um contacto no CRM do
// banco.
//
// O que este pacote sabe foi medido a 2026-07-28 contra o simulador a sério, e
// está gravado em `capturas/`.
package santander

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/transporte"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

const (
	// URLConfig é o config.json do SPA, de onde sai tudo o resto.
	URLConfig = "https://simulador-credito-habitacao.santander.pt/pt-PT/gln-key-simuladorchspa/config.json"

	// CabecalhoClientID é a única credencial que estes endpoints exigem.
	CabecalhoClientID = "x-ibm-client-id"

	origem = "https://simulador-credito-habitacao.santander.pt"
	agente = "Mozilla/5.0 (Windows NT 10.0; Win64; x64)"

	// limiteDeCorpo: as respostas andam pelo KB. 4 MB é folgado.
	limiteDeCorpo = 4 << 20

	// PrazoMinAnos e PrazoMaxAnos são os limites do simulador.
	PrazoMinAnos = 1
	PrazoMaxAnos = 40

	// IdadeMaximaFim é o tecto que o /credit_limit publica (maxAge 80). ⚠️ É a
	// idade máxima do TITULAR, e o banco encurta o prazo sozinho — o
	// `loanDurationYears` da resposta diz o que ele aplicou.
	IdadeMaximaFim = 80
)

// Banco é o Santander. O transporte é injectado e nunca instanciado aqui.
type Banco struct {
	http transporte.HTTPSimples

	agora func() time.Time
}

// Novo constrói o Santander sobre o transporte que a infra montou.
func Novo(http transporte.HTTPSimples) *Banco {
	return &Banco{http: http}
}

func (b *Banco) ID() string   { return IDBanco }
func (b *Banco) Nome() string { return NomeBanco }

// Requisitos declara o que o Santander usa, ignora e impõe.
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
		Custo:     dominio.CustoBarato,

		Inputs: []dominio.Input{
			usa(dominio.CampoValorImovel),
			usa(dominio.CampoMontante),
			usa(dominio.CampoPrazoAnos),
			usa(dominio.CampoTipoTaxa),
			usa(dominio.CampoPeriodoFixo),
			usaComNota(dominio.CampoDataNascimento,
				"Vai no pedido. O simulador publica os limites de idade (18 a 80) no próprio /credit_limit, "+
					"e encurta o prazo sozinho quando ele não cabe — o prazo aplicado vem na resposta."),
			usaComNota(dominio.CampoSegundoTitular,
				"Vai no pedido: o /get_by_rates aceita várias datas de nascimento."),
			usaComNota(dominio.CampoLocalizacao,
				"Vai no pedido (Continente, Açores ou Madeira)."),
			usaComNota(dominio.CampoGarantiaPublica,
				"É o `youthPlan` do simulador. O /credit_limit publica a elegibilidade: 18 a 35 anos e "+
					"imóvel até 450 000 €."),
			naoUsa(dominio.CampoIndexante,
				"⚠️ Não é escolhível: a taxa variável do Santander é sempre Euribor a 6 meses (código 6EM), "+
					"e é o catálogo do próprio banco que o impõe."),
			naoUsa(dominio.CampoFinalidade,
				"⚠️ O simulador só aceita aquisição de habitação (`HOUSE_PURCHASE`) — não distingue "+
					"arrendamento nem segunda habitação, e por isso a finalidade não viaja."),
			naoUsa(dominio.CampoRendimentoMensal,
				"O simulador não o pede."),
			naoUsa(dominio.CampoProfissao,
				"O simulador não pergunta pela profissão nem pelo vínculo."),
			naoUsa(dominio.CampoTipologia,
				"O simulador não pergunta pela tipologia."),
			naoUsa(dominio.CampoJaCliente,
				"Ser cliente não é opção do simulador. O que desconta é o plano bonificado."),
		},

		// ⚠️ São os períodos da MISTA. A taxa fixa deste banco é ao contrato
		// todo — 10, 20 ou 30 anos — e o «período» dela é o prazo, que vive na
		// nota. O contrato publica uma lista só.
		PeriodosFixos:     []int{2, 3, 4},
		PeriodosFixosModo: dominio.ModoDaAPI,

		EuriborOpcoes:  nil,
		EuriborImposto: dominio.Euribor6M,

		PrazoMin: PrazoMinAnos,
		PrazoMax: PrazoMaxAnos,

		IdadeMaximaFim: IdadeMaximaFim,

		Produtos: []dominio.Produto{{
			ID:     ProdutoBonificado,
			Rotulo: "Plano bonificado",
			Descricao: "Domiciliação de ordenado e seguros contratados com o banco. As duas colunas de " +
				"preço vêm na mesma resposta, por isso escolher isto não custa um segundo pedido. " +
				"⚠️ Medido a 2026-07-28: na taxa fixa não desconta nada.",
			PorOmissao: true,
		}},

		Notas: []string{
			"TAN, TAEG, spread, prestação e MTIC vêm do próprio simulador, sem lead e sem dados pessoais.",
			"⚠️ O spread promocional da taxa variável é TEMPORÁRIO: medido a 2026-07-28, é 0,5 p.p. nos primeiros 36 meses e 0,8 a partir daí. O valor publicado é o segundo — é o que vigora na maior parte do contrato.",
			"⚠️ A taxa fixa é ao contrato TODO, a 10, 20 ou 30 anos: quem pede uma fixa recebe o prazo encaixado num desses.",
			"A taxa mista existe a 2, 3 e 4 anos de período fixo, e o catálogo é lido em cada simulação — não é uma lista nossa.",
			"O prazo vai de 1 a 40 anos, e o banco encurta-o sozinho quando a idade não deixa.",
		},
	}
}

// Simular interroga o Santander: configuração, limites, catálogo e simulação.
func (b *Banco) Simular(ctx context.Context, p dominio.Pedido) (dominio.Oferta, error) {
	clientID, bff, err := b.configuracao(ctx)
	if err != nil {
		return dominio.Oferta{}, err
	}

	idade, err := dominio.IdadeMaisVelho(p.Titulares, b.hoje())
	if err != nil {
		return dominio.Oferta{}, indisponivel(
			"O Santander precisa da data de nascimento de pelo menos um titular.")
	}

	lim, err := b.limites(ctx, bff, clientID)
	if err != nil {
		return dominio.Oferta{}, err
	}
	// ⚠️ Verificar ANTES de simular: um pedido que o banco vai recusar gasta uma
	// chamada para receber um erro que os limites já diziam.
	if err := verificarLimites(lim, p, idade); err != nil {
		return dominio.Oferta{}, err
	}

	prazo, ajustePrazo, err := dominio.EncaixarPrazo(p.PrazoAnos, b.Requisitos(), idade)
	if err != nil {
		return dominio.Oferta{}, err
	}

	cat, err := b.catalogo(ctx, bff, clientID)
	if err != nil {
		return dominio.Oferta{}, err
	}
	taxa, ajusteTaxa, err := escolherTaxa(cat, p, prazo)
	if err != nil {
		return dominio.Oferta{}, err
	}
	// ⚠️ Na FIXA, o que o catálogo encaixa é o PRAZO — e é ele que vai no
	// payload. Mandar o prazo pedido com uma taxa de outro prazo dava uma
	// combinação que o banco marca como `isValid: false`.
	if p.TipoTaxa == dominio.TaxaFixa && taxa.RateDurationYears != nil {
		prazo = *taxa.RateDurationYears
	}

	corpo, err := construirPayload(p, taxa, prazo)
	if err != nil {
		return dominio.Oferta{}, err
	}

	bruto, err := b.publicar(ctx, bff+"/get_by_rates", clientID, corpo)
	if err != nil {
		return dominio.Oferta{}, err
	}

	oferta, err := lerResposta(bruto, p)
	if err != nil {
		return dominio.Oferta{}, err
	}
	oferta.BancoID, oferta.BancoNome = IDBanco, NomeBanco

	// ⚠️ UM ajuste ao prazo, contra o prazo que a pessoa pediu — e o aplicado sai
	// da RESPOSTA, não do que enviámos: o banco encurta-o sozinho pela idade.
	aplicado := prazo
	if len(oferta.Fases) > 0 {
		aplicado = oferta.Fases[len(oferta.Fases)-1].AteMes / 12
	}
	oferta.Acrescentar(ajusteDoPrazo(p.PrazoAnos, aplicado, ajustePrazo, p.TipoTaxa))
	oferta.Acrescentar(ajusteTaxa)
	return oferta, nil
}

// ajusteDoPrazo junta num só ajuste os motivos por que o prazo mudou.
func ajusteDoPrazo(pedido, aplicado int, porIdade *dominio.Ajuste, tipo dominio.TipoTaxa) *dominio.Ajuste {
	if aplicado == pedido || aplicado < 1 {
		return nil
	}
	if tipo == dominio.TaxaFixa {
		// ⚠️ Na fixa o prazo é o produto: 10, 20 ou 30 anos, e mais nada.
		return dominio.AjustePrazo(pedido, aplicado,
			"a taxa fixa do Santander é ao contrato todo e só existe a 10, 20 e 30 anos")
	}
	if porIdade != nil {
		return porIdade
	}
	return dominio.AjustePrazo(pedido, aplicado, "é o prazo que o Santander aceita para este pedido")
}

func (b *Banco) hoje() dominio.Data {
	agora := b.agora
	if agora == nil {
		agora = time.Now
	}
	return dominio.DataDeInstante(agora())
}

// --- os quatro pedidos -------------------------------------------------------------

// configuracao lê o config.json do SPA.
//
// ⚠️ É daqui que sai o endereço a que se vai publicar os dados do pedido, e é por
// isso que o `bff_url` passa pelo `BFFSeguro` antes de ser usado.
func (b *Banco) configuracao(ctx context.Context) (clientID, bff string, err error) {
	bruto, err := b.obter(ctx, URLConfig, "")
	if err != nil {
		return "", "", err
	}
	return lerConfig(bruto)
}

func (b *Banco) limites(ctx context.Context, bff, clientID string) (limites, error) {
	// ⚠️ O `type` é obrigatório desde 2026-07: sem ele, o BFF responde 400 E309.
	corpo := map[string]string{
		"type":              "SIMULATION_LIMITS",
		"occupationType":    ocupacaoNeutra,
		"simulationPurpose": finalidadeUnica,
	}
	bruto, err := b.publicar(ctx, bff+"/credit_limit", clientID, corpo)
	if err != nil {
		return limites{}, err
	}
	var l limites
	if err := json.Unmarshal(bruto, &l); err != nil {
		return limites{}, ilegivel("os limites", err)
	}
	return l, nil
}

func (b *Banco) catalogo(ctx context.Context, bff, clientID string) (catalogo, error) {
	bruto, err := b.obter(ctx, bff+"/rates", clientID)
	if err != nil {
		return catalogo{}, err
	}
	var c catalogo
	if err := json.Unmarshal(bruto, &c); err != nil {
		return catalogo{}, ilegivel("o catálogo de taxas", err)
	}
	return c, nil
}

// --- transporte ---------------------------------------------------------------------

func (b *Banco) obter(ctx context.Context, url, clientID string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, indisponivelDeErro(err)
	}
	return b.fazer(ctx, req, clientID)
}

func (b *Banco) publicar(ctx context.Context, url, clientID string, corpo any) ([]byte, error) {
	dados, err := json.Marshal(corpo)
	if err != nil {
		return nil, indisponivelDeErro(fmt.Errorf("montar o pedido: %w", err))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(dados))
	if err != nil {
		return nil, indisponivelDeErro(err)
	}
	req.Header.Set("Content-Type", "application/json")
	return b.fazer(ctx, req, clientID)
}

func (b *Banco) fazer(ctx context.Context, req *http.Request, clientID string) ([]byte, error) {
	if clientID != "" {
		req.Header.Set(CabecalhoClientID, clientID)
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "pt-PT")
	req.Header.Set("Origin", origem)
	req.Header.Set("Referer", origem+"/")
	req.Header.Set("User-Agent", agente)

	resp, err := b.http.Fazer(ctx, req)
	if err != nil {
		return nil, indisponivelDeErro(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, indisponivelDeErro(fmt.Errorf("o simulador devolveu %s", resp.Status))
	}
	lido, err := io.ReadAll(io.LimitReader(resp.Body, limiteDeCorpo))
	if err != nil {
		return nil, indisponivelDeErro(fmt.Errorf("ler o corpo: %w", err))
	}
	return lido, nil
}

func indisponivelDeErro(err error) error {
	return &dominio.ErroOferta{
		Codigo:   dominio.ErroBancoIndisponivel,
		Mensagem: fmt.Sprintf("O Santander não respondeu: %v", err),
	}
}
