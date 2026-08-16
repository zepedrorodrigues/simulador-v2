package dominio

import (
	"fmt"
	"strings"
)

// TipoTaxa é a modalidade de taxa de juro.
type TipoTaxa string

const (
	TaxaVariavel TipoTaxa = "variavel"
	TaxaFixa     TipoTaxa = "fixa"
	TaxaMista    TipoTaxa = "mista"
)

// Valido diz se o valor é um dos três. ⚠️ O valor por omissão de um TipoTaxa é
// a string vazia, que não é nenhum deles — é a validação que o apanha, não a
// esperança de que ninguém construa um Pedido à mão.
func (t TipoTaxa) Valido() bool {
	switch t {
	case TaxaVariavel, TaxaFixa, TaxaMista:
		return true
	}
	return false
}

// TemPeriodoFixo diz se a modalidade tem uma fase de taxa fixa.
func (t TipoTaxa) TemPeriodoFixo() bool { return t == TaxaFixa || t == TaxaMista }

// BPI devolve o rótulo que o formulário BPI usa para esta modalidade.
func (t TipoTaxa) BPI() string {
	switch t {
	case TaxaVariavel:
		return "Taxa Variável"
	case TaxaFixa:
		return "Taxa Fixa"
	case TaxaMista:
		return "Taxa Mista"
	default:
		return "Taxa Variável"
	}
}

// Finalidade é o destino do imóvel. Não é decorativa: no Novo Banco o
// arrendamento mede-se em +0,50 p.p. de spread.
type Finalidade string

const (
	FinalidadePropria      Finalidade = "propria"
	FinalidadeSecundaria   Finalidade = "secundaria"
	FinalidadeArrendamento Finalidade = "arrendamento"
)

func (f Finalidade) Valido() bool {
	switch f {
	case FinalidadePropria, FinalidadeSecundaria, FinalidadeArrendamento:
		return true
	}
	return false
}

// Localizacao é onde fica o imóvel.
type Localizacao string

const (
	LocalizacaoContinente Localizacao = "continente"
	LocalizacaoAcores     Localizacao = "acores"
	LocalizacaoMadeira    Localizacao = "madeira"
)

func (l Localizacao) Valido() bool {
	switch l {
	case LocalizacaoContinente, LocalizacaoAcores, LocalizacaoMadeira:
		return true
	}
	return false
}

// Indexante é o tenor da Euribor.
//
// ⚠️ Vazio não é "não sei": é "não foi escolhido", e há bancos que nunca
// deixam escolher — o Banco CTT impõe 12M na variável e 3M no pós-fixo da
// mista, e os outros identificadores respondem por API mas são preço que o
// banco não comercializa.
type Indexante string

const (
	Euribor3M  Indexante = "3m"
	Euribor6M  Indexante = "6m"
	Euribor12M Indexante = "12m"
)

func (i Indexante) Valido() bool {
	switch i {
	case Euribor3M, Euribor6M, Euribor12M:
		return true
	}
	return false
}

// Titular é uma pessoa que pede o crédito.
//
// ⚠️ Só o que o contrato transporta hoje. A profissão, o vínculo e as
// habilitações são pedidos por vários simuladores e ignorados por várias APIs;
// enquanto não se souber, banco a banco e por captura, qual é qual, cada banco
// preenche-os com valor neutro e declara-o nos seus Requisitos.
type Titular struct {
	DataNascimento   Data
	RendimentoMensal Dinheiro
}

// IdadeMinima é a idade a partir da qual se contrata crédito à habitação.
const IdadeMinima = 18

// PrazoMaximoAnos é o tecto absoluto do domínio. Cada banco tem o seu, mais
// baixo; este só existe para recusar disparates antes de se gastar um scrape.
const PrazoMaximoAnos = 50

// Pedido é o que a pessoa quer. É o que entra; o que sai é a Oferta.
type Pedido struct {
	ValorImovel Dinheiro
	Montante    Dinheiro
	PrazoAnos   int
	TipoTaxa    TipoTaxa

	// PeriodoFixoAnos é o período de taxa fixa pretendido, em anos. Nulo quer
	// dizer "sem preferência": o banco aplica o mais longo que tiver.
	PeriodoFixoAnos *int

	// Indexante vazio quer dizer "sem preferência" ou "o banco impõe o seu".
	Indexante Indexante

	Titulares       []Titular
	Finalidade      Finalidade
	Localizacao     Localizacao
	GarantiaPublica bool
	JaCliente       bool

	// Produtos são as bonificações escolhidas, pelos ids que os Requisitos de
	// cada banco publicam ("cgd:packs"). Um pedido vai a vários bancos e cada um
	// lê os seus, pelo prefixo.
	//
	// ⚠️ Vazio quer dizer **nenhum**, e não «os que cada banco liga por omissão».
	// A diferença está medida e inverte a ordem dos bancos — os números e a base
	// legal estão na §4 do ARQUITETURA.md e no ECRAS.md §1.3.
	//
	// Quem escolhe é a pessoa: o PorOmissao do Produto diz à app o que
	// pré-seleccionar, e mais nada.
	Produtos []string
}

