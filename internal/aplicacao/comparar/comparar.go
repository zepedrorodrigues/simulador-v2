// Package comparar responde a um pedido por consulta à grelha e cálculo local,
// sem falar com bancos nem saber que existe SQL — o que o torna afirmável sem
// rede, sem base e sem relógio.
//
// O que sai por consulta e o que sai por cálculo está na §4 do ARQUITETURA.md;
// a razão de não falar com bancos está na §1 e na §3.
package comparar

import (
	"errors"
	"fmt"
	"slices"

	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/grelha"
	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/varrimento"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

var (
	// ErrSemObservacoes: não há grelha nenhuma sobre que responder.
	ErrSemObservacoes = errors.New("não há observações de varrimento para comparar")

	// ErrPedidoInvalido: o pedido não passa a validação do domínio. Devolve-se
	// antes de olhar para banco nenhum — é erro de quem pergunta, e não uma
	// indisponibilidade dos bancos.
	ErrPedidoInvalido = errors.New("pedido inválido")
)

// Catalogo é a fotografia do varrimento sobre a qual se responde.
//
// ⚠️ Construir isto é o que separa a leitura da resposta: quem constrói decide
// QUE observações entram — as do último varrimento, as das últimas horas —, e é
// aí que a regra da §7.3 se aplica («nunca servir através da viragem do dia»).
// Este pacote não tem relógio e não sabe se o que recebeu é fresco: responde com
// o que lhe deram e carimba cada oferta com a hora a que o preço foi medido.
type Catalogo struct {
	// porBanco tem, para cada banco, tudo o que dele se mediu.
	porBanco map[string]*medidoDeUmBanco

	// desactualizados são os bancos com série que a viragem do dia pôs de fora.
	// Não estão em `porBanco` — não há preço deles com que responder — e existem
	// aqui só para a recusa poder dizer a verdade em vez de `sem_serie`.
	desactualizados map[string]bool

	// fiabilidade é o veredicto da sonda por banco. Nulo, ou banco ausente, vale
	// o valor zero de dominio.Fiabilidade — «por confirmar».
	fiabilidade map[string]dominio.Fiabilidade
}

// medidoDeUmBanco é o que a grelha sabe de um banco.
type medidoDeUmBanco struct {
	nome string

	// escala é a função em degraus que leva um LTV ao spread.
	escala    dominio.EscalaDeLTV
	temEscala bool

	// porCenario são TODAS as observações de cada chave, e não uma.
	//
	// ⚠️ A chave `cenario` não identifica uma observação, e isso é desenho e não
	// defeito: a §4 di-lo — «duas linhas do mesmo varrimento podem partilhar o
	// cenario e distinguir-se só por essas colunas — a linha com produtos e a
	// linha sem eles». Medido a 2026-07-28 sobre os quatro bancos registados: a
	// chave `variavel/0/propria` aparece **4 vezes na CGD, 6 no Banco CTT, 7 no
	// Montepio e 9 no Novo Banco**, porque é a chave das famílias dos produtos e
	// da do prazo.
	//
	// ⚠️ Um `map[string]Observacao` perderia 3 a 8 observações por banco, em
	// silêncio e ficando com a última — e o que se perdia era exactamente o que
	// esta resposta precisa: as linhas com produtos (donde saem os descontos) e
	// as de prazos diferentes (donde sai a TAEG). Foi o primeiro desenho deste
	// ficheiro, e a medição matou-o.
	porCenario map[string][]varrimento.Observacao

	// descontos é o desvio de spread de cada produto, em pontos percentuais e
	// com sinal — derivado da diferença entre a linha com ele e a linha sem
	// produtos, que é o que a §4 quer dizer com «derivam-se na leitura».
	descontos map[string]dominio.Taxa

	// encargos é o modelo ajustado às observações de prazos diferentes.
	encargos    dominio.Encargos
	temEncargos bool
}

// Serie é a fotografia com que se responde: as observações que entram, e os
// bancos que a composição deixou de fora por estarem do outro lado da viragem do
// dia (`ARQUITETURA.md` §4, «Que observações compõem a série servida»; §7.3).
//
// ⚠️ Os postos de fora **viajam com a série** em vez de sumirem, e é o ponto do
// tipo: só quem a compôs sabe a diferença entre «tem preços que hoje não se
// podem servir» e «nunca foi varrido», e as duas dão códigos diferentes a quem
// lê — `serie_desactualizada` e `sem_serie`. Separá-las em dois argumentos
// deixava construir um catálogo que esquecesse a segunda metade.
type Serie struct {
	Observacoes     []varrimento.Observacao
	Desactualizados []string

	// Fiabilidade é o que a sonda apurou sobre cada banco. Um banco ausente do
	// mapa vale `FiabilidadePorConfirmar` — o valor zero —, e é o que faz uma
	// série lida de uma base sem sondagens nenhumas dizer a verdade sozinha.
	Fiabilidade map[string]dominio.Fiabilidade
}

// NovoCatalogo organiza uma série varrida para se poder responder com ela.
//
// ⚠️ Um banco cujas observações não cheguem para uma escala, ou para ajustar
// encargos, **não desaparece**: entra com o que tem, e o que lhe faltar aparece
// na resposta como recusa nomeada em vez de como um número inventado. É a mesma
// escolha da §5 — falhar com clareza em vez de servir um número com ar de certo.
func NovoCatalogo(s Serie) (*Catalogo, error) {
	obs := s.Observacoes
	if len(obs) == 0 {
		return nil, ErrSemObservacoes
	}

	escalas := varrimento.EscalasPorBanco(obs)
	c := &Catalogo{
		porBanco:        map[string]*medidoDeUmBanco{},
		desactualizados: map[string]bool{},
		fiabilidade:     s.Fiabilidade,
	}
	for _, id := range s.Desactualizados {
		c.desactualizados[id] = true
	}

	for _, o := range obs {
		if !o.Sucesso() {
			// ⚠️ Uma observação de falha não entra no catálogo de resposta. Ela é
			// informação sobre o banco — e é por isso que se grava —, mas não é
			// preço com que se possa responder a alguém.
			continue
		}
		id := o.Oferta.BancoID
		banco, jaLa := c.porBanco[id]
		if !jaLa {
			escala, tem := escalas[id]
			banco = &medidoDeUmBanco{
				nome:       o.Oferta.BancoNome,
				escala:     escala,
				temEscala:  tem,
				porCenario: map[string][]varrimento.Observacao{},
				descontos:  map[string]dominio.Taxa{},
			}
			c.porBanco[id] = banco
		}
		// ⚠️ As linhas de degrau não entram como cenário: elas medem a ESCALA, e
		// o cenário delas é o de referência — deixá-las entrar punha uma medição
		// de LTV a concorrer com as observações dos pontos.
		if o.Degrau == nil {
			banco.porCenario[o.Ponto.Cenario] = append(banco.porCenario[o.Ponto.Cenario], o)
		}
	}

	for _, banco := range c.porBanco {
		banco.derivarDescontos()
		banco.ajustarEncargos()
	}
	return c, nil
}

// derivarDescontos mede o que cada produto vale, pela diferença de spread entre
// a linha que o traz sozinho e a linha sem produtos do mesmo cenário.
//
// ⚠️ É isto que a §4 quer dizer com «o desvio de cada produto deriva-se na
// leitura», e é a razão de não haver uma terceira tabela. O varrimento gasta um
// ponto por produto (família 5) precisamente para esta subtracção ser possível.
//
// ⚠️ O sinal fica como está: um produto que desconta dá um valor NEGATIVO. Guardar
// o módulo obrigaria quem soma a saber de cor que se subtrai — e o dia em que um
// banco cobrar por um produto em vez de descontar apanha-se sozinho.
func (b *medidoDeUmBanco) derivarDescontos() {
	for _, obs := range b.porCenario {
		semProdutos, tem := semProdutosEntre(obs)
		if !tem || semProdutos.Oferta.Spread == nil {
			continue
		}
		for _, o := range obs {
			// Só as linhas com UM produto: a linha com todos (família 6) existe
			// para verificar que os descontos são aditivos, e usá-la aqui era
			// atribuir a soma a um só.
			if len(o.Oferta.ProdutosAplicados) != 1 || o.Oferta.Spread == nil {
				continue
			}
			b.descontos[o.Oferta.ProdutosAplicados[0]] = o.Oferta.Spread.Sub(*semProdutos.Oferta.Spread)
		}
	}
}

// semProdutosEntre encontra a observação que não tem produto nenhum aplicado.
//
// ⚠️ É ela a referência de preço, e não «a primeira»: um pedido sem produtos
// pede o preço sem produtos (KAN-33), e servir a linha bonificada a quem não a
// pediu foi o erro que inverteu a ordem dos bancos por 0,45 p.p.
func semProdutosEntre(obs []varrimento.Observacao) (varrimento.Observacao, bool) {
	for _, o := range obs {
		if len(o.Oferta.ProdutosAplicados) == 0 {
			return o, true
		}
	}
	return varrimento.Observacao{}, false
}

// ajustarEncargos ajusta o modelo de encargos deste banco às observações que
// tenham TAN e TAEG em prazos diferentes.
//
// ⚠️ Falha em silêncio de propósito — `temEncargos` fica falso e a oferta sai sem
// TAEG nem MTIC, com nota. Um banco sem observações em dois prazos não tem os
// encargos identificáveis (ver `dominio.AjustarEncargos`), e inventar uma
// repartição era exactamente o que essa função existe para recusar.
func (b *medidoDeUmBanco) ajustarEncargos() {
	// ⚠️ A ordem é FIXADA antes de ajustar, e não é preciosismo — é um defeito
	// medido. O `AjustarEncargos` ancora na observação de prazo mais curto e na
	// de mais longo; quando duas partilham o prazo (a variável e a mista a 30
	// anos, por exemplo) e têm TAN diferentes, **qual delas é a âncora decide o
	// resultado**. A iteração de um mapa em Go é aleatória a cada corrida, e
	// isso fazia a TAEG variar entre execuções do mesmo teste: medido a
	// 2026-07-28, uma falha em seis corridas, com a TAEG a saltar de 4,4 % para
	// 5,476 %.
	//
	// Uma resposta que muda sem os dados mudarem não é servível, e um teste que
	// falha uma vez em seis é pior do que um que falha sempre: se ninguém o
	// tivesse corrido de seguida, entrava assim.
	cenarios := make([]string, 0, len(b.porCenario))
	for cenario := range b.porCenario {
		cenarios = append(cenarios, cenario)
	}
	slices.Sort(cenarios)

	var obs []dominio.ObservacaoDeEncargo
	for _, cenario := range cenarios {
		for _, o := range b.porCenario[cenario] {
			// ⚠️ Só as linhas SEM produtos. Misturar preços bonificados com
			// preços de tabela no mesmo ajuste faria o modelo atribuir a
			// encargos uma diferença que é desconto — e o desconto já está
			// medido à parte, nos descontos.
			if len(o.Oferta.ProdutosAplicados) != 0 {
				continue
			}
			// ⚠️ E só a TAXA VARIÁVEL, que é onde a 7.ª família varre os prazos.
			//
			// Misturar modalidades parecia inofensivo — os encargos são do banco
			// e não do produto, e cada observação traz a sua TAN — e não é:
			// medido a 2026-07-28, com a fixa a 10 anos (TAN 5,55) como âncora
			// curta e a mista a 30 (TAN 3,1) como longa, o ajuste dava uma TAEG
			// de 5,476 % sobre uma TAN de 3,8 %. As duas âncoras descrevem
			// produtos diferentes, e a diferença entre elas não é encargo.
			//
			// A consequência fica declarada: aplicar à fixa e à mista os
			// encargos ajustados na variável assume que eles não mudam com a
			// modalidade. É hipótese, não medição — e o resíduo contradi-la se
			// for falsa.
			if o.Ponto.Pedido.TipoTaxa != dominio.TaxaVariavel {
				continue
			}
			if o.Oferta.TAN == nil || o.Oferta.TAEG == nil {
				continue
			}
			obs = append(obs, dominio.ObservacaoDeEncargo{
				Capital:    o.Ponto.Pedido.Montante,
				PrazoMeses: varrimento.PrazoAplicado(o),
				TAN:        *o.Oferta.TAN,
				TAEG:       *o.Oferta.TAEG,
			})
		}
	}

	encargos, _, err := dominio.AjustarEncargos(obs)
	if err != nil {
		return
	}
	b.encargos, b.temEncargos = encargos, true
}

// observacaoDe escolhe, entre as observações de um cenário, aquela com que se
// responde — e a regra é explícita porque a chave não chega para decidir.
//
// ⚠️ A **sem produtos** é sempre a escolhida, e o desconto de quem os pediu
// aplica-se depois, pelos descontos derivados. Não é indiferença entre caminhos:
// a linha com produtos existe no varrimento para se medir o desconto, e servi-la
// directamente daria o preço certo só a quem tivesse pedido exactamente aquele
// conjunto — e o preço bonificado a quem não pediu nada, que é a inversão que a
// KAN-33 mediu em 0,45 p.p.
//
// Entre as que sobram, prefere-se a do prazo pedido; e se nenhuma o tem, a
// primeira serve. ⚠️ Isso é seguro para o que daqui se lê — a Euribor, o
// indexante, a TAN da fase fixa e a base — porque nenhum deles depende do prazo
// nas observações que a grelha produz: a família do prazo varre só a variável, e
// aí a TAN não sai da linha, sai de Euribor + spread.
func (b *medidoDeUmBanco) observacaoDe(cenario string, prazoMeses int) (varrimento.Observacao, bool) {
	obs := b.porCenario[cenario]
	if len(obs) == 0 {
		return varrimento.Observacao{}, false
	}

	semProdutos := make([]varrimento.Observacao, 0, len(obs))
	for _, o := range obs {
		if len(o.Oferta.ProdutosAplicados) == 0 {
			semProdutos = append(semProdutos, o)
		}
	}
	if len(semProdutos) == 0 {
		// Só há linhas bonificadas deste cenário. Responder com uma delas era
		// servir um desconto a quem não o pediu; quem chama trata isto como
		// «não medido», que é o que de facto aconteceu ao preço de tabela.
		return varrimento.Observacao{}, false
	}

	for _, o := range semProdutos {
		if varrimento.PrazoAplicado(o) == prazoMeses {
			return o, true
		}
	}
	return semProdutos[0], true
}

// comProdutos aplica ao spread os descontos dos produtos escolhidos, e devolve
// os que não têm desconto medido.
//
// ⚠️ **Somam-se, e essa aditividade é uma assunção da §4 — não uma medição.** O
// varrimento gasta um ponto por banco (família 6) a medir a combinação de todos
// precisamente para a poder contradizer: se a soma dos descontos individuais não
// der o desconto da linha com todos, o resíduo denuncia-o. Enquanto não
// denunciar, somam-se.
func (b *medidoDeUmBanco) comProdutos(spread dominio.Taxa, escolhidos []string) (dominio.Taxa, []string) {
	var semMedida []string
	for _, produto := range escolhidos {
		desconto, medido := b.descontos[produto]
		if !medido {
			semMedida = append(semMedida, produto)
			continue
		}
		spread = spread.Add(desconto)
	}
	return spread, semMedida
}

// Bancos devolve os ids com que se pode responder, por ordem.
func (c *Catalogo) Bancos() []string {
	ids := make([]string, 0, len(c.porBanco))
	for id := range c.porBanco {
		ids = append(ids, id)
	}
	return ordenados(ids)
}

// Comparar responde com uma oferta por banco PEDIDO — e não por banco medido.
//
// `pedidos` são os ids que quem pergunta nomeou; vazio quer dizer todos os
// conhecidos. Um banco que não consegue responder vem como falha nomeada e não
// omitido, e um id que não é banco nenhum é ErrPedidoInvalido. O porquê de cada
// uma dessas três está na KAN-45 e na secção do POST /comparacoes do API.md.
func (c *Catalogo) Comparar(
	p dominio.Pedido, pedidos []string, requisitos map[string]dominio.Requisitos, hoje dominio.Data,
) ([]dominio.Oferta, error) {
	if err := p.Validar(hoje); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPedidoInvalido, err)
	}

	conhecidos := c.conhecidos(requisitos)
	if len(pedidos) == 0 {
		pedidos = conhecidos
	}
	for _, id := range pedidos {
		if !slices.Contains(conhecidos, id) {
			return nil, fmt.Errorf("%w: %w", ErrPedidoInvalido, &dominio.ErroValidacao{
				Campo:    "bancos",
				Mensagem: fmt.Sprintf("Não há nenhum banco com o identificador %q.", id),
			})
		}
	}

	ofertas := make([]dominio.Oferta, 0, len(pedidos))
	for _, id := range ordenados(slices.Clone(pedidos)) {
		ofertas = append(ofertas, c.ofertaDe(id, p, requisitos[id], hoje))
	}
	return ofertas, nil
}

