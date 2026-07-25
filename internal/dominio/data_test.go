package dominio_test

import (
	"testing"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

func TestDataDeRecusaOQueNaoExiste(t *testing.T) {
	// O time.Date normaliza em silêncio: 31 de Fevereiro vira 3 de Março. Sem
	// esta guarda, uma data impossível entrava e ninguém dava por ela.
	if _, err := dominio.DataDe(2026, time.February, 31); err == nil {
		t.Error("31 de Fevereiro foi aceite")
	}
	if _, err := dominio.DataDe(2025, time.February, 29); err == nil {
		t.Error("29 de Fevereiro de 2025 foi aceite, e 2025 não é bissexto")
	}
	if _, err := dominio.DataDe(2024, time.February, 29); err != nil {
		t.Errorf("29 de Fevereiro de 2024 foi recusado, e 2024 é bissexto: %v", err)
	}
}

func TestDataDeTexto(t *testing.T) {
	d, err := dominio.DataDeTexto("1990-04-12")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if querido := (dominio.Data{Ano: 1990, Mes: time.April, Dia: 12}); d != querido {
		t.Errorf("data = %v, queria %v", d, querido)
	}
	for _, s := range []string{"", "12-04-1990", "1990-04-12T00:00:00Z", "1990-13-01"} {
		if _, err := dominio.DataDeTexto(s); err == nil {
			t.Errorf("%q foi aceite e não devia", s)
		}
	}
}

func TestIdade(t *testing.T) {
	casos := []struct {
		nome         string
		nascimento   string
		hoje         string
		queridaIdade int
	}{
		{"faz anos hoje", "1990-07-25", "2026-07-25", 36},
		{"faz anos amanhã", "1990-07-26", "2026-07-25", 35},
		{"fez anos ontem", "1990-07-24", "2026-07-25", 36},
		{"mês seguinte, dia anterior", "1990-08-24", "2026-07-25", 35},
		{"nasceu a 29 de Fevereiro, véspera", "2000-02-29", "2026-02-28", 25},
		{"nasceu a 29 de Fevereiro, a seguir", "2000-02-29", "2026-03-01", 26},
		{"ainda não nasceu", "2027-01-01", "2026-07-25", -1},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			nascimento, err := dominio.DataDeTexto(c.nascimento)
			if err != nil {
				t.Fatalf("data inválida: %v", err)
			}
			hoje, err := dominio.DataDeTexto(c.hoje)
			if err != nil {
				t.Fatalf("data inválida: %v", err)
			}
			if idade := nascimento.Idade(hoje); idade != c.queridaIdade {
				t.Errorf("idade = %d, queria %d", idade, c.queridaIdade)
			}
		})
	}
}

func TestDataDeInstanteSegueOFusoQueLheDao(t *testing.T) {
	// ⚠️ É este o erro de um dia que a Data existe para evitar. O mesmo
	// instante dá dias diferentes conforme o fuso, e o dia é que decide a
	// idade — que decide o prazo máximo e o limite dos 35 anos do DL 44/2024.
	lisboa, err := time.LoadLocation("Europe/Lisbon")
	if err != nil {
		t.Skipf("sem base de dados de fusos nesta máquina: %v", err)
	}
	// 26 de Julho de 2026, 00:30 em Lisboa (verão, UTC+1) é ainda dia 25 em UTC.
	instante := time.Date(2026, time.July, 26, 0, 30, 0, 0, lisboa)

	if d := dominio.DataDeInstante(instante); d.Dia != 26 {
		t.Errorf("em Lisboa o dia é 26 e saiu %v", d)
	}
	if d := dominio.DataDeInstante(instante.UTC()); d.Dia != 25 {
		t.Errorf("em UTC o mesmo instante ainda é dia 25 e saiu %v", d)
	}
}

func TestDataAntesEZero(t *testing.T) {
	var zero dominio.Data
	if !zero.Zero() {
		t.Error("o valor por omissão devia dizer que é zero")
	}
	a := dominio.Data{Ano: 2026, Mes: time.July, Dia: 25}
	if a.Zero() {
		t.Error("uma data preenchida não é zero")
	}
	casos := []struct {
		nome    string
		a, b    dominio.Data
		querido bool
	}{
		{"ano menor", dominio.Data{Ano: 2025, Mes: time.December, Dia: 31}, a, true},
		{"mesmo ano, mês menor", dominio.Data{Ano: 2026, Mes: time.June, Dia: 30}, a, true},
		{"mesmo mês, dia menor", dominio.Data{Ano: 2026, Mes: time.July, Dia: 24}, a, true},
		{"a mesma data", a, a, false},
		{"posterior", dominio.Data{Ano: 2026, Mes: time.July, Dia: 26}, a, false},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if got := c.a.Antes(c.b); got != c.querido {
				t.Errorf("%v antes de %v = %v, queria %v", c.a, c.b, got, c.querido)
			}
		})
	}
}
