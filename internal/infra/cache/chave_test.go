package cache_test

import (
	"strings"
	"testing"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
	"github.com/zepedrorodrigues/simulador-v2/internal/infra/cache"
)

// TestAChaveNaoLevaODadoPessoalEmClaro é o critério de privacidade da KAN-58, e
// a razão de a chave ser um resumo e não o pedido serializado.
//
// ⚠️ Reverter a derivação — devolver o pedido serializado em vez do SHA-256 —
// faz este teste encontrar a data de nascimento e o rendimento na chave, que é
// exactamente o que vai para o disco.
func TestAChaveNaoLevaODadoPessoalEmClaro(t *testing.T) {
	chave, err := cache.Chave("cgd", pedidoDe(t, "1987-03-14", "4321.50", "250000"))
	if err != nil {
		t.Fatalf("derivar a chave: %v", err)
	}

	for _, pessoal := range []string{"1987", "03-14", "1987-03-14", "4321.50", "4321"} {
		if strings.Contains(chave, pessoal) {
			t.Errorf("a chave %q leva %q em claro — isto vai para a tabela", chave, pessoal)
		}
	}

	// Um SHA-256 em hexadecimal e mais nada: 64 caracteres, sem estrutura por
	// onde se leia o pedido.
	if len(chave) != 64 {
		t.Errorf("a chave tem %d caracteres, esperava os 64 de um SHA-256 em hex: %q", len(chave), chave)
	}
}

// TestOMesmoPedidoDaAMesmaChave: sem isto não há acerto nenhum, e a cache era
// uma tabela que só cresce.
func TestOMesmoPedidoDaAMesmaChave(t *testing.T) {
	primeira := chaveDe(t, "cgd", pedidoDe(t, "1987-03-14", "4321.50", "250000"))
	segunda := chaveDe(t, "cgd", pedidoDe(t, "1987-03-14", "4321.50", "250000"))

	if primeira != segunda {
		t.Errorf("o mesmo pedido deu chaves diferentes:\n%s\n%s", primeira, segunda)
	}
}

// TestOQueMudaOPrecoMudaAChave é o teste que impede o acerto errado — o único
// erro grave que esta cache pode causar, e o silencioso: a resposta tem o
// aspecto certo e é de outro pedido.
//
// ⚠️ **O cêntimo está aqui de propósito.** É a `KAN-15` a ser recusada em
// código: arredondar o montante aumentava acertos e cruzava degraus de preço.
func TestOQueMudaOPrecoMudaAChave(t *testing.T) {
	base := pedidoDe(t, "1987-03-14", "4321.50", "250000")
	referencia := chaveDe(t, "cgd", base)

	casos := map[string]dominio.Pedido{
		"um cêntimo no montante":   comMontante(t, base, "250000.01"),
		"um cêntimo no rendimento": comRendimento(t, base, "4321.51"),
		"um dia na data":           comNascimento(t, base, "1987-03-15"),
		"outro prazo":              comPrazo(base, base.PrazoAnos+1),
		"outra finalidade":         comFinalidade(base, dominio.FinalidadeArrendamento),
		"outro indexante":          comIndexante(base, dominio.Euribor3M),
		"já cliente":               comJaCliente(base),
	}
	for nome, outro := range casos {
		if chaveDe(t, "cgd", outro) == referencia {
			t.Errorf("%s deu a MESMA chave: um pedido serviria a resposta do outro", nome)
		}
	}

	// ⚠️ E o banco entra na chave: a resposta da CGD não pode servir o Montepio.
	if chaveDe(t, "montepio", base) == referencia {
		t.Error("dois bancos partilham chave: um serviria a resposta do outro")
	}
}

// TestUmPeriodoFixoAusenteNaoEZero: nulo é «sem preferência» e o banco aplica o
// mais longo que tiver; zero é outro pedido. Colapsados na mesma chave, um
// servia a resposta do outro.
func TestUmPeriodoFixoAusenteNaoEZero(t *testing.T) {
	base := pedidoDe(t, "1987-03-14", "4321.50", "250000")

	zero := 0
	comZero := base
	comZero.PeriodoFixoAnos = &zero

	if chaveDe(t, "cgd", base) == chaveDe(t, "cgd", comZero) {
		t.Error("«sem preferência» e «zero anos» deram a mesma chave")
	}
}

