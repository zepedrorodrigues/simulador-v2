package aovivo_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/aovivo"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// Um banco que responde depressa devolve a oferta dele, e não outra coisa.
func TestOQueOBancoRespondeEOQueSai(t *testing.T) {
	t.Parallel()

	tan := taxa(t, "3.250")
	b := &bancoFalso{
		id: "prova", nome: "Banco de Prova",
		responder: func(context.Context, dominio.Pedido) (dominio.Oferta, error) {
			return dominio.Oferta{TAN: &tan}, nil
		},
	}

	o := aovivo.Pedir(context.Background(), b, pedido(t), agora, time.Second)

	if !o.Sucesso() {
		t.Fatalf("a oferta saiu em falha: %v", o.Erro)
	}
	if o.TAN == nil || !o.TAN.Equal(tan) {
		t.Errorf("TAN servida %v, o banco respondeu %s", o.TAN, tan)
	}
	// ⚠️ Quem sabe a quem se perguntou é este lado, e a oferta tem de o dizer
	// mesmo que o banco se esqueça de se identificar — e este esqueceu-se.
	if o.BancoID != "prova" || o.BancoNome != "Banco de Prova" {
		t.Errorf("a oferta não se identifica: id=%q nome=%q", o.BancoID, o.BancoNome)
	}
}

// TestAOfertaDizQuandoSeFalouComOBanco é a guarda que faltava, e a falta era
// silenciosa: o `dominio.Oferta` diz que o `CapturadoEm` é preenchido por esta
// camada, o varrimento preenchia-o, e o caminho ao vivo não.
//
// ⚠️ **O efeito não era um preço sem data — era não haver preço nenhum.** A
// fronteira recusa servir uma oferta boa sem instante de captura («um preço sem
// data apresenta-se como se fosse de agora»), portanto **todas** as ofertas ao
// vivo saíam como falha. Reverter o carimbo faz falhar isto e mais o
// TestUmaOfertaAoVivoESERVIDAComoBoa do `infra/web`, que é onde o estrago se vê.
func TestAOfertaDizQuandoSeFalouComOBanco(t *testing.T) {
	t.Parallel()

	b := &bancoFalso{
		id: "prova", nome: "Banco de Prova",
		responder: func(context.Context, dominio.Pedido) (dominio.Oferta, error) {
			return dominio.Oferta{}, nil // o banco não carimba nada, e é o contrato dele
		},
	}

	o := aovivo.Pedir(context.Background(), b, pedido(t), agora, time.Second)

	if o.CapturadoEm.IsZero() {
		t.Fatal("a oferta não diz quando se falou com o banco, e sem isso a fronteira não a serve")
	}
	// ⚠️ O instante é o do relógio que entrou, e não o de um varrimento: ao vivo
	// o preço é de agora, e é essa a afirmação que o `capturado_em` transporta.
	if !o.CapturadoEm.Equal(agora()) {
		t.Errorf("capturado em %s, e falou-se com o banco em %s", o.CapturadoEm, agora())
	}
}

// ⚠️ Um banco em baixo não é uma falha do pedido: é uma linha com o banco
// nomeado. O `Pedir` nunca devolve erro, e é isso que se afirma aqui.
func TestUmBancoEmBaixoSaiComoOfertaEmFaltaENaoComoErro(t *testing.T) {
	t.Parallel()

	b := &bancoFalso{
		id: "prova", nome: "Banco de Prova",
		responder: func(context.Context, dominio.Pedido) (dominio.Oferta, error) {
			return dominio.Oferta{}, errors.New("ligação recusada")
		},
	}

	o := aovivo.Pedir(context.Background(), b, pedido(t), agora, time.Second)

	if o.Sucesso() {
		t.Fatal("o banco recusou a ligação e a oferta saiu como boa")
	}
	if o.Erro.Codigo != dominio.ErroBancoIndisponivel {
		t.Errorf("código %q, esperava %q", o.Erro.Codigo, dominio.ErroBancoIndisponivel)
	}
	if !strings.Contains(o.Erro.Mensagem, "Banco de Prova") {
		t.Errorf("a mensagem não nomeia o banco: %q", o.Erro.Mensagem)
	}
}

// ⚠️ O prazo é por banco, e um banco surdo não pode segurar quem espera. Medido
// a 2026-08-06: o Montepio não respondeu dentro de 10 s em 4 cenários.
func TestUmBancoQueNaoRespondeADesistenciaNaoSeguraOPedido(t *testing.T) {
	t.Parallel()

	// Ignora o ctx de propósito: é o caso que o prazo existe para cobrir.
	surdo := &bancoFalso{
		id: "surdo", nome: "Banco Surdo",
		responder: func(context.Context, dominio.Pedido) (dominio.Oferta, error) {
			time.Sleep(2 * time.Second)
			return dominio.Oferta{}, nil
		},
	}

	inicio := time.Now()
	o := aovivo.Pedir(context.Background(), surdo, pedido(t), agora, 50*time.Millisecond)
	demorou := time.Since(inicio)

	if demorou > time.Second {
		t.Errorf("esperou-se %s por um banco com prazo de 50 ms", demorou)
	}
	if o.Sucesso() {
		t.Fatal("o banco não respondeu e a oferta saiu como boa")
	}
	if o.Erro.Codigo != dominio.ErroBancoIndisponivel {
		t.Errorf("código %q, esperava %q", o.Erro.Codigo, dominio.ErroBancoIndisponivel)
	}
	if !strings.Contains(o.Erro.Mensagem, "não desistiu") {
		t.Errorf("a mensagem não diz que o banco ignorou a desistência: %q", o.Erro.Mensagem)
	}
}

