package web

import "net/http"

// Os cabeçalhos de defesa (KAN-46). Quais, com que valores e porque é que o
// HSTS NÃO sai daqui está na §3 do API.md.
//
// ⚠️ Aplicam-se a tudo, respostas de erro incluídas — é por isso que o middleware
// é o primeiro da cadeia (ver `Rotas`).
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
