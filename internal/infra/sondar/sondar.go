// Package sondar monta a sonda para o subcomando `simulador sondar`.
//
// ⚠️ Existe pela mesma razão que o `infra/varrer`: o depguard nega ao `cmd` os
// imports de `dominio`, `bancos` e `aplicacao` (ARQUITETURA.md §3). O binário
// imprime o que a infra apurar.
//
// O desenho está na decisão 6 da §7. Em duas linhas: uma sonda por degrau, logo
// abaixo do `Ate`, e na divergência revarre-se **aquele** banco.
package sondar

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/grelha"
	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/sonda"
	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/varrimento"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/transporte"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
	"github.com/zepedrorodrigues/simulador-v2/internal/infra/catalogo"
	"github.com/zepedrorodrigues/simulador-v2/internal/infra/varrer"
)

// Opcoes é o que o subcomando deixa escolher.
type Opcoes struct {
	// Bancos limita a corrida a alguns ids. Vazio sonda os que têm escala
	// guardada.
	Bancos []string

	// SemRevarrer sonda e relata, sem disparar varrimento nenhum.
	//
	// ⚠️ Existe para se poder ver o estado sem o mudar — a corrida que responde
	// «mudou alguma coisa?» sem gastar os ~96 pedidos de a corrigir. Não é o
	// modo normal: a §7 decide que na divergência se revarre.
	SemRevarrer bool
}

// Divergencia é um degrau onde o banco deixou de concordar com a grelha.
//
// ⚠️ Só strings, como no `varrer.Relatorio`: é o que o binário imprime, e o
// binário não conhece o domínio (§3).
type Divergencia struct {
	// LTV é onde se perguntou, e De/Ate são o degrau que este ponto confirmava.
	LTV string
	De  string
	Ate string

	Esperado  string
	Observado string

	// Desvio leva sinal: mais caro e mais barato são notícias diferentes.
	Desvio string
}

// BancoSondado é o que a sonda apurou sobre um banco.
type BancoSondado struct {
	ID string

	// Motivo, quando preenchido, é a razão de não se ter sondado — não havia
	// escala guardada, ou o banco não se conseguiu montar. ⚠️ Não é o mesmo que
	// confirmado: não se mediu nada.
	Motivo string

	Degraus     int
	Divergentes int

	// Cegos são as sondas que não se conseguiram medir. Uma sonda cega não
	// confirma — tratar silêncio como concordância é como um detector avariado
	// passa por satisfeito.
	Cegos int

	Confirmada   bool
	Cobertura    string
	Divergencias []Divergencia

	// Revarrido diz se a divergência disparou o varrimento deste banco, e
	// MotivoDoRevarrimento diz porque não, quando não.
	Revarrido            bool
	MotivoDoRevarrimento string
}

// Relatorio é o que a corrida tem para dizer, pela ordem dos ids.
type Relatorio struct {
	Bancos []BancoSondado
}

// Confirmados e Divergentes contam, para quem lê não ter de contar.
func (r Relatorio) Divergentes() int {
	n := 0
	for _, b := range r.Bancos {
		if b.Divergentes > 0 {
			n++
		}
	}
	return n
}

// Revarrer dispara o varrimento de um banco. Devolve o motivo de não se ter
// varrido quando a guarda ou o travão o impediram — que não é erro.
type Revarrer func(ctx context.Context, bancoID string) (saltado bool, motivo string, err error)

// Correr monta tudo e sonda.
//
// Abre e fecha o seu próprio pool, como o `varrer.Correr`: o subcomando é um
// processo que corre e sai.
func Correr(ctx context.Context, url string, o Opcoes) (Relatorio, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return Relatorio{}, fmt.Errorf("abrir o pool de ligações: %w", err)
	}
	defer pool.Close()

	revarrer := revarrerPelaBase(url)
	if o.SemRevarrer {
		revarrer = nil
	}

	// ⚠️ O "hoje" sai do fuso de Lisboa e não de UTC, pela razão que o
	// `varrer.Correr` regista: é dele que sai a data de nascimento do titular
	// neutro, e com ela o prazo máximo.
	hoje := dominio.DataDeInstante(time.Now().In(lisboa()))

	obs, err := catalogo.NovoPostgres(pool).UltimoVarrimento(ctx)
	if err != nil {
		return Relatorio{}, fmt.Errorf("ler o último varrimento: %w", err)
	}
	escalas := varrimento.EscalasPorBanco(obs)

	escolhidos, err := escolher(o.Bancos, escalas)
	if err != nil {
		return Relatorio{}, err
	}
	return correr(ctx, escolhidos, escalas, hoje, revarrer), nil
}

// correr é a orquestração, sem base de dados nem registo de bancos à vista —
// recebe os bancos já construídos. É esta que os testes exercitam, com bancos
// falsos e um Revarrer que conta em vez de varrer.
func correr(
	ctx context.Context, escolhidos []bancos.Banco,
	escalas map[string]dominio.EscalaDeLTV, hoje dominio.Data, revarrer Revarrer,
) Relatorio {
	rel := Relatorio{Bancos: make([]BancoSondado, 0, len(escolhidos))}
	for _, b := range escolhidos {
		rel.Bancos = append(rel.Bancos, sondarBanco(ctx, b, escalas[b.ID()], hoje, revarrer))
	}
	return rel
}

