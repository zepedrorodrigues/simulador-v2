package santander

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// Todos os testes deste ficheiro correm offline, contra `capturas/` — respostas
// reais do Santander gravadas a 2026-07-28. O parser é puro: é o corpo que entra
// e a oferta que sai.
//
// ⚠️ Teste INTERNO ao pacote, como o do payload do Banco CTT: o que se afirma é a
// leitura antes de haver transporte nenhum.

func TestAVariavelLeOSpreadQueVIGORAENaoOPromocional(t *testing.T) {
	// ⚠️ É a armadilha deste banco. No plano bonificado, o spread é 0,5 nos
	// primeiros 36 meses e 0,8 a partir do 37.º — sobre o mesmo indexante. O
	// catálogo anuncia «SPREAD PROMOCIONAL 0,5%», e publicá-lo seria anunciar o
	// preço de três anos de trinta.
	o := ler(t, "variavel_6m", comProdutos(pedidoBase()))

	verTaxa(t, "spread", o.Spread, "0.8")
	verTaxa(t, "Euribor", o.EuriborValor, "2.596")
	if o.Indexante != dominio.Euribor6M {
		t.Errorf("indexante = %q, e o Santander impõe a Euribor a 6 meses", o.Indexante)
	}

	// A TAN de topo continua a ser a da primeira fase — é o que o cliente começa
	// a pagar —, e é por isso que ela e o spread não fecham a identidade.
	verTaxa(t, "TAN", o.TAN, "3.096")
	verDinheiro(t, "prestação", o.Prestacao, "853.6")
}

func TestOPromocionalTemporarioEDeclarado(t *testing.T) {
	o := ler(t, "variavel_6m", comProdutos(pedidoBase()))

	if !contemNota(o, "sobe de 0.5 para 0.8") {
		t.Errorf("a subida do spread passou em silêncio: %v", o.Notas())
	}
	if !contemNota(o, "36 meses") {
		t.Errorf("a nota não diz quantos meses dura o promocional: %v", o.Notas())
	}
}

// TestSemProdutosNaoHaPromocional: o plano não bonificado tem spread 1,9
// constante, e não há nota nenhuma a dar.
func TestSemProdutosNaoHaPromocional(t *testing.T) {
	o := ler(t, "variavel_6m", pedidoBase())

	verTaxa(t, "spread", o.Spread, "1.9")
	verTaxa(t, "TAN", o.TAN, "4.496")
	if contemNota(o, "sobe de") {
		t.Errorf("anunciou-se uma subida de spread num plano de spread constante: %v", o.Notas())
	}
}

func TestAMistaLeOSpreadDaFaseIndexadaENaoODaFixa(t *testing.T) {
	// A primeira fase é fixa (M28, 2,85 %) e a segunda é indexada (6EM). O
	// spread contratual é o da segunda — é a mesma regra que resolve o
	// promocional da variável, e é por isso que é uma regra e não dois casos.
	o := ler(t, "mista_3a", comProdutos(comMista(pedidoBase(), 3)))

	verTaxa(t, "spread", o.Spread, "0.8")
	verTaxa(t, "Euribor", o.EuriborValor, "2.596")
	verTaxa(t, "TAN da fase fixa", o.TAN, "2.85")

	if len(o.Fases) != 2 {
		t.Fatalf("a mista tem duas fases, vieram %d", len(o.Fases))
	}
	// ⚠️ O banco diz onde cada troço COMEÇA (1 e 37) e o domínio guarda onde
	// ACABA: 36 e 360.
	if o.Fases[0].AteMes != 36 {
		t.Errorf("a fase fixa acaba ao mês %d, e o troço seguinte começa no 37", o.Fases[0].AteMes)
	}
	if o.Fases[1].AteMes != 360 {
		t.Errorf("a última fase acaba ao mês %d, e o contrato é de 30 anos", o.Fases[1].AteMes)
	}
}

// TestAFixaPuraNaoPublicaSpreadNemEuribor: nenhum troço tem indexante Euribor —
// o `P30` é uma taxa do próprio banco.
func TestAFixaPuraNaoPublicaSpreadNemEuribor(t *testing.T) {
	o := ler(t, "fixa_30a", pedidoBase())

	if o.Spread != nil || o.EuriborValor != nil || o.Indexante != "" {
		t.Errorf("a fixa publicou spread %v, Euribor %v e indexante %q — não tem fase indexada",
			o.Spread, o.EuriborValor, o.Indexante)
	}
	verTaxa(t, "TAN", o.TAN, "4.4")
	if len(o.Fases) != 1 {
		t.Errorf("a fixa a 30 anos tem uma fase, vieram %d", len(o.Fases))
	}
}

