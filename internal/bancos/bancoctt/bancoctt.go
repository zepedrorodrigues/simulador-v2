// Package bancoctt é o simulador de crédito à habitação do Banco CTT.
//
// Um só pedido, sem autenticação, sem sessão e sem página a raspar — o mais
// simples dos que estão escritos, a par da CGD. A resposta traz as duas colunas
// de preço no mesmo corpo, com e sem vendas associadas.
//
// É o banco que prova duas coisas que nenhum dos três anteriores provava:
//
//   - **um `Spread` que não é um spread.** Na mista e na fixa, o campo de topo
//     vem igual à TAN, e publicá-lo punha o Banco CTT com 4,650 de spread ao
//     lado dos 1,350 dos outros. O contratual está em `VariableSpread*`. A mesma
//     armadilha está registada no Santander e no Bankinter;
//   - **um limite que o banco não valida e nós temos de validar.** A API aceitou
//     um titular de 66 anos com 40 de prazo — fim do contrato aos 106. O tecto
//     dos 75 anos é imposto deste lado.
//
// O que este pacote sabe foi medido a 2026-07-27 contra o simulador a sério, e
// está gravado em `capturas/`. Onde um comentário diz "medido", há uma captura
// ou uma sondagem por trás.
package bancoctt

import (
	"bytes"
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
	// URL é o endpoint que calcula.
	//
	// ⚠️ E é o único que este pacote conhece. O simulador do Banco CTT tem
	// caminhos de contacto que registam um lead no CRM do banco; nenhum deles é
	// chamado aqui, nem em teste (CONTRATO-BANCO.md §3).
	URL = "https://simuladorch.bancoctt.pt/api/simulation/simulate"

	// limiteDeCorpo: a resposta anda pelos 6 KB. 4 MB é folgado e impede que uma
	// resposta desgovernada coma a memória do varrimento.
	limiteDeCorpo = 4 << 20

	// agente é o que o simulador espera ver. Não é disfarce: é um cliente de
	// browser a falar com um endpoint de browser.
	agente = "Mozilla/5.0 (Windows NT 10.0; Win64; x64)"
)

// Banco é o Banco CTT. O transporte é injectado e nunca instanciado aqui.
type Banco struct {
	http transporte.HTTPSimples

	// agora é o relógio, e existe para os testes o poderem congelar. Nulo vale
	// time.Now. ⚠️ O relógio entra aqui e não no domínio: é a idade do titular
	// mais velho que decide o prazo máximo, e a idade depende de que dia é hoje.
	agora func() time.Time
}

// Novo constrói o Banco CTT sobre o transporte que a infra montou.
//
// ⚠️ Recebe o transporte e devolve *Banco, e não bancos.Banco, para este pacote
// não importar o `bancos` — se o importasse, o registo não podia importar este,
// e o ciclo fechava-se. Ver a nota em cgd.Novo.
func Novo(http transporte.HTTPSimples) *Banco {
	return &Banco{http: http}
}

func (b *Banco) ID() string   { return IDBanco }
func (b *Banco) Nome() string { return NomeBanco }

