// Package web serve as duas fronteiras HTTP do §6: a da app e a congelada.
//
// ⚠️ **Traduz, não calcula.** Os números saem do `aplicacao/comparar`; aqui
// converte-se `dominio` para os tipos do contrato e escreve-se JSON. É a
// fronteira, e a §3 quer-la fina — cada regra que aqui entrasse deixava de poder
// ser afirmada sem um servidor de pé.
//
// ⚠️ E **um pedido, uma resposta**. Não há 202, não há identificador para
// sondar, não há progresso — nem no caminho antigo nem no novo. O que mudou com
// a reversão da §1 (2026-08-06) é de onde vem a resposta: o `/comparacoes` lê o
// último varrimento e calcula, o `/ofertas/{banco}` **pergunta ao banco**, e o
// progresso que a app mostra vem de ela pedir um banco de cada vez (D2).
package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/zepedrorodrigues/simulador-v2/api"
	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/aovivo"
	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/comparar"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/transporte"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// Fonte é de onde vêm as observações com que se responde.
//
// ⚠️ Declara-se aqui, do lado de quem a usa, e implementa-se no
// `infra/catalogo`. É o que deixa estes handlers ser afirmados com um catálogo
// em memória, sem Postgres — e é a mesma razão por que o `varrimento.Catalogo`
// existe.
type Fonte interface {
	SerieServivel(ctx context.Context) (comparar.Serie, error)
}

// Servidor serve o contrato.
type Servidor struct {
	fonte    Fonte
	catalogo FonteDoCatalogo
	registo  *bancos.Registo
	agora    func() time.Time

	// bancoAoVivo constrói um banco pronto a ser interrogado NESTE pedido.
	//
	// ⚠️ **Um por pedido, e não um partilhado.** O `transporte.ClienteComSessao`
	// tem um `cookiejar` — estado mutável partilhado. No varrimento isso era
	// seguro: um processo, um banco de cada vez, com o travão a impedir duas
	// corridas. Ao vivo, um jar partilhado faz o `Arrancar` de um cliente
	// sobrepor-se à sessão de outro a meio, e o efeito é o pior possível — falhas
	// intermitentes que parecem do banco.
	//
	// ⚠️ O cliente HTTP **sem** sessão continua partilhado: não tem estado e o
	// `http.Client` é seguro para uso concorrente. É a sessão que obriga, e só ela.
	bancoAoVivo func(id string) (bancos.Banco, error)

	// prazoDoBanco é quanto se espera por um banco antes de o dar como
	// indisponível. ⚠️ Medido a 2026-08-06: o Montepio não respondeu dentro de
	// 10 s em 4 cenários, em hora de expediente.
	prazoDoBanco time.Duration

	// chaves são as credenciais de máquina do /api/rate-catalog. Vazio deixa a
	// fronteira aberta — modo de desenvolvimento, com aviso alto no arranque.
	chaves []string

	// contador e tecto são o limite de pedidos por IP. Contador nulo, ou tecto a
	// zero, desliga-o.
	contador Contador
	tecto    Tecto

	// diario escreve uma linha por pedido. Nulo desliga-o.
	//
	// ⚠️ Chama-se `diario` e não `registo` porque `registo` já é o registo de
	// BANCOS, três campos acima. Dois significados no mesmo nome, dentro da
	// mesma struct, é como se escreve um erro que compila.
	diario *slog.Logger

	// origens são as que podem chamar `/api/v1/*` e o `/healthz` de dentro de um
	// browser. Vazio desliga o CORS — e desligado quer dizer «só a mesma
	// origem», não «toda a gente». ⚠️ O `/api/rate-catalog` fica sempre de fora:
	// autentica-se por chave, e uma chave num browser é pública (§6).
	origens []string
}

// ComOrigens liga o CORS à lista dada.
//
// ⚠️ Método e não parâmetro do `Novo`, pela mesma razão do `ComTecto`: é
// opcional, e obrigar os testes da tradução a escolher uma lista fazia-os
// declarar uma política que não estão a medir.
func (s *Servidor) ComOrigens(origens []string) *Servidor {
	s.origens = origens
	return s
}