// ⚠️ Um pânico nosso não derruba a resposta ao cliente. No v1 isto obrigava a um
// except Exception dentro de cada scraper; aqui fica num sítio só.
func TestUmPanicoNossoNaoDerrubaARespostaAoCliente(t *testing.T) {
	t.Parallel()

	b := &bancoFalso{
		id: "prova", nome: "Banco de Prova",
		responder: func(context.Context, dominio.Pedido) (dominio.Oferta, error) {
			panic("índice fora dos limites")
		},
	}

	o := aovivo.Pedir(context.Background(), b, pedido(t), agora, time.Second)

	if o.Sucesso() {
		t.Fatal("houve um pânico e a oferta saiu como boa")
	}
	if !strings.Contains(o.Erro.Mensagem, "Erro nosso") {
		t.Errorf("a mensagem não assume o erro como nosso: %q", o.Erro.Mensagem)
	}
}

// ⚠️ Um erro que o banco já estruturou não se sobrepõe: ele sabe melhor do que
// nós o que lhe aconteceu.
func TestOErroQueOBancoEstruturouNaoSeSobrepoe(t *testing.T) {
	t.Parallel()

	b := &bancoFalso{
		id: "prova", nome: "Banco de Prova",
		responder: func(context.Context, dominio.Pedido) (dominio.Oferta, error) {
			return dominio.Oferta{}, &dominio.ErroOferta{
				Codigo:   dominio.ErroPrazoImpossivel,
				Mensagem: "O prazo não cabe na idade do titular.",
			}
		},
	}

	o := aovivo.Pedir(context.Background(), b, pedido(t), agora, time.Second)

	if o.Erro.Codigo != dominio.ErroPrazoImpossivel {
		t.Errorf("código %q, esperava %q — o erro do banco foi substituído pelo nosso",
			o.Erro.Codigo, dominio.ErroPrazoImpossivel)
	}
}

// Um pedido inválido não chega a incomodar o banco.
func TestUmPedidoInvalidoNaoChegaAoBanco(t *testing.T) {
	t.Parallel()

	var foi bool
	b := &bancoFalso{
		id: "prova", nome: "Banco de Prova",
		responder: func(context.Context, dominio.Pedido) (dominio.Oferta, error) {
			foi = true
			return dominio.Oferta{}, nil
		},
	}

	mau := pedido(t)
	mau.Montante = dominio.DinheiroDeInteiro(0)

	o := aovivo.Pedir(context.Background(), b, mau, agora, time.Second)

	if foi {
		t.Error("gastou-se um pedido a um banco com um pedido que não é válido")
	}
	if o.Sucesso() {
		t.Error("um pedido inválido saiu como oferta boa")
	}
}

// --- ajudantes -------------------------------------------------------------

type bancoFalso struct {
	id, nome  string
	responder func(context.Context, dominio.Pedido) (dominio.Oferta, error)
}

func (b *bancoFalso) ID() string                     { return b.id }
func (b *bancoFalso) Nome() string                   { return b.nome }
func (b *bancoFalso) Requisitos() dominio.Requisitos { return dominio.Requisitos{BancoID: b.id} }
func (b *bancoFalso) Simular(ctx context.Context, p dominio.Pedido) (dominio.Oferta, error) {
	return b.responder(ctx, p)
}

// agora é o relógio destes testes, e está **parado**. Com o `time.Now` o
// carimbo da captura só se podia afirmar por intervalo; com um instante fixo
// afirma-se por igualdade, e a falha diz que valor saiu.
var agora = func() time.Time { return time.Date(2026, 8, 6, 11, 30, 0, 0, time.UTC) }

func pedido(t *testing.T) dominio.Pedido {
	t.Helper()
	montante, err := dominio.DinheiroDeTexto("320000")
	if err != nil {
		t.Fatal(err)
	}
	return dominio.Pedido{
		ValorImovel: dominio.DinheiroDeInteiro(400_000),
		Montante:    montante,
		PrazoAnos:   30,
		TipoTaxa:    dominio.TaxaVariavel,
		Finalidade:  dominio.FinalidadePropria,
		Localizacao: dominio.LocalizacaoContinente,
		Titulares: []dominio.Titular{{
			DataNascimento:   dominio.DataDeInstante(agora().AddDate(-36, 0, 0)),
			RendimentoMensal: dominio.DinheiroDeInteiro(3200),
		}},
	}
}

func taxa(t *testing.T, s string) dominio.Taxa {
	t.Helper()
	v, err := dominio.TaxaDeTexto(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
