// Package montepio é o simulador de crédito à habitação do Banco Montepio.
//
// É o banco que prova a estratégia `HTTPComSessao`: um GET à página que fixa os
// cookies e traz o `HashRequest`, e só depois o POST ao gateway. Sem o GET
// primeiro o gateway responde **410 «New open window with different context»** —
// está capturado em `capturas/erro_sem_sessao.resposta.json`.
//
// E é o banco que prova o mecanismo «ajusta e anota», porque **não tem taxa fixa
// pura**: aproxima-a com o produto de taxa mista e o período fixo igual ao prazo.
//
// A plataforma é a ITSCredit, partilhada por vários bancos — se algum dia entrar
// outro banco dela, o que está aqui é reaproveitável.
//
// O que este pacote sabe foi medido a 2026-07-27 contra o simulador a sério, e
// está gravado em `capturas/`. Onde um comentário diz "medido", há uma captura ou
// uma sondagem por trás.
package montepio

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
	BancoID   = "montepio"
	BancoNome = "Banco Montepio"

	// Base é a raiz do simulador. Serve de Origin nos cabeçalhos.
	Base = "https://simuladores.bancomontepio.pt"

	// App é a aplicação da ITSCredit dentro do site.
	App = Base + "/ITSCredit.External/Calculator/ITSCredit.Calculator.UI.External"

	// URLArranque é a página que fixa os cookies e traz o HashRequest.
	URLArranque = App + "/calculator/HOUSINGJOURNEY"

	// urlCalculo é o gateway que calcula. Leva o hash na query string.
	//
	// ⚠️ Nenhum endpoint de lead é tocado por este pacote: os `CustomerJourneyLeadCreate`,
	// `Save*`, `Simulation/Save` e `Contact*` da mesma aplicação registam um
	// contacto no CRM do banco, e um simulador público é para simular.
	urlCalculo = App + "/gateway/Calculator/api/Calculator/Calculate?hash="

	ProdutoContrapartidas = "montepio:contrapartidas"

	// IdadeMaximaFimContrato é a idade a que o contrato tem de terminar. Medida a
	// 2026-07-27: com 70 anos, 6 anos de prazo passam (fim aos 76) e 7 não.
	IdadeMaximaFimContrato = 76

	// PrazoMaximo é o tecto absoluto do banco, abaixo do qual o escalão de idade
	// ainda pode apertar. PrazoMinimo foi medido: 4 anos são recusados, 5 passam.
	PrazoMaximo = 40
	PrazoMinimo = 5

	// limiteDeCorpo: a resposta anda pelos 6,5 KB e a página do arranque pelos
	// 7,7 KB. 4 MB é folgado e impede que uma resposta desgovernada coma a
	// memória do varrimento.
	limiteDeCorpo = 4 << 20
)

// Banco é o Banco Montepio. O transporte é injectado e nunca instanciado aqui.
//
// ⚠️ Exige um HTTPComSessao, e não um HTTPSimples: o POST tem de sair do mesmo
// cliente que fez o arranque, senão vai sem os cookies que ele fixou. É a razão
// por que HTTPComSessao estende HTTPSimples (CONTRATO-BANCO.md §4).
type Banco struct {
	http transporte.HTTPComSessao
}

// Novo constrói o Banco Montepio sobre o transporte que a infra montou.
//
// ⚠️ Recebe o transporte e devolve *Banco, e não bancos.Banco, para este pacote
// não importar o `bancos` — se o importasse, o registo não podia importar este, e
// o ciclo fechava-se.
func Novo(http transporte.HTTPComSessao) *Banco {
	return &Banco{http: http}
}

func (b *Banco) ID() string   { return BancoID }
func (b *Banco) Nome() string { return BancoNome }

