package dominio

import "fmt"

// CampoCanonico é uma chave do vocabulário do formulário.
//
// O vocabulário é fechado e vive aqui. É a única forma de um banco não poder
// declarar que usa um campo que não existe: no v1 a tabela estava no base.go
// dos scrapers e cada um declarava o que lhe apetecia.
//
// ⚠️ Os nomes seguem o que o contrato publica, e o contrato tem duas heranças
// em inglês — rate_type e fixed_period_years — que vêm do v1 e que não se
// "arrumam" sem partir a app. O resto é português.
type CampoCanonico string

const (
	CampoValorImovel      CampoCanonico = "valor_imovel"
	CampoMontante         CampoCanonico = "montante"
	CampoPrazoAnos        CampoCanonico = "prazo_anos"
	CampoTipoTaxa         CampoCanonico = "rate_type"
	CampoPeriodoFixo      CampoCanonico = "fixed_period_years"
	CampoIndexante        CampoCanonico = "euribor_indexante"
	CampoDataNascimento   CampoCanonico = "data_nascimento"
	CampoRendimentoMensal CampoCanonico = "rendimento_mensal"
	CampoProfissao        CampoCanonico = "profissao"
	CampoSegundoTitular   CampoCanonico = "segundo_titular"
	CampoFinalidade       CampoCanonico = "finalidade"
	CampoLocalizacao      CampoCanonico = "localizacao"
	CampoTipologia        CampoCanonico = "tipologia"
	CampoGarantiaPublica  CampoCanonico = "garantia_publica"
	CampoJaCliente        CampoCanonico = "ja_cliente"
)

// TipoCampo diz à app como desenhar o controlo.
type TipoCampo string

const (
	TipoDinheiro TipoCampo = "dinheiro"
	TipoInteiro  TipoCampo = "inteiro"
	TipoData     TipoCampo = "data"
	TipoOpcao    TipoCampo = "opcao"
	TipoBooleano TipoCampo = "booleano"
)

// InputCanonico é uma entrada do vocabulário: a chave, o rótulo que a pessoa
// lê, e o tipo do controlo.
type InputCanonico struct {
	Campo  CampoCanonico
	Rotulo string
	Tipo   TipoCampo
}

// inputsCanonicos é a tabela única, pela ordem do formulário.
//
// ⚠️ Há aqui campos que o Pedido não transporta — profissao e tipologia. É
// deliberado (ver API.md §1): vários simuladores pedem-nos e várias APIs
// ignoram-nos, e qual é qual apura-se banco a banco, por captura. Até lá, o
// banco que os use preenche-os com valor neutro e declara-o com nota.
var inputsCanonicos = []InputCanonico{
	{CampoValorImovel, "Valor do imóvel", TipoDinheiro},
	{CampoMontante, "Montante a financiar", TipoDinheiro},
	{CampoPrazoAnos, "Prazo", TipoInteiro},
	{CampoTipoTaxa, "Tipo de taxa", TipoOpcao},
	{CampoPeriodoFixo, "Período fixo", TipoInteiro},
	{CampoIndexante, "Indexante Euribor", TipoOpcao},
	{CampoDataNascimento, "Data de nascimento", TipoData},
	{CampoRendimentoMensal, "Rendimento mensal", TipoDinheiro},
	{CampoProfissao, "Profissão", TipoOpcao},
	{CampoSegundoTitular, "2.º titular", TipoBooleano},
	{CampoFinalidade, "Finalidade", TipoOpcao},
	{CampoLocalizacao, "Localização", TipoOpcao},
	{CampoTipologia, "Tipologia", TipoOpcao},
	{CampoGarantiaPublica, "Garantia Pública Jovens", TipoBooleano},
	{CampoJaCliente, "Já é cliente", TipoBooleano},
}

// InputsCanonicos devolve o vocabulário, pela ordem do formulário. A cópia é
// deliberada: a tabela é única e não se altera a partir de fora.
func InputsCanonicos() []InputCanonico {
	saida := make([]InputCanonico, len(inputsCanonicos))
	copy(saida, inputsCanonicos)
	return saida
}

// CampoCanonicoConhecido diz se a chave pertence ao vocabulário.
func CampoCanonicoConhecido(c CampoCanonico) bool {
	for _, i := range inputsCanonicos {
		if i.Campo == c {
			return true
		}
	}
	return false
}

