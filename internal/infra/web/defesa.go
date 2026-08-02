package web

import "net/http"

// Os cabeçalhos de defesa (KAN-46).
//
// ⚠️ A API.md §3 prometeu-os desde que a secção existe e **nunca foram
// emitidos** — a falta só apareceu ao olhar para uma resposta a sério, no ensaio
// de produção de 2026-08-01. Um cabeçalho em falta não parte pedido nenhum, não
// aparece em log nenhum e não muda número nenhum.
//
// Duas decisões que a issue deixou em aberto, e ficam aqui:
//
// **Aplica-se a tudo, incluindo o /api/rate-catalog.** Acrescentar cabeçalhos de
// resposta não toca no corpo, e a fronteira congelada é congelada no corpo — o
// teste de contrato confirma-o, não se assume.
//
// **O HSTS não sai daqui.** O serviço fala HTTP em claro atrás do proxy e não
// tem como saber se o que está à frente serve TLS; a condição que a API.md
// escrevia — «apenas quando já se serve HTTPS» — não é observável de dentro.
// Inferi-la do X-Forwarded-Proto obrigaria ao PROXIES_DE_CONFIANCA, que está por
// medir, e passariam a ser duas coisas a falhar juntas. Fica no proxy, que é
// quem termina o TLS e quem sabe a resposta.
func defesa(seguinte http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cabecalhos := w.Header()

		// Este serviço devolve JSON e mais nada. Sem isto, um corpo que um
		// browser decida adivinhar como HTML executa-se como HTML.
		cabecalhos.Set("X-Content-Type-Options", "nosniff")

		// `no-referrer` e não uma política mais frouxa: não se serve HTML nem
		// há ligação para fora, logo não há referrer que valha a pena emitir —
		// e os caminhos desta API dizem o que se está a comparar.
		cabecalhos.Set("Referrer-Policy", "no-referrer")

		seguinte.ServeHTTP(w, r)
	})
}