// Requisitos declara o que o Banco Montepio usa, ignora e impõe.
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
		BancoID:   BancoID,
		BancoNome: BancoNome,
		Custo:     dominio.CustoBarato,

		Inputs: []dominio.Input{
			usaComNota(dominio.CampoValorImovel,
				"Entra no pedido e nos encargos iniciais (IMT, imposto do selo, avaliação), mas não no preço do "+
					"crédito: medido a 2026-07-27, o spread é o mesmo de 50 % a 100 % de LTV."),
			usa(dominio.CampoMontante),
			usa(dominio.CampoPrazoAnos),
			usa(dominio.CampoTipoTaxa),
			usa(dominio.CampoPeriodoFixo),
			usa(dominio.CampoIndexante),
			usaComNota(dominio.CampoFinalidade,
				"A primeira e a segunda habitação dão o mesmo preço ao cêntimo — medido. O arrendamento tem a "+
					"mesma TAN e o mesmo spread, e uma prestação mais alta por incluir o Imposto do Selo."),
			usaComNota(dominio.CampoDataNascimento,
				"Manda no prazo máximo, por duas vias: um escalão por idade que o próprio simulador publica "+
					"(até aos 30 anos, 40; dos 31 aos 35, 37; daí para cima, 35) e o contrato terminar até aos 76. "+
					"⚠️ E entra no MTIC pelo prémio do seguro de vida — medido a 2026-07-27: 15,25 € por mês aos "+
					"30 anos e 20,32 € aos 36, no mesmo crédito. A TAN, o spread e a prestação não mudam com ela."),
			usaComNota(dominio.CampoSegundoTitular,
				"Manda a idade do titular mais velho, que é quem aperta o prazo máximo. O preço não muda com o "+
					"número de titulares."),
			naoUsa(dominio.CampoRendimentoMensal,
				"O simulador do Montepio não o pede, e o payload não o leva."),
			naoUsa(dominio.CampoProfissao,
				"O simulador do Montepio não pede profissão, vínculo, habilitações nem situação profissional."),
			naoUsa(dominio.CampoLocalizacao,
				"O simulador pede o distrito e nós enviamos um valor neutro: não mexe no preço."),
			naoUsa(dominio.CampoTipologia, "O simulador do Montepio não pede a tipologia do imóvel."),
			naoUsa(dominio.CampoGarantiaPublica,
				"O simulador público do Montepio não tem a Garantia Pública Jovens."),
			naoUsa(dominio.CampoJaCliente,
				"Ser cliente não é pergunta do simulador. O que desconta é deter produtos — e isso são as "+
					"contrapartidas, que se escolhem no pedido."),
		},

		PeriodosFixos:     periodosValidos,
		PeriodosFixosModo: dominio.ModoLista,

		// Os três tenores dão preços diferentes — medido a 2026-07-27, no mesmo
		// cenário: 3M 3,839 %, 6M 4,096 %, 12M 4,298 % de TAN. Escolhem-se pela
		// família do produto (H9/H5/H0), e por isso não há indexante imposto.
		EuriborOpcoes:  []dominio.Indexante{dominio.Euribor3M, dominio.Euribor6M, dominio.Euribor12M},
		EuriborImposto: "",

		PrazoMin: PrazoMinimo,
		PrazoMax: PrazoMaximo,

		IdadeMaximaFim: IdadeMaximaFimContrato,

		Produtos: []dominio.Produto{
			{
				ID:     ProdutoContrapartidas,
				Rotulo: "Quatro contrapartidas Montepio (spread −0,80 p.p.)",
				Descricao: "Deter quatro dos produtos elegíveis (ordenado domiciliado, cartão de crédito, " +
					"associado da Mutualista, seguros, depósito a prazo, …). Medido a 2026-07-27: o spread " +
					"desce de 1,500 para 0,700 com quatro; com três dá 0,900 e com dois 1,100. " +
					"⚠️ O tooltip do banco anuncia −0,1 p.p. por produto, até −0,4 — o medido é o dobro.",
				PorOmissao: false,
			},
		},

		Notas: []string{
			"TAN, TAEG, spread, prestação e MTIC vêm do próprio simulador, sem lead e sem dados pessoais.",
			"Euribor à escolha: 3, 6 ou 12 meses. Por omissão, 3 meses — que é o que o simulador traz.",
			"⚠️ Não há taxa fixa pura. A taxa fixa faz-se com o produto de taxa mista e o período fixo igual " +
				"ao prazo, e fica anotado. Quando o prazo não é um dos períodos praticados, sobra uma cauda " +
				"indexada e o que sai é taxa mista.",
			"Períodos de taxa fixa de 2, 5, 7, 10, 15, 25 e 30 anos. Um período fora desta lista é recusado " +
				"pelo banco sem mensagem nenhuma.",
			"Prazo de 5 a 40 anos, apertado pelo escalão de idade e por o contrato terminar até aos 76.",
			"Preçário base, sem filtro de campanha: as condições de campanha não são o comparável.",
			"⚠️ O valor do imóvel não muda o preço do crédito — medido de 50 % a 100 % de LTV, o spread é o " +
				"mesmo. Muda os encargos iniciais, que são impostos e comissões.",
			"⚠️ O MTIC inclui os seguros de vida e multirriscos, e o prémio do de vida depende da idade do " +
				"titular. Duas pessoas com o mesmo crédito e idades diferentes têm a mesma TAN e MTIC " +
				"diferentes — medido a 2026-07-27.",
		},
	}
}

