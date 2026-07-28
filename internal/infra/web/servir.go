package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
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

	chaves, err := ChavesDe(os.Getenv("API_KEYS"))
	if err != nil {
		return fmt.Errorf("ler as chaves de API: %w", err)
	}
	if len(chaves) == 0 {
		avisoDeChaveAberta(saida)
	}

	proxies, err := RedesDe(os.Getenv("PROXIES_DE_CONFIANCA"))
	if err != nil {
		return fmt.Errorf("ler os proxies de confiança: %w", err)
	}

	// ⚠️ Uma origem mal escrita **falha o arranque**, e não é severidade a mais.
	// Uma entrada com barra final ou com caminho nunca casa com o `Origin` que o
	// browser envia: o CORS fica ligado, a configuração parece correcta, e a app
	// recebe erros que não nomeiam nada. Recusar aqui é o único sítio onde ainda
	// há quem leia a mensagem.
	origens, err := OrigensDe(os.Getenv("ORIGENS_PERMITIDAS"))
	if err != nil {
		return fmt.Errorf("ler as origens permitidas: %w", err)
	}

	cat := catalogo.NovoPostgres(pool)
	servidor, err := Novo(cat, cat, bancos.Predefinido(), chaves, time.Now)
	if err != nil {
		return fmt.Errorf("montar o servidor: %w", err)
	}
	// ⚠️ O tecto liga-se sempre. Sem proxies declarados ele conta pelo endereço
	// da ligação, que é o comportamento seguro: é atrás de um proxy que ele
	// precisa de ajuda para saber quem é quem, e é aí que o v1 se enganou.
	servidor = servidor.
		ComDiario(DiarioDeOmissao(saida)).
		ComOrigens(origens)
	if len(origens) == 0 {
		_, _ = fmt.Fprintln(saida,
			"CORS desligado (nenhuma ORIGENS_PERMITIDAS declarada): só a mesma origem chama esta API")
	}

	servidor = servidor.ComTecto(cat, Tecto{
		Pedidos:            PedidosOmissao,
		Janela:             JanelaOmissao,
		ProxiesDeConfianca: proxies,
	})
	if len(proxies) == 0 {
		_, _ = fmt.Fprintln(saida,
			"tecto por IP ligado pelo endereço da ligação (nenhum PROXIES_DE_CONFIANCA declarado)")
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
