package web_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/zepedrorodrigues/simulador-v2/api"
	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/aovivo"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
	"github.com/zepedrorodrigues/simulador-v2/internal/infra/lotacao"
	"github.com/zepedrorodrigues/simulador-v2/internal/infra/web"
)

// O tecto de pedidos em voo por banco, visto da fronteira (KAN-7, §7.2).

// TestUmBancoNoTectoResponde503ENao200ComOfertaEmFalha é onde a distinção se vê
// de fora.
//
// ⚠️ **As duas respostas dizem coisas diferentes a quem lê logs e à app.** O 200
// com `banco_indisponivel` diz «este banco não respondeu» — e a app risca-o da
// lista. O 503 `banco_ocupado` diz «nós é que não temos lugar», passa sozinho, e
// traz `Retry-After`. Reverter para servir a oferta em falha faz este teste dizer
// que se culpou o banco por um limite nosso.
func TestUmBancoNoTectoResponde503ENao200ComOfertaEmFalha(t *testing.T) {
	var perguntas atomic.Int32
	s := servidorAoVivo(t, &bancoFalso{
		id: "cgd", nome: "CGD",
		responder: func(context.Context, dominio.Pedido) (dominio.Oferta, error) {
			perguntas.Add(1)
			return dominio.Oferta{}, nil
		},
	}).ComLotacao(&lotacaoFalsa{erro: aovivo.ErrSemVaga})

	resposta := pedirOferta(t, s, "cgd", corpoDeOferta(nil))
	if resposta.Code != http.StatusServiceUnavailable {
		t.Fatalf("estado %d, esperava 503: %s", resposta.Code, resposta.Body.String())
	}

	var e api.RespostaErro
	lerJSON(t, resposta, &e)
	if e.Erro.Codigo != "banco_ocupado" {
		t.Errorf("código %q — «estamos cheios» tem de se distinguir de «o banco não respondeu»",
			e.Erro.Codigo)
	}
	// ⚠️ Nomeado, como a recusa de um banco em baixo: a app pede um banco de cada
	// vez, e uma resposta que não diz qual obriga-a a adivinhar de qual era.
	if !strings.Contains(e.Erro.Mensagem, "CGD") {
		t.Errorf("a recusa não nomeia o banco: %q", e.Erro.Mensagem)
	}
	// ⚠️ O Retry-After é o que separa isto de uma avaria: isto passa por si.
	if retry := resposta.Header().Get("Retry-After"); retry == "" {
		t.Error("um 503 que passa sozinho tem de dizer daqui a quanto")
	}
	if n := perguntas.Load(); n != 0 {
		t.Errorf("com o banco no tecto saíram-lhe %d pedidos", n)
	}
}

// TestUmaLotacaoAvariadaNaoSeServeSemTecto: a lotação que não responde fecha o
// caminho, e a resposta diz que o avariado somos nós.
//
// ⚠️ 500 e não 503: um 503 com `Retry-After` prometia que passa daqui a um
// segundo, e uma base em baixo não passa por se esperar um segundo.
func TestUmaLotacaoAvariadaNaoSeServeSemTecto(t *testing.T) {
	var perguntas atomic.Int32
	s := servidorAoVivo(t, &bancoFalso{
		id: "cgd", nome: "CGD",
		responder: func(context.Context, dominio.Pedido) (dominio.Oferta, error) {
			perguntas.Add(1)
			return dominio.Oferta{}, nil
		},
	}).ComLotacao(&lotacaoFalsa{erro: errors.New("a base não responde")})

	resposta := pedirOferta(t, s, "cgd", corpoDeOferta(nil))
	if resposta.Code != http.StatusInternalServerError {
		t.Fatalf("estado %d, esperava 500: %s", resposta.Code, resposta.Body.String())
	}
	if n := perguntas.Load(); n != 0 {
		t.Errorf("sem tecto garantido saíram %d pedidos ao banco", n)
	}
}

// TestComVagaOCaminhoSegueEAVagaVolta: o caso normal continua a ser o caso
// normal, e a vaga não fica presa a um pedido que já acabou.
func TestComVagaOCaminhoSegueEAVagaVolta(t *testing.T) {
	tan := taxa(t, "3.250")
	lot := &lotacaoFalsa{}
	s := servidorAoVivo(t, &bancoFalso{
		id: "cgd", nome: "CGD",
		responder: func(context.Context, dominio.Pedido) (dominio.Oferta, error) {
			return dominio.Oferta{TAN: &tan}, nil
		},
	}).ComLotacao(lot)

	resposta := pedirOferta(t, s, "cgd", corpoDeOferta(nil))
	if resposta.Code != http.StatusOK {
		t.Fatalf("estado %d: %s", resposta.Code, resposta.Body.String())
	}
	if n := lot.saidas.Load(); n != 1 {
		t.Errorf("a vaga voltou %d vezes, esperava 1", n)
	}
}

// TestUmVagasPorBancoIlegivelFalhaOArranque, pela mesma razão das origens: um
// `VAGAS_POR_BANCO=dez` que caísse no de omissão deixava alguém convencido de
// que tinha subido o tecto — e o sintoma seria o tráfego que não subiu.
func TestUmVagasPorBancoIlegivelFalhaOArranque(t *testing.T) {
	for _, bruto := range []string{"dez", "2,5", "0", "-1"} {
		if vagas, err := web.VagasDe(bruto); err == nil {
			t.Errorf("VAGAS_POR_BANCO=%q passou e valeu %d", bruto, vagas)
		}
	}

	vagas, err := web.VagasDe("")
	if err != nil {
		t.Fatalf("vazio devia valer o de omissão: %v", err)
	}
	if vagas != lotacao.VagasOmissao {
		t.Errorf("vazio valeu %d, esperava o de omissão (%d)", vagas, lotacao.VagasOmissao)
	}
}

// lotacaoFalsa recusa com `erro`, ou dá vaga e conta as saídas.
type lotacaoFalsa struct {
	erro   error
	saidas atomic.Int32
}

func (l *lotacaoFalsa) Entrar(context.Context, string) (aovivo.Sair, error) {
	if l.erro != nil {
		return nil, l.erro
	}
	return func() { l.saidas.Add(1) }, nil
}