// ComAoVivo troca quem constrói o banco a interrogar e quanto se espera por ele.
//
// ⚠️ Método e não parâmetro do `Novo`, pela mesma razão do `ComTecto`: por
// omissão o servidor fala com os bancos a sério, e obrigar cada teste da
// tradução a declarar um construtor fazia-o escolher uma coisa que não está a
// medir.
//
// ⚠️ **E é isto que torna o caminho ao vivo afirmável sem rede.** Sem esta
// porta, o único teste possível a esta rota era um que fosse mesmo ao banco — e
// aí o portão passava a depender do expediente deles e a somar aos ~10 pedidos
// por comparação que a §7 conta. Prazo a zero ou negativo mantém o de omissão.
func (s *Servidor) ComAoVivo(construir func(id string) (bancos.Banco, error), prazo time.Duration) *Servidor {
	if construir != nil {
		s.bancoAoVivo = construir
	}
	if prazo > 0 {
		s.prazoDoBanco = prazo
	}
	return s
}

// ComTecto liga o limite de pedidos por IP.
//
// ⚠️ É um método e não um parâmetro do Novo porque o tecto é opcional: os testes
// da tradução e das guardas não precisam dele, e obrigá-los a passá-lo fazia
// cada um decidir um número que não está a medir.
func (s *Servidor) ComTecto(contador Contador, tecto Tecto) *Servidor {
	s.contador, s.tecto = contador, tecto
	return s
}

// Novo monta o servidor. `agora` nulo vale time.Now.
func Novo(
	fonte Fonte, catalogo FonteDoCatalogo, registo *bancos.Registo,
	chaves []string, agora func() time.Time,
) (*Servidor, error) {
	if fonte == nil {
		return nil, errors.New("servidor sem fonte de observações — não haveria com que responder")
	}
	if registo == nil {
		return nil, errors.New("servidor sem registo de bancos")
	}
	if agora == nil {
		agora = time.Now
	}
	s := &Servidor{fonte: fonte, catalogo: catalogo, registo: registo, chaves: chaves, agora: agora}
	s.bancoAoVivo = s.construirAoVivo
	s.prazoDoBanco = PrazoDoBancoOmissao
	return s, nil
}

