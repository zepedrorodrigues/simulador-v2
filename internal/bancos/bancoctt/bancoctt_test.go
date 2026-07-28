package bancoctt_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/bancos"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/bancoctt"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/prova"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/transporte"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// O banco inteiro, contra as capturas e sem rede. O teste ao vivo está no
// bancoctt_rede_test.go, por trás de `//go:build rede`, e não corre no portão.

func TestOsRequisitosSaoServiveis(t *testing.T) {
	r := bancoctt.Novo(&transporte.Falso{}).Requisitos()
	if err := r.Validar(); err != nil {
		t.Fatalf("os requisitos do Banco CTT não são servíveis pelo contrato: %v", err)
	}

	// ⚠️ O indexante é imposto e não escolhido, e a declaração tem de o dizer:
	// é por ela que a app sabe não desenhar o selector. O bundle do simulador
	// força 12M na variável e 3M no pós-período-fixo da mista.
	if r.EuriborImposto != dominio.Euribor12M {
		t.Errorf("EuriborImposto = %q, e o Banco CTT impõe a 12 meses", r.EuriborImposto)
	}
	if len(r.EuriborOpcoes) != 0 {
		t.Errorf("o Banco CTT não deixa escolher o indexante, e declara %d opções", len(r.EuriborOpcoes))
	}
}

func TestSimularLeACapturaDaVariavel(t *testing.T) {
	banco := comCaptura(t, "variavel_12m")

	oferta, err := banco.Simular(t.Context(), pedidoBase(t))
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}

	if oferta.BancoID != "bancoctt" || oferta.BancoNome != "Banco CTT" {
		t.Errorf("a oferta não se identifica: %q / %q", oferta.BancoID, oferta.BancoNome)
	}
	if oferta.TAN == nil || oferta.TAEG == nil || oferta.Prestacao == nil || oferta.MTIC == nil {
		t.Fatalf("faltam campos nucleares na oferta: %+v", oferta)
	}
	if oferta.Indexante != dominio.Euribor12M {
		t.Errorf("indexante = %q, e a variável do Banco CTT é sempre a 12 meses", oferta.Indexante)
	}
	if len(oferta.Ajustes()) != 0 {
		t.Errorf("o pedido coube tal como foi feito: não devia haver ajustes, vieram %+v", oferta.Ajustes())
	}
}

// TestUmPrazoQueNaoCabeNaIdadeEncolheAntesDeIrARede é a regra que este banco
// obriga a impor deste lado.
//
// ⚠️ A API do Banco CTT **não valida** a idade: medido a 2026-07-27, aceitou um
// titular de 66 anos com 40 anos de prazo — fim do contrato aos 106. Se o pedido
// fosse à rede tal como veio, voltava uma oferta plausível e inexistente.
func TestUmPrazoQueNaoCabeNaIdadeEncolheAntesDeIrARede(t *testing.T) {
	banco := comCaptura(t, "variavel_12m")

	p := pedidoBase(t)
	p.PrazoAnos = 40
	p.Titulares = []dominio.Titular{{DataNascimento: nascidoHaAnos(t, 50)}}

	oferta, err := banco.Simular(t.Context(), p)
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}

	ajustes := oferta.Ajustes()
	if len(ajustes) != 1 {
		t.Fatalf("aos 50 anos, 40 de prazo põem o fim aos 90: esperava um ajuste, vieram %+v", ajustes)
	}
	if campo := ajustes[0].Campo(); campo != dominio.AjustadoPrazoAnos {
		t.Fatalf("o ajuste é ao campo %q e devia ser ao prazo", campo)
	}
	// 75 - 50 = 25 anos, e é isso que tem de ir no pedido.
	if para, ok := ajustes[0].Para().(int); !ok || para != 25 {
		t.Errorf("o prazo aplicado é %v, e aos 50 anos o contrato tem de acabar aos 75 — logo 25", ajustes[0].Para())
	}
	if !strings.Contains(ajustes[0].Nota(), "75") {
		t.Errorf("a nota não diz porque é que o prazo mudou: %q", ajustes[0].Nota())
	}
}

// TestSemIdadeParaTrintaAnosNaoHaTaxaFixa: a fixa do Banco CTT é ao contrato
// todo, e o mais curto são 30 anos.
//
// ⚠️ É o único sítio deste banco em que encaixar o prazo o faria CRESCER. Sem a
// guarda, quem pedisse uma fixa aos 50 anos recebia 30 anos de contrato a
// terminar aos 80 — cinco anos para lá do limite. Falhar é a resposta certa:
// não há aqui produto nenhum para esta pessoa.
func TestSemIdadeParaTrintaAnosNaoHaTaxaFixa(t *testing.T) {
	banco := comCaptura(t, "fixa_30a")

	p := pedidoBase(t)
	p.TipoTaxa = dominio.TaxaFixa
	p.PrazoAnos = 20
	p.Titulares = []dominio.Titular{{DataNascimento: nascidoHaAnos(t, 50)}}

	_, err := banco.Simular(t.Context(), p)
	if err == nil {
		t.Fatal("simulou-se uma fixa de 30 anos a quem só pode contratar 25")
	}

	var erroOferta *dominio.ErroOferta
	if !comoErroOferta(err, &erroOferta) {
		t.Fatalf("o erro não é estruturado: %v", err)
	}
	if erroOferta.Codigo != dominio.ErroPrazoImpossivel {
		t.Errorf("código = %q, e o que aqui não cabe é o prazo", erroOferta.Codigo)
	}
	if !strings.Contains(erroOferta.Mensagem, "30 e 34") {
		t.Errorf("a mensagem não nomeia os prazos que existem: %q", erroOferta.Mensagem)
	}
}

