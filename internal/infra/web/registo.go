package web

import (
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
)

// O registo de pedidos. Uma linha JSON por pedido, em `stdout`.
//
// ⚠️ **Escrito à mão, e o `middleware.Logger` do chi continua de fora.** A razão
// está no `Rotas()` desde que ele existe: aquele middleware regista o URL
// **inteiro**, query string incluída — e é na query string que um cliente
// distraído põe uma chave ou um dado pessoal. Hoje não há chave nenhuma (o
// `/api/rate-catalog` saiu a 2026-08-07), mas a regra fica: o que entra num log
// não se apaga — fica na plataforma, nos backups dela, e em quem os leia.
//
// ⚠️ E `stdout` e mais nada. É o que o Fly, o Docker e qualquer plataforma
// esperam recolher; um ficheiro dentro de um contentor não sobrevive ao
// reinício, e escolher um caminho era inventar um problema de rotação que
// ninguém tem.

// registar devolve o middleware que escreve uma linha por pedido.
//
// Diário nulo desliga-o por completo — é o que os testes que não estão a medir
// registo nenhum querem, e evita que cada um deles tenha de escolher um destino.
func (s *Servidor) registar(seguinte http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.diario == nil {
			seguinte.ServeHTTP(w, r)
			return
		}

		inicio := s.agora()
		envolvido := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		seguinte.ServeHTTP(envolvido, r)

		s.diario.LogAttrs(r.Context(), slog.LevelInfo, "pedido",
			slog.String("metodo", r.Method),

			// ⚠️ `r.URL.Path` e **nunca** `r.URL.String()` nem `r.URL.RequestURI()`.
			// Os dois últimos trazem a query string, que é precisamente o que
			// não pode entrar aqui. É uma linha de diferença e é a linha toda.
			slog.String("caminho", r.URL.Path),

			slog.Int("estatuto", envolvido.Status()),
			slog.Int("bytes", envolvido.BytesWritten()),
			slog.Int64("duracao_ms", s.agora().Sub(inicio).Milliseconds()),

			// ⚠️ O mesmo identificador que o cliente recebeu no cabeçalho
			// `X-Request-ID` (ver `devolverRequestID`). Sem ele, quem reclama com
			// um identificador na mão não tem com que se cruzar do nosso lado — e
			// era esse o estado até 2026-07-28: o identificador era gerado,
			// publicado, e nunca escrito.
			slog.String("request_id", middleware.GetReqID(r.Context())),
		)
	})
}

// diarioDoPedido devolve o diário já preso a ESTE pedido, para quem escreve uma
// linha fora do middleware (KAN-61).
//
// ⚠️ **É aqui que o `request_id` se junta, e não na camada de aplicação.** O
// `aovivo` é quem recupera o pânico e quem tem o banco à mão, mas não conhece
// `X-Request-ID` nem devia — é um cabeçalho HTTP, e ele não sabe que existe HTTP.
// Entregando-lhe um diário já com o identificador dentro, a linha do pânico
// cruza-se com a linha do pedido sem que o caso de uso saiba o que as liga.
//
// Nulo quando não há diário, porque `(*slog.Logger)(nil).With(...)` entra em
// pânico — e um pânico dentro do que existe para registar pânicos seria uma
// forma particularmente má de descobrir isto.
func (s *Servidor) diarioDoPedido(r *http.Request) *slog.Logger {
	if s.diario == nil {
		return nil
	}
	return s.diario.With(slog.String("request_id", middleware.GetReqID(r.Context())))
}

// DiarioDeOmissao é o que o serviço usa quando ninguém escolhe: JSON para o
// destino dado, ao nível `info`.
func DiarioDeOmissao(destino io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(destino, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

// ComDiario liga o registo de pedidos.
//
// ⚠️ Método e não parâmetro do `Novo`, como o `ComTecto` e o `ComOrigens`.
func (s *Servidor) ComDiario(diario *slog.Logger) *Servidor {
	s.diario = diario
	return s
}
