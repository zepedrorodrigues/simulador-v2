// Package sonda confirma, por um punhado de pedidos, se a grelha que temos
// ainda descreve o banco.
//
// É a decisão 6 da §7 do ARQUITETURA.md (2026-08-01). Entre varrer tudo e não
// saber nada havia um vazio: uma grelha varrida há seis horas pode já não
// descrever o banco, e a única forma de o saber era varrer outra vez — ~96
// pedidos por banco para descobrir que nada mudou. Isto custa ~4.
//
// ⚠️ **Não é o resíduo da §7.4, e confundi-los perde os dois.** O resíduo compara
// a prestação que o banco devolveu com a que a francesa dá sobre o plano do
// PRÓPRIO banco — é coerência interna da resposta dele. A sonda compara o que a
// NOSSA grelha prevê com o que o banco responde agora — é deriva da nossa
// fotografia contra a realidade. O resíduo apanha um campo mal lido; a sonda
// apanha um preço que mudou. Um está a 0,00 € e o outro nunca foi medido.
//
// ⚠️ **E não substitui o varrimento.** Encurta o intervalo em que se está às
// escuras. O que ela não vê está escrito no `Cobertura` e na §7.
package sonda

import (
	"context"
	"errors"
	"fmt"

	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// ErrEscalaSemDegraus: não há escala guardada sobre que sondar.
var ErrEscalaSemDegraus = errors.New("a escala guardada não tem degraus para sondar")

// RecuoOmissao é a distância abaixo do `Ate` a que a sonda se coloca.
//
// ⚠️ **Um décimo de ponto percentual, e a escolha não é neutra.** Tem de ser
// pequeno o suficiente para a sonda continuar dentro do degrau que quer
// confirmar, e grande o suficiente para não cair na fronteira por erro de
// arredondamento — a `grelha.Medicao` avisa que pedir 66,5625 % pode medir
// 66,5624 %, porque o montante arredonda ao cêntimo.
//
// ⚠️ E é uma ordem de grandeza acima da `grelha.ToleranciaOmissao` (0,05 p.p.),
// que é onde a descoberta pára de refinar: uma sonda mais perto da fronteira do
// que a própria resolução com que ela foi medida estaria a afirmar uma precisão
// que a escala não tem.
var RecuoOmissao = racio("0.001")

// Ponto é um sítio onde se vai perguntar ao banco, e o que se espera ouvir.
type Ponto struct {
	// LTV é onde se pergunta: logo ABAIXO do `Ate` do degrau.
	//
	// ⚠️ **Abaixo do topo e não no meio, e a assimetria é deliberada** (§7,
	// decisão 6). Se a fronteira DESCER, este ponto passa a cair no degrau
	// seguinte e vê spread diferente — detecta. Se a fronteira SUBIR, continua
	// no degrau antigo e não vê nada — não detecta, e o erro que daí resulta é
	// servirmos o spread mais ALTO a quem já qualificava para o mais baixo, que
	// é a direcção que o Anexo I, Parte II, alínea (d) da MCD manda presumir.
	//
	// Ou seja: apanha a direcção que nos faria servir barato de mais, e falha a
	// que nos faz servir caro de mais.
	LTV dominio.Racio

	// Esperado é o spread que a grelha guardada diz que o banco pratica aqui.
	Esperado dominio.Taxa

	// Tolerancia é quanto se aceita de diferença ANTES de chamar divergência.
	//
	// ⚠️ **Não é escolhida — é lida do degrau.** Num degrau resolvido é ZERO, e
	// isso é defensável porque a medição de fidelidade deu zero divergências em
	// 147 098 comparações: se hoje reproduzimos o banco exactamente, qualquer
	// diferença é sinal e não ruído. Num degrau por resolver é
	// `Spread - SpreadMinimo`, que é a largura da incerteza que já foi medida.
	//
	// ⚠️ Uma tolerância constante escolhida à cabeça seria o defeito que a §7.4
	// nomeia — um número com ar de certo — aplicado ao próprio detector.
	Tolerancia dominio.Taxa

	// Degrau é o intervalo que este ponto confirma. Viaja para o relatório
	// poder nomear o que mexeu, e não só dizer que alguma coisa mexeu.
	Degrau dominio.DegrauLTV
}

// PontosDe deriva, de uma escala guardada, onde sondar.
//
// Um ponto por degrau. O `recuo` a zero vale RecuoOmissao.
//
// ⚠️ Um degrau mais estreito do que o recuo é sondado no seu ponto médio, e não
// se salta. Saltá-lo deixava um troço do domínio sem confirmação nenhuma e sem
// nada a dizê-lo; o ponto médio confirma o spread, que é o que quase sempre
// muda, e perde só a detecção de fronteira — que num degrau desses já era
// duvidosa. O relatório di-lo pelo `Cobertura`.
func PontosDe(escala dominio.EscalaDeLTV, recuo dominio.Racio) ([]Ponto, error) {
	degraus := escala.Degraus()
	if len(degraus) == 0 {
		return nil, ErrEscalaSemDegraus
	}
	if recuo.Decimal().IsZero() {
		recuo = RecuoOmissao
	}

	pontos := make([]Ponto, 0, len(degraus))
	for _, d := range degraus {
		pontos = append(pontos, Ponto{
			LTV:        ltvDaSonda(d, recuo),
			Esperado:   d.Spread,
			Tolerancia: toleranciaDe(d),
			Degrau:     d,
		})
	}
	return pontos, nil
}

// ltvDaSonda escolhe o rácio: `Ate - recuo`, ou o meio de um degrau demasiado
// estreito para o recuo caber.
func ltvDaSonda(d dominio.DegrauLTV, recuo dominio.Racio) dominio.Racio {
	if candidato := d.Ate.Sub(recuo); candidato.Cmp(d.De) > 0 {
		return candidato
	}
	return d.De.Meio(d.Ate)
}

// toleranciaDe lê do degrau quanto se aceita de diferença.
func toleranciaDe(d dominio.DegrauLTV) dominio.Taxa {
	if d.Resolvido() {
		return dominio.Taxa{}
	}
	// Num degrau por resolver, `Spread` é o lado mais caro e `SpreadMinimo` o
	// mais barato — o construtor da escala recusa o contrário. A largura da
	// incerteza é a diferença, e é exactamente o que aqui se tolera.
	return d.Spread.Sub(*d.SpreadMinimo)
}

// Medir pergunta a um banco o spread que ele pratica num LTV.
//
// É a mesma forma que a `grelha.Amostrar` tem, de propósito: quem sonda e quem
// descobre a escala perguntam a mesma coisa ao mesmo sítio, e reaproveitar a
// forma evita duas maneiras de fazer o mesmo pedido divergirem em silêncio.
type Medir func(ctx context.Context, ltv dominio.Racio) (dominio.Taxa, error)

// Leitura é o que uma sonda encontrou num ponto.
type Leitura struct {
	Ponto Ponto

	// Observado é o spread que o banco pratica agora neste LTV.
	Observado dominio.Taxa

	// Desvio é `Observado - Esperado`, com sinal. ⚠️ Com sinal e não em módulo:
	// o banco a ficar mais caro e o banco a ficar mais barato são notícias
	// diferentes, e quem lê o relatório precisa de as distinguir.
	Desvio dominio.Taxa

	// Divergiu é `|Desvio| > Tolerancia`.
	Divergiu bool

	// Erro é não se ter conseguido medir aqui. ⚠️ Um ponto que não se mede NÃO
	// é uma confirmação: é uma sonda cega, e conta como tal na Cobertura. O
	// contrário — tratar o silêncio como concordância — é como um detector
	// avariado passa por um detector satisfeito.
	Erro error
}

// Relatorio é o que uma corrida da sonda apurou sobre um banco.
type Relatorio struct {
	BancoID string

	Leituras []Leitura

	// Divergentes e Cegos são contagens, para quem lê não ter de as fazer.
	Divergentes int
	Cegos       int
}

// Confirmada diz se a grelha deste banco continua a servir.
//
// ⚠️ **Uma sonda cega NÃO confirma.** Se não se conseguiu medir um ponto, não se
// sabe o que lá está — e um relatório que dissesse «confirmado» com metade das
// sondas em erro seria o pior resultado possível: a tranquilidade sem a
// medição.
func (r Relatorio) Confirmada() bool { return r.Divergentes == 0 && r.Cegos == 0 }

// Cobertura é a frase que acompanha o veredicto, em português, e diz o que esta
// corrida NÃO viu.
//
// ⚠️ Existe porque o veredicto sozinho engana. «Confirmada» quer dizer «os
// degraus que conhecíamos continuam onde estavam e com o preço que tinham» —
// não quer dizer «o banco não mudou». Uma fronteira que suba, e um patamar novo
// mais estreito do que a distância entre sondas, passam aqui sem serem vistos.
// É o que a KAN-35 já mediu na CGD: o 2,050 entre 66,75 % e 67,75 %, 1 p.p. de
// largura e não monótono, que só uma descoberta densa apanha.
func (r Relatorio) Cobertura() string {
	return fmt.Sprintf(
		"%d degrau(s) sondado(s). Uma sonda confirma o preço do degrau em que "+
			"cai e apanha uma fronteira que desça; não apanha uma fronteira que "+
			"suba nem um patamar novo mais estreito do que o espaço entre sondas.",
		len(r.Leituras))
}

// Correr sonda os pontos e devolve o que encontrou.
//
// ⚠️ Não pára no primeiro desvio. Um banco que mudou dois degraus é informação
// diferente de um banco que mudou um, e parar cedo custava um pedido a menos
// para perder a diferença entre «afinaram um escalão» e «refizeram a tabela».
func Correr(ctx context.Context, bancoID string, pontos []Ponto, medir Medir) (Relatorio, error) {
	if len(pontos) == 0 {
		return Relatorio{}, ErrEscalaSemDegraus
	}

	r := Relatorio{BancoID: bancoID, Leituras: make([]Leitura, 0, len(pontos))}
	for _, p := range pontos {
		// ⚠️ O cancelamento interrompe e devolve o que já se apurou, em vez de
		// devolver um relatório curto com ar de completo. Um `Confirmada()`
		// verdadeiro sobre metade das sondas seria uma mentira barata.
		if err := ctx.Err(); err != nil {
			return r, fmt.Errorf("sonda de %s interrompida ao fim de %d ponto(s): %w",
				bancoID, len(r.Leituras), err)
		}

		observado, err := medir(ctx, p.LTV)
		if err != nil {
			r.Leituras = append(r.Leituras, Leitura{Ponto: p, Erro: err})
			r.Cegos++
			continue
		}

		desvio := observado.Sub(p.Esperado)
		l := Leitura{
			Ponto:     p,
			Observado: observado,
			Desvio:    desvio,
			Divergiu:  excede(desvio, p.Tolerancia),
		}
		if l.Divergiu {
			r.Divergentes++
		}
		r.Leituras = append(r.Leituras, l)
	}
	return r, nil
}

// excede compara o módulo do desvio com a tolerância.
//
// ⚠️ Estritamente maior: um desvio IGUAL à tolerância não diverge. Num degrau
// por resolver a tolerância é a largura conhecida da incerteza, e um desvio
// dessa exacta largura é o outro lado do degrau — que é o valor que já sabíamos
// ser possível ali, não uma surpresa.
func excede(desvio, tolerancia dominio.Taxa) bool {
	return desvio.Decimal().Abs().GreaterThan(tolerancia.Decimal().Abs())
}

// racio constrói um Racio de um literal do próprio ficheiro. Pânico é o certo:
// um literal errado aqui é defeito de quem o escreveu, não estado de execução.
func racio(s string) dominio.Racio {
	r, err := dominio.RacioDeTexto(s)
	if err != nil {
		panic(err)
	}
	return r
}