// sondarBanco corre a sonda de um banco e, se ela divergir, manda revarrê-lo.
//
// ⚠️ Um banco que falhe não derruba a corrida — sai com Motivo preenchido, como
// a §5 manda e o varrimento já faz.
func sondarBanco(
	ctx context.Context, b bancos.Banco, escala dominio.EscalaDeLTV,
	hoje dominio.Data, revarrer Revarrer,
) BancoSondado {
	saida := BancoSondado{ID: b.ID()}

	pontos, err := sonda.PontosDe(escala, sonda.RecuoOmissao)
	if err != nil {
		saida.Motivo = err.Error()
		return saida
	}

	amostrar, err := grelha.AmostrarBanco(b, grelha.Referencia{}, hoje, time.Now)
	if err != nil {
		saida.Motivo = fmt.Sprintf("montar a amostragem: %v", err)
		return saida
	}

	rel, err := sonda.Correr(ctx, b.ID(), pontos, medirCom(amostrar))
	if err != nil {
		saida.Motivo = err.Error()
		return saida
	}

	saida.Degraus = len(rel.Leituras)
	saida.Divergentes = rel.Divergentes
	saida.Cegos = rel.Cegos
	saida.Confirmada = rel.Confirmada()
	saida.Cobertura = rel.Cobertura()
	saida.Divergencias = divergenciasDe(rel)

	// ⚠️ Revarre-se por DIVERGÊNCIA e não por «não confirmada». Uma sonda cega
	// também não confirma, e disparar 96 pedidos porque o banco não respondeu a
	// quatro é castigá-lo por estar em baixo — exactamente quando não convém.
	if rel.Divergentes > 0 && revarrer != nil {
		saltado, motivo, err := revarrer(ctx, b.ID())
		switch {
		case err != nil:
			saida.MotivoDoRevarrimento = err.Error()
		case saltado:
			saida.MotivoDoRevarrimento = motivo
		default:
			saida.Revarrido = true
		}
	}
	return saida
}

// medirCom adapta a amostragem da grelha à assinatura da sonda. São a mesma
// pergunta ao mesmo sítio, e só o tipo de saída difere.
func medirCom(a grelha.Amostrar) sonda.Medir {
	return func(ctx context.Context, ltv dominio.Racio) (dominio.Taxa, error) {
		m, err := a(ctx, ltv)
		if err != nil {
			return dominio.Taxa{}, err
		}
		return m.Spread, nil
	}
}

func divergenciasDe(r sonda.Relatorio) []Divergencia {
	var saida []Divergencia
	for _, l := range r.Leituras {
		if !l.Divergiu {
			continue
		}
		saida = append(saida, Divergencia{
			LTV:       l.Ponto.LTV.String(),
			De:        l.Ponto.Degrau.De.String(),
			Ate:       l.Ponto.Degrau.Ate.String(),
			Esperado:  l.Ponto.Esperado.String(),
			Observado: l.Observado.String(),
			Desvio:    l.Desvio.String(),
		})
	}
	return saida
}

// revarrerPelaBase é o Revarrer de produção: um varrimento de um banco só.
//
// ⚠️ `SeAntigo: 0` — a divergência passa por cima da guarda de frescura, e tem
// de passar. A guarda existe para travar quem varre duas vezes sem razão; aqui
// há razão medida, e respeitá-la era ignorar o próprio detector.
//
// ⚠️ O que trava a escalada é o travão em Postgres, não esta guarda: duas
// corridas em paralelo sobre o mesmo banco não tomam o travão as duas, e a
// segunda sai. É o que a §7 diz, e diz **em paralelo** — duas corridas
// sequenciais que divirjam revarrem as duas, o que é o comportamento pretendido
// (a primeira não terá resolvido a divergência) e custa ~96 pedidos de cada vez.
func revarrerPelaBase(url string) Revarrer {
	return func(ctx context.Context, bancoID string) (bool, string, error) {
		rel, err := varrer.Correr(ctx, url, varrer.Opcoes{
			Bancos:   []string{bancoID},
			SeAntigo: 0,
		})
		if err != nil {
			return false, "", fmt.Errorf("revarrer %s: %w", bancoID, err)
		}
		if rel.Saltado {
			return true, rel.Motivo, nil
		}
		for _, s := range rel.BancosSaltados {
			if s.ID == bancoID {
				return true, s.Motivo, nil
			}
		}
		return false, "", nil
	}
}

// escolher constrói os bancos a sondar.
//
// ⚠️ Sem lista pedida, sondam-se os que **têm escala guardada** — e não todos os
// registados. Um banco que nunca foi varrido não tem grelha para confirmar, e
// aparecer no relatório como «não sondado» em todas as corridas era ruído
// permanente que ensina a não ler a saída.
func escolher(pedidos []string, escalas map[string]dominio.EscalaDeLTV) ([]bancos.Banco, error) {
	registo := bancos.Predefinido()

	// Um cliente HTTP por corrida, como no `varrer`: o subcomando é um processo
	// curto e os transportes são partilháveis por construção.
	comSessao, err := transporte.NovoClienteComSessao(nil)
	if err != nil {
		return nil, fmt.Errorf("montar o transporte com sessão: %w", err)
	}
	ts := bancos.Transportes{
		HTTP:      transporte.NovoCliente(nil),
		ComSessao: comSessao,
	}

	if len(pedidos) == 0 {
		pedidos = make([]string, 0, len(escalas))
		for id := range escalas {
			pedidos = append(pedidos, id)
		}
	}
	// A ordem é alfabética e não a de um mapa: uma saída que muda de ordem entre
	// corridas não se compara com a da véspera.
	slices.Sort(pedidos)

	escolhidos := make([]bancos.Banco, 0, len(pedidos))
	for _, id := range pedidos {
		b, err := registo.Construir(id, ts)
		if err != nil {
			return nil, fmt.Errorf("%w (registados: %v)", err, registo.IDs())
		}
		escolhidos = append(escolhidos, b)
	}
	return escolhidos, nil
}

// lisboa é o fuso em que o "hoje" se decide.
func lisboa() *time.Location {
	l, err := time.LoadLocation("Europe/Lisbon")
	if err != nil {
		return time.UTC
	}
	return l
}