// conhecidos são os bancos sobre que se pode dizer alguma coisa: os do registo
// mais os que a grelha mediu.
//
// ⚠️ É a UNIÃO dos dois e não só o registo, e isso resolve dois casos de uma vez
// sem um ramo de excepção: quem chama sem registo nenhum (o handler devolve um
// mapa vazio se o registo não se montar) continua a responder pelo que mediu, em
// vez de responder por nada; e um banco medido que já saiu do registo continua a
// aparecer, em vez de se evaporar — que é exactamente o defeito que esta issue
// corrige, só que pela outra ponta.
func (c *Catalogo) conhecidos(requisitos map[string]dominio.Requisitos) []string {
	ids := make([]string, 0, len(requisitos)+len(c.porBanco))
	for id := range requisitos {
		ids = append(ids, id)
	}
	for id := range c.porBanco {
		if _, jaLa := requisitos[id]; !jaLa {
			ids = append(ids, id)
		}
	}
	// ⚠️ Um banco posto de fora pela viragem do dia não está em `porBanco`, e sem
	// esta volta desaparecia da lista de todos — que é o defeito da KAN-45 a
	// entrar pela porta que a KAN-50 abriu.
	for id := range c.desactualizados {
		if _, jaLa := requisitos[id]; !jaLa {
			ids = append(ids, id)
		}
	}
	return ordenados(ids)
}

