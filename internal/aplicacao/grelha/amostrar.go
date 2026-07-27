package grelha

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/bancos"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// A ponte entre a descoberta e um banco a sério.
//
// O escala.go descreve o QUE se pergunta — amostrar aqui, bissectar ali — sem
// saber o que é um banco, e é isso que o torna afirmável contra funções de
// preço medidas, sem rede. Este ficheiro é a outra metade: pega num
// bancos.Banco e devolve o Amostrar que a descoberta consome.
//
// ⚠️ Varre-se o cenário de REFERÊNCIA e mais nada: taxa variável, habitação
// própria, sem produtos. Não é economia — é o que torna as dimensões somáveis
// em vez de multiplicáveis (ver o pontos.go). Está medido que o spread não
// depende do tipo de taxa nem do prazo nem do montante, por isso a escala de
// LTV medida na variável vale para as outras.

var (
	// ErrBancoSemSpread: o banco respondeu e não disse o spread. Não é falha de
	// rede e não se inventa: sem spread não há ponto na escala.
	ErrBancoSemSpread = errors.New("o banco respondeu sem spread")

	// ErrBancoRecusou: o banco não simulou este LTV. É o caso normal acima do
	// tecto que cada um financia, e é informação — é assim que a descoberta
	// descobre onde a escala acaba.
	ErrBancoRecusou = errors.New("o banco não simulou este LTV")
)

// AmostrarBanco constrói o Amostrar de um banco: varia o montante para obter o
// LTV pedido e lê o spread da resposta.
//
// hoje entra por parâmetro porque o domínio não tem relógio — é dele que sai a
// data de nascimento do titular neutro.
//
// ⚠️ agora é o relógio que carimba a captura de cada oferta, e não é opcional
// por capricho: um degrau grava-se como linha de catalogo_taxas, a coluna
// capturado_em é NOT NULL, e o carimbo só existia no caminho dos PONTOS (é o
// varrimento.simular que o põe). Sem ele, tudo o que a escala mede era recusado
// na gravação. Nulo vale time.Now, como no varrimento.Config.
//
// ⚠️ Carimba-se por amostra, e não uma vez no fim: as ~86 amostras de um banco
// levam perto de um minuto, e dizer que os quatro degraus foram capturados no
// mesmo instante era escrever uma hora que não aconteceu.
func AmostrarBanco(b bancos.Banco, ref Referencia, hoje dominio.Data, agora func() time.Time) (Amostrar, error) {
	if b == nil {
		return nil, errors.New("amostrar um banco nulo")
	}
	if agora == nil {
		agora = time.Now
	}
	base, err := ref.comOmissoes().pedido(hoje)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context, ltv dominio.Racio) (Medicao, error) {
		p := base
		p.Montante = dominio.MontanteParaLTV(p.ValorImovel, ltv)

		// ⚠️ O LTV que conta é o do montante ARREDONDADO, e é recalculado aqui.
		// Devolver o que se pediu punha na escala uma fronteira num ponto que
		// ninguém observou — e a escala é toda ela uma afirmação sobre pontos
		// observados.
		medido, err := dominio.LTV(p.Montante, p.ValorImovel)
		if err != nil {
			return Medicao{}, fmt.Errorf("LTV de %s sobre %s: %w", p.Montante, p.ValorImovel, err)
		}

		// Validar antes de ir à rede: um pedido que o próprio domínio recusa
		// gasta um pedido ao banco para receber um erro nosso, e escreve no
		// catálogo uma falha com o nome dele.
		if err := p.Validar(hoje); err != nil {
			return Medicao{}, fmt.Errorf("LTV %s dá um pedido inválido: %w", medido, err)
		}

		oferta, err := b.Simular(ctx, p)
		if err != nil {
			return Medicao{}, fmt.Errorf("%s em LTV %s: %w", b.ID(), medido, err)
		}
		if !oferta.Sucesso() {
			return Medicao{}, fmt.Errorf("%w: %s em LTV %s: %s",
				ErrBancoRecusou, b.ID(), medido, oferta.Erro.Mensagem)
		}
		if oferta.Spread == nil {
			return Medicao{}, fmt.Errorf("%w: %s em LTV %s", ErrBancoSemSpread, b.ID(), medido)
		}
		oferta.CapturadoEm = agora()

		// ⚠️ O pedido e a oferta vão inteiros, e não só o spread. Um degrau
		// grava-se como linha completa de catalogo_taxas — com TAN, TAEG,
		// prestação e MTIC —, e nada disso se deriva de um spread (§4).
		return Medicao{LTV: medido, Spread: *oferta.Spread, Pedido: p, Oferta: oferta}, nil
	}, nil
}

// DescobrirBanco mede a escala de LTV de um banco. É o AmostrarBanco e o
// Descobrir numa chamada, que é como quem varre os quer.
func DescobrirBanco(
	ctx context.Context, b bancos.Banco, ref Referencia, hoje dominio.Data,
	cfg Config, agora func() time.Time,
) (Descoberta, error) {
	amostrar, err := AmostrarBanco(b, ref, hoje, agora)
	if err != nil {
		return Descoberta{}, err
	}
	d, err := Descobrir(ctx, cfg, amostrar)
	if err != nil {
		return d, fmt.Errorf("escala de LTV do %s: %w", b.ID(), err)
	}
	return d, nil
}
