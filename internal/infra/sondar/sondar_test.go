package sondar

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/zepedrorodrigues/simulador-v2/internal/bancos"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// A orquestração da sonda, sem base de dados e sem rede.
//
// ⚠️ O que estes testes têm de apanhar é a **decisão de revarrer**: quando se
// dispara, quando não se dispara, e sobre que banco. Errar isso não parte
// nenhuma resposta ao cliente — manda ~96 pedidos a um banco que não precisava,
// ou deixa de os mandar a um que precisava, e nas duas direcções em silêncio.

// escalaDeProva é a forma da CGD medida a 2026-07-26, na fatia com a fronteira
// em LTV não inteiro. Os números estão no DOSSIE-BANCOS.md.
func escalaDeProva(t *testing.T) dominio.EscalaDeLTV {
	t.Helper()
	escala, err := dominio.NovaEscalaDeLTV([]dominio.DegrauLTV{
		{De: racioDe(t, "0.30"), Ate: racioDe(t, "0.6650"), Spread: taxaDe(t, "2.000")},
		{De: racioDe(t, "0.6650"), Ate: racioDe(t, "0.6775"), Spread: taxaDe(t, "2.050")},
		{De: racioDe(t, "0.6775"), Ate: racioDe(t, "0.90"), Spread: taxaDe(t, "1.350")},
	})
	if err != nil {
		t.Fatalf("montar a escala de prova: %v", err)
	}
	return escala
}

func TestUmBancoQueNaoMudouEConfirmadoENaoSeRevarre(t *testing.T) {
	escala := escalaDeProva(t)
	banco := &bancoFalso{id: "cgd", escala: escala}
	var disparos contador

	rel := correr(context.Background(), []bancos.Banco{banco},
		map[string]dominio.EscalaDeLTV{"cgd": escala}, hojeDeProva(t), disparos.revarrer)

	if len(rel.Bancos) != 1 {
		t.Fatalf("%d bancos no relatório, esperava 1", len(rel.Bancos))
	}
	b := rel.Bancos[0]
	if b.Motivo != "" {
		t.Fatalf("o banco não foi sondado: %s", b.Motivo)
	}
	if !b.Confirmada {
		t.Errorf("a grelha não foi confirmada: %d divergente(s), %d cego(s)", b.Divergentes, b.Cegos)
	}
	if b.Degraus != 3 {
		t.Errorf("sondou %d degraus, e a escala tem 3", b.Degraus)
	}
	// ⚠️ É esta a asserção que interessa. Um detector que confirma e revarre à
	// mesma custa os 96 pedidos que ele existe para poupar.
	if n := disparos.total(); n != 0 {
		t.Errorf("%d varrimento(s) disparado(s) num banco confirmado, e não devia ser nenhum", n)
	}
	// ⚠️ Quatro pedidos, não noventa e seis. É o número que justifica a sonda.
	if n := banco.chamadas.Load(); n != 3 {
		t.Errorf("a sonda fez %d pedidos ao banco, e a escala tem 3 degraus", n)
	}
}

func TestUmBancoQueMudouODeSpreadDisparaOVarrimentoDele(t *testing.T) {
	escala := escalaDeProva(t)
	// O banco passou a praticar 1,500 onde a grelha diz 1,350 — o degrau de
	// cima. Os outros dois continuam iguais.
	banco := &bancoFalso{id: "cgd", escala: escala, substituir: map[string]string{"1.350": "1.500"}}
	var disparos contador

	rel := correr(context.Background(), []bancos.Banco{banco},
		map[string]dominio.EscalaDeLTV{"cgd": escala}, hojeDeProva(t), disparos.revarrer)

	b := rel.Bancos[0]
	if b.Confirmada {
		t.Error("a grelha foi dada por confirmada, e o banco mudou de preço")
	}
	if b.Divergentes != 1 {
		t.Errorf("%d divergências, esperava 1", b.Divergentes)
	}
	if !b.Revarrido {
		t.Errorf("o banco divergiu e não foi revarrido (motivo: %q)", b.MotivoDoRevarrimento)
	}
	if n := disparos.total(); n != 1 {
		t.Errorf("%d varrimentos disparados, esperava 1", n)
	}
	// A divergência tem de dizer ONDE e QUANTO. «Não bate certo» não serve para
	// decidir se o preçário mudou ou se o parser partiu.
	if len(b.Divergencias) != 1 {
		t.Fatalf("%d divergências detalhadas, esperava 1", len(b.Divergencias))
	}
	d := b.Divergencias[0]
	if !mesmaTaxaTexto(d.Esperado, "1.350") || !mesmaTaxaTexto(d.Observado, "1.500") {
		t.Errorf("a divergência diz esperado=%s observado=%s, e mediu-se 1.350 → 1.500", d.Esperado, d.Observado)
	}
}