// Requisitos declara o que o Banco CTT usa, ignora e impõe.
//
// ⚠️ A distinção do CONTRATO-BANCO.md §5 aplica-se aqui a três campos que o
// pedido transporta e que este banco **não** usa para preçar: a finalidade, a
// localização e o rendimento. Declará-los a `Usa: false` com nota é o que impede
// a app de os pedir por nada — e o que impede alguém, daqui a seis meses, de
// concluir que o Banco CTT é caro no arrendamento por ter visto o mesmo número.
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
				fmt.Sprintf("Vai no pedido e decide o prazo máximo — o contrato tem de terminar até aos %d anos. "+
					"⚠️ Esse limite é imposto por nós: a API do Banco CTT não o valida, e aceitou 40 anos de prazo a um "+
					"titular de 66 (fim aos 106).", IdadeMaximaFim)),
			usaComNota(dominio.CampoSegundoTitular,
				"Vai no pedido. Manda a idade do titular mais velho, que é quem aperta o prazo máximo."),
			usaComNota(dominio.CampoGarantiaPublica,
				"É a medida jovem do DL 44/2024, e vai no pedido (`YouthMeasuresIsActive`)."),
			naoUsa(dominio.CampoIndexante,
				"⚠️ Não é escolhível: a variável é sempre Euribor a 12 meses e o pós-período-fixo da mista é sempre a 3 "+
					"meses — o próprio bundle do simulador força os identificadores. Os outros respondem por API, e isso "+
					"não faz deles oferta: é preço que o banco não comercializa."),
			naoUsa(dominio.CampoFinalidade,
				"O simulador tem o campo e nenhum dos valores que ele aceita muda o preço — medido a 2026-07-27. "+
					"Enviamos sempre o que a captura trouxe, e não um palpite sobre qual dos três é o arrendamento."),
			naoUsa(dominio.CampoLocalizacao,
				"O Banco CTT não preça por região. O distrito vai constante, como na captura."),
			naoUsa(dominio.CampoRendimentoMensal,
				"O simulador não o pede para calcular o preço."),
			naoUsa(dominio.CampoProfissao,
				"O simulador não pergunta pela profissão nem pelo vínculo."),
			naoUsa(dominio.CampoTipologia,
				"O simulador não pergunta pela tipologia."),
			naoUsa(dominio.CampoJaCliente,
				"Ser cliente não é opção do simulador. O que desconta são as vendas associadas."),
		},

		// ⚠️ Duas listas e não uma: os períodos da mista {1,2,3,5} não são os
		// prazos da fixa {30,34}. O contrato publica uma lista só, e a que ela
		// descreve é a da mista — a fixa é ao contrato todo, logo o «período
		// fixo» dela é o prazo, e vive na nota.
		PeriodosFixos:     PeriodosMistos,
		PeriodosFixosModo: dominio.ModoLista,

		// Vazio não é "não sei": é "o banco impõe o seu e ignora a escolha".
		EuriborOpcoes:  nil,
		EuriborImposto: dominio.Euribor12M,

		PrazoMin: PrazoMinAnos,
		PrazoMax: PrazoMaxAnos,

		IdadeMaximaFim: IdadeMaximaFim,

		Produtos: []dominio.Produto{
			{
				ID:     ProdutoVendasAssociadas,
				Rotulo: "Vendas associadas",
				Descricao: "Domiciliar o ordenado e contratar os seguros com o Banco CTT. " +
					"As duas colunas de preço vêm na mesma resposta, por isso escolher isto não custa um segundo pedido ao banco.",
				PorOmissao: true,
			},
			{
				ID:     ProdutoSustentavel,
				Rotulo: "Campanha sustentável (-0,10 p.p.)",
				Descricao: "Exige imóvel com classe energética A ou superior. " +
					"Medido a 2026-07-27: desconta 0,10 p.p. Vai no pedido, e não tem coluna própria na resposta.",
				PorOmissao: false,
			},
		},

		Notas: []string{
			"TAN, TAEG, spread, prestação e MTIC vêm do próprio simulador, sem lead e sem dados pessoais.",
			"A taxa mista existe a 1, 2, 3 e 5 anos de período fixo, e o período tem de ser menor do que o prazo.",
			"⚠️ A taxa fixa é ao contrato TODO e só existe a 30 e 34 anos. Quem pede uma fixa de 10 anos recebe 30, com o ajuste declarado — e quem não tem idade para 30 anos de prazo não tem aqui taxa fixa.",
			"⚠️ O contrato tem de terminar até aos 75 anos. O limite é nosso: a API do Banco CTT aceita prazos que o ultrapassam.",
			"O prazo vai de 5 a 40 anos.",
		},
	}
}

// Simular interroga o Banco CTT. É um pedido só.
func (b *Banco) Simular(ctx context.Context, p dominio.Pedido) (dominio.Oferta, error) {
	requisitos := b.Requisitos()

	idade, err := dominio.IdadeMaisVelho(p.Titulares, b.hoje())
	if err != nil {
		return dominio.Oferta{}, &dominio.ErroOferta{
			Codigo:   dominio.ErroProdutoIndisponivel,
			Mensagem: "O Banco CTT precisa da data de nascimento de pelo menos um titular.",
		}
	}

	// ⚠️ O prazo encaixa-se ANTES de ir à rede, e é aqui que o tecto dos 75 anos
	// se impõe. O banco não o faz — aceitou 66 anos com 40 de prazo —, por isso
	// um pedido impossível que fosse à rede voltaria com uma oferta plausível e
	// inexistente, em vez de com uma recusa.
	prazo, ajustePrazo, err := dominio.EncaixarPrazo(p.PrazoAnos, requisitos, idade)
	if err != nil {
		return dominio.Oferta{}, err
	}
	prazoMaximo := min(requisitos.PrazoMax, requisitos.IdadeMaximaFim-idade)

	corpo, ajustes, motivoDaFixa, err := construirPayload(p, prazo, prazoMaximo)
	if err != nil {
		return dominio.Oferta{}, err
	}

	bruto, err := b.publicar(ctx, corpo)
	if err != nil {
		return dominio.Oferta{}, err
	}

	oferta, err := lerResposta(bruto, p)
	if err != nil {
		return dominio.Oferta{}, err
	}

	oferta.BancoID, oferta.BancoNome = IDBanco, NomeBanco

	// ⚠️ **Um** ajuste ao prazo, nunca uma cadeia, e sempre contra o prazo que a
	// pessoa pediu. Há dois sítios que lhe mexem — o tecto da idade e o encaixe
	// da fixa nos dois prazos que existem —, e emitir um em cada um dava um
	// segundo a dizer «Pediu 30 anos» a quem tinha pedido 40. É a lição que a
	// corrida de fidelidade da CGD deixou.
	oferta.Acrescentar(ajusteDoPrazo(p.PrazoAnos, corpo.AmortizationPeriod/12, ajustePrazo, motivoDaFixa))
	for _, a := range ajustes {
		oferta.Acrescentar(a)
	}
	anotarPrazoAplicado(&oferta, p, corpo.AmortizationPeriod/12)
	return oferta, nil
}

