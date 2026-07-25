package bancos

import (
	"errors"
	"fmt"
	"slices"

	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/transporte"
)

// Transportes é o que a infra monta uma vez e injecta em cada banco. Um banco
// usa o que precisa, e nenhum instancia o seu — é isso que os deixa correr
// contra um transporte.Falso nos testes.
//
// As duas estratégias de browser entram aqui na fase 3, com a decisão da
// biblioteca (KAN-20). Até lá não têm campo: um campo que ninguém preenche é uma
// promessa por cumprir a fingir de arquitectura.
type Transportes struct {
	HTTP      transporte.HTTPSimples
	ComSessao transporte.HTTPComSessao
}

// Construtor monta um banco a partir dos transportes injectados.
//
// Recebe os transportes todos e não só o que lhe serve, para a assinatura ser
// uma só: é o que permite ao registo ser uma tabela em vez de um switch.
type Construtor func(Transportes) Banco

// ErrBancoDesconhecido: pediu-se um id que não está registado.
var ErrBancoDesconhecido = errors.New("banco desconhecido")

// Registo mapeia bancoID → construtor.
//
// É um valor e não uma variável de pacote de propósito: um registo global e
// mutável seria escrito por init()s e lido por toda a gente, e um teste que
// quisesse afirmar seja o que fosse teria de o sujar primeiro. ⚠️ A camada de
// aplicação não o importa — recebe os bancos já construídos, e é o que a torna
// testável com bancos falsos (ARQUITETURA.md §3).
type Registo struct {
	construtores map[string]Construtor
}

// NovoRegisto devolve um registo vazio.
func NovoRegisto() *Registo {
	return &Registo{construtores: make(map[string]Construtor)}
}

// Registar liga um id ao seu construtor.
//
// Recusa o id vazio, o construtor nulo e o id repetido. O repetido não é
// preciosismo: um banco registado duas vezes calaria o primeiro em silêncio, e o
// que desapareceria da comparação era uma oferta, não uma linha de log.
func (r *Registo) Registar(id string, c Construtor) error {
	if id == "" {
		return errors.New("banco sem id")
	}
	if c == nil {
		return fmt.Errorf("banco %q sem construtor", id)
	}
	if _, jaLa := r.construtores[id]; jaLa {
		return fmt.Errorf("banco %q registado duas vezes", id)
	}
	r.construtores[id] = c
	return nil
}

// Construir devolve o banco de um id, com os transportes injectados.
func (r *Registo) Construir(id string, ts Transportes) (Banco, error) {
	c, existe := r.construtores[id]
	if !existe {
		return nil, fmt.Errorf("%w: %q", ErrBancoDesconhecido, id)
	}
	b := c(ts)
	if b == nil {
		return nil, fmt.Errorf("o construtor de %q devolveu nada", id)
	}
	return b, nil
}

// IDs devolve os ids registados, por ordem alfabética.
//
// A ordem é fixa e não a do mapa: a de um mapa é aleatória a cada corrida, e uma
// lista de bancos que troca de ordem sozinha faz o GET /api/v1/bancos parecer
// instável a quem o lê do outro lado.
func (r *Registo) IDs() []string {
	ids := make([]string, 0, len(r.construtores))
	for id := range r.construtores {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// Todos constrói todos os bancos registados, pela ordem de IDs.
func (r *Registo) Todos(ts Transportes) ([]Banco, error) {
	ids := r.IDs()
	todos := make([]Banco, 0, len(ids))
	for _, id := range ids {
		b, err := r.Construir(id, ts)
		if err != nil {
			return nil, err
		}
		todos = append(todos, b)
	}
	return todos, nil
}

// Predefinido é o registo do projecto: os bancos que existem.
//
// A lista é explícita, e não montada por init() com imports cegos: quem lê este
// ficheiro vê os bancos todos, e acrescentar um é uma linha que aparece no diff.
//
// Está vazio porque ainda não há bancos — o CGD (KAN-9), o Novo Banco (KAN-10),
// o Montepio (KAN-11), o Banco CTT (KAN-12), o Santander (KAN-18) e o Crédito
// Agrícola (KAN-19) entram cada um aqui, no passo 9 da lista de
// CONTRATO-BANCO.md §7.
func Predefinido() *Registo {
	return NovoRegisto()
}
