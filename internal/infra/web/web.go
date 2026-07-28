// Package web serve as duas fronteiras HTTP do §6: a da app e a congelada.
//
// ⚠️ **Traduz, não calcula.** Os números saem do `aplicacao/comparar`; aqui
// converte-se `dominio` para os tipos do contrato e escreve-se JSON. É a
// fronteira, e a §3 quer-la fina — cada regra que aqui entrasse deixava de poder
// ser afirmada sem um servidor de pé.
//
// ⚠️ E **um pedido, uma resposta**. Não há 202, não há identificador para
// sondar, não há progresso: a comparação sai de uma consulta ao último
// varrimento mais aritmética local (§1, invertida a 2026-07-25).
package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/zepedrorodrigues/simulador-v2/api"
	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/comparar"
	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/varrimento"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// Fonte é de onde vêm as observações com que se responde.
//
// ⚠️ Declara-se aqui, do lado de quem a usa, e implementa-se no
// `infra/catalogo`. É o que deixa estes handlers ser afirmados com um catálogo
// em memória, sem Postgres — e é a mesma razão por que o `varrimento.Catalogo`
// existe.
type Fonte interface {
	UltimoVarrimento(ctx context.Context) ([]varrimento.Observacao, error)
}

// Servidor serve o contrato.
type Servidor struct {
	fonte    Fonte
	catalogo FonteDoCatalogo
	registo  *bancos.Registo
	agora    func() time.Time
}

// Novo monta o servidor. `agora` nulo vale time.Now.
func Novo(fonte Fonte, catalogo FonteDoCatalogo, registo *bancos.Registo, agora func() time.Time) (*Servidor, error) {
	if fonte == nil {
		return nil, errors.New("servidor sem fonte de observações — não haveria com que responder")
	}
	if registo == nil {
		return nil, errors.New("servidor sem registo de bancos")
	}
	if agora == nil {
		agora = time.Now
	}
	return &Servidor{fonte: fonte, catalogo: catalogo, registo: registo, agora: agora}, nil
}

// Rotas devolve o router com tudo montado.
func (s *Servidor) Rotas() http.Handler {
	r := chi.NewRouter()

	// ⚠️ O RequestID vai em todas as respostas, e o Recoverer impede que um
	// pânico nosso feche a ligação sem uma palavra. O Logger NÃO entra: ele
	// regista a query string, e a §6 diz que ela leva chaves — o
	// `/api/rate-catalog` autentica-se por `X-API-Key`.
	r.Use(middleware.RequestID)
	r.Use(devolverRequestID)
	r.Use(middleware.Recoverer)

	r.Get("/healthz", s.saude)
	r.Get("/api/v1/bancos", s.listarBancos)
	r.Post("/api/v1/comparacoes", s.compararOfertas)
	r.Get("/api/rate-catalog", s.obterRateCatalog)

	return r
}

// devolverRequestID põe na resposta o identificador que o middleware gerou.
//
// ⚠️ Vai na resposta e não só no log: sem ele, quem reporta um problema não tem
// como dizer QUAL das respostas correu mal, e nós não temos como a encontrar.
func devolverRequestID(seguinte http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id := middleware.GetReqID(r.Context()); id != "" {
			w.Header().Set("X-Request-ID", id)
		}
		seguinte.ServeHTTP(w, r)
	})
}

func (s *Servidor) saude(w http.ResponseWriter, _ *http.Request) {
	escrever(w, http.StatusOK, api.Saude{Estado: "ok"})
}

// listarBancos é a única fonte do formulário adaptativo da app.
//
// ⚠️ Sai do registo e dos `Requisitos` de cada banco, e não de uma lista escrita
// à mão: um banco que entre no registo aparece aqui sem que ninguém se lembre de
// o acrescentar, e um campo que ele deixe de usar desaparece do formulário.
func (s *Servidor) listarBancos(w http.ResponseWriter, r *http.Request) {
	todos, err := s.registo.Todos(transportesVazios())
	if err != nil {
		erro(w, http.StatusInternalServerError, "registo_indisponivel",
			fmt.Sprintf("Não se conseguiu montar a lista de bancos: %v", err))
		return
	}

	resposta := api.BancosResposta{
		Bancos:          make([]api.Banco, 0, len(todos)),
		InputsCanonicos: inputsCanonicos(),
	}
	for _, b := range todos {
		resposta.Bancos = append(resposta.Bancos, bancoDe(b.Requisitos()))
	}
	escrever(w, http.StatusOK, resposta)
}