// ofertaDe monta a resposta de um banco.
func (c *Catalogo) ofertaDe(
	id string, p dominio.Pedido, req dominio.Requisitos, hoje dominio.Data,
) dominio.Oferta {
	banco, medido := c.porBanco[id]
	if !medido {
		nome := nomeNoRegisto(id, req)

		// ⚠️ Há preços deste banco e não se servem: são do outro lado da viragem
		// do dia, e a Euribor fixou entretanto (§7.3). Dizer `sem_serie` aqui era
		// afirmar que não se foi lá, quando se foi e se trouxe preço.
		if c.desactualizados[id] {
			return dominio.Falhar(id, nome, &dominio.ErroOferta{
				Codigo: dominio.ErroSerieDesactualizada,
				Mensagem: fmt.Sprintf(
					"Os preços que temos do %s são de antes da última viragem do dia, e a Euribor fixa "+
						"diariamente. Compará-los com os dos outros bancos seria comparar preços de dias "+
						"diferentes, por isso este fica de fora até ao próximo varrimento.", nome),
			})
		}

		// ⚠️ O banco existe e não foi varrido. A recusa nomeia-o e diz de quem é
		// a falta — nossa —, porque o contrário fazia a pessoa concluir que o
		// banco está em baixo. E é `sem_serie` e não `produto_indisponivel`: esse
		// é «varreu-se e não se mediu ESTE cenário», que é outra frase e outra
		// decisão para quem lê.
		return dominio.Falhar(id, nome, &dominio.ErroOferta{
			Codigo: dominio.ErroSemSerie,
			Mensagem: fmt.Sprintf(
				"Ainda não há preços varridos do %s, e por isso não se lhe conhece oferta para este "+
					"pedido. O banco não foi consultado — a falta é nossa e não dele.", nome),
		})
	}
	nome := banco.nome

	// 1. Os ajustes que o pedido leva para caber no que este banco pratica. São
	//    os mesmos que o varrimento faria, e saem dos Requisitos.
	pedido, ajustes, err := encaixar(p, req, hoje)
	if err != nil {
		return falhar(id, nome, err)
	}

	// 2. A linha da grelha que corresponde ao cenário pedido.
	cenario, err := grelha.CenarioDe(pedido)
	if err != nil {
		return falhar(id, nome, &dominio.ErroOferta{
			Codigo:   dominio.ErroProdutoIndisponivel,
			Mensagem: fmt.Sprintf("O %s não tem preço para este tipo de pedido: %v", nome, err),
		})
	}
	observada, temCenario := banco.observacaoDe(cenario.Chave(), pedido.PrazoAnos*12)
	if !temCenario {
		// ⚠️ Recusa-se este banco, e não se interpola nem se serve o vizinho. A
		// KAN-31 deixou a escolha em aberto entre servir o varrimento anterior,
		// recusar, ou interpolar; interpolar exigia erro medido, e servir outro
		// ponto é dar o preço de outro cenário. Recusar é a única que não mente,
		// e quem escolhe as observações (a infra) é que decide a frescura.
		return falhar(id, nome, &dominio.ErroOferta{
			Codigo: dominio.ErroProdutoIndisponivel,
			Mensagem: fmt.Sprintf(
				"O último varrimento do %s não mediu %s, e não se serve o preço de outro cenário no lugar deste.",
				nome, cenario.Chave()),
		})
	}

	// 3. O spread do intervalo de LTV de quem pergunta, mais o desconto dos
	//    produtos que ele escolheu.
	spread, notaDoDegrau, err := spreadPara(banco, pedido, nome)
	if err != nil {
		return falhar(id, nome, err)
	}
	spread, semMedida := banco.comProdutos(spread, pedido.ProdutosDoBanco(id))

	// 4. As taxas de cada fase, e o plano.
	oferta, err := montar(pedido, observada, banco.escala, spread, nome)
	if err != nil {
		return falhar(id, nome, err)
	}

	oferta.BancoID, oferta.BancoNome = id, nome
	oferta.CapturadoEm = observada.Oferta.CapturadoEm

	// ⚠️ A fiabilidade e a nota dela entram JUNTAS, e é por isso que são duas
	// linhas e não uma chamada a cada sítio: um `em_duvida` sem frase seria
	// exactamente o defeito que a KAN-49 corrige, uma camada acima.
	oferta.Fiabilidade = c.fiabilidade[id].Ou()
	if oferta.Fiabilidade == dominio.FiabilidadeEmDuvida {
		oferta.Anotar(dominio.NotaDaDuvida(nome))
	}

	for _, a := range ajustes {
		oferta.Acrescentar(a)
	}
	oferta.Anotar(notaDoDegrau)
	for _, produto := range semMedida {
		// ⚠️ Um produto escolhido cujo desconto não foi medido não desconta nada,
		// e diz-se. Aplicar-lhe zero em silêncio dava um preço plausível e mais
		// caro do que o real; inventar-lhe um desconto dava um mais barato do que
		// o real. A pessoa fica a saber que aquele produto não entrou na conta.
		oferta.Anotar(fmt.Sprintf(
			"O produto %q foi escolhido e não está reflectido neste preço: o último varrimento do %s "+
				"não mediu quanto ele desconta.", produto, nome))
	}

	// 5. A TAEG e o MTIC, derivados — e os pressupostos que os sustentam.
	derivarTAEG(&oferta, banco, pedido, nome)
	return oferta
}