// TestNaFixaABonificacaoNaoDescontaEDizSe é a terceira descoberta de 2026-07-28.
func TestNaFixaABonificacaoNaoDescontaEDizSe(t *testing.T) {
	sem := ler(t, "fixa_30a", pedidoBase())
	com := ler(t, "fixa_30a", comProdutos(pedidoBase()))

	if sem.MTIC == nil || com.MTIC == nil {
		t.Fatal("faltou o MTIC")
	}
	if !sem.MTIC.Equal(*com.MTIC) {
		t.Fatalf("os dois planos da fixa deviam custar o mesmo: %s e %s", sem.MTIC, com.MTIC)
	}
	// ⚠️ Deixar passar em silêncio fazia a pessoa acreditar que domiciliar o
	// ordenado lhe valeu alguma coisa neste produto.
	if !contemNota(com, "não descontam nada aqui") {
		t.Errorf("a bonificação inútil passou em silêncio: %v", com.Notas())
	}
}

func TestOsDoisPlanosVemNoMesmoCorpo(t *testing.T) {
	// É o que torna este banco barato de varrer: um pedido dá as duas linhas.
	sem := ler(t, "variavel_6m", pedidoBase())
	com := ler(t, "variavel_6m", comProdutos(pedidoBase()))

	if sem.TAEG == nil || com.TAEG == nil {
		t.Fatal("faltou a TAEG")
	}
	if com.TAEG.Cmp(*sem.TAEG) >= 0 {
		t.Errorf("o plano bonificado (TAEG %s) devia ser mais barato do que o outro (%s)", com.TAEG, sem.TAEG)
	}
	if len(com.ProdutosAplicados) != 1 || com.ProdutosAplicados[0] != ProdutoBonificado {
		t.Errorf("os produtos aplicados são %v", com.ProdutosAplicados)
	}
}

func TestUmaEntradaInvalidaNaoViraOferta(t *testing.T) {
	// ⚠️ `isValid: false` é o banco a dizer que não faz aquela combinação. Ler
	// os números na mesma daria um preço que ele não pratica.
	corpo := strings.ReplaceAll(string(captura(t, "variavel_6m.resposta.json")), `"isValid": true`, `"isValid": false`)

	if _, err := lerResposta([]byte(corpo), pedidoBase()); err == nil {
		t.Fatal("uma entrada marcada como inválida virou oferta")
	}
}

func TestUmCorpoVazioNaoViraOferta(t *testing.T) {
	if _, err := lerResposta([]byte(`[]`), pedidoBase()); err == nil {
		t.Fatal("um corpo sem entradas virou oferta")
	}
}

// --- ajudantes ------------------------------------------------------------------

func ler(t *testing.T, nome string, p dominio.Pedido) dominio.Oferta {
	t.Helper()
	o, err := lerResposta(captura(t, nome+".resposta.json"), p)
	if err != nil {
		t.Fatalf("lerResposta(%s): %v", nome, err)
	}
	return o
}

func captura(t *testing.T, nome string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("capturas", nome))
	if err != nil {
		t.Fatalf("ler a captura %s: %v", nome, err)
	}
	return b
}

func pedidoBase() dominio.Pedido {
	return dominio.Pedido{
		ValorImovel: dominio.DinheiroDeInteiro(250_000),
		Montante:    dominio.DinheiroDeInteiro(200_000),
		PrazoAnos:   30,
		TipoTaxa:    dominio.TaxaVariavel,
		Finalidade:  dominio.FinalidadePropria,
		Localizacao: dominio.LocalizacaoContinente,
	}
}

func comProdutos(p dominio.Pedido) dominio.Pedido {
	p.Produtos = append(p.Produtos, ProdutoBonificado)
	return p
}

func comMista(p dominio.Pedido, periodo int) dominio.Pedido {
	p.TipoTaxa = dominio.TaxaMista
	p.PeriodoFixoAnos = &periodo
	return p
}

func verTaxa(t *testing.T, nome string, obtida *dominio.Taxa, esperada string) {
	t.Helper()
	if obtida == nil {
		t.Fatalf("%s: veio nulo, esperava %s", nome, esperada)
	}
	if obtida.String() != esperada {
		t.Errorf("%s = %s, esperava %s", nome, obtida, esperada)
	}
}

func verDinheiro(t *testing.T, nome string, obtido *dominio.Dinheiro, esperado string) {
	t.Helper()
	if obtido == nil {
		t.Fatalf("%s: veio nulo, esperava %s", nome, esperado)
	}
	if obtido.String() != esperado {
		t.Errorf("%s = %s, esperava %s", nome, obtido, esperado)
	}
}

func contemNota(o dominio.Oferta, pedaco string) bool {
	for _, n := range o.Notas() {
		if strings.Contains(n, pedaco) {
			return true
		}
	}
	return false
}
