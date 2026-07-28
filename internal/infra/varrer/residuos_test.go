package varrer

import (
	"testing"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/varrimento"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// O resumo do resíduo por banco, que é o que aparece a quem corre o subcomando.
//
// ⚠️ Teste interno ao pacote (`package varrer`) e não `varrer_test`: o que se
// afirma é a estatística, e o caminho público — o `Correr` — precisa de Postgres
// e de bancos a sério para lá chegar. O resto do pacote continua a ser afirmado
// de fora.

func TestOResumoDaMedianaEDoMaiorPorBanco(t *testing.T) {
	// Resíduos de -0,04, +0,02, +6,10 e -0,01 num banco: a mediana dos módulos é
	// 0,03 e o maior é 6,10. ⚠️ São os dois números e não um: 6,10 sozinho é uma
	// observação estranha, 0,03 sozinho esconde-a.
	obs := []varrimento.Observacao{
		observacaoComResiduo(t, "cgd", "-0.04"),
		observacaoComResiduo(t, "cgd", "0.02"),
		observacaoComResiduo(t, "cgd", "6.10"),
		observacaoComResiduo(t, "cgd", "-0.01"),
		observacaoComResiduo(t, "montepio", "0.03"),
	}

	resumo := residuosPorBanco([]string{"cgd", "montepio"}, obs)

	if len(resumo) != 2 {
		t.Fatalf("esperava dois bancos no resumo, vieram %d: %+v", len(resumo), resumo)
	}
	// ⚠️ A ordem é a dos bancos escolhidos e não a de um mapa: uma saída que
	// muda de ordem entre corridas não se compara com a da véspera.
	if resumo[0].ID != "cgd" || resumo[1].ID != "montepio" {
		t.Errorf("a ordem do resumo não é a dos bancos: %+v", resumo)
	}

	cgd := resumo[0]
	if cgd.Medidos != 4 {
		t.Errorf("mediram-se 4 resíduos na CGD e o resumo conta %d", cgd.Medidos)
	}
	if cgd.Mediano != "0.03" {
		t.Errorf("a mediana dos módulos de (0.04, 0.02, 6.10, 0.01) é 0.03 e deu %s", cgd.Mediano)
	}
	// O sinal preserva-se: o maior em módulo continua a dizer para que lado.
	if cgd.Maior != "6.10" {
		t.Errorf("o maior é 6.10 e deu %s", cgd.Maior)
	}
}

func TestOMaiorGuardaOSinalDoDesvio(t *testing.T) {
	resumo := residuosPorBanco([]string{"cgd"}, []varrimento.Observacao{
		observacaoComResiduo(t, "cgd", "-6.10"),
		observacaoComResiduo(t, "cgd", "0.02"),
	})

	if resumo[0].Maior != "-6.10" {
		t.Errorf("o maior desvio foi de -6,10 € e o resumo diz %s — perdeu-se de que lado se diverge",
			resumo[0].Maior)
	}
	// Ímpar contra par: com dois valores, a mediana dos módulos é a média deles.
	if resumo[0].Mediano != "3.06" {
		t.Errorf("a mediana de (6.10, 0.02) é 3.06 e deu %s", resumo[0].Mediano)
	}
}

// TestUmBancoSemMedicaoNenhumaNaoEntraNoResumo: uma linha «cgd: 0,00 €» quando o
// que houve foram três falhas seria o número mais enganador da saída toda.
func TestUmBancoSemMedicaoNenhumaNaoEntraNoResumo(t *testing.T) {
	falha := varrimento.Observacao{
		Ponto: varrimento.Ponto{Cenario: "variavel/0/propria", Pedido: pedidoDeResumo()},
		Oferta: dominio.Falhar("cgd", "CGD", &dominio.ErroOferta{
			Codigo: dominio.ErroBancoIndisponivel, Mensagem: "em baixo",
		}),
	}

	if resumo := residuosPorBanco([]string{"cgd"}, []varrimento.Observacao{falha}); len(resumo) != 0 {
		t.Errorf("um banco que só falhou apareceu no resumo do resíduo: %+v", resumo)
	}
}

// observacaoComResiduo constrói uma observação cujo resíduo é exactamente o
// pedido: a prestação publicada é a francesa mais o desvio.
//
// ⚠️ A francesa vem do dominio.PrestacaoFrancesa e não de uma constante escrita
// à mão. É a mesma conta que o Residuo faz — o que se está a afirmar aqui é a
// estatística por cima dela, e não a aritmética, que tem os seus testes.
func observacaoComResiduo(t *testing.T, bancoID, desvio string) varrimento.Observacao {
	t.Helper()

	pedido := pedidoDeResumo()
	tan := taxaDeResumo(t, "4.500")

	francesa, err := dominio.PrestacaoFrancesa(pedido.Montante, tan, pedido.PrazoAnos*12)
	if err != nil {
		t.Fatalf("a prestação de prova não calculou: %v", err)
	}
	prestacao := dominio.DinheiroDeDecimal(francesa.Decimal().Add(dinheiroDeResumo(t, desvio).Decimal()))

	return varrimento.Observacao{
		Ponto: varrimento.Ponto{Cenario: "variavel/0/propria", Pedido: pedido},
		Oferta: dominio.Oferta{
			BancoID:     bancoID,
			BancoNome:   bancoID,
			TAN:         &tan,
			Prestacao:   &prestacao,
			Fases:       []dominio.Fase{{AteMes: pedido.PrazoAnos * 12, Taxa: tan, Prestacao: prestacao}},
			CapturadoEm: time.Now(),
		},
	}
}

func pedidoDeResumo() dominio.Pedido {
	return dominio.Pedido{
		ValorImovel: dominio.DinheiroDeInteiro(400_000),
		Montante:    dominio.DinheiroDeInteiro(320_000),
		PrazoAnos:   30,
		TipoTaxa:    dominio.TaxaVariavel,
		Finalidade:  dominio.FinalidadePropria,
		Localizacao: dominio.LocalizacaoContinente,
	}
}

func taxaDeResumo(t *testing.T, s string) dominio.Taxa {
	t.Helper()
	x, err := dominio.TaxaDeTexto(s)
	if err != nil {
		t.Fatalf("taxa de prova inválida %q: %v", s, err)
	}
	return x
}

func dinheiroDeResumo(t *testing.T, s string) dominio.Dinheiro {
	t.Helper()
	d, err := dominio.DinheiroDeTexto(s)
	if err != nil {
		t.Fatalf("montante de prova inválido %q: %v", s, err)
	}
	return d
}