// nomeNoRegisto dá o nome por que a pessoa conhece o banco, quando a grelha não
// o tem para dar.
//
// ⚠️ Cai para o id quando o registo também não o traz. É feio de propósito: o id
// numa frase em português denuncia o buraco a quem lê a resposta, e um «este
// banco» genérico escondia-o.
func nomeNoRegisto(id string, req dominio.Requisitos) string {
	if req.BancoNome != "" {
		return req.BancoNome
	}
	return id
}

// encaixar aplica ao pedido os limites do banco, devolvendo os ajustes.
func encaixar(p dominio.Pedido, req dominio.Requisitos, hoje dominio.Data) (dominio.Pedido, []*dominio.Ajuste, error) {
	var ajustes []*dominio.Ajuste

	// Sem requisitos declarados não se encaixa nada: seria inventar limites.
	if req.BancoID == "" {
		return p, nil, nil
	}

	idade, err := dominio.IdadeMaisVelho(p.Titulares, hoje)
	if err != nil {
		return p, nil, &dominio.ErroOferta{
			Codigo:   dominio.ErroProdutoIndisponivel,
			Mensagem: "O pedido não traz a data de nascimento de nenhum titular.",
		}
	}

	prazo, ajustePrazo, err := dominio.EncaixarPrazo(p.PrazoAnos, req, idade)
	if err != nil {
		return p, nil, err
	}
	p.PrazoAnos = prazo
	ajustes = append(ajustes, ajustePrazo)

	// O período fixo só existe na mista e na fixa.
	if p.TipoTaxa == dominio.TaxaMista || p.TipoTaxa == dominio.TaxaFixa {
		periodo, ajustePeriodo, err := dominio.EncaixarPeriodoFixo(req.PeriodosFixos, p.PeriodoFixoAnos, &prazo)
		if err != nil {
			return p, nil, &dominio.ErroOferta{
				Codigo:   dominio.ErroProdutoIndisponivel,
				Mensagem: fmt.Sprintf("Não há período de taxa fixa deste banco que caiba neste prazo: %v", err),
			}
		}
		p.PeriodoFixoAnos = &periodo
		ajustes = append(ajustes, ajustePeriodo)
	}

	return p, ajustes, nil
}

