package varrimento_test

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/varrimento"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// Os bancos falsos com latências diferentes fecham no tempo do mais lento e não
// na soma. É o critério que justifica o fan-out: em série, uma corrida de 10
// bancos arrastava-se até 52,2 s no v1.
func TestVarrimentoFechaNoTempoDoMaisLentoENaoNaSoma(t *testing.T) {
	semFugas(t)

	rapido := &bancoFalso{id: "rapido", atraso: 100 * time.Millisecond}
	medio := &bancoFalso{id: "medio", atraso: time.Second}
	lento := &bancoFalso{id: "lento", atraso: 5 * time.Second}

	v := varredor(t, varrimento.Config{
		Bancos: []bancos.Banco{rapido, medio, lento},
		Prazos: varrimento.Prazos{Barato: 30 * time.Second},
	})

	inicio := time.Now()
	r := v.Varrer(t.Context(), pontos("ltv80/variavel/propria"))
	demorou := time.Since(inicio)

	// Em paralelo o total é o do mais lento (5 s); em série seria a soma (6,1 s).
	// O tecto fica no meio, e a folga é para o escalonador, não para a diferença.
	if tecto := 5*time.Second + 700*time.Millisecond; demorou > tecto {
		t.Errorf(
			"o varrimento demorou %s: em paralelo eram ~5 s, em série 6,1 s — não está a correr em paralelo",
			demorou)
	}
	if demorou < 4900*time.Millisecond {
		t.Errorf("o varrimento demorou %s e o banco mais lento leva 5 s: não se esperou por ele", demorou)
	}

	if len(r.Observacoes) != 3 {
		t.Fatalf("esperava 3 observações, vieram %d", len(r.Observacoes))
	}
	for _, o := range r.Observacoes {
		if !o.Sucesso() {
			t.Errorf("%s: esperava sucesso, veio %v", o.Oferta.BancoID, o.Oferta.Erro)
		}
	}
}

// O prazo sai do Custo declarado e não é uniforme: com o mesmo atraso de 1 s, o
// banco `barato` estoura o seu prazo de 200 ms e o `caro` responde dentro dos
// seus 3 s. Um tecto único não distinguiria os dois.
func TestPrazoDerivaDoCustoENaoEUniforme(t *testing.T) {
	semFugas(t)

	barato := &bancoFalso{id: "barato", custo: dominio.CustoBarato, atraso: time.Second}
	caro := &bancoFalso{id: "caro", custo: dominio.CustoCaro, atraso: time.Second}

	v := varredor(t, varrimento.Config{
		Bancos: []bancos.Banco{barato, caro},
		Prazos: varrimento.Prazos{Barato: 200 * time.Millisecond, Caro: 3 * time.Second},
	})

	r := v.Varrer(t.Context(), pontos("ltv80/variavel/propria"))

	obs := porBanco(t, r)
	// Fatal e não Error: sem erro preenchido, a asserção seguinte rebentava em
	// nil e a falha aparecia como um pânico em vez do número que a explica.
	if obs["barato"].Sucesso() {
		t.Fatal("o banco barato demorou 1 s com um prazo de 200 ms: devia ter falhado")
	}
	if codigo := obs["barato"].Oferta.Erro.Codigo; codigo != dominio.ErroBancoIndisponivel {
		t.Errorf("prazo expirado: esperava banco_indisponivel, veio %q", codigo)
	}
	if !obs["caro"].Sucesso() {
		t.Errorf("o banco caro demorou 1 s com um prazo de 3 s: devia ter passado, veio %v", obs["caro"].Oferta.Erro)
	}
}

