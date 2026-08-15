// Package novobanco é o simulador de crédito à habitação do Novo Banco.
//
// Uma simulação custa dois pedidos: o GET /configuracoes, que publica os
// limites com que o banco simula e que fica guardado em catálogo (KAN-37), e o
// POST /simulacao/calculo. Um só cabeçalho obrigatório (`x-nb-oc-channel`), e a
// resposta mais rica dos dez: taxas, prestação, MTIC, seguros, comissões,
// despesas e o rácio DSTI — tudo em duas variantes, com e sem bonificações.
//
// É o banco que prova três coisas que a CGD não provava: **produtos ligados por
// omissão**, **erros estruturados como sinal** (o prazo máximo vem dentro do
// próprio erro, e reaplica-se) e **finalidade que muda o preço** (o arrendamento
// custa +0,50 p.p.).
//
// O que este pacote sabe foi medido a 2026-07-26 contra o simulador a sério, e
// está gravado em `capturas/`. Onde um comentário diz "medido", há uma captura
// ou uma sondagem por trás.
package novobanco

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
	BancoID   = "novobanco"
	BancoNome = "Novo Banco"

	// URL é o endpoint que calcula. ⚠️ Não é o óbvio — o `/simulacao` não
	// recalcula — e não há aqui endpoint de lead nenhum: nenhum contacto é
	// registado no CRM do banco por este pacote.
	URL = "https://srv.novobanco.pt/web/ocb/simhb/site/simulacao/calculo?step=SIM_AVANCADO"

	// URLConfiguracoes é o endpoint que publica os limites do banco (KAN-37),
	// sem parâmetros nenhuns. ⚠️ Não confundir com o `/simulacao/configuracoes`
	// do site, que é a página do SPA e não o JSON.
	URLConfiguracoes = "https://srv.novobanco.pt/web/ocb/simhb/site/configuracoes"

	// canal é o único cabeçalho obrigatório. Sem ele o banco não responde.
	canal = "5.2"

	ProdutoPrimeiroBanco = "novobanco:primeiro_banco"
	ProdutoProtecao      = "novobanco:protecao"

	// limiteDeCorpo: a resposta anda pelos 2,5 KB. 4 MB é folgado e impede que
	// uma resposta desgovernada coma a memória do varrimento.
	limiteDeCorpo = 4 << 20

	// IdadeMaximaFimContrato é a idade a que o contrato tem de terminar.
	// Medida a 2026-07-26 por varrimento de idades contra o V159: dos 41 anos
	// para cima o máximo é exactamente 75 menos a idade.
	IdadeMaximaFimContrato = 75

	// PrazoMaximo é o tecto absoluto do banco, abaixo do qual a idade ainda
	// pode apertar.
	PrazoMaximo = 40
	PrazoMinimo = 1
)

// Catalogos guarda o que o Novo Banco publica e que é preciso saber antes de
// lhe montar o pedido — aqui, os limites do /configuracoes (KAN-37).
//
// ⚠️ **Declarada outra vez aqui, e não importada do `bancos`.** É a mesma razão
// que faz o `Novo` receber o transporte em vez do `bancos.Transportes`: se este
// pacote importasse o `bancos`, o registo — que vive lá — não podia importar
// este. As interfaces em Go são estruturais, portanto a `bancos.Catalogos`
// satisfaz esta sem que nenhum dos dois pacotes conheça o outro.
//
// ⚠️ **Nula desliga**, e é o que os testes usam: o /configuracoes é pedido a
// cada simulação, que é o que este banco fazia antes de a tabela existir.
type Catalogos interface {
	Ler(ctx context.Context, bancoID, nome string) (valor []byte, achou bool)
	Guardar(ctx context.Context, bancoID, nome string, valor []byte)
}

// catalogoConfiguracoes é o nome desta entrada dentro do banco.
const catalogoConfiguracoes = "configuracoes"

// Banco é o Novo Banco. O transporte é injectado e nunca instanciado aqui.
type Banco struct {
	http      transporte.HTTPSimples
	catalogos Catalogos
}

