package dominio

import (
	"fmt"
	"time"
)

// Data é uma data civil: ano, mês e dia, sem hora e sem fuso.
//
// Uma data de nascimento não tem fuso horário, e tratá-la como instante é como
// se produz o erro de um dia. Com time.Now().UTC(), entre a meia-noite e a uma
// da manhã de Lisboa no horário de Verão, o "hoje" em UTC ainda é ontem: quem
// faz anos nesse dia conta com menos um ano, e a idade decide o prazo máximo do
// crédito e o limite de 35 anos do DL 44/2024. O v1 pagou esta conta noutro
// sítio — a utcnow() com um comentário a explicar porque não se podia trocar.
//
// ⚠️ Ao contrário de Dinheiro, Taxa e Racio, a Data é comparável com ==, e
// isso está certo: são três inteiros, sem ponteiro por baixo e sem escalas
// diferentes a representar o mesmo valor.
type Data struct {
	Ano int
	Mes time.Month
	Dia int
}

// DataDe constrói uma data e recusa o que não existe no calendário.
func DataDe(ano int, mes time.Month, dia int) (Data, error) {
	t := time.Date(ano, mes, dia, 0, 0, 0, 0, time.UTC)
	// O time.Date normaliza em silêncio — 31 de Fevereiro vira 3 de Março. Se
	// o que sai não é o que entrou, a data não existia.
	if t.Year() != ano || t.Month() != mes || t.Day() != dia {
		return Data{}, fmt.Errorf("data inexistente: %04d-%02d-%02d", ano, mes, dia)
	}
	return Data{Ano: ano, Mes: mes, Dia: dia}, nil
}

// DataDeTexto lê uma data ISO 8601 sem hora ("1990-04-12"), que é o formato
// que o contrato publica para data_nascimento.
func DataDeTexto(s string) (Data, error) {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return Data{}, fmt.Errorf("data inválida %q: %w", s, err)
	}
	return Data{Ano: t.Year(), Mes: t.Month(), Dia: t.Day()}, nil
}

// DataDeInstante extrai a data civil de um instante. É a porta que a infra usa
// para converter o relógio em "hoje".
//
// ⚠️ O fuso do instante é o que decide o dia. Quem chama passa um instante já
// no fuso certo (Europe/Lisbon), não em UTC.
func DataDeInstante(t time.Time) Data {
	return Data{Ano: t.Year(), Mes: t.Month(), Dia: t.Day()}
}

// Zero diz se a data é o valor por omissão, que não é uma data.
func (d Data) Zero() bool { return d == Data{} }

// Antes diz se d é anterior a o.
func (d Data) Antes(o Data) bool {
	if d.Ano != o.Ano {
		return d.Ano < o.Ano
	}
	if d.Mes != o.Mes {
		return d.Mes < o.Mes
	}
	return d.Dia < o.Dia
}

// Idade devolve os anos completos entre d e a data em.
//
// Conta a data toda e não só o ano: quem ainda não fez anos este ano tem menos
// um. Devolve negativo se d for posterior a em — cabe a quem valida o Pedido
// recusar uma data de nascimento no futuro.
func (d Data) Idade(em Data) int {
	anos := em.Ano - d.Ano
	if em.Mes < d.Mes || (em.Mes == d.Mes && em.Dia < d.Dia) {
		anos--
	}
	return anos
}

func (d Data) String() string {
	return fmt.Sprintf("%04d-%02d-%02d", d.Ano, int(d.Mes), d.Dia)
}