// Um banco que entra em pânico produz uma observação de falha e os outros três
// completam. O recover está num sítio só — no orquestrador — e não dentro de
// cada banco.
func TestBancoQuePanicaNaoDerrubaOVarrimento(t *testing.T) {
	semFugas(t)

	explosivo := &bancoFalso{id: "explosivo", panico: true}
	v := varredor(t, varrimento.Config{Bancos: []bancos.Banco{
		explosivo,
		&bancoFalso{id: "um"},
		&bancoFalso{id: "dois"},
		&bancoFalso{id: "tres"},
	}})

	r := v.Varrer(t.Context(), pontos("ltv80/variavel/propria"))

	obs := porBanco(t, r)
	if len(obs) != 4 {
		t.Fatalf("esperava 4 observações, vieram %d", len(obs))
	}
	if obs["explosivo"].Sucesso() {
		t.Fatal("o banco que entrou em pânico devia ter dado observação de falha")
	}
	// ⚠️ O código é banco_indisponivel e culpa o banco por um erro nosso: é
	// conhecido, está nomeado na mensagem e resolve-se em KAN-30.
	if !strings.Contains(obs["explosivo"].Oferta.Erro.Mensagem, "Erro nosso") {
		t.Errorf("a mensagem devia dizer que o erro é nosso, veio %q", obs["explosivo"].Oferta.Erro.Mensagem)
	}
	for _, id := range []string{"um", "dois", "tres"} {
		if !obs[id].Sucesso() {
			t.Errorf("%s: o pânico do vizinho não o devia ter afectado, veio %v", id, obs[id].Oferta.Erro)
		}
	}
}

// Um banco surdo ao ctx — que dorme o seu atraso inteiro sem olhar para o
// cancelamento — não segura o varrimento: o prazo dele expira deste lado e os
// outros fecham. É a afirmação de prova.RespeitaPrazo vista do orquestrador.
func TestBancoSurdoAoCtxNaoSeguraOVarrimento(t *testing.T) {
	// ⚠️ Sem sonda de fugas, e de propósito: a goroutine do banco surdo acaba
	// quando ele decidir (2 s), não quando nós desistimos. Essa é a fuga dele, e
	// quem a reprova é o prova.RespeitaPrazo no teste desse banco. O que aqui se
	// mede é que não somos nós a esperar por ele.
	surdo := &bancoFalso{id: "surdo", atraso: 2 * time.Second, surdo: true}
	bomAluno := &bancoFalso{id: "bom-aluno", atraso: 50 * time.Millisecond}

	v := varredor(t, varrimento.Config{
		Bancos: []bancos.Banco{surdo, bomAluno},
		Prazos: varrimento.Prazos{Barato: 200 * time.Millisecond},
	})

	inicio := time.Now()
	r := v.Varrer(t.Context(), pontos("ltv80/variavel/propria"))
	demorou := time.Since(inicio)

	if demorou > time.Second {
		t.Errorf("o varrimento demorou %s: ficou à espera dos 2 s do banco surdo em vez dos 200 ms de prazo", demorou)
	}

	obs := porBanco(t, r)
	if obs["surdo"].Sucesso() {
		t.Fatal("o banco surdo estourou o prazo: devia ter dado observação de falha")
	}
	if !strings.Contains(obs["surdo"].Oferta.Erro.Mensagem, "não desistiu") {
		t.Errorf("a mensagem devia nomear que o banco ignorou o cancelamento, veio %q",
			obs["surdo"].Oferta.Erro.Mensagem)
	}
	if !obs["bom-aluno"].Sucesso() {
		t.Errorf("bom-aluno: devia ter fechado, veio %v", obs["bom-aluno"].Oferta.Erro)
	}
}

// O tecto de concorrência por banco é um tecto: com PorBanco a 2 e quatro
// pontos, nunca há três pedidos ao mesmo tempo contra o mesmo banco.
func TestTectoDeConcorrenciaPorBanco(t *testing.T) {
	semFugas(t)

	b := &bancoFalso{id: "cgd", atraso: 100 * time.Millisecond}
	v := varredor(t, varrimento.Config{Bancos: []bancos.Banco{b}, PorBanco: 2})

	r := v.Varrer(t.Context(), pontos("p1", "p2", "p3", "p4"))

	if len(r.Observacoes) != 4 {
		t.Fatalf("esperava 4 observações, vieram %d", len(r.Observacoes))
	}
	if maximo := b.maximo.Load(); maximo != 2 {
		t.Errorf("com PorBanco a 2, o máximo de pedidos simultâneos foi %d", maximo)
	}
}