// TestRevarreSoOBancoQueDivergiu é o âmbito da escalada, e é o que a §7 limita
// de propósito: «não se revarre o mundo».
func TestRevarreSoOBancoQueDivergiu(t *testing.T) {
	escala := escalaDeProva(t)
	bom := &bancoFalso{id: "cgd", escala: escala}
	mau := &bancoFalso{id: "novobanco", escala: escala, substituir: map[string]string{"2.000": "2.400"}}
	var disparos contador

	correr(context.Background(), []bancos.Banco{bom, mau},
		map[string]dominio.EscalaDeLTV{"cgd": escala, "novobanco": escala},
		hojeDeProva(t), disparos.revarrer)

	if got := disparos.ids(); len(got) != 1 || got[0] != "novobanco" {
		t.Errorf("revarreram-se %v, e só o novobanco divergiu", got)
	}
}

// TestUmaSondaCegaNaoConfirmaENaoRevarre separa as duas coisas que se confundem
// com facilidade.
//
// ⚠️ Silêncio não é concordância — a grelha fica POR CONFIRMAR. Mas também não é
// divergência: disparar 96 pedidos porque o banco não respondeu a quatro é
// carregá-lo exactamente quando ele está em baixo.
func TestUmaSondaCegaNaoConfirmaENaoRevarre(t *testing.T) {
	escala := escalaDeProva(t)
	banco := &bancoFalso{id: "cgd", escala: escala, erro: errors.New("o simulador não respondeu")}
	var disparos contador

	rel := correr(context.Background(), []bancos.Banco{banco},
		map[string]dominio.EscalaDeLTV{"cgd": escala}, hojeDeProva(t), disparos.revarrer)

	b := rel.Bancos[0]
	if b.Confirmada {
		t.Error("uma sonda que não mediu nada deu a grelha por confirmada")
	}
	if b.Cegos != 3 {
		t.Errorf("%d sondas cegas, esperava 3", b.Cegos)
	}
	if b.Divergentes != 0 {
		t.Errorf("%d divergências, e não se mediu nada — cego não é divergente", b.Divergentes)
	}
	if n := disparos.total(); n != 0 {
		t.Errorf("%d varrimentos disparados por um banco que não respondeu, e não devia ser nenhum", n)
	}
}

// TestComSemRevarrerDetectaENaoDispara: ver o estado sem o mudar.
func TestComSemRevarrerDetectaENaoDispara(t *testing.T) {
	escala := escalaDeProva(t)
	banco := &bancoFalso{id: "cgd", escala: escala, substituir: map[string]string{"1.350": "1.500"}}

	rel := correr(context.Background(), []bancos.Banco{banco},
		map[string]dominio.EscalaDeLTV{"cgd": escala}, hojeDeProva(t), nil)

	b := rel.Bancos[0]
	if b.Divergentes != 1 {
		t.Errorf("%d divergências, esperava 1 — o modo sem-revarrer continua a detectar", b.Divergentes)
	}
	if b.Revarrido {
		t.Error("revarreu com --sem-revarrer")
	}
}

// TestUmBancoSemEscalaGuardadaNaoDerrubaACorrida: a §5 diz que um banco não
// derruba os outros, e isso vale também aqui.
func TestUmBancoSemEscalaGuardadaNaoDerrubaACorrida(t *testing.T) {
	escala := escalaDeProva(t)
	semEscala := &bancoFalso{id: "montepio", escala: escala}
	comEscala := &bancoFalso{id: "cgd", escala: escala}
	var disparos contador

	rel := correr(context.Background(), []bancos.Banco{comEscala, semEscala},
		map[string]dominio.EscalaDeLTV{"cgd": escala}, hojeDeProva(t), disparos.revarrer)

	if len(rel.Bancos) != 2 {
		t.Fatalf("%d bancos no relatório, esperava 2", len(rel.Bancos))
	}
	if rel.Bancos[1].Motivo == "" {
		t.Error("o banco sem escala guardada não diz porque é que não foi sondado")
	}
	if !rel.Bancos[0].Confirmada {
		t.Error("o banco com escala não foi sondado por causa do que não tinha")
	}
	if n := semEscala.chamadas.Load(); n != 0 {
		t.Errorf("fizeram-se %d pedidos a um banco sem escala para confirmar", n)
	}
}

