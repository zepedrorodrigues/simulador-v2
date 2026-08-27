package bancos_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/zepedrorodrigues/simulador-v2/internal/bancos"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/transporte"
)

// transportesDeTeste é o que a infra injectaria, com os dois transportes falsos.
func transportesDeTeste() bancos.Transportes {
	falso := &transporte.Falso{}
	return bancos.Transportes{HTTP: falso, ComSessao: falso}
}

func registarOuFalhar(t *testing.T, r *bancos.Registo, id string, c bancos.Construtor) {
	t.Helper()

	if err := r.Registar(id, c); err != nil {
		t.Fatalf("registar %q: %v", id, err)
	}
}

func TestRegistoConstroiComOsTransportesInjectados(t *testing.T) {
	t.Parallel()

	r := bancos.NovoRegisto()
	registarOuFalhar(t, r, "mentira", func(ts bancos.Transportes) bancos.Banco {
		return bancoSimples{tr: ts.HTTP}
	})
	registarOuFalhar(t, r, "mentira-com-sessao", func(ts bancos.Transportes) bancos.Banco {
		return bancoComSessao{tr: ts.ComSessao}
	})

	b, err := r.Construir("mentira-com-sessao", transportesDeTeste())
	if err != nil {
		t.Fatalf("construir: %v", err)
	}
	if b.ID() != "mentira-com-sessao" {
		t.Errorf("construiu %q, esperava %q", b.ID(), "mentira-com-sessao")
	}
}

func TestRegistoRecusaIDDesconhecido(t *testing.T) {
	t.Parallel()

	_, err := bancos.NovoRegisto().Construir("banco-que-nao-existe", transportesDeTeste())
	if !errors.Is(err, bancos.ErrBancoDesconhecido) {
		t.Fatalf("erro = %v, esperava ErrBancoDesconhecido", err)
	}
	if !strings.Contains(err.Error(), "banco-que-nao-existe") {
		t.Errorf("o erro não nomeia o id pedido: %v", err)
	}
}

// Um banco registado duas vezes calaria o primeiro em silêncio, e o que
// desaparecia da comparação era uma oferta — não uma linha de log.
func TestRegistoRecusaRegistoRepetido(t *testing.T) {
	t.Parallel()

	construtor := func(ts bancos.Transportes) bancos.Banco { return bancoSimples{tr: ts.HTTP} }

	r := bancos.NovoRegisto()
	registarOuFalhar(t, r, "mentira", construtor)

	if err := r.Registar("mentira", construtor); err == nil {
		t.Fatal("registar o mesmo id duas vezes passou")
	}
	if ids := r.IDs(); len(ids) != 1 {
		t.Errorf("ids = %v, esperava um só", ids)
	}
}

func TestRegistoRecusaIDVazioEConstrutorNulo(t *testing.T) {
	t.Parallel()

	r := bancos.NovoRegisto()

	if err := r.Registar("", func(bancos.Transportes) bancos.Banco { return nil }); err == nil {
		t.Error("registar com id vazio passou")
	}
	if err := r.Registar("mentira", nil); err == nil {
		t.Error("registar sem construtor passou")
	}
	if ids := r.IDs(); len(ids) != 0 {
		t.Errorf("ids = %v, esperava nenhum", ids)
	}
}

// A ordem é fixa e não a do mapa: a de um mapa é aleatória a cada corrida, e uma
// lista de bancos que troca de ordem sozinha faz o GET /api/v1/bancos parecer
// instável a quem o lê do outro lado.
func TestRegistoDevolveOsIDsPorOrdemAlfabetica(t *testing.T) {
	t.Parallel()

	r := bancos.NovoRegisto()
	for _, id := range []string{"santander", "cgd", "montepio", "bancoctt"} {
		registarOuFalhar(t, r, id, func(ts bancos.Transportes) bancos.Banco {
			return bancoSimples{tr: ts.HTTP}
		})
	}

	querido := []string{"bancoctt", "cgd", "montepio", "santander"}
	if ids := r.IDs(); !slices.Equal(ids, querido) {
		t.Errorf("ids = %v, esperava %v", ids, querido)
	}

	todos, err := r.Todos(transportesDeTeste())
	if err != nil {
		t.Fatalf("todos: %v", err)
	}
	if len(todos) != len(querido) {
		t.Fatalf("construiu %d bancos, esperava %d", len(todos), len(querido))
	}
}

func TestRegistoRecusaConstrutorQueDevolveNada(t *testing.T) {
	t.Parallel()

	r := bancos.NovoRegisto()
	registarOuFalhar(t, r, "vazio", func(bancos.Transportes) bancos.Banco { return nil })

	_, err := r.Construir("vazio", transportesDeTeste())
	if err == nil {
		t.Fatal("construir passou com um construtor que devolve nada")
	}
	// ⚠️ Nomeado, e não um erro qualquer: a fronteira distingue-o de um id que
	// não existe e de uma avaria nossa — é o que faz o BPI sem browser sair 404.
	if !errors.Is(err, bancos.ErrBancoIndisponivel) {
		t.Errorf("erro %v, esperava ErrBancoIndisponivel", err)
	}
}

// O registo do projecto: por cada banco que entra, o id que o registo anuncia é
// o id que o banco diz ter, e os requisitos que ele declara são servíveis pelo
// contrato.
//
// ⚠️ A contagem à cabeça não é decoração. Enquanto o registo esteve vazio, este
// ciclo não afirmava nada e passava a verde — um teste que percorre uma lista
// vazia é um teste que ninguém viu falhar. Com a CGD lá dentro (KAN-9) passa a
// haver mínimo, e apagar um banco do Predefinido reprova aqui.
func TestPredefinidoConstroiTodosOsBancosQueAnuncia(t *testing.T) {
	t.Parallel()

	r := bancos.Predefinido()
	if len(r.IDs()) == 0 {
		t.Fatal("o registo do projecto está vazio — não há banco nenhum para o varrimento correr")
	}

	for _, id := range r.IDs() {
		b, err := r.Construir(id, transportesDeTeste())
		if err != nil {
			// Bancos que precisam de browser (BPI) ficam indisponíveis sem ele.
			// O construtor devolve nil, e o registo trata isso como indisponível.
			if id == "bpi" {
				t.Skipf("bpi indisponível sem browser (esperado nos testes)")
			}
			t.Fatalf("construir %q: %v", id, err)
		}
		if b.ID() != id {
			t.Errorf("o banco registado em %q diz chamar-se %q", id, b.ID())
		}
		req := b.Requisitos()
		if err := req.Validar(); err != nil {
			t.Errorf("%q: requisitos não servíveis: %v", id, err)
		}
		if req.BancoID != id {
			t.Errorf("%q: os requisitos dizem BancoID %q", id, req.BancoID)
		}
	}
}
