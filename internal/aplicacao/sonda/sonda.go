// Package sonda confirma, com ~4 pedidos por banco, se a grelha guardada ainda
// descreve o banco. Não substitui o varrimento — encurta o intervalo às escuras.
//
// O desenho, e o que ela deliberadamente não vê, estão na decisão 6 da §7 do
// ARQUITETURA.md. Não confundir com o resíduo da §7.4: esse mede coerência
// interna da resposta do banco, esta mede deriva da nossa grelha contra ele.
package sonda

import (
	"context"
	"errors"
	"fmt"

	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// ErrEscalaSemDegraus: não há escala guardada sobre que sondar.
var ErrEscalaSemDegraus = errors.New("a escala guardada não tem degraus para sondar")

// RecuoOmissao é a distância abaixo do Ate a que a sonda se coloca. Tem de ser
// grande o bastante para o arredondamento ao cêntimo não a atirar para a
// fronteira, e é uma ordem de grandeza acima da grelha.ToleranciaOmissao —
// abaixo disso afirmaria uma precisão que a escala não tem.
var RecuoOmissao = racio("0.001")

// Ponto é um sítio onde se vai perguntar ao banco, e o que se espera ouvir.
type Ponto struct {
	// LTV é onde se pergunta: logo abaixo do Ate do degrau, e não no meio.
	// A assimetria é deliberada — ver a §7, decisão 6.
	LTV dominio.Racio

	// Esperado é o spread que a grelha guardada diz que o banco pratica aqui.
	Esperado dominio.Taxa

	// Tolerancia é lida do degrau, não escolhida: zero se resolvido, e a largura
	// da incerteza medida (Spread - SpreadMinimo) se não.
	Tolerancia dominio.Taxa

	// Degrau é o intervalo que este ponto confirma — viaja para o relatório
	// poder nomear o que mexeu.
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

// Medir pergunta a um banco o spread que pratica num LTV. Tem de propósito a
// mesma forma que a grelha.Amostrar: são a mesma pergunta ao mesmo sítio.
type Medir func(ctx context.Context, ltv dominio.Racio) (dominio.Taxa, error)

// Leitura é o que uma sonda encontrou num ponto.
type Leitura struct {
	Ponto Ponto

	// Observado é o spread que o banco pratica agora neste LTV.
	Observado dominio.Taxa

	// Desvio é Observado - Esperado, com sinal: mais caro e mais barato são
	// notícias diferentes.
	Desvio dominio.Taxa

	// Divergiu é `|Desvio| > Tolerancia`.
	Divergiu bool

	// Erro é não se ter conseguido medir aqui — uma sonda cega, que não
	// confirma nada. Tratar silêncio como concordância é como um detector
	// avariado passa por satisfeito.
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

// Confirmada diz se a grelha deste banco continua a servir. Uma sonda cega não
// confirma: seria tranquilidade sem medição.
func (r Relatorio) Confirmada() bool { return r.Divergentes == 0 && r.Cegos == 0 }

// Cobertura diz, em português, o que esta corrida NÃO viu. Existe porque o
// veredicto sozinho engana: «confirmada» quer dizer «os degraus que
// conhecíamos não mexeram», e não «o banco não mudou».
func (r Relatorio) Cobertura() string {
	return fmt.Sprintf(
		"%d degrau(s) sondado(s). Uma sonda confirma o preço do degrau em que "+
			"cai e apanha uma fronteira que desça; não apanha uma fronteira que "+
			"suba nem um patamar novo mais estreito do que o espaço entre sondas.",
		len(r.Leituras))
}

// Correr sonda os pontos e devolve o que encontrou. Não pára no primeiro
// desvio: um banco que mudou dois degraus é notícia diferente de um que mudou
// um, e parar cedo poupava um pedido para perder essa distinção.
func Correr(ctx context.Context, bancoID string, pontos []Ponto, medir Medir) (Relatorio, error) {
	if len(pontos) == 0 {
		return Relatorio{}, ErrEscalaSemDegraus
	}

	r := Relatorio{BancoID: bancoID, Leituras: make([]Leitura, 0, len(pontos))}
	for _, p := range pontos {
		// Interrompe devolvendo o apurado, em vez de um relatório curto com ar
		// de completo.
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

// excede compara o módulo do desvio com a tolerância. Estritamente maior: um
// desvio igual à largura da incerteza é o outro lado do degrau, que já sabíamos
// ser possível ali.
func excede(desvio, tolerancia dominio.Taxa) bool {
	return desvio.Decimal().Abs().GreaterThan(tolerancia.Decimal().Abs())
}

// racio constrói um Racio de um literal deste ficheiro; pânico é o certo.
func racio(s string) dominio.Racio {
	r, err := dominio.RacioDeTexto(s)
	if err != nil {
		panic(err)
	}
	return r
}
