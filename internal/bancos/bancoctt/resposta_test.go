package bancoctt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// Todos os testes deste ficheiro correm offline, contra `capturas/` — respostas
// reais do Banco CTT gravadas a 2026-07-27. O parser é puro, por isso não é
// preciso banco nem transporte: é o corpo que entra e a oferta que sai.

func TestVariavelLeOSpreadContratualEAEuriborDaResposta(t *testing.T) {
	t.Parallel()

	o := lida(t, "variavel_12m", pedido(dominio.TaxaVariavel, 0))

	confereTaxa(t, "TAN", o.TAN, "4.148")
	confereTaxa(t, "spread", o.Spread, "1.35")
	confereTaxa(t, "Euribor", o.EuriborValor, "2.798")
	confereTaxa(t, "TAEG", o.TAEG, "4.7")
	if o.Indexante != dominio.Euribor12M {
		t.Errorf("indexante = %q, e a resposta diz «EURIBOR 12M»", o.Indexante)
	}
	if len(o.Fases) != 1 {
		t.Fatalf("a variável tem uma fase e saíram %d", len(o.Fases))
	}
	if o.Fases[0].AteMes != 360 {
		t.Errorf("a fase acaba ao mês %d, e o prazo é 360", o.Fases[0].AteMes)
	}
}

func TestMistaNaoConfundeOSpreadDaFaseFixaComOContratual(t *testing.T) {
	t.Parallel()

	// ⚠️ É o teste que dá razão de ser a este parser. Na mista o banco preenche
	// `SpreadWithoutBenefits` com 4,000 — a própria TAN da fase fixa, que não
	// tem indexante. O spread contratual é o da fase indexada: 1,350.
	//
	// Publicar 4,000 punha o Banco CTT com o triplo do spread dos outros bancos
	// numa tabela de comparação, e cada número estaria «certo».
	o := lida(t, "mista_5a", pedido(dominio.TaxaMista, 5))

	confereTaxa(t, "spread", o.Spread, "1.35")
	confereTaxa(t, "Euribor", o.EuriborValor, "2.339")
	if o.Indexante != dominio.Euribor3M {
		t.Errorf("indexante = %q, e a resposta diz «EURIBOR 3M» na fase indexada", o.Indexante)
	}

	// A TAN de topo é a da PRIMEIRA fase — o que o cliente começa a pagar.
	confereTaxa(t, "TAN", o.TAN, "4")

	if len(o.Fases) != 2 {
		t.Fatalf("a mista tem duas fases e saíram %d", len(o.Fases))
	}
	if o.Fases[0].AteMes != 60 {
		t.Errorf("a fase fixa acaba ao mês %d, e o banco diz 60", o.Fases[0].AteMes)
	}
	if o.Fases[1].AteMes != 360 {
		t.Errorf("a fase indexada acaba ao mês %d, e o prazo é 360", o.Fases[1].AteMes)
	}
	if got := o.Fases[1].Taxa.String(); got != "3.689" {
		t.Errorf("a fase indexada leva a TAN %s, e o banco diz 3,689", got)
	}
}

func TestAIdentidadeDaFaseIndexadaFecha(t *testing.T) {
	t.Parallel()

	// A §4 diz que a TAN da fase indexada é Euribor + spread. Está medido que
	// fecha exactamente nos três bancos escritos, e aqui afirma-se no quarto —
	// ⚠️ na FASE e não na TAN de topo: na mista a de topo é a da fase fixa, e
	// comparar aí dava um desvio falso a cada oferta mista.
	for _, caso := range []struct {
		captura string
		pedido  dominio.Pedido
		fase    int
	}{
		{"variavel_12m", pedido(dominio.TaxaVariavel, 0), 0},
		{"mista_5a", pedido(dominio.TaxaMista, 5), 1},
	} {
		o := lida(t, caso.captura, caso.pedido)
		if o.Spread == nil || o.EuriborValor == nil {
			t.Fatalf("%s: sem spread ou sem Euribor", caso.captura)
		}
		soma := o.EuriborValor.Add(*o.Spread)
		taxa := o.Fases[caso.fase].Taxa
		if !taxa.Equal(soma) {
			t.Errorf("%s: a fase indexada leva %s e Euribor+spread dá %s — resíduo %s",
				caso.captura, taxa, soma, taxa.Sub(soma))
		}
	}
}

func TestFixaNaoPublicaSpreadNemEuribor(t *testing.T) {
	t.Parallel()

	// ⚠️ A mesma armadilha, no seu caso mais puro: na fixa a 30 anos o banco
	// devolve `Spread` 4,650, igual à TAN. Não há indexante nenhum sobre o qual
	// isso seja um spread, e a §4 manda as colunas nulas.
	o := lida(t, "fixa_30a", pedido(dominio.TaxaFixa, 0))

	confereTaxa(t, "TAN", o.TAN, "4.65")
	if o.Spread != nil {
		t.Errorf("a fixa publicou spread %s, e numa fixa pura não há spread sobre indexante", o.Spread)
	}
	if o.EuriborValor != nil {
		t.Errorf("a fixa publicou Euribor %s, e não há indexante", o.EuriborValor)
	}
	if o.Indexante != "" {
		t.Errorf("a fixa publicou o indexante %q", o.Indexante)
	}
}