// Por omissão são dois pedidos de cada vez contra o mesmo banco.
//
// ⚠️ O número é medido, não escolhido: contra a CGD a sério (2026-07-26), oito
// pontos custaram 1,086 s cada com um pedido de cada vez e 603 ms com dois, sem
// falhas. Com quatro custavam 367 ms e também não falhavam — e mesmo assim ficou
// em dois, porque o varrimento corre em hora morta e não vale quadruplicar a
// carga num simulador alheio para ganhar tempo que ninguém está à espera. A
// medição está no TestE2ETectoDeConcorrenciaDaCGD, atrás de `//go:build rede`.
func TestPorOmissaoSaoDoisPedidosDeCadaVezPorBanco(t *testing.T) {
	semFugas(t)

	b := &bancoFalso{id: "cgd", atraso: 50 * time.Millisecond}
	v := varredor(t, varrimento.Config{Bancos: []bancos.Banco{b}})

	v.Varrer(t.Context(), pontos("p1", "p2", "p3", "p4"))

	if maximo := b.maximo.Load(); maximo != varrimento.PorBancoOmissao {
		t.Errorf("sem PorBanco definido, o máximo de pedidos simultâneos foi %d e o de omissão é %d",
			maximo, varrimento.PorBancoOmissao)
	}
}

// A ordem da saída é a dos bancos e a dos pontos, e não a de quem chegou
// primeiro: um varrimento que trocasse de ordem sozinho tornava qualquer
// comparação entre corridas ilegível.
func TestOrdemDaSaidaEADosBancosEDosPontos(t *testing.T) {
	semFugas(t)

	v := varredor(t, varrimento.Config{Bancos: []bancos.Banco{
		&bancoFalso{id: "lento", atraso: 300 * time.Millisecond},
		&bancoFalso{id: "rapido"},
	}})

	r := v.Varrer(t.Context(), pontos("p1", "p2"))

	querida := []string{"lento/p1", "lento/p2", "rapido/p1", "rapido/p2"}
	for i, o := range r.Observacoes {
		if veio := o.Oferta.BancoID + "/" + o.Ponto.Cenario; veio != querida[i] {
			t.Errorf("observação %d: esperava %s, veio %s", i, querida[i], veio)
		}
	}
}

// Cada observação leva o cenário do ponto e a hora da captura. É o que a torna
// uma linha identificável de catalogo_taxas: sem o cenário não se sabe que
// ponto da grelha é, e sem a hora não se sabe de quando é o número.
func TestObservacaoTrazOCenarioEAHoraDaCaptura(t *testing.T) {
	semFugas(t)

	relogio := time.Date(2026, 7, 26, 4, 30, 0, 0, time.UTC)
	v := varredor(t, varrimento.Config{
		Bancos: []bancos.Banco{&bancoFalso{id: "cgd"}, &bancoFalso{id: "avariado", err: errors.New("500")}},
		Agora:  func() time.Time { return relogio },
	})

	r := v.Varrer(t.Context(), pontos("ltv80/fixa5/propria"))

	for _, o := range r.Observacoes {
		if o.Ponto.Cenario != "ltv80/fixa5/propria" {
			t.Errorf("%s: o cenário do ponto não chegou à observação, veio %q", o.Oferta.BancoID, o.Ponto.Cenario)
		}
		// ⚠️ Também na falha: uma tentativa falhada tem hora, e é a hora em que
		// se tentou.
		if !o.Oferta.CapturadoEm.Equal(relogio) {
			t.Errorf("%s: esperava a captura a %s, veio %s", o.Oferta.BancoID, relogio, o.Oferta.CapturadoEm)
		}
	}
}

