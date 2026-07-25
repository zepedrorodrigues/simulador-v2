package transporte_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/transporte"
)

func pedidoPOST(t *testing.T, ctx context.Context, corpo string) *http.Request {
	t.Helper()

	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, "https://exemplo.invalido/simular", strings.NewReader(corpo),
	)
	if err != nil {
		t.Fatalf("montar o pedido: %v", err)
	}
	return req
}

// O corpo fica gravado e continua por ler para quem receba o pedido a seguir —
// ler o corpo de um pedido esvazia-o, e um teste sobre o payload não pode
// depender de ser o primeiro a lê-lo.
func TestFalsoGravaOCorpoESemOGastar(t *testing.T) {
	t.Parallel()

	falso := &transporte.Falso{
		Responder: func(p transporte.PedidoGravado) (*http.Response, error) {
			return transporte.RespostaDeTexto(http.StatusOK, "vi: "+string(p.Corpo)), nil
		},
	}

	req := pedidoPOST(t, t.Context(), "montante=200000")
	resp, err := falso.Fazer(t.Context(), req)
	if err != nil {
		t.Fatalf("Fazer: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if corpo := lerTudo(t, resp); corpo != "vi: montante=200000" {
		t.Errorf("o Responder não viu o corpo: %q", corpo)
	}

	pedidos := falso.Pedidos()
	if len(pedidos) != 1 || string(pedidos[0].Corpo) != "montante=200000" {
		t.Fatalf("pedidos gravados = %+v", pedidos)
	}

	sobrou, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("reler o corpo: %v", err)
	}
	if string(sobrou) != "montante=200000" {
		t.Errorf("o corpo do pedido ficou gasto: %q", sobrou)
	}
}

func TestFalsoSemResponderResponde200(t *testing.T) {
	t.Parallel()

	falso := &transporte.Falso{}
	resp, err := falso.Fazer(t.Context(), pedidoPOST(t, t.Context(), ""))
	if err != nil {
		t.Fatalf("Fazer: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("estado = %d, esperava 200", resp.StatusCode)
	}
}

// O atraso é o que torna mensurável um banco que ignore o ctx — e para isso o
// falso tem de desistir com o ctx, como o net/http desiste.
func TestFalsoDesisteComOCtxEmVezDeCumprirOAtraso(t *testing.T) {
	t.Parallel()

	const atraso = 5 * time.Second

	falso := &transporte.Falso{Atraso: atraso}

	ctx, cancelar := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancelar()

	inicio := time.Now()
	resp, err := falso.Fazer(ctx, pedidoPOST(t, ctx, ""))
	if err == nil {
		_ = resp.Body.Close()
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("erro = %v, esperava o prazo esgotado", err)
	}
	if demorou := time.Since(inicio); demorou > time.Second {
		t.Errorf("desistiu ao fim de %s, com um prazo de 20ms", demorou)
	}
}

func TestFalsoArrancaEDevolveOHTML(t *testing.T) {
	t.Parallel()

	falso := &transporte.Falso{HTMLDeArranque: []byte("<html>H4SH</html>")}

	sessao, err := falso.Arrancar(t.Context(), "https://exemplo.invalido/simulador")
	if err != nil {
		t.Fatalf("Arrancar: %v", err)
	}
	if string(sessao.HTML) != "<html>H4SH</html>" {
		t.Errorf("HTML = %q", sessao.HTML)
	}
	if arranques := falso.Arranques(); len(arranques) != 1 {
		t.Errorf("arranques = %v, esperava um só", arranques)
	}
}

func TestFalsoDevolveOErroDeArranqueProgramado(t *testing.T) {
	t.Parallel()

	programado := errors.New("410 New open window with different context")
	falso := &transporte.Falso{ErroDeArranque: programado}

	if _, err := falso.Arrancar(t.Context(), "https://exemplo.invalido/simulador"); !errors.Is(err, programado) {
		t.Fatalf("erro = %v, esperava o programado", err)
	}
}

// O portão corre com -race e o modelo é fan-out sobre N bancos. Um transporte
// falso com corrida de dados dá um portão vermelho por culpa do teste — ou, pior,
// um portão verde e uma corrida a sério por descobrir.
func TestFalsoAguentaChamadasEmParalelo(t *testing.T) {
	t.Parallel()

	falso := &transporte.Falso{}

	var espera sync.WaitGroup
	for range 20 {
		espera.Add(1)
		go func() {
			defer espera.Done()
			resp, err := falso.Fazer(context.Background(), pedidoPOST(t, context.Background(), "x=1"))
			if err == nil {
				_ = resp.Body.Close()
			}
			_, _ = falso.Arrancar(context.Background(), "https://exemplo.invalido/simulador")
			_ = falso.Pedidos()
			_ = falso.Arranques()
		}()
	}
	espera.Wait()

	if n := len(falso.Pedidos()); n != 20 {
		t.Errorf("gravou %d pedidos, esperava 20", n)
	}
	if n := len(falso.Arranques()); n != 20 {
		t.Errorf("gravou %d arranques, esperava 20", n)
	}
}