// TestAOrdemDosProdutosContaParaAChave.
//
// ⚠️ **É conservador de propósito, e o comentário do `resumoDoPedido` di-lo:**
// tratar os produtos como conjunto era decidir por nossa conta que o banco os
// considera iguais em qualquer ordem. Um acerto a menos custa um pedido; um
// acerto a mais custa um preço errado, e é sempre esse o lado por que se erra.
func TestAOrdemDosProdutosContaParaAChave(t *testing.T) {
	base := pedidoDe(t, "1987-03-14", "4321.50", "250000")

	umaOrdem := base
	umaOrdem.Produtos = []string{"cgd:packs", "cgd:seguros"}
	outraOrdem := base
	outraOrdem.Produtos = []string{"cgd:seguros", "cgd:packs"}

	if chaveDe(t, "cgd", umaOrdem) == chaveDe(t, "cgd", outraOrdem) {
		t.Error("a ordem dos produtos não contou — é uma normalização, e não se fez nenhuma")
	}
	if chaveDe(t, "cgd", umaOrdem) == chaveDe(t, "cgd", base) {
		t.Error("um pedido com produtos deu a mesma chave que um sem nenhum")
	}
}

func chaveDe(t *testing.T, banco string, p dominio.Pedido) string {
	t.Helper()
	chave, err := cache.Chave(banco, p)
	if err != nil {
		t.Fatalf("derivar a chave: %v", err)
	}
	return chave
}

func pedidoDe(t *testing.T, nascimento, rendimento, montante string) dominio.Pedido {
	t.Helper()
	return dominio.Pedido{
		ValorImovel: dinheiro(t, "300000"),
		Montante:    dinheiro(t, montante),
		PrazoAnos:   30,
		TipoTaxa:    dominio.TaxaVariavel,
		Indexante:   dominio.Euribor12M,
		Titulares: []dominio.Titular{{
			DataNascimento:   data(t, nascimento),
			RendimentoMensal: dinheiro(t, rendimento),
		}},
		Finalidade:  dominio.FinalidadePropria,
		Localizacao: dominio.LocalizacaoContinente,
	}
}

func comMontante(t *testing.T, p dominio.Pedido, v string) dominio.Pedido {
	t.Helper()
	p.Montante = dinheiro(t, v)
	return p
}

func comRendimento(t *testing.T, p dominio.Pedido, v string) dominio.Pedido {
	t.Helper()
	titulares := make([]dominio.Titular, len(p.Titulares))
	copy(titulares, p.Titulares)
	titulares[0].RendimentoMensal = dinheiro(t, v)
	p.Titulares = titulares
	return p
}

func comNascimento(t *testing.T, p dominio.Pedido, v string) dominio.Pedido {
	t.Helper()
	titulares := make([]dominio.Titular, len(p.Titulares))
	copy(titulares, p.Titulares)
	titulares[0].DataNascimento = data(t, v)
	p.Titulares = titulares
	return p
}

func comPrazo(p dominio.Pedido, anos int) dominio.Pedido {
	p.PrazoAnos = anos
	return p
}

func comFinalidade(p dominio.Pedido, f dominio.Finalidade) dominio.Pedido {
	p.Finalidade = f
	return p
}

func comIndexante(p dominio.Pedido, i dominio.Indexante) dominio.Pedido {
	p.Indexante = i
	return p
}

func comJaCliente(p dominio.Pedido) dominio.Pedido {
	p.JaCliente = true
	return p
}

func dinheiro(t *testing.T, s string) dominio.Dinheiro {
	t.Helper()
	d, err := dominio.DinheiroDeTexto(s)
	if err != nil {
		t.Fatalf("dinheiro inválido %q: %v", s, err)
	}
	return d
}

func data(t *testing.T, s string) dominio.Data {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatalf("data inválida %q: %v", s, err)
	}
	feita, err := dominio.DataDe(d.Year(), d.Month(), d.Day())
	if err != nil {
		t.Fatalf("data inválida %q: %v", s, err)
	}
	return feita
}