// TestUmVarrimentoTravadoNaoPassaPorFeito: o travão em Postgres é o que torna a
// escalada segura, e quando ele segura o revarrimento isso tem de aparecer.
//
// ⚠️ Sem isto, um banco cujo varrimento não chegou a correr saía do relatório
// igual a um que correu — e a razão de ter falhado era precisamente a
// interessante.
func TestUmVarrimentoTravadoNaoPassaPorFeito(t *testing.T) {
	escala := escalaDeProva(t)
	banco := &bancoFalso{id: "cgd", escala: escala, substituir: map[string]string{"1.350": "1.500"}}
	travado := func(context.Context, string) (bool, string, error) {
		return true, "outro varrimento deste banco está a correr", nil
	}

	rel := correr(context.Background(), []bancos.Banco{banco},
		map[string]dominio.EscalaDeLTV{"cgd": escala}, hojeDeProva(t), travado)

	b := rel.Bancos[0]
	if b.Revarrido {
		t.Error("o relatório diz que revarreu, e o travão segurou o varrimento")
	}
	if b.MotivoDoRevarrimento == "" {
		t.Error("o varrimento não correu e o relatório não diz porquê")
	}
}

// contador é um Revarrer que regista em vez de varrer.
type contador struct {
	mu       sync.Mutex
	pedidos  []string
	chamadas atomic.Int64
}

func (c *contador) revarrer(_ context.Context, bancoID string) (bool, string, error) {
	c.chamadas.Add(1)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pedidos = append(c.pedidos, bancoID)
	return false, "", nil
}

func (c *contador) total() int64 { return c.chamadas.Load() }

func (c *contador) ids() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.pedidos...)
}

// bancoFalso responde o spread que a escala dada prevê para o LTV pedido, com
// as substituições que o teste quiser. É o banco que «não mudou» — e, com
// `substituir`, o que mudou num degrau só.
type bancoFalso struct {
	id     string
	escala dominio.EscalaDeLTV

	// substituir troca o spread previsto por outro, por valor. Chaveado pela
	// representação do spread para o teste se ler como a tabela do dossiê.
	substituir map[string]string

	erro     error
	chamadas atomic.Int64
}

func (b *bancoFalso) ID() string   { return b.id }
func (b *bancoFalso) Nome() string { return b.id }

func (b *bancoFalso) Requisitos() dominio.Requisitos {
	return dominio.Requisitos{BancoID: b.id, BancoNome: b.Nome(), Custo: dominio.CustoBarato}
}

func (b *bancoFalso) Simular(_ context.Context, p dominio.Pedido) (dominio.Oferta, error) {
	b.chamadas.Add(1)
	if b.erro != nil {
		return dominio.Oferta{}, b.erro
	}

	ltv, err := dominio.LTV(p.Montante, p.ValorImovel)
	if err != nil {
		return dominio.Oferta{}, err
	}
	spread, _, err := b.escala.SpreadEm(ltv)
	if err != nil {
		return dominio.Oferta{}, err
	}

	// ⚠️ A comparação é por VALOR e não por texto: o Taxa.String() normaliza —
	// "1.350" sai "1.35" e "2.000" sai "2" —, e uma chave escrita como o dossiê
	// a escreve nunca casaria.
	for de, para := range b.substituir {
		if !mesmaTaxa(de, spread) {
			continue
		}
		s, err := dominio.TaxaDeTexto(para)
		if err != nil {
			return dominio.Oferta{}, err
		}
		spread = s
		break
	}
	return dominio.Oferta{BancoID: b.id, BancoNome: b.Nome(), Spread: &spread}, nil
}

func racioDe(t *testing.T, s string) dominio.Racio {
	t.Helper()
	r, err := dominio.RacioDeTexto(s)
	if err != nil {
		t.Fatalf("RacioDeTexto(%q): %v", s, err)
	}
	return r
}

func taxaDe(t *testing.T, s string) dominio.Taxa {
	t.Helper()
	x, err := dominio.TaxaDeTexto(s)
	if err != nil {
		t.Fatalf("TaxaDeTexto(%q): %v", s, err)
	}
	return x
}

// hojeDeProva é uma data fixa: a idade do titular neutro entra no prazo máximo,
// e um "hoje" que anda faz o teste mudar de resposta no dia de anos de ninguém.
func hojeDeProva(t *testing.T) dominio.Data {
	t.Helper()
	d, err := dominio.DataDeTexto("2026-08-02")
	if err != nil {
		t.Fatalf("DataDeTexto: %v", err)
	}
	return d
}

// mesmaTaxa compara um literal com uma Taxa, por valor.
func mesmaTaxa(literal string, t dominio.Taxa) bool {
	x, err := dominio.TaxaDeTexto(literal)
	if err != nil {
		return false
	}
	return x.Decimal().Equal(t.Decimal())
}

func mesmaTaxaTexto(a, b string) bool {
	x, err := dominio.TaxaDeTexto(a)
	if err != nil {
		return false
	}
	return mesmaTaxa(b, x)
}
