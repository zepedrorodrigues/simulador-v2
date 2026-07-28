package bancoctt

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// O passo 5 do CONTRATO-BANCO.md §7: «`pedido.go` + teste que compara com o
// payload capturado».
//
// ⚠️ Teste INTERNO ao pacote (`package bancoctt`), ao contrário do
// `resposta_test.go`. O que se afirma é o corpo que sai daqui antes de haver
// transporte nenhum — e o payload é, e deve continuar a ser, privado: quem o
// quiser ver de fora vê a oferta que ele produziu.
//
// A comparação é contra o pedido REAL que o simulador do Banco CTT enviou,
// gravado a 2026-07-27 ao lado da resposta que dele veio. Um payload afirmado
// contra uma expectativa escrita à mão prova que o código não mudou; contra a
// captura, prova que o banco o reconhece.

// O titular das capturas: nascido a 1990-05-10, um só, e o cenário de
// 250 000 € / 200 000 € a 30 anos.
func pedidoDaCaptura(t *testing.T) dominio.Pedido {
	t.Helper()

	nascimento, err := dominio.DataDe(1990, 5, 10)
	if err != nil {
		t.Fatalf("a data de nascimento da captura não é uma data: %v", err)
	}
	return dominio.Pedido{
		ValorImovel: dominio.DinheiroDeInteiro(250_000),
		Montante:    dominio.DinheiroDeInteiro(200_000),
		PrazoAnos:   30,
		TipoTaxa:    dominio.TaxaVariavel,
		Finalidade:  dominio.FinalidadePropria,
		Localizacao: dominio.LocalizacaoContinente,
		Titulares:   []dominio.Titular{{DataNascimento: nascimento}},
	}
}

func TestOPayloadBateComOQueOSimuladorEnviou(t *testing.T) {
	anos := func(n int) *int { return &n }

	casos := []struct {
		nome    string
		captura string
		pedido  func(dominio.Pedido) dominio.Pedido
	}{
		{
			nome:    "variável",
			captura: "variavel_12m",
			pedido:  func(p dominio.Pedido) dominio.Pedido { return p },
		},
		{
			nome:    "mista a 5 anos",
			captura: "mista_5a",
			pedido: func(p dominio.Pedido) dominio.Pedido {
				p.TipoTaxa = dominio.TaxaMista
				p.PeriodoFixoAnos = anos(5)
				return p
			},
		},
		{
			nome:    "fixa a 30 anos",
			captura: "fixa_30a",
			pedido: func(p dominio.Pedido) dominio.Pedido {
				p.TipoTaxa = dominio.TaxaFixa
				return p
			},
		},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			p := c.pedido(pedidoDaCaptura(t))

			corpo, ajustes, _, err := construirPayload(p, p.PrazoAnos, PrazoMaxAnos)
			if err != nil {
				t.Fatalf("construirPayload: %v", err)
			}
			if len(ajustes) != 0 {
				t.Errorf("o cenário da captura coube tal como foi pedido: não devia haver ajustes, vieram %+v", ajustes)
			}

			gerado := paraMapa(t, marshal(t, corpo))
			capturado := paraMapa(t, capturaDePedido(t, c.captura))

			// ⚠️ Compara-se o objecto inteiro e não os campos que interessam: um
			// campo A MAIS no nosso payload, ou A MENOS, é exactamente o tipo de
			// diferença que o banco aceita hoje e recusa quando lhe apetecer.
			if d := cmp.Diff(capturado, gerado); d != "" {
				t.Errorf("o payload gerado não é o que o simulador enviou (-capturado +gerado):\n%s", d)
			}
		})
	}
}

// TestOSegundoTitularVaiNuloENaoVazio: a captura traz `null`, e uma string
// vazia é outro valor — que este banco não foi visto a aceitar.
func TestOSegundoTitularVaiNuloENaoVazio(t *testing.T) {
	corpo, _, _, err := construirPayload(pedidoDaCaptura(t), 30, PrazoMaxAnos)
	if err != nil {
		t.Fatalf("construirPayload: %v", err)
	}

	bruto := marshal(t, corpo)
	if !bytes.Contains(bruto, []byte(`"BorrowerTwoBirthDate":null`)) {
		t.Errorf("com um titular só, o segundo devia ir a null:\n%s", bruto)
	}

	p := pedidoDaCaptura(t)
	segundo, err := dominio.DataDe(1985, 3, 2)
	if err != nil {
		t.Fatalf("data: %v", err)
	}
	p.Titulares = append(p.Titulares, dominio.Titular{DataNascimento: segundo})

	comDois, _, _, err := construirPayload(p, 30, PrazoMaxAnos)
	if err != nil {
		t.Fatalf("construirPayload com dois titulares: %v", err)
	}
	if comDois.BorrowerTwoBirthDate == nil || *comDois.BorrowerTwoBirthDate != "1985-03-02T00:00:00.000Z" {
		t.Errorf("o segundo titular não foi para o payload: %v", comDois.BorrowerTwoBirthDate)
	}
}

