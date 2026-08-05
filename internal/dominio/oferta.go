package dominio

import (
	"fmt"
	"time"
)

// CampoAjustado nomeia o que o banco mudou face ao pedido. Os valores são as
// chaves com que o ajuste sai no JSON, e por isso são as do contrato.
type CampoAjustado string

const (
	AjustadoPeriodoFixo CampoAjustado = "fixed_period_years"
	AjustadoPrazoAnos   CampoAjustado = "prazo_anos"
	AjustadoTipoTaxa    CampoAjustado = "rate_type"
	AjustadoIndexante   CampoAjustado = "euribor_indexante"
)

// Ajuste é uma diferença entre o que se pediu e o que o banco simulou — com a
// explicação agarrada.
//
// ⚠️ A nota é campo do ajuste, e não uma lista ao lado, de propósito: assim um
// ajuste sem nota não é representável. Não é preciosismo de desenho. A
// Directiva 2006/114/CE, art. 4.º, só admite comparação que compare "goods or
// services meeting the same needs" e que compare características "material,
// relevant, verifiable and representative". Uma oferta simulada com 4 anos de
// período fixo quando se pediram 5, apresentada ao lado das outras sem o
// dizer, é uma comparação entre coisas diferentes — e um número que ninguém
// consegue verificar. A MCD resolve o mesmo problema da mesma maneira: o Anexo
// I prescreve os pressupostos a usar quando um dado não é conhecido, e o Anexo
// II obriga a declará-los na ficha.
//
// De guarda-se por isso mesmo: sem o valor pedido, o desvio não é verificável.
//
// ⚠️ Os campos são não exportados de propósito, e isso não é encapsulamento por
// hábito: com eles exportados, qualquer pacote podia escrever
// Ajuste{Campo: ..., Para: 4} e ficar com um ajuste mudo — que é exactamente o
// que este desenho existe para impedir. Assim, um ajuste com valores só se
// constrói pelos construtores abaixo, e cada um deles escreve a nota.
type Ajuste struct {
	campo CampoAjustado

	// de e para são any porque o `aplicado` do contrato sai em JSON com o tipo
	// de cada campo — {"fixed_period_years": 30} é número, não string.
	de, para any

	// nota é para pessoas, em português, e nomeia os números.
	nota string
}

// Campo diz o que mudou.
func (a Ajuste) Campo() CampoAjustado { return a.campo }

// De é o valor pedido. Sem ele o desvio não é verificável.
func (a Ajuste) De() any { return a.de }

// Para é o valor que o banco aplicou.
func (a Ajuste) Para() any { return a.para }

// Nota é a frase que a pessoa lê junto do número.
func (a Ajuste) Nota() string { return a.nota }

// AjustePeriodoFixo regista que o banco não tinha o período pedido.
func AjustePeriodoFixo(de, para int) *Ajuste {
	return &Ajuste{
		campo: AjustadoPeriodoFixo,
		de:    de,
		para:  para,
		nota: fmt.Sprintf(
			"Pediu %d anos de taxa fixa e este banco não os pratica. A simulação foi feita com %d anos.",
			de, para),
	}
}

// AjustePrazo regista que o prazo pedido não cabia — por limite do banco ou
// pela idade do titular mais velho. O motivo entra na frase e é obrigatório.
func AjustePrazo(de, para int, motivo string) *Ajuste {
	return &Ajuste{
		campo: AjustadoPrazoAnos,
		de:    de,
		para:  para,
		nota: fmt.Sprintf(
			"Pediu %d anos de prazo e este banco não os aceita (%s). A simulação foi feita com %d anos.",
			de, motivo, para),
	}
}

// AjusteTipoTaxa regista que o banco não tem a modalidade pedida. O caso que
// obrigou a isto é o Montepio, que não tem taxa fixa pura e a aproxima com
// mista do prazo todo.
func AjusteTipoTaxa(de, para TipoTaxa, motivo string) *Ajuste {
	return &Ajuste{
		campo: AjustadoTipoTaxa,
		de:    string(de),
		para:  string(para),
		nota: fmt.Sprintf(
			"Este banco não pratica taxa %s (%s). A simulação foi feita com taxa %s.",
			de, motivo, para),
	}
}

// AjusteIndexante regista que o banco impôs o seu tenor de Euribor.
func AjusteIndexante(de, para Indexante, motivo string) *Ajuste {
	return &Ajuste{
		campo: AjustadoIndexante,
		de:    string(de),
		para:  string(para),
		nota: fmt.Sprintf(
			"Escolheu a Euribor a %s e este banco impõe a de %s (%s). É esse o preço que comercializa.",
			de, para, motivo),
	}
}

// CodigoErro é o que a app usa para decidir. A mensagem é o que a pessoa lê.
type CodigoErro string

