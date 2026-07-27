package grelha_test

import (
	"testing"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/grelha"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/cgd"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/montepio"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/novobanco"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// hoje é fixo para os testes não mudarem de resultado com o calendário.
var hoje = dominio.Data{Ano: 2026, Mes: time.July, Dia: 27}

func TestPontosSaemDosRequisitosDeCadaBanco(t *testing.T) {
	t.Parallel()

	// As contas estão escritas por extenso de propósito. Se um banco ganhar um
	// período fixo ou um produto, este teste falha e obriga a olhar para o
	// custo que isso põe em cima do simulador dele — que é a única coisa nesta
	// grelha com um custo externo.
	for _, caso := range []struct {
		bancoID string
		pontos  int
		conta   string
	}{
		{"cgd", 18, "1 referência + 7 períodos × 2 (fixa e mista) + 0 tenores (impõe 6M) + 2 finalidades + 1 produto"},
		{"novobanco", 27, "1 + 9 períodos × 2 + 3 tenores + 2 finalidades + 2 produtos + 1 com todos"},
		{"montepio", 21, "1 + 7 períodos × 2 + 3 tenores + 2 finalidades + 1 produto"},
	} {
		t.Run(caso.bancoID, func(t *testing.T) {
			t.Parallel()

			pontos, err := grelha.Pontos(requisitosDe(t, caso.bancoID), grelha.Referencia{}, hoje)
			if err != nil {
				t.Fatalf("Pontos: %v", err)
			}
			if len(pontos) != caso.pontos {
				t.Errorf("%d pontos, esperava %d (%s)", len(pontos), caso.pontos, caso.conta)
			}
			t.Logf("%s: %d pontos", caso.bancoID, len(pontos))
		})
	}
}

func TestTodoOPontoDaGrelhaEUmPedidoValido(t *testing.T) {
	t.Parallel()

	// Um ponto que o próprio domínio recusa gasta um pedido a um banco para
	// receber um erro nosso — e, pior, escreve no catálogo uma falha com o nome
	// do banco. É a KAN-30 pelo lado da grelha.
	for _, id := range []string{"cgd", "novobanco", "montepio"} {
		pontos, err := grelha.Pontos(requisitosDe(t, id), grelha.Referencia{}, hoje)
		if err != nil {
			t.Fatalf("%s: Pontos: %v", id, err)
		}
		for _, p := range pontos {
			if err := p.Pedido.Validar(hoje); err != nil {
				t.Errorf("%s, ponto %s: o pedido não é válido: %v", id, p.Cenario, err)
			}
		}
	}
}

func TestOCenarioDeUmPontoDerivaDoSeuPedido(t *testing.T) {
	t.Parallel()

	// É a frase da §4 — «chave estruturada, derivável do pedido» — posta à
	// prova nos dois sentidos: a chave escrita no ponto tem de ser exactamente
	// a que se deriva do pedido que lá vai. Sem isto, a pergunta de um cliente
	// não encontra a linha que o varrimento gravou.
	for _, id := range []string{"cgd", "novobanco", "montepio"} {
		pontos, err := grelha.Pontos(requisitosDe(t, id), grelha.Referencia{}, hoje)
		if err != nil {
			t.Fatalf("%s: Pontos: %v", id, err)
		}
		for _, p := range pontos {
			doPedido, err := grelha.CenarioDe(p.Pedido)
			if err != nil {
				t.Errorf("%s, ponto %s: não se deriva cenário do pedido: %v", id, p.Cenario, err)
				continue
			}
			if doPedido.Chave() != p.Cenario {
				t.Errorf("%s: o ponto diz %q e o pedido dele deriva %q", id, p.Cenario, doPedido.Chave())
			}
		}
	}
}

func TestNaoHaDoisPontosIndistinguiveis(t *testing.T) {
	t.Parallel()

	// Duas linhas do mesmo varrimento distinguem-se pelo cenário, pelos
	// produtos e pelo tenor — as três coisas que a §4 guarda, entre a chave e
	// as colunas. Dois pontos iguais nas três são um pedido gasto a medir o que
	// já se mediu, e duas linhas que ninguém sabe distinguir na leitura.
	for _, id := range []string{"cgd", "novobanco", "montepio"} {
		pontos, err := grelha.Pontos(requisitosDe(t, id), grelha.Referencia{}, hoje)
		if err != nil {
			t.Fatalf("%s: Pontos: %v", id, err)
		}

		vistos := map[string]bool{}
		for _, p := range pontos {
			identidade := p.Cenario + "|" + string(p.Pedido.Indexante) + "|"
			for _, produto := range p.Pedido.Produtos {
				identidade += produto + ","
			}
			if vistos[identidade] {
				t.Errorf("%s: dois pontos indistinguíveis em %s", id, identidade)
			}
			vistos[identidade] = true
		}
	}
}

func TestUmBancoQueNaoUsaAFinalidadeNaoAVarre(t *testing.T) {
	t.Parallel()

	// A grelha sai dos Requisitos, e não de uma lista à mão: um banco que
	// declare não usar o campo não gasta dois pedidos a medir um desvio que
	// não existe.
	r := requisitosMinimos()
	comFinalidade, err := grelha.Pontos(r, grelha.Referencia{}, hoje)
	if err != nil {
		t.Fatalf("Pontos: %v", err)
	}

	for i := range r.Inputs {
		if r.Inputs[i].Campo == dominio.CampoFinalidade {
			r.Inputs[i].Usa = false
			r.Inputs[i].Nota = "este banco não pergunta a finalidade"
		}
	}
	semFinalidade, err := grelha.Pontos(r, grelha.Referencia{}, hoje)
	if err != nil {
		t.Fatalf("Pontos: %v", err)
	}

	if diferenca := len(comFinalidade) - len(semFinalidade); diferenca != 2 {
		t.Errorf("desligar a finalidade tirou %d pontos, esperava 2", diferenca)
	}
	for _, p := range semFinalidade {
		c, err := grelha.LerCenario(p.Cenario)
		if err != nil {
			t.Fatalf("LerCenario(%q): %v", p.Cenario, err)
		}
		if c.Finalidade != dominio.FinalidadePropria {
			t.Errorf("varreu a finalidade %s num banco que não a usa", c.Finalidade)
		}
	}
}

func TestChaveDoCenarioERedonda(t *testing.T) {
	t.Parallel()

	for _, caso := range []struct {
		cenario grelha.Cenario
		chave   string
	}{
		{grelha.Cenario{TipoTaxa: dominio.TaxaVariavel, Finalidade: dominio.FinalidadePropria}, "variavel/0/propria"},
		{grelha.Cenario{TipoTaxa: dominio.TaxaFixa, PeriodoFixoAnos: 10, Finalidade: dominio.FinalidadePropria}, "fixa/10/propria"},
		{grelha.Cenario{TipoTaxa: dominio.TaxaMista, PeriodoFixoAnos: 5, Finalidade: dominio.FinalidadeArrendamento}, "mista/5/arrendamento"},
	} {
		t.Run(caso.chave, func(t *testing.T) {
			t.Parallel()

			if got := caso.cenario.Chave(); got != caso.chave {
				t.Errorf("Chave() = %q, esperava %q", got, caso.chave)
			}
			lido, err := grelha.LerCenario(caso.chave)
			if err != nil {
				t.Fatalf("LerCenario: %v", err)
			}
			if lido != caso.cenario {
				t.Errorf("a volta deu %+v, esperava %+v", lido, caso.cenario)
			}
		})
	}
}

func TestUmaChaveQueNaoSeReconheceFalhaAlto(t *testing.T) {
	t.Parallel()

	// ⚠️ O ponto todo de validar os três segmentos. Uma chave estranha que
	// devolvesse um cenário plausível punha o cliente na linha de outro — e
	// isso não aparece a ninguém, que é o modo de falha da §7.4.
	for _, chave := range []string{
		"variavel/propria",              // dois segmentos: o formato antigo, sem período
		"variavel/0/propria/ltv80",      // quatro: alguém acrescentou uma dimensão à chave
		"variavel/x/propria",            // período que não é número
		"trimestral/0/propria",          // tipo de taxa que não existe
		"variavel/0/arrendamento_curto", // finalidade que não existe
		"variavel/10/propria",           // variável com período fixo
		"fixa/0/propria",                // fixa sem período fixo
		"",                              // vazio
	} {
		if _, err := grelha.LerCenario(chave); err == nil {
			t.Errorf("LerCenario(%q) passou, e não devia", chave)
		}
	}
}

func TestUmPedidoSemPeriodoEscolhidoNaoTemCenario(t *testing.T) {
	t.Parallel()

	// «Sem preferência» é uma pergunta, não um ponto da grelha. Quem responde
	// fecha-a primeiro com o EncaixarPeriodoFixo, contra os períodos que o
	// banco pratica — inventar aqui um período punha o cliente na linha errada.
	p := dominio.Pedido{
		TipoTaxa:   dominio.TaxaFixa,
		Finalidade: dominio.FinalidadePropria,
	}
	if _, err := grelha.CenarioDe(p); err == nil {
		t.Fatal("um pedido de taxa fixa sem período escolhido deu cenário")
	}
}

// ---------------------------------------------------------------- utilitários

// requisitosDe devolve o que um banco declara.
//
// ⚠️ Constrói-se com o transporte a nulo, e está certo: os Requisitos são uma
// declaração e não fazem I/O — é a mesma separação que torna cada banco
// testável offline (§5). Se um dia precisarem de rede, este teste rebenta, e é
// o aviso que se quer.
func requisitosDe(t *testing.T, bancoID string) dominio.Requisitos {
	t.Helper()
	switch bancoID {
	case cgd.BancoID:
		return cgd.Novo(nil).Requisitos()
	case novobanco.BancoID:
		return novobanco.Novo(nil).Requisitos()
	case montepio.BancoID:
		return montepio.Novo(nil).Requisitos()
	}
	t.Fatalf("banco desconhecido no teste: %s", bancoID)
	return dominio.Requisitos{}
}

// requisitosMinimos é um banco de laboratório: o mínimo que o Validar aceita,
// com uma dimensão de cada coisa, para se poder ligar e desligar uma delas.
func requisitosMinimos() dominio.Requisitos {
	return dominio.Requisitos{
		BancoID:   "prova",
		BancoNome: "Banco de Prova",
		Custo:     dominio.CustoBarato,
		Inputs: []dominio.Input{
			{Campo: dominio.CampoFinalidade, Usa: true},
		},
		PeriodosFixos:     []int{10},
		PeriodosFixosModo: dominio.ModoLista,
		EuriborImposto:    dominio.Euribor6M,
		PrazoMin:          1,
		PrazoMax:          40,
		IdadeMaximaFim:    75,
	}
}