// TestUmaFixaCurtaEncolheParaTrintaEDizPorque: quem tem idade recebe o ajuste,
// e não a recusa.
func TestUmaFixaCurtaEncolheParaTrintaEDizPorque(t *testing.T) {
	banco := comCaptura(t, "fixa_30a")

	p := pedidoBase(t)
	p.TipoTaxa = dominio.TaxaFixa
	p.PrazoAnos = 10

	oferta, err := banco.Simular(t.Context(), p)
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}

	ajustes := oferta.Ajustes()
	if len(ajustes) != 1 {
		t.Fatalf("pediram-se 10 anos de fixa e o banco só tem 30 e 34: esperava um ajuste, vieram %+v", ajustes)
	}
	if para, ok := ajustes[0].Para().(int); !ok || para != 30 {
		t.Errorf("o prazo aplicado é %v e devia ser 30", ajustes[0].Para())
	}
	// ⚠️ UM ajuste, e não dois. O tecto da idade e o encaixe da fixa mexem os
	// dois no prazo; emitir um em cada um dava um segundo a dizer «Pediu 30
	// anos» a quem tinha pedido 10.
	if !strings.Contains(ajustes[0].Nota(), "30 e 34") {
		t.Errorf("a nota não diz porque é que o prazo cresceu: %q", ajustes[0].Nota())
	}
}

// TestARecusaDoBancoNaoViraOferta: um corpo sem simulação é uma falha
// estruturada, não uma oferta vazia com ar de resposta.
func TestARecusaDoBancoNaoViraOferta(t *testing.T) {
	falso := &transporte.Falso{
		Estado: http.StatusOK,
		Corpo:  `{"success":false,"data":null,"error":"Não é possível simular com estes dados."}`,
	}

	_, err := bancoctt.Novo(falso).Simular(t.Context(), pedidoBase(t))
	if err == nil {
		t.Fatal("uma recusa do banco virou oferta")
	}
	if !strings.Contains(err.Error(), "Não é possível simular") {
		t.Errorf("a razão do banco perdeu-se pelo caminho: %v", err)
	}
}

func TestUmEstadoDeErroNaoViraOferta(t *testing.T) {
	falso := &transporte.Falso{Estado: http.StatusInternalServerError, Corpo: "{}"}

	_, err := bancoctt.Novo(falso).Simular(t.Context(), pedidoBase(t))
	if err == nil {
		t.Fatal("um 500 do simulador virou oferta")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("o erro não diz o que o banco devolveu: %v", err)
	}
}

// TestRespeitaOPrazoDoCtx é a afirmação que o CONTRATO-BANCO.md §7 exige de cada
// banco, um a um: o Simular volta dentro do prazo do ctx.
//
// ⚠️ Contra um transporte LENTO. Sem o atraso, o teste passava por não haver
// nada que demorasse — e um teste que nunca se viu falhar não prova nada.
func TestRespeitaOPrazoDoCtx(t *testing.T) {
	lento := &transporte.Falso{
		Estado: http.StatusOK,
		Corpo:  string(captura(t, "variavel_12m.resposta.json")),
		Atraso: 5 * time.Second,
	}

	var banco bancos.Banco = bancoctt.Novo(lento)
	prova.RespeitaPrazo(t, banco, pedidoBase(t), 300*time.Millisecond)
}

// --- ajudantes ------------------------------------------------------------------

func comCaptura(t *testing.T, nome string) *bancoctt.Banco {
	t.Helper()
	return bancoctt.Novo(&transporte.Falso{
		Estado: http.StatusOK,
		Corpo:  string(captura(t, nome+".resposta.json")),
	})
}

func captura(t *testing.T, nome string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("capturas", nome))
	if err != nil {
		t.Fatalf("ler a captura %s: %v", nome, err)
	}
	return b
}

func pedidoBase(t *testing.T) dominio.Pedido {
	t.Helper()
	return dominio.Pedido{
		ValorImovel: dominio.DinheiroDeInteiro(250_000),
		Montante:    dominio.DinheiroDeInteiro(200_000),
		PrazoAnos:   30,
		TipoTaxa:    dominio.TaxaVariavel,
		Finalidade:  dominio.FinalidadePropria,
		Localizacao: dominio.LocalizacaoContinente,
		Titulares:   []dominio.Titular{{DataNascimento: nascidoHaAnos(t, 35)}},
	}
}

// nascidoHaAnos dá uma data que faz o titular ter exactamente esta idade hoje —
// sem congelar relógio nenhum.
func nascidoHaAnos(t *testing.T, n int) dominio.Data {
	t.Helper()
	return dominio.DataDeInstante(time.Now().AddDate(-n, 0, 0))
}

func comoErroOferta(err error, alvo **dominio.ErroOferta) bool {
	e, ok := err.(*dominio.ErroOferta)
	if ok {
		*alvo = e
	}
	return ok
}