func TestOTenorSaiDaRespostaENaoDoNossoMapa(t *testing.T) {
	t.Parallel()

	// O banco impõe 12M na variável, e é por isso mesmo que se lê o que ele
	// aplicou: se ele mudar a omissão, aparece — em vez de a nossa constante
	// passar a mentir em silêncio.
	corpo := comCampo(t, "variavel_12m", `"IndexRateDescription": "EURIBOR 12M"`, `"IndexRateDescription": "EURIBOR 6M"`)

	o, err := lerResposta(corpo, pedido(dominio.TaxaVariavel, 0))
	if err != nil {
		t.Fatalf("lerResposta: %v", err)
	}
	if o.Indexante != dominio.Euribor6M {
		t.Errorf("indexante = %q, e a resposta passou a dizer «EURIBOR 6M»", o.Indexante)
	}
}

func TestUmaEtiquetaDeIndexanteDesconhecidaFalhaAlto(t *testing.T) {
	t.Parallel()

	// Um tenor que não se reconhece não vira omissão: sem saber a que Euribor o
	// preço se refere, o número não é comparável com o de banco nenhum.
	corpo := comCampo(t, "variavel_12m", `"IndexRateDescription": "EURIBOR 12M"`, `"IndexRateDescription": "EURIBOR 9M"`)

	if _, err := lerResposta(corpo, pedido(dominio.TaxaVariavel, 0)); err == nil {
		t.Fatal("um tenor desconhecido passou sem erro")
	}
}

func TestOsProdutosEscolhemAColunaDoPreco(t *testing.T) {
	t.Parallel()

	// Quem escolhe é o pedido, não o banco (KAN-33). Sem produtos serve-se o
	// preçário base; com eles, a coluna bonificada.
	base := lida(t, "variavel_12m", pedido(dominio.TaxaVariavel, 0))

	p := pedido(dominio.TaxaVariavel, 0)
	p.Produtos = []string{ProdutoVendasAssociadas}
	comProdutos := lida(t, "variavel_12m", p)

	if base.Spread == nil || comProdutos.Spread == nil {
		t.Fatal("faltou spread numa das colunas")
	}
	if base.Spread.Cmp(*comProdutos.Spread) <= 0 {
		t.Errorf("o preço com vendas associadas (%s) não é mais barato do que o base (%s)",
			comProdutos.Spread, base.Spread)
	}
	if len(comProdutos.ProdutosAplicados) != 1 {
		t.Errorf("gravaram-se %d produtos aplicados, e escolheu-se 1", len(comProdutos.ProdutosAplicados))
	}
	if len(base.ProdutosAplicados) != 0 {
		t.Errorf("um pedido sem produtos gravou %d aplicados", len(base.ProdutosAplicados))
	}
}

func TestOPrecoDizSempreOQuePressupoe(t *testing.T) {
	t.Parallel()

	// Sem nota, um preço bonificado ao lado do preçário base de outro banco
	// compara coisas diferentes sem o dizer.
	for _, produtos := range [][]string{nil, {ProdutoVendasAssociadas}} {
		p := pedido(dominio.TaxaVariavel, 0)
		p.Produtos = produtos
		if o := lida(t, "variavel_12m", p); len(o.Notas()) == 0 {
			t.Errorf("produtos=%v: a oferta saiu sem nota nenhuma sobre as vendas associadas", produtos)
		}
	}
}

func TestUmaRecusaNaoViraOferta(t *testing.T) {
	t.Parallel()

	corpo := []byte(`{"success":false,"data":null,"error":"Não existem condições disponíveis"}`)

	_, err := lerResposta(corpo, pedido(dominio.TaxaVariavel, 0))
	if err == nil {
		t.Fatal("uma recusa passou como oferta")
	}
}

// --- utilitários ---------------------------------------------------------------

func lida(t *testing.T, captura string, p dominio.Pedido) dominio.Oferta {
	t.Helper()
	o, err := lerResposta(corpoDe(t, captura), p)
	if err != nil {
		t.Fatalf("lerResposta(%s): %v", captura, err)
	}
	return o
}

func corpoDe(t *testing.T, captura string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("capturas", captura+".resposta.json"))
	if err != nil {
		t.Fatalf("ler a captura %s: %v", captura, err)
	}
	return b
}

// comCampo troca um campo da captura, para afirmar o que o parser faz quando o
// banco muda o que devolve. ⚠️ A troca é verificada: um `velho` que não exista
// deixaria o teste a afirmar sobre a captura intacta, e a passar por engano.
func comCampo(t *testing.T, captura, velho, novo string) []byte {
	t.Helper()
	corpo := string(corpoDe(t, captura))
	if !strings.Contains(corpo, velho) {
		t.Fatalf("a captura %s não contém %q — o teste não estaria a trocar nada", captura, velho)
	}
	return []byte(strings.Replace(corpo, velho, novo, 1))
}

func pedido(tipo dominio.TipoTaxa, periodoFixo int) dominio.Pedido {
	p := dominio.Pedido{
		ValorImovel: dominio.DinheiroDeInteiro(250_000),
		Montante:    dominio.DinheiroDeInteiro(200_000),
		PrazoAnos:   30,
		TipoTaxa:    tipo,
		Finalidade:  dominio.FinalidadePropria,
		Localizacao: dominio.LocalizacaoContinente,
	}
	if periodoFixo > 0 {
		p.PeriodoFixoAnos = &periodoFixo
	}
	return p
}

func confereTaxa(t *testing.T, nome string, medida *dominio.Taxa, esperado string) {
	t.Helper()
	if medida == nil {
		t.Errorf("%s: veio nula, e a captura traz %s", nome, esperado)
		return
	}
	if got := medida.String(); got != esperado {
		t.Errorf("%s = %s, e a captura traz %s", nome, got, esperado)
	}
}