const (
	// ErroPrazoImpossivel: nem o prazo mínimo do banco cabe na idade.
	ErroPrazoImpossivel CodigoErro = "prazo_impossivel"
	// ErroProdutoIndisponivel: o banco não faz isto de todo — modalidade,
	// período, LTV fora da banda que pratica.
	ErroProdutoIndisponivel CodigoErro = "produto_indisponivel"
	// ErroBancoIndisponivel: não respondeu, expirou, deu 5xx.
	ErroBancoIndisponivel CodigoErro = "banco_indisponivel"
	// ErroRespostaIlegivel: respondeu, e o que veio não se consegue ler.
	ErroRespostaIlegivel CodigoErro = "resposta_ilegivel"
	// ErroSemSerie: não há preços varridos deste banco. Ninguém lhe perguntou.
	//
	// ⚠️ **Não é o** ErroBancoIndisponivel, **e a distinção é o ponto** (KAN-45):
	// esse quer dizer que se foi lá e o banco não respondeu; este quer dizer que
	// não se foi lá. O banco não teve culpa nenhuma, e culpá-lo mandava a pessoa
	// tirar sobre ele uma conclusão que os dados não sustentam. É a mesma
	// distinção que a KAN-30 faz para os pânicos nossos.
	//
	// ⚠️ E também não é o ErroProdutoIndisponivel: esse é «foi varrido e não
	// mediu ESTE cenário», que é sobre o pedido; este é sobre o banco inteiro.
	ErroSemSerie CodigoErro = "sem_serie"
	// ErroSerieDesactualizada: há preços deste banco, e são do outro lado da
	// viragem do dia — a Euribor fixou entretanto (ARQUITETURA.md §4, «Que
	// observações compõem a série servida»; §7.3).
	//
	// ⚠️ Não é o ErroSemSerie: ali não há preço nenhum, aqui há e não se serve.
	// Dá-lo como «não se foi lá» era falso, e servi-lo à mesma era comparar
	// preços de fixings diferentes.
	ErroSerieDesactualizada CodigoErro = "serie_desactualizada"
)

// ErroOferta é a falha de um banco, estruturada.
//
// O v1 devolvia texto solto e a interface não conseguia distinguir "este banco
// não faz isto" de "este banco está em baixo" — duas coisas com respostas
// diferentes para quem está do outro lado.
type ErroOferta struct {
	Codigo   CodigoErro
	Mensagem string
}

func (e *ErroOferta) Error() string { return string(e.Codigo) + ": " + e.Mensagem }

// ErroDePrazo constrói a falha em que nem o prazo mínimo cabe na idade.
func ErroDePrazo(bancoNome string, fimAos, idadeMaximaFim int) *ErroOferta {
	return &ErroOferta{
		Codigo: ErroPrazoImpossivel,
		Mensagem: fmt.Sprintf(
			"O crédito teria de terminar aos %d anos; o %s exige que termine até aos %d.",
			fimAos, bancoNome, idadeMaximaFim),
	}
}

// Fase é um troço do plano com taxa própria.
type Fase struct {
	// AteMes é o mês em que a fase acaba, contado desde o início do contrato —
	// acumulado, não duração. É o que o contrato publica e o que a app lê.
	AteMes    int
	Taxa      Taxa
	Prestacao Dinheiro
}

// FaseDuracao é como os bancos devolvem: a duração da fase, não o acumulado.
type FaseDuracao struct {
	Meses     int
	Taxa      Taxa
	Prestacao Dinheiro
}

// FasesDeDuracoes converte durações em acumulado. É a conversão que o v1 fazia
// em cada scraper, cada um à sua maneira.
func FasesDeDuracoes(ds []FaseDuracao) ([]Fase, error) {
	fases := make([]Fase, 0, len(ds))
	acumulado := 0
	for i, d := range ds {
		if d.Meses < 1 {
			return nil, fmt.Errorf("fase %d com duração de %d meses", i+1, d.Meses)
		}
		acumulado += d.Meses
		fases = append(fases, Fase{AteMes: acumulado, Taxa: d.Taxa, Prestacao: d.Prestacao})
	}
	return fases, nil
}

// ValidarFases confirma que as fases cobrem o contrato inteiro, sem buracos e
// sem sobreposições: crescentes, contíguas por construção, e a última a fechar
// exactamente no fim do prazo.
func ValidarFases(fs []Fase, prazoAnos int) error {
	if len(fs) == 0 {
		return fmt.Errorf("plano sem fases")
	}
	anterior := 0
	for i, f := range fs {
		if f.AteMes <= anterior {
			return fmt.Errorf("fase %d acaba ao mês %d, que não é depois do %d", i+1, f.AteMes, anterior)
		}
		anterior = f.AteMes
	}
	if esperado := prazoAnos * 12; anterior != esperado {
		return fmt.Errorf("as fases acabam ao mês %d e o prazo é de %d meses", anterior, esperado)
	}
	return nil
}