// Novo constrói o Novo Banco sobre o transporte que a infra montou.
//
// ⚠️ Recebe o transporte e devolve *Banco, e não bancos.Banco, para este pacote
// não importar o `bancos` — se o importasse, o registo não podia importar este,
// e o ciclo fechava-se. Ver a nota em cgd.Novo.
//
// ⚠️ `catalogos` nulo é válido: o /configuracoes é pedido a cada simulação, que
// era o comportamento até 2026-08-15.
func Novo(http transporte.HTTPSimples, catalogos Catalogos) *Banco {
	return &Banco{http: http, catalogos: catalogos}
}

func (b *Banco) ID() string   { return BancoID }
func (b *Banco) Nome() string { return BancoNome }

// Requisitos declara o que o Novo Banco usa, ignora e impõe.
//
// ⚠️ A distinção do CONTRATO-BANCO.md §5 aplica-se aqui em cheio, e é este o
// banco em que o v1 a errou: ele declarava `requires_occupation=False` e ao
// mesmo tempo enviava profissão, habilitações, situação profissional e vínculo
// laboral fixos. São duas afirmações diferentes. O simulador **pede** esses
// campos — a API recusa o pedido sem eles — e nós preenchemo-los com valor
// neutro; que não mexam no preço foi **medido** a 2026-07-26, variando cada um
// contra a mesma simulação. Por isso vão a `Usa: true` com nota, e não a
// `Usa: false`.
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
			usa(dominio.CampoValorImovel),
			usa(dominio.CampoMontante),
			usa(dominio.CampoPrazoAnos),
			usa(dominio.CampoTipoTaxa),
			usa(dominio.CampoPeriodoFixo),
			usa(dominio.CampoIndexante),
			usa(dominio.CampoFinalidade),
			usaComNota(dominio.CampoDataNascimento,
				"O Novo Banco usa-a para o prazo máximo: o contrato tem de terminar até aos 75 anos, e até aos 30 do titular o tecto é de 40 anos, até aos 35 de 37, e daí para cima de 35."),
			usaComNota(dominio.CampoSegundoTitular,
				"Manda a idade do titular mais velho, que é quem aperta o prazo máximo. O preço não muda com o número de titulares."),
			usaComNota(dominio.CampoRendimentoMensal,
				"O simulador exige-o, mas ele não mexe no preço — medido: só entra na recomendação e no rácio de esforço."),
			usaComNota(dominio.CampoProfissao,
				"O simulador exige a profissão, o vínculo, as habilitações e a situação profissional, e nós enviamos valores neutros: medido a 2026-07-26, nenhum deles muda um cêntimo do preço."),
			usaComNota(dominio.CampoLocalizacao,
				"O simulador pede-a e nós enviamos a do pedido, mas o preço não muda com ela — medido no Continente, Açores e Madeira."),
			usaComNota(dominio.CampoTipologia,
				"O simulador exige-a e nós enviamos T3: medido, a tipologia não muda o preço."),
			naoUsa(dominio.CampoGarantiaPublica,
				"O simulador público do Novo Banco não tem a Garantia Pública Jovens."),
			naoUsa(dominio.CampoJaCliente,
				"O simulador assume conta no banco — é obrigatório e não é opção. As bonificações é que dependem de domiciliar o ordenado."),
		},

		PeriodosFixos:     periodosValidos,
		PeriodosFixosModo: dominio.ModoLista,

		// ⚠️ É o único banco de HTTP puro que deixa escolher o indexante, e por
		// isso EuriborImposto fica vazio. Os três tenores dão preços
		// diferentes — medido: 3M 3,239 %, 6M 3,496 %, 12M 3,698 % de TAN no
		// mesmo cenário.
		EuriborOpcoes:  []dominio.Indexante{dominio.Euribor3M, dominio.Euribor6M, dominio.Euribor12M},
		EuriborImposto: "",

		PrazoMin: PrazoMinimo,
		PrazoMax: PrazoMaximo,

		IdadeMaximaFim: IdadeMaximaFimContrato,

		Produtos: []dominio.Produto{
			{
				ID:     ProdutoPrimeiroBanco,
				Rotulo: "Primeiro Banco (spread -0,50 p.p.)",
				Descricao: "Ter o Novo Banco como banco principal, com domiciliação de ordenado. " +
					"Medido a 2026-07-26: desconta 0,50 p.p. de spread.",
				PorOmissao: true,
			},
			{
				ID:     ProdutoProtecao,
				Rotulo: "Proteção (spread -0,20 p.p.)",
				Descricao: "Contratar os seguros de proteção associados ao crédito. " +
					"Medido a 2026-07-26: desconta 0,20 p.p. de spread.",
				PorOmissao: true,
			},
		},

		Notas: []string{
			"TAN, TAEG, spread, prestação e MTIC vêm do próprio simulador, sem lead e sem dados pessoais.",
			"O arrendamento custa mais 0,50 p.p. de spread do que a habitação própria — medido. A segunda habitação tem o preço da própria.",
			"A taxa fixa é ao prazo todo: o período de taxa fixa e o prazo são o mesmo número, e só existem 2, 3, 4, 5, 10, 15, 20, 25 e 30 anos.",
			"Na taxa mista o período fixo tem de ser menor do que o prazo do crédito.",
			"⚠️ Na taxa mista o banco não diz que taxa nem que indexante aplica depois do período fixo. Por isso a oferta traz o preço do período fixo e não um plano de fases.",
		},
	}
}