// spreadPara devolve o spread que o banco pratica no LTV do pedido, e a nota que
// tem de o acompanhar num degrau por resolver.
func spreadPara(banco *medidoDeUmBanco, p dominio.Pedido, nome string) (dominio.Taxa, string, error) {
	if !banco.temEscala {
		return dominio.Taxa{}, "", &dominio.ErroOferta{
			Codigo: dominio.ErroProdutoIndisponivel,
			Mensagem: fmt.Sprintf(
				"O último varrimento do %s não mediu a escala de LTV, e sem ela o preço deste banco "+
					"só valeria no rácio a que foi medido.", nome),
		}
	}

	ltv, err := dominio.LTV(p.Montante, p.ValorImovel)
	if err != nil {
		return dominio.Taxa{}, "", &dominio.ErroOferta{
			Codigo:   dominio.ErroRespostaIlegivel,
			Mensagem: fmt.Sprintf("Não se conseguiu calcular o LTV do pedido: %v", err),
		}
	}

	// ⚠️ Fora da escala é recusa e não extrapolação: o banco não preça ali, e o
	// degrau mais próximo é o preço de outro cliente.
	spread, nota, err := banco.escala.SpreadEm(ltv)
	if err != nil {
		return dominio.Taxa{}, "", &dominio.ErroOferta{
			Codigo:   dominio.ErroProdutoIndisponivel,
			Mensagem: fmt.Sprintf("O %s não financia neste rácio: %v", nome, err),
		}
	}
	return spread, nota, nil
}