// compararOfertas responde a um pedido com as ofertas de todos os bancos.
func (s *Servidor) compararOfertas(w http.ResponseWriter, r *http.Request) {
	var corpo api.ComparacaoPedido
	if err := json.NewDecoder(r.Body).Decode(&corpo); err != nil {
		erro(w, http.StatusBadRequest, "pedido_ilegivel",
			fmt.Sprintf("O corpo do pedido não é JSON válido: %v", err))
		return
	}

	pedido, err := pedidoDe(corpo)
	if err != nil {
		var validacao *dominio.ErroValidacao
		if errors.As(err, &validacao) {
			erroComCampo(w, http.StatusBadRequest, "pedido_invalido", validacao.Mensagem, validacao.Campo)
			return
		}
		erro(w, http.StatusBadRequest, "pedido_invalido", err.Error())
		return
	}

	obs, err := s.fonte.UltimoVarrimento(r.Context())
	if err != nil {
		// ⚠️ Um serviço sem varrimento nenhum não é «erro do banco»: é este
		// serviço ainda não ter dados. Dizê-lo assim evita que quem lê conclua
		// que os bancos estão em baixo.
		erro(w, http.StatusServiceUnavailable, "sem_varrimento",
			fmt.Sprintf("Ainda não há preços varridos com que responder: %v", err))
		return
	}

	catalogo, err := comparar.NovoCatalogo(obs)
	if err != nil {
		erro(w, http.StatusServiceUnavailable, "sem_varrimento",
			fmt.Sprintf("O último varrimento não serve para responder: %v", err))
		return
	}

	hoje := dominio.DataDeInstante(s.agora())
	ofertas, err := catalogo.Comparar(pedido, requisitosDe(s.registo), hoje)
	if err != nil {
		var validacao *dominio.ErroValidacao
		if errors.As(err, &validacao) {
			erroComCampo(w, http.StatusBadRequest, "pedido_invalido", validacao.Mensagem, validacao.Campo)
			return
		}
		erro(w, http.StatusBadRequest, "pedido_invalido", err.Error())
		return
	}

	escolhidos := escolher(ofertas, corpo.Bancos)
	resposta := api.Comparacao{
		CalculadoEm: s.agora().UTC(),
		Ofertas:     make([]api.Oferta, 0, len(escolhidos)),
	}
	for _, o := range escolhidos {
		resposta.Ofertas = append(resposta.Ofertas, ofertaDe(o))
	}
	escrever(w, http.StatusOK, resposta)
}

// escolher filtra as ofertas pelos bancos que o pedido nomeou. Lista vazia quer
// dizer todos — é o caso comum, e é o que a app faz no primeiro ecrã.
func escolher(ofertas []dominio.Oferta, ids []string) []dominio.Oferta {
	if len(ids) == 0 {
		return ofertas
	}
	querido := make(map[string]bool, len(ids))
	for _, id := range ids {
		querido[id] = true
	}
	saida := make([]dominio.Oferta, 0, len(ids))
	for _, o := range ofertas {
		if querido[o.BancoID] {
			saida = append(saida, o)
		}
	}
	return saida
}

func requisitosDe(r *bancos.Registo) map[string]dominio.Requisitos {
	todos, err := r.Todos(transportesVazios())
	if err != nil {
		return nil
	}
	m := make(map[string]dominio.Requisitos, len(todos))
	for _, b := range todos {
		m[b.ID()] = b.Requisitos()
	}
	return m
}

// transportesVazios constrói os bancos sem transporte nenhum.
//
// ⚠️ É seguro **e é o ponto**: nada nesta fronteira fala com um banco. Os
// construtores só guardam o que recebem, e um transporte nulo aqui garante que
// um handler que tentasse simular ao vivo rebentava em vez de o fazer em
// silêncio. A §1 diz que o caminho do cliente não toca nos bancos; isto torna-o
// verdade por construção.
func transportesVazios() bancos.Transportes { return bancos.Transportes{} }

// --- as respostas ----------------------------------------------------------------

func escrever(w http.ResponseWriter, estado int, corpo any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(estado)
	if err := json.NewEncoder(w).Encode(corpo); err != nil {
		// A resposta já foi começada; não há como a corrigir. Fica o registo.
		_ = err
	}
}

// erro escreve um erro estruturado.
//
// ⚠️ Duas partes, e é a lição do v1: `codigo` para a app decidir o que faz, e
// `mensagem` em português para a pessoa ler. O v1 devolvia texto solto, e a UI
// não conseguia distinguir «este banco não faz isto» de «este banco está em
// baixo».
func erro(w http.ResponseWriter, estado int, codigo, mensagem string) {
	erroComCampo(w, estado, codigo, mensagem, "")
}

func erroComCampo(w http.ResponseWriter, estado int, codigo, mensagem, campo string) {
	detalhe := api.DetalheErro{Codigo: codigo, Mensagem: mensagem}
	if campo != "" {
		detalhe.Campo = &campo
	}
	escrever(w, estado, api.RespostaErro{Erro: detalhe})
}