// ajusteDoPrazo junta num só ajuste os motivos por que o prazo mudou.
//
// Devolve nil quando o prazo enviado é o que se pediu — que é o caso em que um
// ajuste seria uma mentira, mesmo tendo havido um encaixe pelo meio: se o
// encaixe da fixa devolveu o prazo ao valor pedido, não há desvio a declarar.
func ajusteDoPrazo(pedido, aplicado int, porIdade *dominio.Ajuste, motivoDaFixa string) *dominio.Ajuste {
	if aplicado == pedido || aplicado < 1 {
		return nil
	}

	var motivos []string
	if motivoDaFixa != "" {
		motivos = append(motivos, motivoDaFixa)
	}
	// ⚠️ O motivo da idade só entra se ele ainda explica alguma coisa: quando a
	// fixa esticou o prazo, o tecto da idade não é o que mudou o número, e
	// nomeá-lo mandava a pessoa olhar para o sítio errado.
	if porIdade != nil && motivoDaFixa == "" {
		return porIdade
	}
	if len(motivos) == 0 {
		motivos = append(motivos, "é o prazo que o Banco CTT aceita para este pedido")
	}
	return dominio.AjustePrazo(pedido, aplicado, strings.Join(motivos, "; e "))
}

// anotarPrazoAplicado avisa quando o prazo que o banco devolveu não é o que se
// lhe enviou.
//
// ⚠️ Não substitui o ajuste: é a rede de segurança para uma mudança do lado do
// banco. Ler da resposta o que ele aplicou, em vez de confiar no nosso mapa, é o
// que a §5 manda — e sem isto uma renumeração silenciosa produzia números certos
// para um prazo que ninguém pediu.
func anotarPrazoAplicado(o *dominio.Oferta, p dominio.Pedido, enviado int) {
	if len(o.Fases) == 0 {
		return
	}
	aplicado := o.Fases[len(o.Fases)-1].AteMes / 12
	if aplicado == p.PrazoAnos || aplicado == enviado {
		return
	}
	o.Anotar(fmt.Sprintf(
		"O Banco CTT simulou %d anos de prazo, e não os %d que lhe foram pedidos.", aplicado, p.PrazoAnos))
}

func (b *Banco) hoje() dominio.Data {
	agora := b.agora
	if agora == nil {
		agora = time.Now
	}
	return dominio.DataDeInstante(agora())
}

// --- transporte ---------------------------------------------------------------

func (b *Banco) publicar(ctx context.Context, corpo payload) ([]byte, error) {
	dados, err := json.Marshal(corpo)
	if err != nil {
		return nil, indisponivel(fmt.Errorf("montar o pedido: %w", err))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, URL, bytes.NewReader(dados))
	if err != nil {
		return nil, indisponivel(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "pt-PT")
	req.Header.Set("Origin", "https://simuladorch.bancoctt.pt")
	req.Header.Set("Referer", "https://simuladorch.bancoctt.pt/")
	req.Header.Set("User-Agent", agente)

	resp, err := b.http.Fazer(ctx, req)
	if err != nil {
		return nil, indisponivel(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, indisponivel(fmt.Errorf("o simulador devolveu %s", resp.Status))
	}

	lido, err := io.ReadAll(io.LimitReader(resp.Body, limiteDeCorpo))
	if err != nil {
		return nil, indisponivel(fmt.Errorf("ler o corpo: %w", err))
	}
	return lido, nil
}

func indisponivel(err error) error {
	return &dominio.ErroOferta{
		Codigo:   dominio.ErroBancoIndisponivel,
		Mensagem: fmt.Sprintf("O Banco CTT não respondeu: %v", err),
	}
}