// Simular interroga o Banco Montepio.
//
// São dois pedidos: o arranque, que fixa os cookies e traz o HashRequest e a
// tabela de prazo por idade, e o cálculo.
func (b *Banco) Simular(ctx context.Context, p dominio.Pedido) (dominio.Oferta, error) {
	sessao, err := b.http.Arrancar(ctx, URLArranque)
	if err != nil {
		return dominio.Oferta{}, indisponivel(fmt.Errorf("o arranque falhou: %w", err))
	}

	hash, err := LerHashRequest(sessao.HTML)
	if err != nil {
		// ⚠️ Isto é falha nossa ou mudança do lado do banco, não indisponibilidade:
		// a página respondeu, e o que não se conseguiu foi lê-la.
		return dominio.Oferta{}, &dominio.ErroOferta{
			Codigo:   dominio.ErroRespostaIlegivel,
			Mensagem: fmt.Sprintf("Não se conseguiu ler a página do simulador do Banco Montepio: %v", err),
		}
	}
	escaloes, _ := LerEscaloesDePrazo(sessao.HTML)

	corpo, ajustes, notas, err := construirPayload(p, escaloes, b.idadeMaisVelho(p))
	if err != nil {
		return dominio.Oferta{}, err
	}

	bruto, err := b.publicar(ctx, hash, corpo)
	if err != nil {
		return dominio.Oferta{}, err
	}
	if erroOferta := lerErro(bruto); erroOferta != nil {
		return dominio.Oferta{}, erroOferta
	}

	oferta, err := lerResposta(bruto, corpo)
	if err != nil {
		return dominio.Oferta{}, err
	}
	for _, a := range ajustes {
		oferta.Acrescentar(a)
	}
	for _, n := range notas {
		oferta.Anotar(n)
	}

	oferta.BancoID, oferta.BancoNome = BancoID, BancoNome
	return oferta, nil
}

// idadeMaisVelho devolve a idade que manda no prazo. Sem titulares devolve zero,
// que é o que desliga o limite por idade — e é o caso do varrimento, que corre
// com titular neutro.
//
// ⚠️ É o único sítio deste pacote onde entra um relógio, e entra aqui e não no
// pedido.go porque esse é puro.
func (*Banco) idadeMaisVelho(p dominio.Pedido) int {
	idade, err := dominio.IdadeMaisVelho(p.Titulares, dominio.DataDeInstante(time.Now()))
	if err != nil {
		return 0
	}
	return idade
}

// --- transporte --------------------------------------------------------------

func (b *Banco) publicar(ctx context.Context, hash string, corpo payload) ([]byte, error) {
	dados, err := json.Marshal(corpo)
	if err != nil {
		return nil, indisponivel(fmt.Errorf("montar o pedido: %w", err))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlCalculo+hash, bytes.NewReader(dados))
	if err != nil {
		return nil, indisponivel(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "pt-PT,pt;q=0.9")
	req.Header.Set("Origin", Base)
	req.Header.Set("Referer", URLArranque)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("User-Agent", agente)

	resp, err := b.http.Fazer(ctx, req)
	if err != nil {
		return nil, indisponivel(err)
	}
	defer func() { _ = resp.Body.Close() }()

	// ⚠️ O 410 é o que o gateway responde a quem não passou pelo arranque, e não
	// é uma avaria: nomeia-se, porque a mensagem dele ("New open window with
	// different context") não diz nada a quem a leia sem este contexto.
	if resp.StatusCode == http.StatusGone {
		return nil, indisponivel(fmt.Errorf(
			"o gateway respondeu 410 — o pedido foi sem os cookies que o arranque fixa"))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, indisponivel(fmt.Errorf("o simulador devolveu %s", resp.Status))
	}

	lido, err := io.ReadAll(io.LimitReader(resp.Body, limiteDeCorpo))
	if err != nil {
		return nil, indisponivel(fmt.Errorf("ler o corpo: %w", err))
	}
	return lido, nil
}

// agente é o que o simulador espera ver. Não é disfarce: é um cliente de browser
// a falar com um endpoint de browser.
const agente = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/126.0 Safari/537.36"

func indisponivel(err error) error {
	return &dominio.ErroOferta{
		Codigo:   dominio.ErroBancoIndisponivel,
		Mensagem: fmt.Sprintf("O Banco Montepio não respondeu: %v", err),
	}
}
