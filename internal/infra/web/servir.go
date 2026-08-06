package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/zepedrorodrigues/simulador-v2/internal/bancos"
	"github.com/zepedrorodrigues/simulador-v2/internal/infra/catalogo"
	"github.com/zepedrorodrigues/simulador-v2/internal/infra/lotacao"
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

// folgaDeLigacoes é o que o pool tem de ter para além das ligações que a
// lotação segura. As consultas do serviço — o catálogo, o tecto por IP — não
// podem ficar à espera de que um banco lento devolva a vaga dele.
const folgaDeLigacoes = 6

// VagasDe lê o tecto de pedidos em voo por banco. Vazio vale VagasOmissao.
//
// ⚠️ Um valor ilegível **falha o arranque** em vez de cair no de omissão, pela
// mesma razão das origens: um `VAGAS_POR_BANCO=dez` que arrancasse a 2 deixava
// alguém convencido de que tinha subido o tecto.
func VagasDe(bruto string) (int, error) {
	bruto = strings.TrimSpace(bruto)
	if bruto == "" {
		return lotacao.VagasOmissao, nil
	}
	vagas, err := strconv.Atoi(bruto)
	if err != nil {
		return 0, fmt.Errorf("VAGAS_POR_BANCO %q não é um número inteiro", bruto)
	}
	if vagas < 1 {
		return 0, fmt.Errorf("VAGAS_POR_BANCO %d: um tecto abaixo de 1 é não servir banco nenhum", vagas)
	}
	return vagas, nil
}

// abrirPool abre o pool com ligações que cheguem para a lotação.
//
// ⚠️ **O omissão do pgxpool não chega, e o efeito seria invisível.** Ele abre o
// maior de 4 e o número de CPUs; a lotação segura uma ligação por pedido em voo,
// e com cinco bancos a duas vagas são dez. Num servidor de 2 CPUs o tecto
// esfomeava as consultas que existe para proteger, e o sintoma aparecia como
// lentidão em toda a API — não como um pool pequeno.
func abrirPool(ctx context.Context, url string, seguradasPelaLotacao int) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("ler a ligação à base: %w", err)
	}
	if minimas := int32(seguradasPelaLotacao) + folgaDeLigacoes; cfg.MaxConns < minimas {
		cfg.MaxConns = minimas
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("abrir o pool de ligações: %w", err)
	}
	return pool, nil
}

// Servir monta o servidor sobre a base e serve até o ctx acabar.
//
// ⚠️ Abre e fecha o seu próprio pool, como o `varrer`: o processo corre e sai, e
// um pool que lhe sobrevivesse não teria quem o fechasse.
func Servir(ctx context.Context, url, endereco string, saida io.Writer) error {
	if endereco == "" {
		endereco = EnderecoOmissao
	}

	vagas, err := VagasDe(os.Getenv("VAGAS_POR_BANCO"))
	if err != nil {
		return fmt.Errorf("ler as vagas por banco: %w", err)
	}

	registo := bancos.Predefinido()

	pool, err := abrirPool(ctx, url, len(registo.IDs())*vagas)
	if err != nil {
		return err
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
	servidor, err := Novo(cat, cat, registo, chaves, time.Now)
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

	// ⚠️ A lotação liga-se sempre, e não há definição que a desligue. É ela que
	// impede um pedido nosso de virar carga sem tecto contra o simulador público
	// de outra pessoa (§7.5); o que se configura é o número, não a existência.
	servidor = servidor.ComLotacao(lotacao.NovoPostgres(pool, vagas))
	_, _ = fmt.Fprintf(saida, "tecto de %d pedidos em voo por banco\n", vagas)

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