// Um banco que já se explicou em dominio.ErroOferta sabe melhor do que nós o
// que lhe aconteceu: produto indisponível não se converte em banco em baixo.
func TestErroEstruturadoDoBancoNaoESobreposto(t *testing.T) {
	semFugas(t)

	recusa := &dominio.ErroOferta{
		Codigo:   dominio.ErroProdutoIndisponivel,
		Mensagem: "Este banco não faz taxa fixa a 35 anos.",
	}
	v := varredor(t, varrimento.Config{Bancos: []bancos.Banco{
		&bancoFalso{id: "cgd", err: fmt.Errorf("ao simular: %w", recusa)},
	}})

	r := v.Varrer(t.Context(), pontos("ltv80/fixa35/propria"))

	if r.Observacoes[0].Sucesso() {
		t.Fatal("o banco recusou o produto: a observação devia ser de falha")
	}
	erro := r.Observacoes[0].Oferta.Erro
	if erro.Codigo != dominio.ErroProdutoIndisponivel {
		t.Errorf("esperava produto_indisponivel, veio %q — o erro do banco foi sobreposto", erro.Codigo)
	}
	if erro.Mensagem != recusa.Mensagem {
		t.Errorf("a mensagem do banco perdeu-se: veio %q", erro.Mensagem)
	}
}

// Um banco travado por outro varrimento não se varre, e sai identificado como
// travado — não como falha. Os outros varrem-se na mesma.
func TestBancoTravadoNaoSeVarreEOsOutrosVarrem(t *testing.T) {
	semFugas(t)

	tr := novoTravaoFalso()
	tr.ocupar("cgd")

	cgd := &bancoFalso{id: "cgd"}
	v := varredor(t, varrimento.Config{
		Bancos: []bancos.Banco{cgd, &bancoFalso{id: "novobanco"}},
		Travao: tr,
	})

	r := v.Varrer(t.Context(), pontos("p1", "p2"))

	if n := cgd.chamadas.Load(); n != 0 {
		t.Errorf("o banco travado foi chamado %d vezes — o travão não travou nada", n)
	}
	if len(r.Saltados) != 1 || r.Saltados[0].BancoID != "cgd" {
		t.Fatalf("esperava a cgd saltada, veio %+v", r.Saltados)
	}
	if !errors.Is(r.Saltados[0].Motivo, varrimento.ErrBancoTravado) {
		t.Errorf("o motivo devia ser ErrBancoTravado, veio %v", r.Saltados[0].Motivo)
	}
	if len(r.Observacoes) != 2 {
		t.Errorf("o novobanco devia ter sido varrido nos dois pontos, vieram %d observações", len(r.Observacoes))
	}
	// O travão que se tomou larga-se, mesmo quando o vizinho ficou de fora.
	if largados := tr.largados(); len(largados) != 1 || largados[0] != "novobanco" {
		t.Errorf("esperava o travão do novobanco largado, veio %v", largados)
	}
}

// ⚠️ Uma falha nossa ao tomar o travão — a base em baixo — não vira observação
// de falha. Escrever banco_indisponivel no catálogo diria que o banco está em
// baixo quando quem está em baixo somos nós, e esse número ficaria lá com ar de
// medido.
func TestFalhaAoTomarOTravaoNaoViraObservacaoDeFalha(t *testing.T) {
	semFugas(t)

	base := errors.New("ligação à base recusada")
	tr := novoTravaoFalso()
	tr.falhar("cgd", base)

	v := varredor(t, varrimento.Config{
		Bancos: []bancos.Banco{&bancoFalso{id: "cgd"}},
		Travao: tr,
	})

	r := v.Varrer(t.Context(), pontos("p1"))

	if len(r.Observacoes) != 0 {
		t.Errorf("não devia haver observações, vieram %d", len(r.Observacoes))
	}
	if len(r.Saltados) != 1 || !errors.Is(r.Saltados[0].Motivo, base) {
		t.Fatalf("esperava a cgd saltada com o erro da base, veio %+v", r.Saltados)
	}
	if errors.Is(r.Saltados[0].Motivo, varrimento.ErrBancoTravado) {
		t.Error("uma avaria nossa não é o travão a funcionar, e não se confunde com ele")
	}
}

