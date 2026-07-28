// O comando simulador serve a API JSON e os subcomandos de manutenção.
//
// Monta-se aqui o que a infra construir; nenhuma regra de negócio vive neste
// pacote — o depguard nega-lhe os imports de `dominio`, `bancos` e `aplicacao`
// (ARQUITETURA.md §3). Por agora há quatro subcomandos:
//
//	simulador servir     (o default) — verifica o esquema e recusa-se a
//	                     arrancar com a base por migrar; o servidor HTTP
//	                     ainda não está montado (KAN-13).
//	simulador varrer     corre o varrimento sobre a grelha e grava o lote.
//	simulador migrar     aplica as migrações pendentes.
//	simulador reverter   desfaz a última migração.
//
// Todos lêem a base em DATABASE_URL.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/zepedrorodrigues/simulador-v2/internal/infra/esquema"
	"github.com/zepedrorodrigues/simulador-v2/internal/infra/varrer"
)

func main() {
	if err := executar(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "simulador:", err)
		os.Exit(1)
	}
}

func executar(ctx context.Context, args []string, saida io.Writer) error {
	comando := "servir"
	if len(args) > 0 {
		comando = args[0]
	}

	// ⚠️ O nome do comando verifica-se ANTES de se abrir seja o que for. Hoje o
	// sql.Open é preguiçoso e não liga, portanto a ordem não muda o resultado —
	// mas passaria a mudar no dia em que o Ligar fizesse um Ping, e aí um erro
	// de ligação escondia um erro de digitação. Quem escreveu `varre` perde a
	// tarde a olhar para o Postgres.
	if !slices.Contains(comandos, comando) {
		return fmt.Errorf("comando desconhecido %q (usa: %s)", comando, strings.Join(comandos, " | "))
	}

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return errors.New("falta DATABASE_URL no ambiente")
	}

	db, err := esquema.Ligar(url)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	switch comando {
	case "servir":
		// O arranque não migra: verifica e recusa-se a servir se a base
		// estiver por migrar. Aplicar é `simulador migrar`, de propósito.
		if err := esquema.ExigirEmDia(ctx, db); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(saida, "esquema em dia; servidor HTTP ainda não montado (KAN-13)")
		return nil
	case "varrer":
		// ⚠️ O varrimento exige a base em dia pela mesma razão que o servir: um
		// varrimento contra um esquema velho grava linhas a que faltam colunas
		// que a §4 já decidiu, e só se descobre ao ler.
		if err := esquema.ExigirEmDia(ctx, db); err != nil {
			return err
		}
		return varrimento(ctx, url, args[1:], saida)
	case "migrar":
		if err := esquema.Migrar(ctx, db); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(saida, "migrações aplicadas")
		return nil
	case "reverter":
		if err := esquema.Reverter(ctx, db); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(saida, "migração revertida")
		return nil
	}
	// Inalcançável: o comando foi verificado contra a lista à cabeça.
	return fmt.Errorf("comando %q sem tratamento", comando)
}

// comandos é a lista única dos subcomandos. Está aqui e não espalhada pelo
// switch para a mensagem de erro não se desactualizar quando entrar o próximo —
// que é como um `usa:` acaba a mentir.
var comandos = []string{"servir", "varrer", "migrar", "reverter"}

// ⚠️ Os `_, _ =` nos Fprint são deliberados e não preguiça. A saída é o stdout
// de um processo curto: se escrever nele falhar, não há para onde reportar —
// escrever o erro seria escrever no mesmo sítio que acabou de falhar. O
// errcheck isenta o fmt.Println para stdout e não isenta o Fprintln para um
// io.Writer, que é o que este ficheiro usa para os testes poderem ler a saída.

// varrimento corre o subcomando `varrer`.
func varrimento(ctx context.Context, url string, args []string, saida io.Writer) error {
	fs := flag.NewFlagSet("varrer", flag.ContinueOnError)
	fs.SetOutput(saida)
	seAntigo := fs.Duration("se-antigo", varrer.SeAntigoOmissao,
		"só varre se o último varrimento for mais velho do que isto; 0 desliga a guarda")
	quais := fs.String("bancos", "",
		"lista de ids separados por vírgula; vazio corre os registados todos")
	if err := fs.Parse(args); err != nil {
		return err
	}

	rel, err := varrer.Correr(ctx, url, varrer.Opcoes{
		SeAntigo: *seAntigo,
		Bancos:   lista(*quais),
	})
	if err != nil {
		return err
	}

	// ⚠️ A guarda a travar não é erro, e sai com 0. Um subcomando cíclico que
	// saísse com 1 por não ter de correr enchia o log do agendador de falhas
	// que não são falhas — e quem as visse deixava de as ler.
	if rel.Saltado {
		_, _ = fmt.Fprintln(saida, "varrimento saltado:", rel.Motivo)
		return nil
	}

	_, _ = fmt.Fprintf(saida, "varrimento %s: %d observações de %d pontos em %v, %d falhas\n",
		rel.VarrimentoID, rel.Observacoes, rel.Pontos, rel.Bancos, rel.Falhas)
	for _, s := range rel.BancosSaltados {
		_, _ = fmt.Fprintf(saida, "  banco saltado — %s: %s\n", s.ID, s.Motivo)
	}
	// ⚠️ Uma escala não medida é um banco que ficou sem a dimensão do LTV, e não
	// é um banco saltado: os pontos dele estão gravados. O relatório já a
	// trazia desde a sétima fatia da KAN-16 e ninguém a imprimia — ou seja, a
	// corrida em que nenhum banco deu escala nenhuma era, para quem a corria,
	// indistinguível de uma corrida boa.
	for _, s := range rel.EscalasNaoMedidas {
		_, _ = fmt.Fprintf(saida, "  escala de LTV não medida — %s: %s\n", s.ID, s.Motivo)
	}
	// O resíduo da §7.4. Se ele cresce, alguma coisa mudou do lado do banco — e
	// tem de aparecer como número a quem acabou de varrer, não só na coluna.
	for _, r := range rel.Residuos {
		_, _ = fmt.Fprintf(saida, "  resíduo — %s: mediana %s €, maior %s € (%d medidos)\n",
			r.ID, r.Mediano, r.Maior, r.Medidos)
	}
	return nil
}

// lista parte a opção `--bancos`, ignorando espaços e entradas vazias.
func lista(s string) []string {
	var saida []string
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			saida = append(saida, v)
		}
	}
	return saida
}
