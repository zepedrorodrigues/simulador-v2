package dominio_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

func TestSucessoDerivaDoErro(t *testing.T) {
	boa := dominio.Oferta{BancoID: "cgd", BancoNome: "CGD"}
	if !boa.Sucesso() {
		t.Error("uma oferta sem erro é um sucesso")
	}

	ma := dominio.Falhar("bancoctt", "Banco CTT", dominio.ErroDePrazo("Banco CTT", 78, 75))
	if ma.Sucesso() {
		t.Error("uma oferta com erro não é um sucesso")
	}
	if ma.Erro.Codigo != dominio.ErroPrazoImpossivel {
		t.Errorf("código = %q, queria %q", ma.Erro.Codigo, dominio.ErroPrazoImpossivel)
	}
	for _, n := range []string{"78", "75", "Banco CTT"} {
		if !strings.Contains(ma.Erro.Mensagem, n) {
			t.Errorf("a mensagem não nomeia %q: %q", n, ma.Erro.Mensagem)
		}
	}
}

func TestAplicadoENotasDerivamDosAjustes(t *testing.T) {
	o := dominio.Oferta{BancoID: "montepio", BancoNome: "Banco Montepio"}

	if len(o.Aplicado()) != 0 {
		t.Error("uma oferta sem ajustes tem o aplicado vazio")
	}
	if len(o.Notas()) != 0 {
		t.Error("uma oferta sem ajustes nem notas não tem notas")
	}

	// Um ajuste nulo é um não-fazer-nada: é o que deixa as políticas devolver
	// "não houve ajuste" sem obrigar cada banco a um if.
	o.Acrescentar(nil)
	o.Anotar("")
	if len(o.Aplicado()) != 0 || len(o.Notas()) != 0 {
		t.Error("acrescentar nada mudou a oferta")
	}

	o.Acrescentar(dominio.AjusteTipoTaxa(dominio.TaxaFixa, dominio.TaxaMista, "não tem taxa fixa pura"))
	o.Acrescentar(dominio.AjustePeriodoFixo(5, 30))
	o.Anotar("O MTIC não acompanha a selecção de produtos e foi omitido.")

	aplicado := o.Aplicado()
	if aplicado[string(dominio.AjustadoTipoTaxa)] != string(dominio.TaxaMista) {
		t.Errorf("aplicado[rate_type] = %v, queria mista", aplicado[string(dominio.AjustadoTipoTaxa)])
	}
	if aplicado[string(dominio.AjustadoPeriodoFixo)] != 30 {
		t.Errorf("aplicado[fixed_period_years] = %v, queria 30 (número, não texto)", aplicado[string(dominio.AjustadoPeriodoFixo)])
	}

	notas := o.Notas()
	if len(notas) != 3 {
		t.Fatalf("notas = %d, queria 3 (duas de ajuste, uma livre)", len(notas))
	}
	// ⚠️ Cada ajuste traz a sua nota. Números diferentes dos pedidos sem uma
	// frase que o diga fazem da comparação uma comparação entre coisas
	// diferentes — art. 4.º da Directiva 2006/114/CE.
	for i, nota := range notas {
		if strings.TrimSpace(nota) == "" {
			t.Errorf("a nota %d está vazia", i+1)
		}
	}
	if !strings.Contains(notas[len(notas)-1], "MTIC") {
		t.Error("a nota livre devia vir depois das dos ajustes")
	}
}

func TestAjusteMudoNaoEntra(t *testing.T) {
	// Com os campos não exportados, dominio.Ajuste{Campo: ...} nem compila —
	// mas o valor zero continua a construir-se de fora. É este o remate: um
	// ajuste sem nota não chega à oferta, e por isso não há forma de sair um
	// número ajustado sem a frase que o explica.
	o := dominio.Oferta{BancoID: "cgd", BancoNome: "CGD"}
	o.Acrescentar(&dominio.Ajuste{})

	if len(o.Aplicado()) != 0 {
		t.Errorf("um ajuste mudo entrou no aplicado: %v", o.Aplicado())
	}
	if len(o.Notas()) != 0 {
		t.Errorf("um ajuste mudo entrou nas notas: %v", o.Notas())
	}
}

func TestAjustesDevolveCopia(t *testing.T) {
	o := dominio.Oferta{BancoID: "cgd", BancoNome: "CGD"}
	o.Acrescentar(dominio.AjustePeriodoFixo(5, 10))

	roubado := o.Ajustes()
	roubado[0] = dominio.Ajuste{}

	if o.Ajustes()[0].Nota() == "" {
		t.Error("mexer na cópia mexeu no original — a nota tem de ser inviolável de fora")
	}
}

func TestFasesDeDuracoesAcumula(t *testing.T) {
	fases, err := dominio.FasesDeDuracoes([]dominio.FaseDuracao{
		{Meses: 60, Taxa: taxa(t, "3.25"), Prestacao: dinheiro(t, "870.42")},
		{Meses: 300, Taxa: taxa(t, "3.90"), Prestacao: dinheiro(t, "910.10")},
	})
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	querido := []int{60, 360}
	if got := []int{fases[0].AteMes, fases[1].AteMes}; !slices.Equal(got, querido) {
		t.Errorf("acumulado = %v, queria %v", got, querido)
	}
	if err := dominio.ValidarFases(fases, 30); err != nil {
		t.Errorf("as fases cobrem 30 anos e foram recusadas: %v", err)
	}
}

func TestFasesDeDuracoesRecusaDuracaoVazia(t *testing.T) {
	if _, err := dominio.FasesDeDuracoes([]dominio.FaseDuracao{{Meses: 0}}); err == nil {
		t.Error("uma fase de zero meses foi aceite")
	}
}

func TestValidarFases(t *testing.T) {
	casos := []struct {
		nome      string
		fases     []dominio.Fase
		prazoAnos int
		queroErro bool
	}{
		{
			nome:      "uma fase que cobre o contrato todo",
			fases:     []dominio.Fase{{AteMes: 360}},
			prazoAnos: 30,
		},
		{
			nome:      "plano sem fases",
			fases:     nil,
			prazoAnos: 30,
			queroErro: true,
		},
		{
			nome:      "fases que recuam",
			fases:     []dominio.Fase{{AteMes: 120}, {AteMes: 60}},
			prazoAnos: 30,
			queroErro: true,
		},
		{
			nome:      "duas fases a acabar no mesmo mês",
			fases:     []dominio.Fase{{AteMes: 360}, {AteMes: 360}},
			prazoAnos: 30,
			queroErro: true,
		},
		{
			nome:      "as fases acabam antes do fim do prazo",
			fases:     []dominio.Fase{{AteMes: 60}, {AteMes: 300}},
			prazoAnos: 30,
			queroErro: true,
		},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			err := dominio.ValidarFases(c.fases, c.prazoAnos)
			if c.queroErro && err == nil {
				t.Error("passou e devia ter sido recusado")
			}
			if !c.queroErro && err != nil {
				t.Errorf("recusado sem razão: %v", err)
			}
		})
	}
}