// montar constrói a oferta a partir da observação e do spread.
//
// ⚠️ É aqui que a diferença entre as três modalidades se paga, e cada uma lê uma
// coisa diferente da linha observada:
//
//   - VARIÁVEL: a TAN é a Euribor medida mais o spread do intervalo de quem
//     pergunta. É a identidade que a §4 chama cálculo;
//   - MISTA: a fase fixa leva a TAN observada — é preço do período, e não
//     depende do LTV pela mesma via — e a fase indexada leva Euribor + spread;
//   - FIXA: a TAN é a BASE observada mais o spread do intervalo. Foi o
//     cartesiano de 2026-07-28 que o mediu, e é a razão de a linha guardar a
//     base e não a TAN (KAN-41).
func montar(
	p dominio.Pedido, observada varrimento.Observacao,
	escala dominio.EscalaDeLTV, spread dominio.Taxa, nome string,
) (dominio.Oferta, error) {
	meses := p.PrazoAnos * 12
	o := observada.Oferta

	var trechos []dominio.Trecho
	var oferta dominio.Oferta

	switch p.TipoTaxa {
	case dominio.TaxaVariavel:
		if o.EuriborValor == nil {
			return dominio.Oferta{}, incompleta(nome, "o valor da Euribor")
		}
		tan := o.EuriborValor.Add(spread)
		trechos = []dominio.Trecho{{Meses: meses, Anual: tan}}
		oferta.Spread, oferta.EuriborValor, oferta.Indexante = &spread, o.EuriborValor, o.Indexante

	case dominio.TaxaMista:
		if o.EuriborValor == nil || o.TAN == nil {
			return dominio.Oferta{}, incompleta(nome, "a TAN da fase fixa ou o valor da Euribor")
		}
		if p.PeriodoFixoAnos == nil {
			return dominio.Oferta{}, incompleta(nome, "o período de taxa fixa")
		}
		fixos := *p.PeriodoFixoAnos * 12
		if fixos >= meses {
			return dominio.Oferta{}, &dominio.ErroOferta{
				Codigo:   dominio.ErroProdutoIndisponivel,
				Mensagem: fmt.Sprintf("O período de taxa fixa não cabe no prazo pedido, no %s.", nome),
			}
		}
		indexada := o.EuriborValor.Add(spread)
		trechos = []dominio.Trecho{
			{Meses: fixos, Anual: *o.TAN},
			{Meses: meses - fixos, Anual: indexada},
		}
		oferta.Spread, oferta.EuriborValor, oferta.Indexante = &spread, o.EuriborValor, o.Indexante

	case dominio.TaxaFixa:
		// ⚠️ A base, e não a TAN observada. O cartesiano da CGD (2026-07-28)
		// mediu que a TAN da fixa muda com o LTV — nas mesmas fronteiras da
		// variável e com a mesma altura de degrau —, e servir a TAN medida a
		// quem cai noutro degrau erra 0,70 p.p. A base é o que sobra depois de
		// lhe descontar o spread do intervalo em que foi medida, e aqui
		// soma-se-lhe o spread do intervalo de quem pergunta (KAN-41).
		base, temBase := varrimento.BaseDaTaxaFixa(observada, escala)
		if !temBase {
			return dominio.Oferta{}, &dominio.ErroOferta{
				Codigo: dominio.ErroProdutoIndisponivel,
				Mensagem: fmt.Sprintf(
					"O último varrimento do %s não deixa extrair a base da taxa fixa, e sem ela o preço "+
						"só valeria no rácio a que foi medido.", nome),
			}
		}
		tan := base.Add(spread)
		trechos = []dominio.Trecho{{Meses: meses, Anual: tan}}
		// ⚠️ Sem spread nem indexante na oferta: numa fixa pura não há fase
		// indexada, e publicá-los era inventar. O spread entrou no cálculo da
		// TAN, que é outra coisa — e é o que a §4 chama consulta mais cálculo.

	default:
		return dominio.Oferta{}, incompleta(nome, "o tipo de taxa")
	}

	plano, err := dominio.PlanoFrances(p.Montante, trechos)
	if err != nil {
		return dominio.Oferta{}, &dominio.ErroOferta{
			Codigo:   dominio.ErroRespostaIlegivel,
			Mensagem: fmt.Sprintf("Não se conseguiu montar o plano de prestações do %s: %v", nome, err),
		}
	}

	oferta.Fases = plano.Fases
	tan := plano.Fases[0].Taxa
	prestacao := plano.Fases[0].Prestacao
	oferta.TAN, oferta.Prestacao = &tan, &prestacao
	oferta.ProdutosAplicados = p.ProdutosDoBanco(observada.Oferta.BancoID)
	return oferta, nil
}