// Cancelar o varrimento termina as goroutines e larga os travões: interromper o
// subcomando não deixa scrapes pendurados contra os bancos.
func TestCancelarOVarrimentoTerminaAsGoroutinesEELargaOsTravoes(t *testing.T) {
	semFugas(t)

	ctx, cancelar := context.WithCancel(t.Context())
	tr := novoTravaoFalso()
	v := varredor(t, varrimento.Config{
		Bancos: []bancos.Banco{
			&bancoFalso{id: "um", atraso: 5 * time.Second},
			&bancoFalso{id: "dois", atraso: 5 * time.Second},
		},
		Travao: tr,
		Prazos: varrimento.Prazos{Barato: time.Minute},
	})

	inicio := time.Now()
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancelar()
	}()
	v.Varrer(ctx, pontos("p1", "p2", "p3"))

	if demorou := time.Since(inicio); demorou > time.Second {
		t.Errorf("depois do cancelamento o varrimento ainda demorou %s a fechar", demorou)
	}
	if largados := tr.largados(); len(largados) != 2 {
		t.Errorf("os dois travões deviam ter sido largados, foram %v", largados)
	}
}

// Novo recusa o que não é corrível: sem travão não há regra da §7.2, e sem
// custo válido não há prazo derivável.
func TestNovoRecusaTravaoNuloEBancoSemCusto(t *testing.T) {
	if _, err := varrimento.Novo(varrimento.Config{Bancos: []bancos.Banco{&bancoFalso{id: "cgd"}}}); err == nil {
		t.Error("um varredor sem travão devia ser recusado")
	}

	malDeclarado := &bancoFalso{id: "cgd", custo: dominio.Custo("rapidinho")}
	_, err := varrimento.Novo(varrimento.Config{Bancos: []bancos.Banco{malDeclarado}, Travao: novoTravaoFalso()})
	if err == nil {
		t.Fatal("um banco que não declara custo devia ser recusado: é do custo que sai o prazo")
	}
	if !strings.Contains(err.Error(), "cgd") {
		t.Errorf("o erro devia nomear o banco, veio %q", err)
	}
}

// --- ajudantes -------------------------------------------------------------

// prazoDaSonda é quanto a sonda espera pela contagem de goroutines voltar ao
// valor de partida.
//
// ⚠️ A sonda existe porque o -race não a substitui: medido a 2026-07-25, um
// teste que deixa uma goroutine bloqueada para sempre passa `go test -race` a
// verde, sem uma palavra. A forma que espera — e não a que compara antes e
// depois — deu 0 falsos positivos em 300 corridas e apanhou 50 fugas em 50.
const prazoDaSonda = 100 * time.Millisecond

// semFugas afirma que o teste não deixa goroutines para trás.
//
// ⚠️ Incompatível com t.Parallel: a contagem é do processo inteiro, e testes a
// correr ao lado tornavam-na ruído.
func semFugas(t *testing.T) {
	t.Helper()
	partida := runtime.NumGoroutine()

	t.Cleanup(func() {
		limite := time.Now().Add(prazoDaSonda)
		for {
			agora := runtime.NumGoroutine()
			if agora <= partida {
				return
			}
			if time.Now().After(limite) {
				t.Errorf(
					"ficaram goroutines por acabar: %d à partida, %d ao fim de %s",
					partida, agora, prazoDaSonda)
				return
			}
			time.Sleep(time.Millisecond)
		}
	})
}

// varredor constrói um varredor com um travão livre, quando o teste não trouxe
// um seu.
func varredor(t *testing.T, c varrimento.Config) *varrimento.Varredor {
	t.Helper()
	if c.Travao == nil {
		c.Travao = novoTravaoFalso()
	}
	v, err := varrimento.Novo(c)
	if err != nil {
		t.Fatalf("Novo: %v", err)
	}
	return v
}

func pontos(cenarios ...string) []varrimento.Ponto {
	ps := make([]varrimento.Ponto, 0, len(cenarios))
	for _, c := range cenarios {
		ps = append(ps, varrimento.Ponto{
			Cenario: c,
			Pedido: dominio.Pedido{
				ValorImovel: dominio.DinheiroDeInteiro(250_000),
				Montante:    dominio.DinheiroDeInteiro(200_000),
				PrazoAnos:   30,
				TipoTaxa:    dominio.TaxaVariavel,
				Finalidade:  dominio.FinalidadePropria,
				Localizacao: dominio.LocalizacaoContinente,
			},
		})
	}
	return ps
}

