package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// CORS: quem pode chamar esta API de dentro de um browser.
//
// ⚠️ Aplica-se a `/api/v1/*` e ao `/healthz`, e **não** ao `/api/rate-catalog`
// (§6 do ARQUITETURA.md). Aquele endpoint autentica-se por `X-API-Key`, e uma
// chave dentro de um bundle de browser é uma chave pública: qualquer pessoa a lê
// nas ferramentas de programador. Não abrir a porta custa uma linha; confiar que
// ninguém a atravessa custa a chave.

// OrigensDe lê a lista de origens permitidas da variável de ambiente.
//
// Formato: origens separadas por vírgula, cada uma `esquema://host[:porta]`.
// Vazio devolve lista vazia, e isso desliga o CORS por completo — que é o
// comportamento seguro: sem cabeçalhos, um browser só deixa passar pedidos da
// mesma origem.
//
// ⚠️ Recusa `*` **a nomear-lhe a razão**. É a configuração que alguém escreve às
// duas da manhã para «desbloquear» a app, e o custo dela não é visível no
// momento em que resolve o problema.
func OrigensDe(bruto string) ([]string, error) {
	var origens []string
	for _, campo := range strings.Split(bruto, ",") {
		campo = strings.TrimSpace(campo)
		if campo == "" {
			continue
		}
		if campo == "*" {
			return nil, fmt.Errorf(
				"ORIGENS_PERMITIDAS contém %q: a §6 exige uma lista explícita, e não há caso em que "+
					"o coringa seja a resposta — a app é servida de uma origem conhecida", campo)
		}
		if err := validarOrigem(campo); err != nil {
			return nil, fmt.Errorf("ORIGENS_PERMITIDAS: %w", err)
		}
		origens = append(origens, campo)
	}
	return origens, nil
}

// validarOrigem confirma que a origem tem a forma que um browser envia.
//
// ⚠️ Um browser manda no `Origin` exactamente `esquema://host[:porta]` — sem
// caminho, sem barra final, sem query. Uma entrada com caminho **nunca** casa, e
// falha em silêncio: os pedidos passam a ser recusados sem que a configuração
// pareça errada. Por isso recusa-se ao arranque, onde ainda há quem leia.
func validarOrigem(origem string) error {
	u, err := url.Parse(origem)
	if err != nil {
		return fmt.Errorf("%q não é uma origem válida: %w", origem, err)
	}
	switch {
	case u.Scheme != "http" && u.Scheme != "https":
		return fmt.Errorf("%q: a origem tem de começar por http:// ou https://", origem)
	case u.Host == "":
		return fmt.Errorf("%q: falta o host", origem)
	case u.Path != "":
		return fmt.Errorf(
			"%q: uma origem não leva caminho — o browser envia só %s://%s, e esta entrada nunca casaria",
			origem, u.Scheme, u.Host)
	case u.RawQuery != "" || u.Fragment != "" || u.User != nil:
		return fmt.Errorf("%q: uma origem não leva query, fragmento nem credenciais", origem)
	}
	return nil
}

// permitirOrigens põe os cabeçalhos de CORS quando a origem está na lista.
//
// ⚠️ Escrito à mão e não trazido de uma biblioteca, e a razão é o tamanho da
// política: **lista exacta, sem credenciais, sem padrões**. O que sobra são
// quatro cabeçalhos e uma regra de cache, tudo visível neste ficheiro e coberto
// por testes. Uma biblioteca traz o que não se usa — coringas, sufixos,
// `AllowCredentials` — e o que não se usa numa política de acesso é
// exactamente o que se liga por engano.
//
// ⚠️ **O `Vary: Origin` não é enfeite.** A resposta muda com a origem, e sem
// esse cabeçalho uma cache pelo caminho serve a quem quer que seja a resposta
// que guardou para a primeira origem que passou — incluindo o
// `Access-Control-Allow-Origin` dela. Põe-se **sempre que a lista não está
// vazia**, mesmo quando a origem é recusada: o que varia é a resposta, não o
// facto de ela ter sido permitida.
func (s *Servidor) permitirOrigens(seguinte http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(s.origens) == 0 {
			seguinte.ServeHTTP(w, r)
			return
		}
		w.Header().Add("Vary", "Origin")

		origem := r.Header.Get("Origin")
		if origem == "" || !permitida(s.origens, origem) {
			// ⚠️ Recusa-se sem cabeçalhos e **sem erro**: o pedido segue e é
			// servido. Quem bloqueia é o browser, ao não encontrar a permissão —
			// e devolver 403 aqui partia todos os clientes que não são browsers
			// e que mandam `Origin` por outras razões.
			seguinte.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Access-Control-Allow-Origin", origem)
		if ehPreflight(r) {
			w.Header().Add("Vary", "Access-Control-Request-Method")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Max-Age", "600")
		}
		seguinte.ServeHTTP(w, r)
	})
}

// ehPreflight distingue o pedido de sondagem do browser de um OPTIONS qualquer.
//
// ⚠️ O `Access-Control-Request-Method` é o que o define — um `OPTIONS` sem ele
// não é preflight nenhum, e tratá-lo como tal dava a um cliente a possibilidade
// de saltar o tecto (ver `limitar`) só por escolher o método.
func ehPreflight(r *http.Request) bool {
	return r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != ""
}

// preflight responde ao pedido de sondagem. Os cabeçalhos são do middleware; o
// corpo é vazio por definição.
func preflight(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// permitida diz se a origem está na lista. Comparação exacta, sem sufixos nem
// padrões.
//
// ⚠️ Sem correspondência por sufixo, de propósito. Uma regra do género «acaba em
// `.exemplo.pt`» é casada por `atacante-exemplo.pt` em várias implementações
// ingénuas, e a lista deste serviço tem duas entradas — não vale a pena inventar
// uma linguagem de padrões para as escrever.
func permitida(origens []string, origem string) bool {
	for _, o := range origens {
		if o == origem {
			return true
		}
	}
	return false
}