// Rotas devolve o router com tudo montado.
func (s *Servidor) Rotas() http.Handler {
	r := chi.NewRouter()

	// ⚠️ O RequestID vai em todas as respostas, e o Recoverer impede que um
	// pânico nosso feche a ligação sem uma palavra. O Logger NÃO entra: ele
	// regista a query string, e a §6 diz que ela leva chaves — o
	// `/api/rate-catalog` autentica-se por `X-API-Key`.
	// ⚠️ A defesa entra PRIMEIRO, e o sítio é a decisão (KAN-46). Montada mais
	// abaixo, punha os cabeçalhos nas respostas boas e não nas que o Recoverer
	// e o `limitar` produzem — que são precisamente aquelas em que uma resposta
	// mal interpretada faz mais estrago.
	r.Use(defesa)
	r.Use(middleware.RequestID)
	r.Use(devolverRequestID)
	// ⚠️ O registo entra a seguir ao RequestID — precisa dele — e **antes** do
	// Recoverer, para que um pânico apareça na linha com o estatuto 500 que o
	// cliente levou. Depois dele, o pedido que rebentou era o único que não
	// deixava rasto.
	r.Use(s.registar)
	r.Use(middleware.Recoverer)
	// ⚠️ O tecto entra DEPOIS do RequestID e do Recoverer: um 429 tem de levar o
	// identificador como qualquer outra resposta, e um pânico dentro do tecto não
	// pode fechar a ligação sem uma palavra.
	r.Use(s.limitar)

	// ⚠️ **Duas superfícies com políticas de acesso diferentes**, e a separação é
	// literal: o que está dentro deste grupo responde a browsers; o que está
	// fora, não. A §6 explica porquê — o `/api/rate-catalog` autentica-se por
	// `X-API-Key`, e uma chave dentro de um bundle de browser é uma chave
	// pública. Juntá-los num router só abria a porta do catálogo à app.
	r.Group(func(g chi.Router) {
		g.Use(s.permitirOrigens)

		g.Get("/healthz", s.saude)
		g.Get("/api/v1/bancos", s.listarBancos)
		g.Post("/api/v1/comparacoes", s.compararOfertas)
		g.Post("/api/v1/ofertas/{banco}", s.ofertaDeUmBanco)

		// ⚠️ O preflight precisa de rota registada. Sem ela o chi responde 405
		// antes de o middleware correr, o browser não vê a permissão, e a app
		// falha com um erro de CORS que não nomeia nada. Os cabeçalhos vêm do
		// middleware; este handler só fecha a resposta com 204.
		g.Options("/api/v1/bancos", preflight)
		g.Options("/api/v1/comparacoes", preflight)
		g.Options("/api/v1/ofertas/{banco}", preflight)
	})

	r.Get("/api/rate-catalog", s.exigirChave(s.obterRateCatalog))
	r.Get("/api/rate-catalog/snapshots", s.exigirChave(s.obterSnapshots))

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

	serie, err := s.fonte.SerieServivel(r.Context())
	if err != nil {
		// ⚠️ Um serviço sem varrimento nenhum não é «erro do banco»: é este
		// serviço ainda não ter dados. Dizê-lo assim evita que quem lê conclua
		// que os bancos estão em baixo.
		erro(w, http.StatusServiceUnavailable, "sem_varrimento",
			fmt.Sprintf("Ainda não há preços varridos com que responder: %v", err))
		return
	}

	catalogo, err := comparar.NovoCatalogo(serie)
	if err != nil {
		erro(w, http.StatusServiceUnavailable, "sem_varrimento",
			fmt.Sprintf("A série varrida não serve para responder: %v", err))
		return
	}

	hoje := dominio.DataDeInstante(s.agora())
	// ⚠️ Os bancos pedidos vão para dentro do `Comparar`, e não se filtram à
	// saída. Filtrar à saída era o que se fazia até 2026-08-01, e só conseguia
	// tirar da lista — um banco pedido que a grelha não tinha nunca lá chegava
	// para ser tirado, e desaparecia sem uma palavra (KAN-45).
	ofertas, err := catalogo.Comparar(pedido, corpo.Bancos, requisitosDe(s.registo), hoje)
	if err != nil {
		var validacao *dominio.ErroValidacao
		if errors.As(err, &validacao) {
			erroComCampo(w, http.StatusBadRequest, "pedido_invalido", validacao.Mensagem, validacao.Campo)
			return
		}
		erro(w, http.StatusBadRequest, "pedido_invalido", err.Error())
		return
	}

	resposta := api.Comparacao{
		CalculadoEm: s.agora().UTC(),
		Ofertas:     make([]api.Oferta, 0, len(ofertas)),
	}
	for _, o := range ofertas {
		resposta.Ofertas = append(resposta.Ofertas, ofertaDe(o))
	}
	escrever(w, http.StatusOK, resposta)
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
// ⚠️ É para quem só lhes quer **ler os requisitos** — o formulário e a lista de
// bancos. Os construtores só guardam o que recebem, e um transporte nulo garante
// que um handler que tentasse simular por aqui rebentava em vez de falar com o
// banco em silêncio.
//
// ⚠️ Dizia aqui «nada nesta fronteira fala com um banco», e deixou de ser
// verdade com a reversão da §1 (2026-08-06): quem fala é o `ofertaDeUmBanco`, e
// por transportes que constrói **para aquele pedido** — nunca por estes.
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

// PrazoDoBancoOmissao é quanto se espera por um banco antes de o dar como
// indisponível.
//
// ⚠️ **É um valor de partida e não uma medição.** Os 15 s vêm de os 10 s do
// varrimento não terem chegado ao Montepio a 2026-08-06 — 4 cenários passaram
// desse prazo. O número certo mede-se banco a banco, e é um dos três que a Fase
// 6 do `PLAN.md` diz que faltam.
const PrazoDoBancoOmissao = 15 * time.Second

// construirAoVivo monta um banco com transportes **deste pedido**.
//
// ⚠️ A sessão é por pedido pela razão escrita no campo `bancoAoVivo`: o jar de
// cookies é estado partilhado, e ao vivo dois clientes pisavam-se.
func (s *Servidor) construirAoVivo(id string) (bancos.Banco, error) {
	comSessao, err := transporte.NovoClienteComSessao(nil)
	if err != nil {
		return nil, fmt.Errorf("montar o transporte com sessão: %w", err)
	}
	return s.registo.Construir(id, bancos.Transportes{
		HTTP:      transporte.NovoCliente(nil),
		ComSessao: comSessao,
	})
}

// ofertaDeUmBanco pergunta a UM banco, ao vivo (KAN-7).
//
// ⚠️ Um banco que não responde a tempo sai com **200** e a oferta em falha, e
// não com 5xx: não é este pedido que falhou, é aquele banco que não respondeu. É
// o mesmo vocabulário do `/api/v1/comparacoes`, e é o que deixa a app pôr uma
// linha com o banco nomeado em vez de um ecrã de erro.
func (s *Servidor) ofertaDeUmBanco(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "banco")

	var corpo api.OfertaPedido
	if err := json.NewDecoder(r.Body).Decode(&corpo); err != nil {
		erro(w, http.StatusBadRequest, "pedido_ilegivel",
			fmt.Sprintf("O corpo do pedido não é JSON válido: %v", err))
		return
	}

	// ⚠️ Os produtos entram pelo mesmo `pedidoDe` do `/comparacoes`, agrupados
	// pelo banco do CAMINHO. Atribuí-los à mão a `pedido.Produtos` saltava a
	// verificação do prefixo, e um produto de outro banco passava em silêncio: o
	// `ProdutosDoBanco` reparte-os por prefixo, este banco não o reclamava, e a
	// oferta saía sem ele — com o cliente convencido de que o tinha escolhido.
	var produtos *map[string][]string
	if corpo.Produtos != nil {
		produtos = &map[string][]string{id: *corpo.Produtos}
	}

	pedido, err := pedidoDe(api.ComparacaoPedido{Pedido: corpo.Pedido, Produtos: produtos})
	if err != nil {
		var validacao *dominio.ErroValidacao
		if errors.As(err, &validacao) {
			erroComCampo(w, http.StatusBadRequest, "pedido_invalido", validacao.Mensagem, validacao.Campo)
			return
		}
		erro(w, http.StatusBadRequest, "pedido_invalido", err.Error())
		return
	}

	// ⚠️ **O pedido valida-se aqui, e não só lá dentro.** O `aovivo.Pedir` também
	// o valida — e tem de o fazer, é a guarda dele contra gastar um pedido a um
	// banco —, mas o que ele devolve é uma OFERTA em falha, com o vocabulário dos
	// bancos: um montante impossível saía com `200` e `resposta_ilegivel`, a
	// culpar a resposta de um banco a quem nunca se perguntou. Quem errou foi quem
	// pediu, e a resposta é a mesma do `/comparacoes`: 400 com o campo nomeado.
	if err := pedido.Validar(dominio.DataDeInstante(s.agora())); err != nil {
		var validacao *dominio.ErroValidacao
		if errors.As(err, &validacao) {
			erroComCampo(w, http.StatusBadRequest, "pedido_invalido", validacao.Mensagem, validacao.Campo)
			return
		}
		erro(w, http.StatusBadRequest, "pedido_invalido", err.Error())
		return
	}

	banco, err := s.bancoAoVivo(id)
	if err != nil {
		if errors.Is(err, bancos.ErrBancoDesconhecido) {
			erro(w, http.StatusNotFound, "banco_desconhecido",
				fmt.Sprintf("Não há banco nenhum com o id %q.", id))
			return
		}
		erro(w, http.StatusInternalServerError, "erro_interno",
			fmt.Sprintf("Não se conseguiu preparar o pedido ao banco: %v", err))
		return
	}

	oferta := aovivo.Pedir(r.Context(), banco, pedido, s.agora, s.prazoDoBanco)
	escrever(w, http.StatusOK, ofertaDe(oferta))
}