// porBanco indexa as observações por banco. Só serve os testes de um ponto só.
func porBanco(t *testing.T, r varrimento.Resultado) map[string]varrimento.Observacao {
	t.Helper()
	m := make(map[string]varrimento.Observacao, len(r.Observacoes))
	for _, o := range r.Observacoes {
		if _, repetido := m[o.Oferta.BancoID]; repetido {
			t.Fatalf("%s: mais do que uma observação — este ajudante é para um ponto só", o.Oferta.BancoID)
		}
		m[o.Oferta.BancoID] = o
	}
	return m
}

// bancoFalso é um banco programável: responde ao fim de um atraso, ou entra em
// pânico, ou devolve erro. Conta as chamadas e o máximo de chamadas
// simultâneas, que é como se mede o tecto de concorrência.
type bancoFalso struct {
	id     string
	custo  dominio.Custo
	atraso time.Duration
	err    error
	panico bool

	// surdo dorme o atraso inteiro sem olhar para o ctx — é o banco que o
	// prova.RespeitaPrazo reprova, visto do outro lado.
	surdo bool

	chamadas atomic.Int64
	emCurso  atomic.Int64
	maximo   atomic.Int64
}

func (b *bancoFalso) ID() string   { return b.id }
func (b *bancoFalso) Nome() string { return "Banco " + b.id }

// Requisitos declara o custo do banco falso. Vazio vale barato — é o caso comum
// dos testes, e escrevê-lo em todos só faria ruído.
func (b *bancoFalso) Requisitos() dominio.Requisitos {
	custo := b.custo
	if custo == "" {
		custo = dominio.CustoBarato
	}
	return dominio.Requisitos{BancoID: b.id, BancoNome: b.Nome(), Custo: custo}
}

func (b *bancoFalso) Simular(ctx context.Context, _ dominio.Pedido) (dominio.Oferta, error) {
	b.chamadas.Add(1)
	emCurso := b.emCurso.Add(1)
	defer b.emCurso.Add(-1)
	for {
		maximo := b.maximo.Load()
		if emCurso <= maximo || b.maximo.CompareAndSwap(maximo, emCurso) {
			break
		}
	}

	if b.panico {
		panic("o parser deste banco não esperava isto")
	}

	if b.atraso > 0 {
		if b.surdo {
			time.Sleep(b.atraso)
		} else {
			temporizador := time.NewTimer(b.atraso)
			defer temporizador.Stop()
			select {
			case <-ctx.Done():
				return dominio.Oferta{}, ctx.Err()
			case <-temporizador.C:
			}
		}
	}

	if b.err != nil {
		return dominio.Oferta{}, b.err
	}
	return dominio.Oferta{BancoID: b.id, BancoNome: b.Nome()}, nil
}

// travaoFalso é o travão dos testes: deixa tomar, a menos que lhe digam que o
// banco está ocupado ou que a base falha.
//
// ⚠️ Não é substituto do de Postgres. Duas instâncias deste não se travam uma à
// outra — é precisamente o defeito do v1 — e é por isso que o critério dos dois
// arranques concorrentes se afirma contra uma base a sério, em
// internal/infra/travao.
type travaoFalso struct {
	mu         sync.Mutex
	ocupados   map[string]bool
	erros      map[string]error
	tomados    []string
	devolvidos []string
}

func novoTravaoFalso() *travaoFalso {
	return &travaoFalso{ocupados: map[string]bool{}, erros: map[string]error{}}
}

func (t *travaoFalso) ocupar(bancoID string) { t.ocupados[bancoID] = true }

func (t *travaoFalso) falhar(bancoID string, err error) { t.erros[bancoID] = err }

func (t *travaoFalso) Tomar(_ context.Context, bancoID string) (varrimento.Largar, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if err := t.erros[bancoID]; err != nil {
		return nil, err
	}
	if t.ocupados[bancoID] {
		return nil, fmt.Errorf("%w: %s", varrimento.ErrBancoTravado, bancoID)
	}
	t.ocupados[bancoID] = true
	t.tomados = append(t.tomados, bancoID)

	return func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		t.ocupados[bancoID] = false
		t.devolvidos = append(t.devolvidos, bancoID)
	}, nil
}

func (t *travaoFalso) largados() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	saida := make([]string, len(t.devolvidos))
	copy(saida, t.devolvidos)
	return saida
}