// TestAsVendasAssociadasNaoVaoNoPedido fecha a distinção entre os dois produtos
// deste banco.
//
// ⚠️ `HasCrossSelling` é sempre true e **não** é a escolha de produtos: a
// resposta traz as duas colunas de preço no mesmo corpo, e quem escolhe a coluna
// é a leitura. Pô-lo a false para quem não pediu vendas associadas devolvia meia
// resposta e obrigava a um segundo pedido ao banco para obter a outra metade.
func TestAsVendasAssociadasNaoVaoNoPedido(t *testing.T) {
	semProdutos, _, _, err := construirPayload(pedidoDaCaptura(t), 30, PrazoMaxAnos)
	if err != nil {
		t.Fatalf("construirPayload: %v", err)
	}
	if !semProdutos.HasCrossSelling {
		t.Error("HasCrossSelling foi a false num pedido sem produtos — a resposta perde a coluna bonificada")
	}

	p := pedidoDaCaptura(t)
	p.Produtos = []string{ProdutoVendasAssociadas}
	comProdutos, _, _, err := construirPayload(p, 30, PrazoMaxAnos)
	if err != nil {
		t.Fatalf("construirPayload: %v", err)
	}
	if d := cmp.Diff(semProdutos, comProdutos); d != "" {
		t.Errorf("escolher as vendas associadas mudou o pedido, e devia mudar só a leitura (-sem +com):\n%s", d)
	}
}

// TestASustentabilidadeEAMedidaJovemVaoNoPedido: estes dois, ao contrário das
// vendas associadas, não têm coluna própria na resposta — o efeito deles aparece
// nas colunas que já existem, logo têm de ir no pedido.
func TestASustentabilidadeEAMedidaJovemVaoNoPedido(t *testing.T) {
	p := pedidoDaCaptura(t)
	p.Produtos = []string{ProdutoSustentavel}
	p.GarantiaPublica = true

	corpo, _, _, err := construirPayload(p, 30, PrazoMaxAnos)
	if err != nil {
		t.Fatalf("construirPayload: %v", err)
	}
	if !corpo.CampaignSustainabilityIsActive {
		t.Error("o produto sustentável foi escolhido e não foi no pedido")
	}
	if !corpo.YouthMeasuresIsActive {
		t.Error("a garantia pública foi pedida e a medida jovem não foi activada")
	}
}

func TestAMistaEncaixaOPeriodoQueOBancoNaoPratica(t *testing.T) {
	anos := func(n int) *int { return &n }

	p := pedidoDaCaptura(t)
	p.TipoTaxa = dominio.TaxaMista
	p.PeriodoFixoAnos = anos(4) // o banco pratica 1, 2, 3 e 5

	corpo, ajustes, _, err := construirPayload(p, 30, PrazoMaxAnos)
	if err != nil {
		t.Fatalf("construirPayload: %v", err)
	}
	if len(ajustes) != 1 {
		t.Fatalf("pediram-se 4 anos de fixo e o banco não os pratica: esperava um ajuste, vieram %d", len(ajustes))
	}
	if campo := ajustes[0].Campo(); campo != dominio.AjustadoPeriodoFixo {
		t.Errorf("o ajuste é ao campo %q e devia ser ao período fixo", campo)
	}
	// ⚠️ O identificador tem de ser o do período aplicado, e não o do pedido:
	// é ele que decide o preço que vem.
	if corpo.IndexTypeID != indexIDMista[3] && corpo.IndexTypeID != indexIDMista[5] {
		t.Errorf("o IndexTypeID %d não corresponde a nenhum período que o banco pratica", corpo.IndexTypeID)
	}
}

// TestAFixaEncaixaOPrazoPorqueOPeriodoEOContratoTodo: na fixa do Banco CTT não
// há período curto — 30 ou 34 anos, e o período é o contrato inteiro. Quem pede
// 10 anos de taxa fixa recebe o prazo encaixado, não uma fixa de 10.
func TestAFixaEncaixaOPrazoPorqueOPeriodoEOContratoTodo(t *testing.T) {
	p := pedidoDaCaptura(t)
	p.TipoTaxa = dominio.TaxaFixa

	corpo, _, _, err := construirPayload(p, 10, PrazoMaxAnos)
	if err != nil {
		t.Fatalf("construirPayload: %v", err)
	}
	if corpo.AmortizationPeriod != 30*12 {
		t.Errorf("pediram-se 10 anos de fixa e o payload leva %d meses; a fixa do Banco CTT é a 30 ou 34 anos",
			corpo.AmortizationPeriod)
	}
	if corpo.IndexTypeID != indexIDFixa[30] {
		t.Errorf("IndexTypeID = %d, e a fixa a 30 anos é %d", corpo.IndexTypeID, indexIDFixa[30])
	}
	if corpo.IndexTypeSubCategoryID != subcategoriaFixa {
		t.Errorf("a subcategoria é %d e a fixa é %d", corpo.IndexTypeSubCategoryID, subcategoriaFixa)
	}
}

func TestUmPedidoSemTitularesNaoVaiAoBanco(t *testing.T) {
	p := pedidoDaCaptura(t)
	p.Titulares = nil

	// ⚠️ Falha aqui e não na rede: o banco pede a data de nascimento e sem ela
	// gastava-se um pedido para receber um erro que já se sabia.
	if _, _, _, err := construirPayload(p, 30, PrazoMaxAnos); err == nil {
		t.Fatal("construiu-se um payload sem titular nenhum")
	}
}

// --- ajudantes ------------------------------------------------------------------

func marshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("serializar o payload: %v", err)
	}
	return b
}

// paraMapa lê JSON preservando os números como texto — comparar 200000 com
// 200000.0 daria igual em float64 e não é o mesmo literal no corpo.
func paraMapa(t *testing.T, b []byte) map[string]any {
	t.Helper()
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()

	var m map[string]any
	if err := d.Decode(&m); err != nil {
		t.Fatalf("ler JSON: %v", err)
	}
	return m
}

func capturaDePedido(t *testing.T, nome string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("capturas", nome+".pedido.json"))
	if err != nil {
		t.Fatalf("ler a captura do pedido %s: %v", nome, err)
	}
	return b
}