// Simular interroga o Novo Banco.
//
// A ordem é a que o KAN-37 pôs: primeiro os limites do /configuracoes, que
// recusam em casa o que o banco recusaria na rede; depois o cálculo — um
// pedido, e um segundo quando o banco recusa o prazo por causa da idade.
func (b *Banco) Simular(ctx context.Context, p dominio.Pedido) (dominio.Oferta, error) {
	// ⚠️ Falha aberto de propósito: se o /configuracoes não responder ou não se
	// deixar ler, simula-se na mesma. O /calculo é a autoridade, e uma guarda
	// que trava por não saber é pior do que não existir.
	cfg, _ := b.configuracoes(ctx)
	if erro := verificarConfiguracoes(cfg, p, b.idadeMaisVelho(p)); erro != nil {
		return dominio.Oferta{}, erro
	}

	oferta, err := b.simularComPrazo(ctx, p, p.PrazoAnos, "")
	if err != nil {
		return dominio.Oferta{}, err
	}
	oferta.BancoID, oferta.BancoNome = BancoID, BancoNome
	return oferta, nil
}

// simularComPrazo faz o pedido e, se o banco recusar por prazo, repete-o uma vez
// com o máximo que ele indicou.
//
// ⚠️ **O prazo máximo vem dentro do próprio erro**, e é este o melhor padrão dos
// dez bancos: em vez de guardarmos uma tabela de idades que envelhece em
// silêncio, pergunta-se ao banco e reaplica-se o que ele responder.
// `motivoDaIdade` não-vazio é o travão da recursão — repete-se uma vez, não em
// ciclo.
func (b *Banco) simularComPrazo(ctx context.Context, p dominio.Pedido, prazo int, motivoDaIdade string) (dominio.Oferta, error) {
	corpo, ajustes, motivoDaFixa, err := construirPayload(p, prazo)
	if err != nil {
		return dominio.Oferta{}, err
	}

	bruto, err := b.publicar(ctx, corpo)
	if err != nil {
		return dominio.Oferta{}, err
	}

	if estado, erroOferta := lerErro(bruto); erroOferta != nil {
		max, temMax := estado.parametro("0")
		podeRepetir := estado.Code == codigoPrazoPorIdade && temMax && max > 0 && max < prazo && motivoDaIdade == ""
		if !podeRepetir {
			return dominio.Oferta{}, erroOferta
		}
		// O banco disse qual é o máximo para esta idade: repete-se lá.
		return b.simularComPrazo(ctx, p, max,
			fmt.Sprintf("o contrato tem de terminar até aos %d anos", IdadeMaximaFimContrato))
	}

	oferta, err := lerResposta(bruto, corpo)
	if err != nil {
		return dominio.Oferta{}, err
	}

	// ⚠️ **Um** ajuste ao prazo, nunca uma cadeia, e sempre contra o prazo que a
	// pessoa pediu. Há dois sítios que o encolhem — a idade, pelo V159, e o
	// encaixe da fixa nos nove valores — e emitir um ajuste em cada um dava um
	// segundo a dizer «Pediu 35 anos» a quem pediu 40. O ajuste do prazo entra
	// primeiro porque é o que muda o produto todo.
	oferta.Acrescentar(ajustePrazoUnico(p.PrazoAnos, corpo.Prazo, motivoDaIdade, motivoDaFixa))
	for _, a := range ajustes {
		oferta.Acrescentar(a)
	}
	anotarPrazoAplicado(&oferta, p, prazo)
	return oferta, nil
}