// Oferta é o que um banco responde.
//
// Os campos opcionais são ponteiros porque nem todos os bancos devolvem tudo —
// o Bankinter, por exemplo, não reporta MTIC quando a selecção de produtos não
// é a de omissão, e omitir é mais honesto do que inventar.
type Oferta struct {
	BancoID   string
	BancoNome string

	TAN    *Taxa
	TAEG   *Taxa
	Spread *Taxa

	Prestacao *Dinheiro
	MTIC      *Dinheiro

	Indexante    Indexante
	EuriborValor *Taxa

	Fases             []Fase
	ProdutosAplicados []string

	// EmCache e CapturadoEm são preenchidos pela camada de aplicação, que tem
	// relógio e cache. Um banco deixa-os como estão.
	EmCache     bool
	CapturadoEm time.Time

	// Erro não-nulo é uma oferta de falha. Sucesso deriva daqui, para não
	// existir o estado impossível "sucesso com erro".
	Erro *ErroOferta

	// Não exportados de propósito: só entram por Acrescentar, Anotar e
	// Pressupor, e é assim que um ajuste sem nota deixa de ser construível a
	// partir de fora deste pacote.
	ajustes      []Ajuste
	notas        []string
	pressupostos []string
}

// Falhar constrói uma oferta de falha.
func Falhar(bancoID, bancoNome string, e *ErroOferta) Oferta {
	return Oferta{BancoID: bancoID, BancoNome: bancoNome, Erro: e}
}

// Sucesso é derivado: não há oferta boa com erro preenchido.
func (o Oferta) Sucesso() bool { return o.Erro == nil }

// Acrescentar regista um ajuste. Um ajuste nulo é um não-fazer-nada, para as
// políticas poderem devolver "não houve ajuste" sem obrigar quem chama a um if.
// ⚠️ Um ajuste sem nota não entra. O valor zero de um Ajuste continua a
// construir-se de fora — Ajuste{} compila em qualquer pacote, mesmo com os
// campos não exportados — e é este o remate que fecha a porta: deixar entrar um
// ajuste mudo era deixar sair números diferentes dos pedidos sem uma frase que
// o dissesse.
func (o *Oferta) Acrescentar(a *Ajuste) {
	if a == nil || a.nota == "" || a.campo == "" {
		return
	}
	o.ajustes = append(o.ajustes, *a)
}

// Anotar acrescenta uma nota que não corresponde a nenhum ajuste — o MTIC
// omitido, um pressuposto que o banco impôs e que não muda um campo do pedido.
func (o *Oferta) Anotar(nota string) {
	if nota == "" {
		return
	}
	o.notas = append(o.notas, nota)
}

// Pressupor regista uma hipótese sob a qual um número desta oferta foi
// DERIVADO — a TAEG e o MTIC de uma resposta local, hoje.
//
// ⚠️ Vive à parte das notas, e o contrato publica-o à parte, porque é outra
// coisa: uma nota explica o que o banco fez ao pedido, um pressuposto declara em
// que assunções NOSSAS o número assenta. Misturá-los deixava a pessoa sem saber
// qual dos números vem do banco e qual sai de um modelo — que é a distinção de
// que a §4 faz depender toda a honestidade desta resposta.
//
// É o Anexo I, Parte II e o Anexo II da MCD: quem serve um valor dependente de
// hipóteses declara-as junto dele.
func (o *Oferta) Pressupor(hipoteses ...string) {
	for _, h := range hipoteses {
		if h != "" {
			o.pressupostos = append(o.pressupostos, h)
		}
	}
}

// Pressupostos devolve as hipóteses declaradas.
//
// ⚠️ Vazio com a TAEG ou o MTIC preenchidos por derivação é defeito nosso, e
// quem serializa recusa-o. Vazio com eles medidos pelo banco é o estado normal:
// aí não há hipótese nenhuma a declarar.
func (o Oferta) Pressupostos() []string {
	saida := make([]string, len(o.pressupostos))
	copy(saida, o.pressupostos)
	return saida
}

// Ajustes devolve o que o banco mudou face ao pedido.
func (o Oferta) Ajustes() []Ajuste {
	saida := make([]Ajuste, len(o.ajustes))
	copy(saida, o.ajustes)
	return saida
}

// Notas devolve tudo o que a pessoa tem de ler junto dos números: primeiro as
// notas dos ajustes, pela ordem em que foram feitos, depois as livres.
func (o Oferta) Notas() []string {
	saida := make([]string, 0, len(o.ajustes)+len(o.notas))
	for _, a := range o.ajustes {
		saida = append(saida, a.nota)
	}
	return append(saida, o.notas...)
}

// Aplicado é o que o banco usou de facto, quando difere do pedido. Vazio quer
// dizer que a simulação é exactamente o que foi pedido.
//
// ⚠️ Se isto não estiver vazio, a app é obrigada a mostrar a nota junto do
// valor. É o que torna a comparação verificável em vez de enganadora.
func (o Oferta) Aplicado() map[string]any {
	if len(o.ajustes) == 0 {
		return map[string]any{}
	}
	m := make(map[string]any, len(o.ajustes))
	for _, a := range o.ajustes {
		m[string(a.campo)] = a.para
	}
	return m
}
