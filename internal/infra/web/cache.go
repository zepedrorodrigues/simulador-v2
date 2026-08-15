package web

import (
	"context"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/api"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// A cache do caminho ao vivo, vista da fronteira (ARQUITETURA.md §4 e §7.6).
//
// ⚠️ **A porta declara-se aqui e não no `aovivo`, ao contrário da `Lotacao`**, e
// é consequência de o que se guarda ser a resposta JÁ TRADUZIDA para o contrato.
// Um caso de uso não conhece a `api.Oferta` — o `depguard` proíbe-lho, e com
// razão. O preço desta escolha está escrito na §4: um `comparar` que voltasse a
// fazer fan-out no servidor não herdava esta cache.

// PrazoParaGravarNaCache é o tecto de tempo da gravação, que corre já depois de
// o cliente ter a resposta. Guardar é uma ida à base e mais nada; se ela não
// responder neste tempo, desiste-se e o próximo cliente vai ao banco.
const PrazoParaGravarNaCache = 5 * time.Second

// Cache guarda e devolve respostas já servidas, por chave.
//
// ⚠️ A chave chega derivada: quem a deriva é o `infra/cache`, e o pedido em
// claro não atravessa esta porta.
type Cache interface {
	Ler(ctx context.Context, chave string, agora time.Time) (api.Oferta, bool, error)
	Gravar(ctx context.Context, chave, bancoID string, oferta api.Oferta, agora time.Time) error
}

// ChaveDeCache deriva a chave de um pedido a um banco. Injectável para que um
// teste possa afirmar sobre a cache sem depender da forma do resumo.
type ChaveDeCache func(bancoID string, p dominio.Pedido) (string, error)

// ComCache liga a cache das respostas ao vivo.
//
// ⚠️ Método e não parâmetro do `Novo`, pela mesma razão do `ComLotacao`: sem ele
// os testes da tradução falam com bancos falsos sem cache nenhuma, que é o que
// estão a medir. Em produção liga-se sempre, e é o `servir.go` que o garante.
//
// Cache ou derivador nulos desligam-na — as duas coisas são precisas para haver
// cache, e ter uma sem a outra é engano e não configuração.
func (s *Servidor) ComCache(cache Cache, chave ChaveDeCache) *Servidor {
	if cache == nil || chave == nil {
		return s
	}
	s.cache, s.chaveDeCache = cache, chave
	return s
}

// ComCatalogos liga a loja dos catálogos que os bancos publicam (KAN-36).
//
// ⚠️ **Método à parte do `ComCache`, e as duas não se juntam.** Guardam coisas
// com naturezas diferentes — uma resposta é de alguém, um catálogo é público — e
// a §4 dá-lhes tabelas diferentes por isso. Um `ComPostgres` que ligasse as duas
// de uma vez apagava a distinção no sítio onde ela se lê.
//
// Nula desliga: o banco vai à fonte de cada vez, que é o que fazia antes.
func (s *Servidor) ComCatalogos(c bancos.Catalogos) *Servidor {
	s.catalogos = c
	return s
}
