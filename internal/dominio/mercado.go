package dominio

import "time"

// A série de mercado, na forma em que se lê para a servir.
//
// ⚠️ Vive no domínio e não na infra porque é o que a §4 chama «a única coisa
// guardada a longo prazo» — e porque o `/api/rate-catalog` a publica como
// contrato congelado. Um tipo destes na fronteira punha o formato do consumidor
// a decidir o que se lê da base.

// FiltroDoCatalogo são os filtros que o consumidor pode aplicar. Vazio não
// filtra.
type FiltroDoCatalogo struct {
	Banco    string
	Cenario  string
	TipoTaxa string

	// Desde é um instante ISO 8601. ⚠️ Chega como texto e valida-se onde se lê:
	// um `since` que não se percebe é erro de quem pergunta, e não uma série
	// vazia com ar de resposta.
	Desde string

	Limite int
}

// PontoDeMercado é uma linha da série, como o consumidor a recebe.
//
// ⚠️ Os ponteiros são os campos que podem faltar, e faltam por razões
// diferentes: o período fixo não existe na taxa variável, e o spread não existe
// numa fixa pura. Um zero em qualquer deles seria uma afirmação — «zero anos»,
// «spread nulo» — em vez de uma ausência.
type PontoDeMercado struct {
	VarrimentoID string
	CapturadoEm  time.Time
	Cenario      string
	BancoID      string
	BancoNome    string
	TipoTaxa     string

	ValorImovel Dinheiro
	Montante    Dinheiro
	PrazoAnos   int

	PeriodoFixoAnos *int
	Indexante       string

	TAN          *Taxa
	TAEG         *Taxa
	Spread       *Taxa
	EuriborValor *Taxa
	Prestacao    *Dinheiro
	MTIC         *Dinheiro

	// Produtos são as bonificações que aquele preço pressupõe. ⚠️ Sem isto a
	// série é incomparável entre bancos e não se nota — a CGD e o BPI apareciam
	// caros por lhes faltar o desconto, não por cobrarem mais.
	Produtos []string
}
