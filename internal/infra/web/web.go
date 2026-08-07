// Package web serve a fronteira HTTP da app (§6).
//
// ⚠️ **Era duas, e passou a uma** (Fase 6, passo 5): a fronteira congelada — o
// `/api/rate-catalog`, com a sua chave de máquina — saiu com o varrimento, que
// era quem lhe dava dados.
//
// ⚠️ **Traduz, não calcula.** Os números vêm do banco, pelo `aplicacao/aovivo`;
// aqui converte-se `dominio` para os tipos do contrato e escreve-se JSON. É a
// fronteira, e a §3 quer-la fina — cada regra que aqui entrasse deixava de poder
// ser afirmada sem um servidor de pé.
//
// ⚠️ E **um pedido, uma resposta**. Não há 202, não há identificador para
// sondar, não há progresso. O `/ofertas/{banco}` pergunta a UM banco, e o
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
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/transporte"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// Servidor serve o contrato.
type Servidor struct {
	registo *bancos.Registo
	agora   func() time.Time

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

	// contador e tecto são o limite de pedidos por IP. Contador nulo, ou tecto a
	// zero, desliga-o.
	contador Contador
	tecto    Tecto

	// lotacao é o tecto de pedidos em voo contra o MESMO banco (§7.2). Nula
	// desliga-o.
	//
	// ⚠️ Não é o `tecto` acima, e os dois nomes têm de continuar diferentes: o
	// `tecto` conta pedidos de um CLIENTE por janela de tempo e protege-nos a nós;
	// a `lotacao` conta pedidos NOSSOS em voo contra um banco e protege-o a ele.
	lotacao aovivo.Lotacao

	// cache serve respostas já dadas sem voltar a perguntar ao banco (§7.6).
	// Nula desliga-a. Vem sempre acompanhada do chaveDeCache.
	cache        Cache
	chaveDeCache ChaveDeCache

	// diario escreve uma linha por pedido. Nulo desliga-o.
	//
	// ⚠️ Chama-se `diario` e não `registo` porque `registo` já é o registo de
	// BANCOS, três campos acima. Dois significados no mesmo nome, dentro da
	// mesma struct, é como se escreve um erro que compila.
	diario *slog.Logger

	// origens são as que podem chamar `/api/v1/*` e o `/healthz` de dentro de um
	// browser. Vazio desliga o CORS — e desligado quer dizer «só a mesma
	// origem», não «toda a gente».
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

// ComLotacao liga o tecto de pedidos em voo por banco (§7.2).
//
// ⚠️ Método e não parâmetro do `Novo`, pela mesma razão do `ComTecto` — e com um
// efeito que não é o mesmo: sem ele, os testes da tradução falam com bancos
// falsos sem tecto nenhum, que é o que estão a medir. **Em produção liga-se
// sempre**, e é o `servir.go` que o garante.
func (s *Servidor) ComLotacao(lotacao aovivo.Lotacao) *Servidor {
	s.lotacao = lotacao
	return s
}

// Novo monta o servidor. `agora` nulo vale time.Now.
//
// ⚠️ **Deixou de receber uma fonte de dados** (Fase 6, passo 5). Recebia duas — a
// série do varrimento e o catálogo — e as duas morreram com ele. O que este
// servidor serve vem dos bancos, no momento do pedido, e o único estado que
// consulta é o tecto, a lotação e a cache, que entram pelos `Com...`.
func Novo(
	registo *bancos.Registo, agora func() time.Time,
) (*Servidor, error) {
	if registo == nil {
		return nil, errors.New("servidor sem registo de bancos")
	}
	if agora == nil {
		agora = time.Now
	}
	s := &Servidor{registo: registo, agora: agora}
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

	// ⚠️ **Havia aqui duas superfícies com políticas de acesso diferentes**, e o
	// grupo separava-as: dentro respondia-se a browsers, fora ficava o
	// `/api/rate-catalog`, que se autentica por `X-API-Key` — e uma chave dentro
	// de um bundle de browser é uma chave pública.
	//
	// **O de fora saiu com o varrimento** (Fase 6, passo 5): a série morreu e a
	// rota não tinha o que servir. O grupo **fica**, e não se dissolve no router:
	// é ele que mantém o CORS aplicado por decisão e não por omissão, e é onde
	// entra a próxima rota que não seja para browsers. Dissolvê-lo poupava uma
	// indentação e transformava a política numa coincidência.
	r.Group(func(g chi.Router) {
		g.Use(s.permitirOrigens)

		g.Get("/healthz", s.saude)
		g.Get("/api/v1/bancos", s.listarBancos)
		g.Post("/api/v1/ofertas/{banco}", s.ofertaDeUmBanco)

		// ⚠️ O preflight precisa de rota registada. Sem ela o chi responde 405
		// antes de o middleware correr, o browser não vê a permissão, e a app
		// falha com um erro de CORS que não nomeia nada. Os cabeçalhos vêm do
		// middleware; este handler só fecha a resposta com 204.
		g.Options("/api/v1/bancos", preflight)
		g.Options("/api/v1/ofertas/{banco}", preflight)
	})

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
// ⚠️ **Medido, e já não um palpite** (2026-08-06, 17h13, hora de expediente; 25
// simulações frias pelo caminho ao vivo — a tabela está no `DOSSIE-BANCOS.md`).
// O máximo observado foi **8,4 s, no Montepio**, e os 15 s são ~1,8× isso.
//
// ⚠️ **A margem é grande de propósito, e a razão é o Montepio.** A cauda dele é
// 4,2× a própria mediana, contra 1,7-2,5× nos outros quatro, e noutra medição do
// mesmo dia passou dos **10 s** em 4 cenários. Logo o 8,4 s é um limite inferior
// do pior caso — cinco amostras dão um máximo, não um percentil.
//
// ⚠️ **Continua a ser um número só para cinco bancos que diferem 7×**: o Novo
// Banco nunca passou de 1,15 s. Um prazo por banco é o que a `KAN-7` pede, e a
// medição que o permite fixar já existe (`make latencia`); o que falta é a
// decisão, porque diferenciar com n=5 arrisca cortar um banco lento que ia
// responder.
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

	// ⚠️ Os produtos vão ao `pedidoDe` agrupados pelo banco do CAMINHO, e não
	// atribuídos à mão a `pedido.Produtos`: atribuí-los saltava a verificação do
	// prefixo, e um produto de outro banco passava em silêncio — o
	// `ProdutosDoBanco` reparte-os por prefixo, este banco não o reclamava, e a
	// oferta saía sem ele, com o cliente convencido de que o tinha escolhido.
	var produtos map[string][]string
	if corpo.Produtos != nil {
		produtos = map[string][]string{id: *corpo.Produtos}
	}

	pedido, err := pedidoDe(corpo.Pedido, produtos)
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

	// ⚠️ **A cache lê-se ANTES da lotação, e a ordem é a razão de ela existir.**
	// Um acerto não pode gastar uma das duas vagas do banco: se gastasse, dez
	// clientes com o mesmo pedido continuavam a fazer fila uns pelos outros para
	// receberem uma resposta que já estava em Postgres.
	chave, acerto := s.lerDaCache(r.Context(), banco.ID(), pedido)
	if acerto != nil {
		escrever(w, http.StatusOK, *acerto)
		return
	}

	oferta, err := aovivo.PedirComVaga(r.Context(), s.lotacao, banco, pedido, s.agora, s.prazoDoBanco)
	if err != nil {
		// ⚠️ **503 e não 200 com a oferta em falha, e é a decisão inteira.** Um 200
		// com `banco_indisponivel` dizia à pessoa que o banco está em baixo; ele
		// está bem, e quem não tem lugar somos nós. O estatuto separa as duas
		// coisas para quem lê logs, e o `Retry-After` diz que isto passa sozinho —
		// ao contrário de um banco em baixo, que não passa por se esperar 1 s.
		if errors.Is(err, aovivo.ErrSemVaga) {
			w.Header().Set("Retry-After", "1")
			erro(w, http.StatusServiceUnavailable, "banco_ocupado",
				fmt.Sprintf(
					"Já vão pedidos nossos a mais em curso contra o %s. Tenta daqui a pouco.", banco.Nome()))
			return
		}
		// A lotação não respondeu: não se sabe se há vaga, e servir sem tecto é o
		// que o `PedirComVaga` recusa fazer. Quem está avariado somos nós.
		erro(w, http.StatusInternalServerError, "erro_interno",
			fmt.Sprintf("Não se conseguiu garantir o tecto de pedidos ao banco: %v", err))
		return
	}
	servida := ofertaDe(oferta)
	s.gravarNaCache(r.Context(), chave, banco.ID(), servida)
	escrever(w, http.StatusOK, servida)
}

// lerDaCache devolve a chave derivada e a resposta guardada, se houver.
//
// ⚠️ **Falha ABERTO, ao contrário da lotação, e a assimetria é deliberada.** Ali
// não se serve sem tecto garantido porque o tecto é a última coisa entre nós e o
// simulador de um terceiro; aqui o tecto continua de pé mesmo com a cache em
// baixo — perde-se a poupança de pedidos, não a protecção. Uma cache avariada a
// derrubar o serviço trocava uma degradação por uma indisponibilidade.
//
// A chave volta mesmo quando não há acerto: é a mesma que o `gravarNaCache` usa,
// e derivá-la duas vezes deixava as duas metades livres de divergir.
func (s *Servidor) lerDaCache(
	ctx context.Context, bancoID string, pedido dominio.Pedido,
) (string, *api.Oferta) {
	if s.cache == nil {
		return "", nil
	}
	chave, err := s.chaveDeCache(bancoID, pedido)
	if err != nil {
		return "", nil
	}
	oferta, houve, err := s.cache.Ler(ctx, chave, s.agora())
	if err != nil || !houve {
		return chave, nil
	}
	return chave, &oferta
}

// gravarNaCache guarda a resposta servida. Chave vazia é «não há cache».
//
// ⚠️ O erro descarta-se de propósito: a resposta já está pronta e correcta, e
// falhar um pedido de cliente porque não se conseguiu guardar uma cópia dela era
// deitar fora o trabalho que se acabou de pedir a um banco. Quem filtra o que
// não se guarda — uma oferta em falha — é o `infra/cache`.
func (s *Servidor) gravarNaCache(ctx context.Context, chave, bancoID string, oferta api.Oferta) {
	if s.cache == nil || chave == "" {
		return
	}

	// ⚠️ `context.WithoutCancel`, pela mesma razão do `sair` da lotação: a
	// resposta custou um pedido a um banco, e deitá-la fora porque o cliente
	// desligou entretanto é pagar o pedido e não ficar com nada. O próximo
	// cliente com o mesmo pedido merece o acerto.
	ctx, cancelar := context.WithTimeout(context.WithoutCancel(ctx), PrazoParaGravarNaCache)
	defer cancelar()

	_ = s.cache.Gravar(ctx, chave, bancoID, oferta, s.agora())
}