// ajustePrazoUnico junta os motivos por que o prazo mudou numa só frase.
//
// Devolve nil quando o prazo enviado é o que se pediu — que é o caso em que um
// ajuste seria uma mentira, mesmo tendo havido um V159 pelo meio: se o encaixe
// da fixa devolveu o prazo ao valor pedido, não há desvio nenhum a declarar.
func ajustePrazoUnico(pedido, aplicado int, motivos ...string) *dominio.Ajuste {
	if aplicado == pedido || aplicado < 1 {
		return nil
	}
	var vivos []string
	for _, m := range motivos {
		if m != "" {
			vivos = append(vivos, m)
		}
	}
	if len(vivos) == 0 {
		vivos = append(vivos, "é o prazo que o Novo Banco aceita para este pedido")
	}
	return dominio.AjustePrazo(pedido, aplicado, strings.Join(vivos, "; e "))
}

// anotarPrazoAplicado avisa quando o prazo que o banco devolveu não é o que se
// lhe pediu e nada disso ficou registado como ajuste.
//
// ⚠️ Não substitui os ajustes: é a rede de segurança para uma renumeração do
// lado do banco. Ler da resposta o que ele aplicou, em vez de confiar no nosso
// mapa, é o que a §5 manda — e sem isto uma mudança silenciosa do lado dele
// produzia números certos para um prazo que ninguém pediu.
func anotarPrazoAplicado(o *dominio.Oferta, p dominio.Pedido, enviado int) {
	if len(o.Fases) == 0 {
		return
	}
	aplicado := o.Fases[len(o.Fases)-1].AteMes / 12
	if aplicado == p.PrazoAnos || aplicado == enviado {
		return
	}
	o.Anotar(fmt.Sprintf(
		"O Novo Banco simulou %d anos de prazo, e não os %d que lhe foram pedidos.", aplicado, p.PrazoAnos))
}

// --- catálogo e limites -------------------------------------------------------

// configuracoes devolve os limites que o banco publica.
//
// Primeiro o catálogo guardado (KAN-37); sem ele, pede-se o /configuracoes e
// guarda-se. Um catálogo ilegível ou vazio conta como não haver, e vai-se à
// rede.
//
// ⚠️ Os erros saem daqui e são para ignorar em quem chama: falha aberto.
// Guarda-se o corpo que se leu do banco, nunca uma falha.
func (b *Banco) configuracoes(ctx context.Context) (configuracoes, error) {
	if cfg, ok := b.configuracoesGuardadas(ctx); ok {
		return cfg, nil
	}

	corpo, err := b.obterConfiguracoes(ctx)
	if err != nil {
		return configuracoes{}, err
	}
	cfg, err := lerConfiguracoes(corpo)
	if err != nil {
		return configuracoes{}, err
	}
	if cfg.temLimites() {
		b.guardarConfiguracoes(ctx, corpo)
	}
	return cfg, nil
}