// derivarTAEG preenche a TAEG e o MTIC a partir dos encargos ajustados, e
// declara os pressupostos que os sustentam.
//
// ⚠️ **Nunca um sem os outros.** A §4 mudou de posição a 2026-07-28 — deixou de
// mandar omitir a TAEG e passou a permitir derivá-la —, mas a condição em que o
// permite é esta: os números vão com as hipóteses declaradas ao lado. Um deles
// sem os outros é o que a §4 chama enganar.
//
// Sem encargos ajustados **não se estima nada**: a oferta sai sem TAEG e sem
// MTIC, com uma nota que diz porquê. Um banco cujas observações não identificam
// a repartição dos encargos não tem TAEG derivável, e inventar uma repartição
// era exactamente o que o `dominio.AjustarEncargos` recusa fazer.
func derivarTAEG(oferta *dominio.Oferta, banco *medidoDeUmBanco, p dominio.Pedido, nome string) {
	if !oferta.Sucesso() || len(oferta.Fases) == 0 {
		return
	}
	if !banco.temEncargos {
		oferta.Anotar(fmt.Sprintf(
			"A TAEG e o MTIC não são apresentados para o %s: seriam calculados a partir dos encargos, e o "+
				"último varrimento não observou este banco em prazos suficientemente diferentes para os "+
				"separar. Um número aqui seria uma repartição escolhida por nós, e não medida.", nome))
		return
	}

	taeg, mtic, err := banco.encargos.Aplicar(p.Montante, trechosDe(oferta.Fases))
	if err != nil {
		oferta.Anotar(fmt.Sprintf(
			"A TAEG e o MTIC não são apresentados para o %s: não se conseguiram derivar do plano (%v).", nome, err))
		return
	}

	oferta.TAEG, oferta.MTIC = &taeg, &mtic
	oferta.Pressupor(banco.encargos.Pressupostos(p.Montante)...)
}