// Input é a declaração de um banco sobre um campo do vocabulário.
type Input struct {
	Campo CampoCanonico
	Usa   bool

	// Nota explica o que o banco faz com o campo. É obrigatória quando o
	// banco o preenche por nós com um valor neutro: um campo preenchido por
	// nós é um pressuposto, e um pressuposto que ninguém vê não existe.
	Nota string
}

// Produto é uma bonificação que um banco sabe aplicar. O id é único por banco
// e vem prefixado por ele ("novobanco:primeiro_banco").
type Produto struct {
	ID         string
	Rotulo     string
	Descricao  string
	PorOmissao bool
}

// Custo é a expectativa honesta de tempo de um banco.
type Custo string

const (
	// CustoBarato é HTTP puro: 1-2 s medidos no v1.
	CustoBarato Custo = "barato"
	// CustoCaro é browser: 30-80 s medidos no v1.
	CustoCaro Custo = "caro"
)

func (c Custo) Valido() bool { return c == CustoBarato || c == CustoCaro }

// ModoPeriodosFixos diz de onde vem a lista de períodos fixos válidos.
type ModoPeriodosFixos string

const (
	ModoLista  ModoPeriodosFixos = "lista"
	ModoDaAPI  ModoPeriodosFixos = "da-api"
	ModoDoHTML ModoPeriodosFixos = "do-html"
)

func (m ModoPeriodosFixos) Valido() bool {
	switch m {
	case ModoLista, ModoDaAPI, ModoDoHTML:
		return true
	}
	return false
}

// Requisitos é o que um banco usa, aceita e impõe.
//
// É a única fonte do formulário adaptativo: se um banco não pede o rendimento,
// a app tem de o saber por aqui, e não por uma lista escrita à mão do lado dela.
type Requisitos struct {
	BancoID   string
	BancoNome string
	Custo     Custo

	Inputs []Input

	PeriodosFixos     []int
	PeriodosFixosModo ModoPeriodosFixos

	// EuriborOpcoes vazio não é "não sei": é "o banco impõe o seu e ignora a
	// escolha". Nesse caso EuriborImposto diz qual.
	EuriborOpcoes  []Indexante
	EuriborImposto Indexante

	PrazoMin, PrazoMax int

	// IdadeMaximaFim é a idade que o titular mais velho pode ter quando o
	// contrato termina.
	IdadeMaximaFim int

	Produtos []Produto
	Notas    []string
}

// Validar apanha os requisitos que não são servíveis pelo contrato: chave fora
// do vocabulário, custo em falta, limites trocados.
//
// Corre no teste de cada banco, não em produção — é uma afirmação sobre código
// nosso, não sobre dados de terceiros.
func (r Requisitos) Validar() error {
	if r.BancoID == "" || r.BancoNome == "" {
		return fmt.Errorf("requisitos sem identificação do banco")
	}
	if !r.Custo.Valido() {
		return fmt.Errorf("%s: custo inválido %q — o contrato exige barato ou caro", r.BancoID, r.Custo)
	}
	if !r.PeriodosFixosModo.Valido() {
		return fmt.Errorf("%s: modo de períodos fixos inválido %q", r.BancoID, r.PeriodosFixosModo)
	}
	vistos := make(map[CampoCanonico]bool, len(r.Inputs))
	for _, in := range r.Inputs {
		if !CampoCanonicoConhecido(in.Campo) {
			return fmt.Errorf("%s: campo %q não está no vocabulário canónico", r.BancoID, in.Campo)
		}
		if vistos[in.Campo] {
			return fmt.Errorf("%s: campo %q declarado duas vezes", r.BancoID, in.Campo)
		}
		vistos[in.Campo] = true
	}
	for _, i := range r.EuriborOpcoes {
		if !i.Valido() {
			return fmt.Errorf("%s: indexante inválido %q nas opções", r.BancoID, i)
		}
	}
	if r.EuriborImposto != "" && !r.EuriborImposto.Valido() {
		return fmt.Errorf("%s: indexante imposto inválido %q", r.BancoID, r.EuriborImposto)
	}
	if r.PrazoMin < 1 || r.PrazoMax < r.PrazoMin {
		return fmt.Errorf("%s: limites de prazo impossíveis (%d a %d)", r.BancoID, r.PrazoMin, r.PrazoMax)
	}
	if r.IdadeMaximaFim <= IdadeMinima {
		return fmt.Errorf("%s: idade máxima de fim impossível (%d)", r.BancoID, r.IdadeMaximaFim)
	}
	return nil
}
