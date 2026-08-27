package bancos

import (
	"errors"
	"fmt"
	"slices"

	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/bancoctt"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/bpi"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/cgd"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/montepio"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/novobanco"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/santander"
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

	// Browser é o transporte para bancos que precisam de um browser headless
	// para conduzir formulários (BPI, Bankinter).
	//
	// ⚠️ Nulo desliga: bancos que precisam de browser ficam indisponíveis.
	Browser transporte.BrowserComoCliente

	// Catalogos é onde ficam os parâmetros que os bancos publicam (KAN-36).
	//
	// ⚠️ **Nulo é válido e desliga**, como a `Lotacao` nula: o banco vai à fonte
	// de cada vez, que é o que fazia antes de isto existir. É o que deixa os
	// testes de cada banco correr sem base de dados.
	Catalogos Catalogos
}

// Construtor monta um banco a partir dos transportes injectados.
//
// Recebe os transportes todos e não só o que lhe serve, para a assinatura ser
// uma só: é o que permite ao registo ser uma tabela em vez de um switch.
type Construtor func(Transportes) Banco

// ErrBancoDesconhecido: pediu-se um id que não está registado.
var ErrBancoDesconhecido = errors.New("banco desconhecido")

// ErrBancoIndisponivel: o id está registado, mas o construtor não o monta com
// os transportes que recebeu — é o BPI sem transporte de browser. Para quem
// pede é indistinguível de um banco que não existe: não sai no `Todos`, logo
// não sai no `GET /api/v1/bancos`.
var ErrBancoIndisponivel = errors.New("banco indisponível com estes transportes")

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
// que desapareceria do varrimento era um banco inteiro — logo, uma coluna a
// faltar na grelha de preço e um banco sem resposta para dar. Não uma linha de
// log.
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
		return nil, fmt.Errorf("%w: o construtor de %q devolveu nada", ErrBancoIndisponivel, id)
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
//
// ⚠️ Bancos que ficam indisponíveis sem o seu transporte (BPI sem browser)
// são ignorados — o registo funciona, mas a lista é mais curta. Quem precisar
// de saber se um banco específico está disponível deve usar Construir.
func (r *Registo) Todos(ts Transportes) ([]Banco, error) {
	ids := r.IDs()
	todos := make([]Banco, 0, len(ids))
	for _, id := range ids {
		b, err := r.Construir(id, ts)
		if err != nil {
			// Bancos que precisam de browser ficam indisponíveis sem ele.
			// O construtor devolve nil, e o registo trata isso como
			// indisponível — não como erro fatal.
			continue
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
// Tem a CGD (KAN-9), o Novo Banco (KAN-10), o Montepio (KAN-11), o Banco CTT
// (KAN-12) e o Santander (KAN-18). O Crédito Agrícola (KAN-19) entra aqui, no
// passo 9 da lista de CONTRATO-BANCO.md §7.
//
// ⚠️ É aqui que se converte a assinatura: cada banco recebe o transporte de que
// precisa e devolve o seu tipo, e não o bancos.Banco. Se um banco importasse
// este pacote para os nomear, este não podia importar o dele — e o registo
// deixava de poder ser uma tabela.
//
// ⚠️ O Montepio é o primeiro a receber o ComSessao, e não o HTTP: o POST dele tem
// de sair do mesmo cliente que fez o arranque, senão vai sem os cookies e o
// gateway responde 410.
func Predefinido() *Registo {
	r := NovoRegisto()
	registarOuExplodir(r, bancoctt.IDBanco, func(ts Transportes) Banco { return bancoctt.Novo(ts.HTTP) })
	// ⚠️ A CGD e o Novo Banco recebem os catálogos: a CGD publica os períodos
	// de taxa fixa dentro de uma página de 73,5 KiB (KAN-36), e o Novo Banco os
	// limites do /configuracoes (KAN-37).
	registarOuExplodir(r, cgd.BancoID, func(ts Transportes) Banco {
		return cgd.Novo(ts.HTTP, ts.Catalogos)
	})
	registarOuExplodir(r, montepio.BancoID, func(ts Transportes) Banco { return montepio.Novo(ts.ComSessao) })
	// ⚠️ O Novo Banco também recebe os catálogos: o /configuracoes publica os
	// limites que se verificam antes do /calculo (KAN-37).
	registarOuExplodir(r, novobanco.BancoID, func(ts Transportes) Banco {
		return novobanco.Novo(ts.HTTP, ts.Catalogos)
	})
	registarOuExplodir(r, santander.IDBanco, func(ts Transportes) Banco { return santander.Novo(ts.HTTP) })
	// ⚠️ O BPI precisa do browser para conduzir o formulário OutSystems.
	// Sem ele, o construtor devolve nil e o registo funciona — mas o banco
	// fica indisponível quando se tenta simulá-lo.
	registarOuExplodir(r, bpi.IDBanco, func(ts Transportes) Banco {
		if ts.Browser == nil {
			return nil
		}
		return bpi.Novo(ts.Browser)
	})
	return r
}

// registarOuExplodir é para o registo do projecto, e só para esse: um id
// repetido ou um construtor em falta aqui é erro de programação, não condição
// de execução, e o que ele produziria era um banco a desaparecer do varrimento
// em silêncio — uma coluna a menos na grelha de preço. Rebenta no arranque, que
// é quando ainda se corrige.
func registarOuExplodir(r *Registo, id string, c Construtor) {
	if err := r.Registar(id, c); err != nil {
		panic("registo predefinido dos bancos: " + err.Error())
	}
}
