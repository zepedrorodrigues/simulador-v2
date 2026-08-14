package web

import (
	"fmt"

	"github.com/shopspring/decimal"

	"github.com/zepedrorodrigues/simulador-v2/api"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// A tradução entre o domínio e o contrato.
//
// ⚠️ Os números saem em `float64` **aqui e só aqui**. O domínio e a base
// trabalham em decimal de ponta a ponta, e a conversão faz-se no último passo
// antes do JSON — que é onde o contrato manda (§4: «a conversão para float64
// faz-se no tipo de saída»). Converter mais cedo punha vírgula flutuante a
// atravessar o cálculo.

// ofertaDe traduz uma oferta do domínio.
//
// ⚠️ **Uma oferta sem instante de captura não é servida como sucesso.** O número
// existe, mas apresentá-lo sem dizer quando foi cotado é apresentá-lo como sendo
// de agora — e num acerto de cache pode ser de há cinco minutos. Desce a falha,
// com o código a dizer porquê.
//
// ⚠️ Foi a guarda que apanhou o `aovivo` a nascer sem o carimbo do `CapturadoEm`:
// os testes do caso de uso passavam todos, porque nenhum ia à fronteira.
func ofertaDe(o dominio.Oferta) api.Oferta {
	saida := api.Oferta{
		BancoId:   o.BancoID,
		BancoNome: o.BancoNome,
		Sucesso:   o.Sucesso(),
	}

	// ⚠️ **`erro_interno` e não `resposta_ilegivel`** (KAN-60). O `CapturadoEm` é
	// carimbado por NÓS — o `dominio.Oferta` di-lo por escrito e o `aovivo.Pedir`
	// fá-lo —, portanto a falta é nossa e o banco pode ter respondido
	// perfeitamente. Com o código antigo, o defeito de 2026-08-07 saiu a acusar
	// cinco bancos de responderem coisa ilegível.
	if o.Sucesso() && o.CapturadoEm.IsZero() {
		return api.Oferta{
			BancoId:   o.BancoID,
			BancoNome: o.BancoNome,
			Sucesso:   false,
			Erro: &api.OfertaErro{
				Codigo: string(dominio.ErroInterno),
				Mensagem: fmt.Sprintf(
					"A oferta do %s não diz de quando é o preço, e um preço sem data apresenta-se "+
						"como se fosse de agora. Não é servida. A falha é nossa, não do banco.", o.BancoNome),
			},
		}
	}

	if !o.Sucesso() {
		saida.Erro = &api.OfertaErro{Codigo: string(o.Erro.Codigo), Mensagem: o.Erro.Mensagem}
		// ⚠️ As notas vão mesmo numa falha: elas dizem o que se tentou e porque
		// não deu, e é isso que distingue «este banco não faz isto» de «este
		// banco está em baixo».
		saida.Notas = listaOuNil(o.Notas())
		return saida
	}

	capturado := o.CapturadoEm.UTC()
	saida.CapturadoEm = &capturado

	saida.Tan = taxaDe(o.TAN)
	saida.Taeg = taxaDe(o.TAEG)
	saida.Spread = taxaDe(o.Spread)
	saida.EuriborValor = taxaDe(o.EuriborValor)
	saida.PrestacaoMensal = dinheiroDe(o.Prestacao)
	saida.Mtic = dinheiroDe(o.MTIC)
	saida.Notas = listaOuNil(o.Notas())
	saida.ProdutosAplicados = listaOuNil(o.ProdutosAplicados)

	if o.Indexante != "" {
		indexante := string(o.Indexante)
		saida.EuriborIndexante = &indexante
	}
	if aplicado := o.Aplicado(); len(aplicado) > 0 {
		saida.Aplicado = &aplicado
	}
	if len(o.Fases) > 0 {
		fases := make([]api.Fase, 0, len(o.Fases))
		for _, f := range o.Fases {
			fases = append(fases, api.Fase{
				AteMes:    f.AteMes,
				Taxa:      float64De(f.Taxa.Decimal()),
				Prestacao: float64De(f.Prestacao.Decimal()),
			})
		}
		saida.Fases = &fases
	}
	return saida
}

// bancoDe traduz os requisitos de um banco para o que a app precisa de saber.
func bancoDe(r dominio.Requisitos) api.Banco {
	b := api.Banco{
		Id:                r.BancoID,
		Nome:              r.BancoNome,
		IdadeMaximaFim:    r.IdadeMaximaFim,
		PrazoMin:          r.PrazoMin,
		PrazoMax:          r.PrazoMax,
		PeriodosFixos:     r.PeriodosFixos,
		PeriodosFixosModo: api.BancoPeriodosFixosModo(r.PeriodosFixosModo),
		EuriborOpcoes:     make([]string, 0, len(r.EuriborOpcoes)),
		Inputs:            make([]api.BancoInput, 0, len(r.Inputs)),
		Produtos:          make([]api.Produto, 0, len(r.Produtos)),
		Notas:             r.Notas,
	}
	if b.PeriodosFixos == nil {
		b.PeriodosFixos = []int{}
	}
	if b.Notas == nil {
		b.Notas = []string{}
	}
	for _, i := range r.EuriborOpcoes {
		b.EuriborOpcoes = append(b.EuriborOpcoes, string(i))
	}
	// ⚠️ Vazio não é «não sei»: é «o banco impõe o seu e ignora a escolha», e é
	// por isso que o imposto vai ao lado em vez de se fingir uma opção única.
	if r.EuriborImposto != "" {
		imposto := string(r.EuriborImposto)
		b.EuriborImposto = &imposto
	}
	for _, i := range r.Inputs {
		entrada := api.BancoInput{Chave: string(i.Campo), Usa: i.Usa}
		if i.Nota != "" {
			nota := i.Nota
			entrada.Nota = &nota
		}
		b.Inputs = append(b.Inputs, entrada)
	}
	for _, p := range r.Produtos {
		b.Produtos = append(b.Produtos, api.Produto{
			Id: p.ID, Rotulo: p.Rotulo, Descricao: p.Descricao, PorOmissao: p.PorOmissao,
		})
	}
	return b
}

func inputsCanonicos() []api.InputCanonico {
	canonicos := dominio.InputsCanonicos()
	saida := make([]api.InputCanonico, 0, len(canonicos))
	for _, i := range canonicos {
		saida = append(saida, api.InputCanonico{
			Chave: string(i.Campo), Rotulo: i.Rotulo, Tipo: string(i.Tipo),
		})
	}
	return saida
}

// pedidoDe traduz o corpo do pedido para o domínio.
//
// ⚠️ Os montantes chegam como `float64` — é o que o JSON transporta — e passam a
// decimal pelo TEXTO e não pelo valor: `decimal.NewFromFloat` de 250000.1 daria
// os dígitos que o float tem, e não os que o cliente escreveu.
// ⚠️ **Recebia um `api.ComparacaoPedido`** — o corpo do `/comparacoes`, que
// levava o pedido e os produtos agrupados por banco. Essa rota saiu com o
// varrimento (Fase 6, passo 5), e o último chamador construía o wrapper só para
// o desmontar aqui. Passa a receber as duas coisas que usa.
func pedidoDe(bruto api.Pedido, produtos map[string][]string) (dominio.Pedido, error) {
	p := dominio.Pedido{
		ValorImovel: dinheiroDeFloat(bruto.ValorImovel),
		Montante:    dinheiroDeFloat(bruto.Montante),
		PrazoAnos:   bruto.PrazoAnos,
		TipoTaxa:    dominio.TipoTaxa(bruto.RateType),
		Finalidade:  dominio.Finalidade(bruto.Finalidade),
		Localizacao: dominio.Localizacao(bruto.Localizacao),
	}
	if bruto.FixedPeriodYears != nil {
		anos := *bruto.FixedPeriodYears
		p.PeriodoFixoAnos = &anos
	}
	if bruto.EuriborIndexante != nil {
		p.Indexante = dominio.Indexante(*bruto.EuriborIndexante)
	}
	if bruto.GarantiaPublica != nil {
		p.GarantiaPublica = *bruto.GarantiaPublica
	}
	if bruto.JaCliente != nil {
		p.JaCliente = *bruto.JaCliente
	}

	for i, t := range bruto.Titulares {
		nascimento, err := dominio.DataDeTexto(t.DataNascimento.String())
		if err != nil {
			return dominio.Pedido{}, fmt.Errorf("titular %d: data de nascimento inválida: %w", i+1, err)
		}
		p.Titulares = append(p.Titulares, dominio.Titular{
			DataNascimento:   nascimento,
			RendimentoMensal: dinheiroDeFloat(t.RendimentoMensal),
		})
	}

	// ⚠️ Os produtos chegam agrupados por banco e o Pedido leva-os numa lista
	// só, prefixada. É o `ProdutosDoBanco` que os reparte de volta, e a chave do
	// mapa tem de bater com o prefixo — senão o produto pertence a um banco que
	// não o reclama e não é aplicado por ninguém.
	for bancoID, ids := range produtos {
		for _, id := range ids {
			if !dominio.ProdutoDoBanco(id, bancoID) {
				return dominio.Pedido{}, fmt.Errorf(
					"o produto %q foi pedido para o banco %q e não lhe pertence", id, bancoID)
			}
			p.Produtos = append(p.Produtos, id)
		}
	}
	return p, nil
}

// --- as conversões ----------------------------------------------------------------

func taxaDe(t *dominio.Taxa) *float64 {
	if t == nil {
		return nil
	}
	v := float64De(t.Decimal())
	return &v
}

func dinheiroDe(d *dominio.Dinheiro) *float64 {
	if d == nil {
		return nil
	}
	v := float64De(d.Decimal())
	return &v
}

// float64De é a última conversão antes do JSON.
//
// ⚠️ O contrato publica NÚMEROS e não strings — o v1 (Python) enviava números, e
// o `shopspring/decimal` serializa com aspas por omissão. É por isso que o tipo
// de saída é float64 e a conversão é aqui, à porta.
func float64De(d decimal.Decimal) float64 {
	v, _ := d.Float64()
	return v
}

func dinheiroDeFloat(v float64) dominio.Dinheiro {
	return dominio.DinheiroDeDecimal(decimal.NewFromFloat(v))
}

// listaOuNil devolve nil quando a lista está vazia, para o campo desaparecer do
// JSON em vez de sair como `[]`.
//
// ⚠️ Excepto onde o contrato exige lista sempre — e aí é o próprio contrato que
// o diz, no `/api/rate-catalog`. Aqui, um campo ausente e uma lista vazia querem
// dizer a mesma coisa, e a ausência é mais barata de ler.
func listaOuNil(l []string) *[]string {
	if len(l) == 0 {
		return nil
	}
	return &l
}