// TemProduto diz se um id de produto foi escolhido.
func (p Pedido) TemProduto(id string) bool {
	for _, escolhido := range p.Produtos {
		if escolhido == id {
			return true
		}
	}
	return false
}

// ProdutosDoBanco devolve os produtos escolhidos que pertencem a um banco, pela
// ordem em que foram pedidos. Nil quer dizer que nenhum é dele — o que é o caso
// comum, porque um pedido leva a selecção de todos os bancos comparados.
func (p Pedido) ProdutosDoBanco(bancoID string) []string {
	var dele []string
	for _, id := range p.Produtos {
		if ProdutoDoBanco(id, bancoID) {
			dele = append(dele, id)
		}
	}
	return dele
}

// ErroValidacao nomeia o campo que reprovou. O contrato serve um campo só
// (DetalheErro.campo), por isso a validação pára no primeiro.
type ErroValidacao struct {
	Campo    string
	Mensagem string
}

func (e *ErroValidacao) Error() string {
	return fmt.Sprintf("%s: %s", e.Campo, e.Mensagem)
}

func invalido(campo, mensagem string) error {
	return &ErroValidacao{Campo: campo, Mensagem: mensagem}
}

// Validar recusa um pedido que não faz sentido, nomeando o campo.
//
// ⚠️ Isto não é cortesia: sem a guarda do valor do imóvel, um zero chega ao
// cálculo de LTV e o decimal entra em pânico (medido: "decimal division by
// 0"). Como o orquestrador corre cada banco com recover, esse pânico
// apareceria à app como "este banco avariou" — que é exactamente o que o v1
// fazia, e o comentário do models.py dele explica porquê é pior do que
// parece: esconde um erro nosso atrás de uma falha do banco.
//
// hoje entra por parâmetro porque o domínio não tem relógio.
func (p Pedido) Validar(hoje Data) error {
	if !p.ValorImovel.Positivo() {
		return invalido("valor_imovel", "o valor do imóvel tem de ser maior do que zero")
	}
	if !p.Montante.Positivo() {
		return invalido("montante", "o montante tem de ser maior do que zero")
	}
	if p.Montante.Cmp(p.ValorImovel) > 0 {
		return invalido("montante", "o montante não pode exceder o valor do imóvel")
	}
	if p.PrazoAnos < 1 || p.PrazoAnos > PrazoMaximoAnos {
		return invalido("prazo_anos", fmt.Sprintf("o prazo tem de estar entre 1 e %d anos", PrazoMaximoAnos))
	}
	if !p.TipoTaxa.Valido() {
		return invalido("rate_type", "tipo de taxa desconhecido")
	}
	if p.PeriodoFixoAnos != nil {
		if !p.TipoTaxa.TemPeriodoFixo() {
			return invalido("fixed_period_years", "a taxa variável não tem período fixo")
		}
		if *p.PeriodoFixoAnos < 1 {
			return invalido("fixed_period_years", "o período fixo tem de ser de pelo menos um ano")
		}
	}
	if p.Indexante != "" && !p.Indexante.Valido() {
		return invalido("euribor_indexante", "indexante desconhecido")
	}
	if !p.Finalidade.Valido() {
		return invalido("finalidade", "finalidade desconhecida")
	}
	if !p.Localizacao.Valido() {
		return invalido("localizacao", "localização desconhecida")
	}
	if err := validarProdutos(p.Produtos); err != nil {
		return err
	}
	if len(p.Titulares) < 1 || len(p.Titulares) > 2 {
		return invalido("titulares", "um pedido tem um ou dois titulares")
	}
	for _, t := range p.Titulares {
		if t.DataNascimento.Zero() {
			return invalido("data_nascimento", "falta a data de nascimento de um titular")
		}
		idade := t.DataNascimento.Idade(hoje)
		if idade < 0 {
			return invalido("data_nascimento", "a data de nascimento está no futuro")
		}
		if idade < IdadeMinima {
			return invalido("data_nascimento", fmt.Sprintf("um titular tem de ter pelo menos %d anos", IdadeMinima))
		}
		if t.RendimentoMensal.Cmp(Dinheiro{}) < 0 {
			return invalido("rendimento_mensal", "o rendimento não pode ser negativo")
		}
	}
	return nil
}

// validarProdutos recusa uma selecção que nenhum banco poderia honrar.
//
// ⚠️ O domínio não sabe que ids existem — isso vive nos Requisitos de cada
// banco — e por isso não os verifica. Verifica a forma, que é o que decide se o
// id chega a algum banco: sem prefixo não chega a nenhum, e um id repetido
// deixaria a Oferta.ProdutosAplicados a reportar duas vezes o mesmo produto.
func validarProdutos(ids []string) error {
	vistos := make(map[string]bool, len(ids))
	for _, id := range ids {
		banco, produto, temSeparador := strings.Cut(id, separadorDeProduto)
		if !temSeparador || banco == "" || produto == "" {
			return invalido("produtos", fmt.Sprintf(
				"o produto %q não tem a forma banco%sproduto e não chegaria a banco nenhum",
				id, separadorDeProduto))
		}
		if vistos[id] {
			return invalido("produtos", fmt.Sprintf("o produto %q foi escolhido duas vezes", id))
		}
		vistos[id] = true
	}
	return nil
}
