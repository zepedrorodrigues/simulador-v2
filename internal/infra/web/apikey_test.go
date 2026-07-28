package web_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zepedrorodrigues/simulador-v2/internal/infra/web"
)

// O portão da fronteira congelada.
//
// ⚠️ Isto não é autenticação de utilizador — este serviço não tem contas e não
// vai ter. É a credencial de máquina que o `viabilidade-imobiliaria` apresenta.

// chaveDeProva tem 32 caracteres, que é o mínimo.
const chaveDeProva = "abcdefghijklmnopqrstuvwxyz012345"

func TestSemChaveConfiguradaAFronteiraFicaAberta(t *testing.T) {
	// ⚠️ Deliberado: é o modo de desenvolvimento, para quem clona o repositório
	// poder correr sem configurar nada. Quem serve avisa alto no arranque.
	s := servidorComCatalogo(t, nil)

	if estado := pedirCatalogo(t, s, ""); estado != http.StatusOK {
		t.Errorf("estado %d sem chaves configuradas, e a fronteira devia estar aberta", estado)
	}
}

func TestComChaveConfiguradaSoPassaQuemAApresenta(t *testing.T) {
	s := servidorComCatalogo(t, nil, chaveDeProva)

	casos := []struct {
		nome      string
		apresenta string
		esperado  int
	}{
		{"a chave certa", chaveDeProva, http.StatusOK},
		{"sem chave nenhuma", "", http.StatusUnauthorized},
		{"uma chave errada", strings.Repeat("x", 32), http.StatusUnauthorized},
		// ⚠️ Um prefixo certo não passa. Parece óbvio, e é o que a comparação em
		// tempo constante existe para garantir: com `==`, o tempo de resposta
		// revela quantos bytes do prefixo estão certos.
		{"um prefixo da chave certa", chaveDeProva[:20], http.StatusUnauthorized},
		{"a chave com um byte a mais", chaveDeProva + "x", http.StatusUnauthorized},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if estado := pedirCatalogo(t, s, c.apresenta); estado != c.esperado {
				t.Errorf("estado %d, esperava %d", estado, c.esperado)
			}
		})
	}
}

// TestUmaChaveCurtaNaoArrancaOServico: uma chave curta é força-brutável, e
// descobri-lo ao primeiro pedido de produção é tarde.
func TestUmaChaveCurtaNaoArrancaOServico(t *testing.T) {
	_, err := web.ChavesDe("demasiado-curta")
	if err == nil {
		t.Fatal("uma chave de 15 caracteres foi aceite")
	}
	if !strings.Contains(err.Error(), "força-brutável") {
		t.Errorf("o erro não diz porque é que é curta de mais: %v", err)
	}

	// ⚠️ E não é ignorada em silêncio: ignorá-la deixava o serviço a correr com
	// menos portas fechadas do que quem o configurou pensa.
	chaves, err := web.ChavesDe(chaveDeProva + ",curta")
	if err == nil {
		t.Errorf("a chave curta passou ao lado de uma boa: %v", chaves)
	}
}

func TestVariasChavesValemTodas(t *testing.T) {
	// Duas chaves: é como se roda uma credencial sem parar o consumidor.
	segunda := strings.Repeat("z", 40)
	chaves, err := web.ChavesDe(chaveDeProva + " , " + segunda)
	if err != nil {
		t.Fatalf("ChavesDe: %v", err)
	}
	if len(chaves) != 2 {
		t.Fatalf("esperava duas chaves, vieram %d", len(chaves))
	}

	s := servidorComCatalogo(t, nil, chaves...)
	for _, chave := range chaves {
		if estado := pedirCatalogo(t, s, chave); estado != http.StatusOK {
			t.Errorf("a chave %q… não passou: estado %d", chave[:8], estado)
		}
	}
}

// TestAChaveNuncaViajaNaQueryString: pô-la no URL punha-a nos logs, no histórico
// do browser e nos Referer.
func TestAChaveNuncaViajaNaQueryString(t *testing.T) {
	s := servidorComCatalogo(t, nil, chaveDeProva)

	pedido := httptest.NewRequest(http.MethodGet, "/api/rate-catalog?api_key="+chaveDeProva, nil)
	resposta := httptest.NewRecorder()
	s.Rotas().ServeHTTP(resposta, pedido)

	if resposta.Code != http.StatusUnauthorized {
		t.Errorf("estado %d: a chave na query string foi aceite", resposta.Code)
	}
}

func pedirCatalogo(t *testing.T, s *web.Servidor, chave string) int {
	t.Helper()
	pedido := httptest.NewRequest(http.MethodGet, "/api/rate-catalog", nil)
	if chave != "" {
		pedido.Header.Set(web.CabecalhoDaChave, chave)
	}
	resposta := httptest.NewRecorder()
	s.Rotas().ServeHTTP(resposta, pedido)
	return resposta.Code
}
