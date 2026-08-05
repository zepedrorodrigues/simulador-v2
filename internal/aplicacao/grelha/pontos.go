package grelha

import (
	"fmt"
	"slices"

	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/varrimento"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// Os pontos da grelha: as sete famílias e o que cada uma mede. Porque são
// dezenas e não milhares, na §4 do ARQUITETURA.md.
// Referencia é o pedido a partir do qual todas as famílias variam.
//
// ⚠️ Titular NEUTRO, por exigência da §4: esta tabela não tem dados pessoais.
// O que se guarda é o preço que não depende de quem pede — e o que depende
// (TAEG e MTIC, pelo prémio do seguro de vida) fica declarado como limite da
// §4, não estimado.
type Referencia struct {
	ValorImovel dominio.Dinheiro
	Montante    dominio.Dinheiro
	PrazoAnos   int

	// IdadeAnos é a idade do titular neutro. Entra no prazo máximo de alguns
	// bancos, e no MTIC de todos os que cobram seguro de vida.
	IdadeAnos int

	// RendimentoMensal existe porque há simuladores que o exigem. Está medido
	// que não mexe no preço (Novo Banco, 2026-07-26).
	RendimentoMensal dominio.Dinheiro

	Localizacao dominio.Localizacao

	// Indexante é o tenor de referência. Um banco que imponha o seu ignora-o.
	Indexante dominio.Indexante
}

// Os valores de referência, e a razão de cada um.
//
// ⚠️ O imóvel a 400 000 € não é arbitrário: com ele, 1 000 € de montante são
// exactamente 0,25 p.p. de LTV e 4 000 € são 1 p.p., o que deixa o varrimento do
// LTV cair em números redondos sem arredondamentos a sujar a medição. É o mesmo
// imóvel com que se mediram as fronteiras que decidiram a §4.
//
// O montante de 320 000 € põe a referência em LTV 80 %, que é onde os três
// bancos têm preço medido e onde a maioria dos pedidos cai.
var (
	ValorImovelOmissao      = dominio.DinheiroDeInteiro(400_000)
	MontanteOmissao         = dominio.DinheiroDeInteiro(320_000)
	RendimentoMensalOmissao = dominio.DinheiroDeInteiro(2_000)
)

const (
	// PrazoOmissao é o prazo de referência. 30 anos é o pedido comum e cabe
	// nos três bancos com o titular de referência.
	PrazoOmissao = 30

	// IdadeOmissao é a idade do titular neutro. 30 anos deixa o prazo máximo
	// no escalão mais largo dos três bancos — 40 anos na CGD, no Novo Banco e
	// no Montepio —, para que seja o prazo pedido a mandar e não a idade.
	IdadeOmissao = 30
)

func (r Referencia) comOmissoes() Referencia {
	if !r.ValorImovel.Positivo() {
		r.ValorImovel = ValorImovelOmissao
	}
	if !r.Montante.Positivo() {
		r.Montante = MontanteOmissao
	}
	if r.PrazoAnos <= 0 {
		r.PrazoAnos = PrazoOmissao
	}
	if r.IdadeAnos <= 0 {
		r.IdadeAnos = IdadeOmissao
	}
	if !r.RendimentoMensal.Positivo() {
		r.RendimentoMensal = RendimentoMensalOmissao
	}
	if !r.Localizacao.Valido() {
		r.Localizacao = dominio.LocalizacaoContinente
	}
	return r
}

// FinalidadeDeReferencia é a que serve de base a todas as famílias. As outras
// medem-se como desvio a partir dela (§4: «desvio da finalidade, aditivo»).
const FinalidadeDeReferencia = dominio.FinalidadePropria

// Pontos devolve o plano de varrimento de um banco: que pedidos se lhe fazem e
// que ponto da grelha cada um representa.
//
// Sai dos Requisitos e mais nada. É lá que vivem os períodos fixos que o banco
// pratica, os tenores que aceita, os produtos que sabe aplicar e os campos que
// usa — e derivar daí, em vez de uma lista à mão por banco, é o que faz um
// banco novo entrar na grelha por escrever os seus Requisitos.
//
// hoje entra por parâmetro porque o domínio não tem relógio: é dele que sai a
// data de nascimento do titular neutro.
func Pontos(r dominio.Requisitos, ref Referencia, hoje dominio.Data) ([]varrimento.Ponto, error) {
	if err := r.Validar(); err != nil {
		return nil, fmt.Errorf("requisitos não servíveis: %w", err)
	}
	ref = ref.comOmissoes()

	base, err := ref.pedido(hoje)
	if err != nil {
		return nil, err
	}

	var pontos []varrimento.Ponto
	junta := func(c Cenario, p dominio.Pedido) error {
		if err := c.Validar(); err != nil {
			return err
		}
		pontos = append(pontos, varrimento.Ponto{Cenario: c.Chave(), Pedido: p})
		return nil
	}

	variavel := Cenario{TipoTaxa: dominio.TaxaVariavel, Finalidade: FinalidadeDeReferencia}

	// 1 — a referência. É a linha contra a qual todas as outras são desvio.
	if err := junta(variavel, base); err != nil {
		return nil, err
	}

	// 2 — a fase fixa, uma observação por período que o banco pratica, na fixa
	// e na mista. Não há interpolação: os bancos vendem uma lista curta, e
	// ajustar uma curva a uma tabela só acrescentaria erro onde não havia (§4).
	for _, anos := range r.PeriodosFixos {
		for _, tipo := range []dominio.TipoTaxa{dominio.TaxaFixa, dominio.TaxaMista} {
			p := base
			p.TipoTaxa = tipo
			p.PeriodoFixoAnos = &anos
			c := Cenario{TipoTaxa: tipo, PeriodoFixoAnos: anos, Finalidade: FinalidadeDeReferencia}
			if err := junta(c, p); err != nil {
				return nil, err
			}
		}
	}

	// 3 — o tenor da Euribor, um ponto por opção além da de referência. Um
	// banco que imponha o seu não declara opções e não gera nenhum: a CGD
	// ignora a escolha e comercializa só 6M.
	//
	// ⚠️ E os tenores dão preços diferentes, medido nos dois bancos que
	// deixam escolher: no Novo Banco 3,239 / 3,496 / 3,698 % de TAN, no
	// Montepio 3,839 / 4,096 / 4,298 %. Não é uma dimensão decorativa.
	for _, i := range r.EuriborOpcoes {
		if i == base.Indexante {
			continue
		}
		p := base
		p.Indexante = i
		if err := junta(variavel, p); err != nil {
			return nil, err
		}
	}

	// 4 — a finalidade, um ponto por cada uma além da de referência, e só se o
	// banco a usar. No Novo Banco o arrendamento mede-se em +0,50 p.p.; no
	// Montepio a TAN é a mesma e muda a prestação, pelo Imposto do Selo.
	if usa(r, dominio.CampoFinalidade) {
		for _, f := range []dominio.Finalidade{dominio.FinalidadeSecundaria, dominio.FinalidadeArrendamento} {
			p := base
			p.Finalidade = f
			c := Cenario{TipoTaxa: dominio.TaxaVariavel, Finalidade: f}
			if err := junta(c, p); err != nil {
				return nil, err
			}
		}
	}

	// 5 — os produtos, um ponto por produto sozinho. O desconto de cada um é a
	// diferença para a referência, que é o que a §4 quer dizer com «derivam-se
	// na leitura» — e é por isso que não há uma terceira tabela.
	for _, produto := range r.Produtos {
		p := base
		p.Produtos = []string{produto.ID}
		if err := junta(variavel, p); err != nil {
			return nil, err
		}
	}

	// 6 — e, havendo mais do que um produto, um ponto com TODOS. ⚠️ Não é
	// redundante: é o que permite verificar que os descontos são mesmo
	// aditivos, que é uma assunção da §4 e não uma medição. Custa um pedido por
	// banco e transforma essa assunção em algo que o resíduo pode contradizer.
	if len(r.Produtos) > 1 {
		p := base
		p.Produtos = make([]string, 0, len(r.Produtos))
		for _, produto := range r.Produtos {
			p.Produtos = append(p.Produtos, produto.ID)
		}
		if err := junta(variavel, p); err != nil {
			return nil, err
		}
	}

	// 7 — os extremos do prazo, para a repartição dos encargos ser identificável.
	//
	// ⚠️ Não medem spread: está medido que o spread não depende do prazo, e essa
	// redundância é o travão que contradiz a medição se ela deixar de valer. O
	// porquê dos EXTREMOS e não de dois prazos quaisquer está na §4 do
	// ARQUITETURA.md («A TAEG deriva-se com os encargos MEDIDOS»), com os rácios
	// medidos. Custa dois pedidos por banco, sobre dezenas.
	for _, anos := range prazosExtremos(r, ref, base.PrazoAnos) {
		p := base
		p.PrazoAnos = anos
		if err := junta(variavel, p); err != nil {
			return nil, err
		}
	}

	return pontos, nil
}

// prazosExtremos devolve o prazo mais curto e o mais longo que o banco serve ao
// titular neutro, sem repetir o de referência.
//
// ⚠️ O tecto é o mais baixo entre o que o banco aceita, o que a idade do titular
// neutro deixa e o tecto absoluto do domínio. Pedir um prazo que o banco recusa
// gasta um pedido para receber um erro nosso e escreve no catálogo uma falha com
// o nome dele — é a KAN-30 pelo lado da grelha, e é a mesma razão por que toda
// esta grelha sai dos Requisitos.
func prazosExtremos(r dominio.Requisitos, ref Referencia, referencia int) []int {
	tecto := min(r.PrazoMax, r.IdadeMaximaFim-ref.IdadeAnos, dominio.PrazoMaximoAnos)
	chao := max(r.PrazoMin, 1)

	// Um banco cujos limites não deixem prazo nenhum de pé não gera pontos, em
	// vez de gerar um inválido. Não é caso hipotético: basta um IdadeMaximaFim
	// baixo com um titular de referência mais velho.
	if chao > tecto {
		return nil
	}

	var saida []int
	for _, anos := range []int{chao, tecto} {
		if anos == referencia || slices.Contains(saida, anos) {
			continue
		}
		saida = append(saida, anos)
	}
	return saida
}

// pedido monta o pedido de referência.
func (r Referencia) pedido(hoje dominio.Data) (dominio.Pedido, error) {
	// ⚠️ O titular neutro faz anos a 1 de Janeiro, e é escolha e não acaso: com
	// essa data a idade é exactamente IdadeAnos em qualquer dia do ano. Uma
	// data de nascimento fixa envelheceria com o calendário e mudaria o prazo
	// máximo a meio da série, sem nada no varrimento a dizê-lo.
	nascimento, err := dominio.DataDe(hoje.Ano-r.IdadeAnos, 1, 1)
	if err != nil {
		return dominio.Pedido{}, fmt.Errorf("titular de referência com %d anos: %w", r.IdadeAnos, err)
	}

	p := dominio.Pedido{
		ValorImovel: r.ValorImovel,
		Montante:    r.Montante,
		PrazoAnos:   r.PrazoAnos,
		TipoTaxa:    dominio.TaxaVariavel,
		Indexante:   r.Indexante,
		Titulares: []dominio.Titular{{
			DataNascimento:   nascimento,
			RendimentoMensal: r.RendimentoMensal,
		}},
		Finalidade:  FinalidadeDeReferencia,
		Localizacao: r.Localizacao,
	}
	if err := p.Validar(hoje); err != nil {
		return dominio.Pedido{}, fmt.Errorf("pedido de referência inválido: %w", err)
	}
	return p, nil
}

// usa diz se o banco declara que usa um campo do vocabulário.
func usa(r dominio.Requisitos, campo dominio.CampoCanonico) bool {
	for _, in := range r.Inputs {
		if in.Campo == campo {
			return in.Usa
		}
	}
	return false
}