// trechosDe converte as fases acumuladas do plano em troços de duração, que é o
// que o motor de amortização recebe.
//
// ⚠️ A conversão faz-se aqui e num sítio só. É a mesma distinção que o
// `FaseDuracao` já regista: o contrato publica acumulado porque é o que a app
// mostra, e o cálculo pede duração porque é o que ele desenrola.
func trechosDe(fases []dominio.Fase) []dominio.Trecho {
	trechos := make([]dominio.Trecho, 0, len(fases))
	anterior := 0
	for _, f := range fases {
		trechos = append(trechos, dominio.Trecho{Meses: f.AteMes - anterior, Anual: f.Taxa})
		anterior = f.AteMes
	}
	return trechos
}

func incompleta(nome, oQue string) error {
	return &dominio.ErroOferta{
		Codigo: dominio.ErroRespostaIlegivel,
		Mensagem: fmt.Sprintf(
			"O último varrimento do %s não trouxe %s, e sem isso não há preço a servir.", nome, oQue),
	}
}

func falhar(id, nome string, err error) dominio.Oferta {
	var estruturado *dominio.ErroOferta
	if errors.As(err, &estruturado) {
		return dominio.Falhar(id, nome, estruturado)
	}
	return dominio.Falhar(id, nome, &dominio.ErroOferta{
		Codigo:   dominio.ErroProdutoIndisponivel,
		Mensagem: fmt.Sprintf("Não se conseguiu responder pelo %s: %v", nome, err),
	})
}

// ordenados dá uma ordem estável à lista de bancos.
//
// ⚠️ Alfabética e não a do mapa: a de um mapa é aleatória a cada corrida, e uma
// lista de ofertas que troca de ordem sozinha faz a resposta parecer instável a
// quem a lê do outro lado. A ordenação por PREÇO é outra coisa, e é a KAN-26 —
// que continua aberta porque ordenar uma oferta ajustada ao lado das outras
// compara coisas diferentes sem o dizer.
func ordenados(ids []string) []string {
	slices.Sort(ids)
	return ids
}
