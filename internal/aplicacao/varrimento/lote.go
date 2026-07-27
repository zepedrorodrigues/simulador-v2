package varrimento

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// O lote de uma corrida, e a guarda que impede dois seguidos.
//
// ⚠️ A guarda não é conveniência: é a correcção da série. O v1 estragou a
// primeira medição por se dispararem varrimentos à mão em cima uns dos outros —
// cinco corridas em 14 minutos não são cinco dias de dados, são cinco cópias do
// mesmo dia com carimbos diferentes. Quem depois lesse a série via movimento
// onde não houve nenhum.
//
// A guarda é de TEMPO e o travão da §7.2 é de CONCORRÊNCIA, e são coisas
// diferentes que se confundem com facilidade: o advisory lock impede duas
// corridas ao mesmo tempo no mesmo banco, e não impede nada a duas corridas
// separadas por um minuto. É esta que impede a segunda.

// Catalogo é a porta de escrita do varrimento.
//
// ⚠️ Declara-se aqui, do lado de quem a usa, e implementa-se na infra — é o que
// mantém este pacote sem SQL (§3), e é o que deixa a guarda ser afirmada com um
// catálogo falso, sem base nenhuma.
type Catalogo interface {
	// UltimoVarrimentoEm devolve o instante da observação mais recente. O bool
	// é falso quando nunca se varreu nada, que é diferente de «varreu-se há
	// muito tempo».
	UltimoVarrimentoEm(ctx context.Context) (time.Time, bool, error)

	// GravarLote grava as observações de uma corrida sob um varrimento_id novo,
	// e devolve-o.
	//
	// ⚠️ Ou grava todas ou não grava nenhuma. Um lote meio gravado é pior do
	// que nenhum: a leitura seguinte veria uma corrida onde faltavam bancos,
	// sem nada a dizer que faltavam, e concluiria que esses bancos não
	// responderam.
	GravarLote(ctx context.Context, obs []Observacao) (string, error)
}

// ErrVarrimentoRecente diz que a guarda travou a corrida: o último varrimento é
// novo de mais.
//
// É sentinela para quem chama distinguir «não corri porque não devia» de «corri
// e correu mal». O subcomando sai com 0 no primeiro caso — não é erro, é a
// guarda a funcionar.
var ErrVarrimentoRecente = errors.New("o último varrimento é recente de mais")

// Lote é o que uma corrida gravou.
type Lote struct {
	// ID é o varrimento_id que agrupa as linhas. Vazio quando a guarda travou.
	ID string

	Resultado Resultado

	// Saltado diz que a guarda não deixou correr. ⚠️ Não é o mesmo que um
	// Resultado vazio: aí ter-se-ia ido aos bancos e não teria vindo nada.
	Saltado bool
}

// VarrerEGravar corre o varrimento e grava o lote, respeitando a guarda.
//
// seMaisVelhoQue é a guarda `--se-antigo`: só se corre se a observação mais
// recente for mais velha do que isto. Zero ou negativo desliga-a — e desligá-la
// é uma escolha explícita de quem chama, não o valor por omissão de um campo
// que ninguém preencheu.
//
// ⚠️ A guarda pergunta ANTES de ir aos bancos. Perguntar depois pouparia a
// escrita e não pouparia o que interessa poupar, que é a carga no simulador
// alheio.
func (v *Varredor) VarrerEGravar(
	ctx context.Context, cat Catalogo, pontos []Ponto, seMaisVelhoQue time.Duration,
) (Lote, error) {
	if cat == nil {
		return Lote{}, errors.New("varrimento sem catálogo onde gravar")
	}

	if seMaisVelhoQue > 0 {
		ultimo, houve, err := cat.UltimoVarrimentoEm(ctx)
		if err != nil {
			return Lote{}, fmt.Errorf("ler o último varrimento: %w", err)
		}
		// ⚠️ Nunca ter varrido não é «varreu-se há muito tempo» por acaso: é o
		// caso em que a guarda TEM de deixar passar, e escrevê-lo assim evita
		// depender de um instante zero comparado com um relógio.
		if houve {
			if idade := v.agora().Sub(ultimo); idade < seMaisVelhoQue {
				return Lote{Saltado: true}, fmt.Errorf(
					"%w: o último foi há %s e a guarda pede %s",
					ErrVarrimentoRecente, idade.Round(time.Second), seMaisVelhoQue)
			}
		}
	}

	resultado := v.Varrer(ctx, pontos)

	// ⚠️ Uma corrida sem observações não abre lote. Não é o mesmo que um lote
	// vazio: um varrimento_id sem linhas nenhumas ficaria na série a dizer que
	// se varreu, e a guarda seguinte contaria com ele para não voltar a correr.
	if len(resultado.Observacoes) == 0 {
		return Lote{Resultado: resultado}, nil
	}

	id, err := cat.GravarLote(ctx, resultado.Observacoes)
	if err != nil {
		return Lote{Resultado: resultado}, fmt.Errorf("gravar o lote: %w", err)
	}
	return Lote{ID: id, Resultado: resultado}, nil
}