// configuracoesGuardadas lê o catálogo guardado, se houver um válido. Um
// catálogo ilegível ou sem limites nenhuns é o mesmo que não haver: vai-se à
// rede, que é o que evita servir uma tabela de ontem por um soluço de hoje.
func (b *Banco) configuracoesGuardadas(ctx context.Context) (configuracoes, bool) {
	if b.catalogos == nil {
		return configuracoes{}, false
	}
	bruto, achou := b.catalogos.Ler(ctx, BancoID, catalogoConfiguracoes)
	if !achou {
		return configuracoes{}, false
	}
	cfg, err := lerConfiguracoes(bruto)
	if err != nil || !cfg.temLimites() {
		return configuracoes{}, false
	}
	return cfg, true
}

// guardarConfiguracoes grava o corpo do /configuracoes tal como veio. A forma é
// do banco e a tabela guarda-a opaca — a coluna é jsonb, e quem a lê e escreve
// é só este pacote (mesma regra do periodosEmJSON da CGD).
func (b *Banco) guardarConfiguracoes(ctx context.Context, corpo []byte) {
	if b.catalogos == nil {
		return
	}
	b.catalogos.Guardar(ctx, BancoID, catalogoConfiguracoes, corpo)
}

// idadeMaisVelho devolve a idade que manda no prazo e nos limites de idade. Sem
// titulares devolve zero, que é o que desliga a verificação por idade.
//
// ⚠️ É o único sítio deste pacote onde entra um relógio, e entra aqui e não no
// pedido.go porque esse é puro — a mesma regra que a CGD segue.
func (*Banco) idadeMaisVelho(p dominio.Pedido) int {
	hoje := dominio.DataDeInstante(time.Now())
	idade, err := dominio.IdadeMaisVelho(p.Titulares, hoje)
	if err != nil {
		return 0
	}
	return idade
}

// --- transporte ---------------------------------------------------------------

// obterConfiguracoes pede ao banco a lista de limites que ele publica. É um GET
// sem corpo nem parâmetros, com os mesmos cabeçalhos do cálculo.
func (b *Banco) obterConfiguracoes(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, URLConfiguracoes, nil)
	if err != nil {
		return nil, indisponivel(err)
	}
	req.Header.Set("x-nb-oc-channel", canal)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "pt-PT")
	req.Header.Set("Origin", "https://www.novobanco.pt")
	req.Header.Set("Referer", "https://www.novobanco.pt/")
	req.Header.Set("User-Agent", agente)

	resp, err := b.http.Fazer(ctx, req)
	if err != nil {
		return nil, indisponivel(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, indisponivel(fmt.Errorf("o /configuracoes devolveu %s", resp.Status))
	}

	lido, err := io.ReadAll(io.LimitReader(resp.Body, limiteDeCorpo))
	if err != nil {
		return nil, indisponivel(fmt.Errorf("ler o corpo do /configuracoes: %w", err))
	}
	return lido, nil
}

func (b *Banco) publicar(ctx context.Context, corpo payload) ([]byte, error) {
	dados, err := json.Marshal(corpo)
	if err != nil {
		return nil, indisponivel(fmt.Errorf("montar o pedido: %w", err))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, URL, bytes.NewReader(dados))
	if err != nil {
		return nil, indisponivel(err)
	}
	// O único cabeçalho obrigatório. Sem ele não há resposta.
	req.Header.Set("x-nb-oc-channel", canal)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "pt-PT")
	req.Header.Set("Origin", "https://www.novobanco.pt")
	req.Header.Set("Referer", "https://www.novobanco.pt/")
	req.Header.Set("User-Agent", agente)

	resp, err := b.http.Fazer(ctx, req)
	if err != nil {
		return nil, indisponivel(err)
	}
	defer func() { _ = resp.Body.Close() }()

	// ⚠️ O 400 não é uma avaria: é como o banco devolve os erros estruturados
	// que trazem o limite lá dentro. Lê-se o corpo à mesma, e é o lerErro que
	// decide o que ele quer dizer.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusBadRequest {
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
const agente = "Mozilla/5.0 (Windows NT 10.0; Win64; x64)"

func indisponivel(err error) error {
	return &dominio.ErroOferta{
		Codigo:   dominio.ErroBancoIndisponivel,
		Mensagem: fmt.Sprintf("O Novo Banco não respondeu: %v", err),
	}
}
