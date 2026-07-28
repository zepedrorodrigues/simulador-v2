package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/zepedrorodrigues/simulador-v2/internal/bancos"
	"github.com/zepedrorodrigues/simulador-v2/internal/infra/catalogo"
)

// O arranque do servidor. Vive aqui e não no `cmd` porque o binário não pode
// montar isto: o `depguard` nega-lhe os imports de `dominio`, `bancos` e
// `aplicacao` (§3, «cmd → infra»). É a mesma razão do `infra/varrer`.

// EnderecoOmissao é onde o servidor escuta quando ninguém escolhe.
//
// ⚠️ Só no loopback. Um bind em `0.0.0.0` por omissão punha o serviço na
// internet no dia em que isto corresse num VPS sem ninguém ter decidido que
// devia estar — é a mesma escolha que o `docker-compose.yml` já faz com o
// Postgres, e pela mesma razão.
const EnderecoOmissao = "127.0.0.1:8080"

// Os prazos do servidor HTTP.
//
// ⚠️ Existem, e não são os do `net/http` por omissão — que são **nenhuns**. Um
// servidor sem prazo de leitura mantém aberta uma ligação que nunca acaba de
// enviar o corpo, e bastam algumas para esgotar o que ele aguenta. O prazo de
// escrita é folgado face ao que uma resposta custa: ela sai de uma consulta e de
// aritmética local, não de uma chamada a um banco.
const (
	prazoDeLeitura   = 10 * time.Second
	prazoDeEscrita   = 30 * time.Second
	prazoDeCabecalho = 5 * time.Second
	prazoDeParagem   = 15 * time.Second
)

// Servir monta o servidor sobre a base e serve até o ctx acabar.
//
// ⚠️ Abre e fecha o seu próprio pool, como o `varrer`: o processo corre e sai, e
// um pool que lhe sobrevivesse não teria quem o fechasse.
func Servir(ctx context.Context, url, endereco string, saida io.Writer) error {
	if endereco == "" {
		endereco = EnderecoOmissao
	}

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return fmt.Errorf("abrir o pool de ligações: %w", err)
	}
	defer pool.Close()

	cat := catalogo.NovoPostgres(pool)
	servidor, err := Novo(cat, cat, bancos.Predefinido(), time.Now)
	if err != nil {
		return fmt.Errorf("montar o servidor: %w", err)
	}

	servidorHTTP := &http.Server{
		Addr:              endereco,
		Handler:           servidor.Rotas(),
		ReadTimeout:       prazoDeLeitura,
		WriteTimeout:      prazoDeEscrita,
		ReadHeaderTimeout: prazoDeCabecalho,
	}

	// ⚠️ Paragem graciosa: um SIGTERM a meio de uma resposta não a corta. Quem
	// cancela o ctx é o `cmd`, e aqui espera-se que as respostas em curso saiam.
	falhou := make(chan error, 1)
	go func() {
		_, _ = fmt.Fprintf(saida, "a servir em http://%s\n", endereco)
		if err := servidorHTTP.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			falhou <- err
			return
		}
		falhou <- nil
	}()

	select {
	case err := <-falhou:
		return err
	case <-ctx.Done():
		parar, cancelar := context.WithTimeout(context.WithoutCancel(ctx), prazoDeParagem)
		defer cancelar()
		if err := servidorHTTP.Shutdown(parar); err != nil {
			return fmt.Errorf("parar o servidor: %w", err)
		}
		_, _ = fmt.Fprintln(saida, "servidor parado")
		return nil
	}
}
