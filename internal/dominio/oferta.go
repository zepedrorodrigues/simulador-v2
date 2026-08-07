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

	// ⚠️ **Havia aqui um `sem_serie` e um `serie_desactualizada`**, e saem com o
	// varrimento (2026-08-07). O primeiro dizia «não se foi lá: não há preços
	// varridos deste banco» e o segundo «há, e são do outro lado da viragem do
	// dia». Ao vivo vai-se sempre lá — o que resta quando não se consegue
	// responder é o banco não ter respondido, e isso é o `banco_indisponivel`.
	//
	// ⚠️ **A distinção que o `sem_serie` existia para fazer não morre com ele**
	// (KAN-45): uma falta NOSSA não se serve como falha do banco, porque isso
	// manda a pessoa tirar sobre ele uma conclusão que os dados não sustentam. É
	// a mesma razão por que o `503 banco_ocupado` não é uma oferta em falha, e a
	// mesma que a KAN-30 tem em aberto para os pânicos nossos — esses ainda saem
	// como `banco_indisponivel`, e é conhecido.
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

	// Não exportados de propósito: só entram por Acrescentar e Anotar, e é assim
	// que um ajuste sem nota deixa de ser construível a partir de fora deste
	// pacote.
	ajustes []Ajuste
	notas   []string
}

// ⚠️ **Havia aqui um tipo `Fiabilidade`** — `por_confirmar | confirmada |
// em_duvida` (KAN-49) — e a `NotaDaDuvida` que acompanhava o terceiro. Diziam o
// que a sonda tinha apurado sobre a GRELHA de onde um preço saía, e saem com as
// duas: ao vivo não há grelha entre a resposta do banco e o que se serve, logo
// não há terceira coisa sobre que ter uma opinião.
//
// ⚠️ **A regra que o tipo carregava fica**, e vale para o que vier a seguir: um
// estado que só se publica quando as notícias são más ensina quem o lê a tratar
// a ausência como boa notícia. Se voltar a haver uma verificação de preço, volta
// com três estados e não com um booleano.

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
// omitido, uma hipótese que o BANCO declarou e que não muda um campo do pedido.
//
// ⚠️ **É por aqui que passam agora as hipóteses de cálculo**, e não por uma lista
// à parte. Havia um `Pressupor`/`Pressupostos()`, publicado à parte no contrato,
// para as assunções sob as quais NÓS derivávamos a TAEG e o MTIC — o Anexo I,
// Parte II e o Anexo II da MCD mandam declarar as hipóteses junto do número que
// delas depende. Ao vivo não derivamos nada: a TAEG é a que o simulador do banco
// devolveu, e as hipóteses que a suportam são dele e chegam nas notas dele (o
// Montepio diz lá que projecta a taxa do período fixo para o resto do prazo).
func (o *Oferta) Anotar(nota string) {
	if nota == "" {
		return
	}
	o.notas = append(o.notas, nota)
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
