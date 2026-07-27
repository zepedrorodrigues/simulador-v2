package dominio_test

import (
	"strings"
	"testing"

	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

func requisitosValidos() dominio.Requisitos {
	return dominio.Requisitos{
		BancoID:   "novobanco",
		BancoNome: "Novo Banco",
		Custo:     dominio.CustoBarato,
		Inputs: []dominio.Input{
			{Campo: dominio.CampoValorImovel, Usa: true},
			{
				Campo: dominio.CampoProfissao, Usa: false,
				Nota: "O simulador tem o campo, mas a API ignora-o: vai um valor neutro.",
			},
		},
		PeriodosFixos:     []int{2, 3, 4, 5, 10, 15, 20, 25, 30},
		PeriodosFixosModo: dominio.ModoLista,
		EuriborOpcoes:     []dominio.Indexante{dominio.Euribor3M, dominio.Euribor6M, dominio.Euribor12M},
		PrazoMin:          1,
		PrazoMax:          40,
		IdadeMaximaFim:    75,
	}
}

func TestRequisitosValidar(t *testing.T) {
	casos := []struct {
		nome      string
		muda      func(r *dominio.Requisitos)
		queroErro string
	}{
		{nome: "os requisitos de referência passam"},
		{
			nome:      "campo fora do vocabulário",
			muda:      func(r *dominio.Requisitos) { r.Inputs[0].Campo = "first_occupation_id" },
			queroErro: "vocabulário",
		},
		{
			nome: "campo declarado duas vezes",
			muda: func(r *dominio.Requisitos) {
				r.Inputs = append(r.Inputs, dominio.Input{Campo: dominio.CampoValorImovel})
			},
			queroErro: "duas vezes",
		},
		{
			nome:      "sem custo não é servível",
			muda:      func(r *dominio.Requisitos) { r.Custo = "" },
			queroErro: "custo",
		},
		{
			nome:      "modo de períodos fixos inventado",
			muda:      func(r *dominio.Requisitos) { r.PeriodosFixosModo = "do-céu" },
			queroErro: "modo",
		},
		{
			nome:      "indexante que não existe nas opções",
			muda:      func(r *dominio.Requisitos) { r.EuriborOpcoes = []dominio.Indexante{"1m"} },
			queroErro: "indexante",
		},
		{
			nome:      "limites de prazo trocados",
			muda:      func(r *dominio.Requisitos) { r.PrazoMin, r.PrazoMax = 40, 5 },
			queroErro: "prazo",
		},
		{
			nome:      "idade máxima de fim impossível",
			muda:      func(r *dominio.Requisitos) { r.IdadeMaximaFim = 17 },
			queroErro: "idade",
		},
		{
			nome:      "sem identificação do banco",
			muda:      func(r *dominio.Requisitos) { r.BancoNome = "" },
			queroErro: "identificação",
		},
		// ⚠️ Um produto sem o prefixo do banco é um produto que a selecção de um
		// pedido nunca lhe entregaria: o Pedido.ProdutosDoBanco reparte pelo
		// prefixo, e sem ele o banco declara uma bonificação que ninguém lhe
		// consegue pedir.
		{
			nome: "produto sem o prefixo do banco",
			muda: func(r *dominio.Requisitos) {
				r.Produtos = []dominio.Produto{{ID: "primeiro_banco", Rotulo: "Primeiro Banco"}}
			},
			queroErro: "prefixado",
		},
		{
			nome: "produto com o prefixo de outro banco",
			muda: func(r *dominio.Requisitos) {
				r.Produtos = []dominio.Produto{{ID: "cgd:packs", Rotulo: "Packs"}}
			},
			queroErro: "prefixado",
		},
		{
			nome: "produto declarado duas vezes",
			muda: func(r *dominio.Requisitos) {
				r.Produtos = []dominio.Produto{
					{ID: "novobanco:protecao", Rotulo: "Proteção"},
					{ID: "novobanco:protecao", Rotulo: "Proteção"},
				}
			},
			queroErro: "duas vezes",
		},
		{
			nome: "produtos bem prefixados passam",
			muda: func(r *dominio.Requisitos) {
				r.Produtos = []dominio.Produto{
					{ID: "novobanco:primeiro_banco", Rotulo: "Primeiro Banco"},
					{ID: "novobanco:protecao", Rotulo: "Proteção"},
				}
			},
		},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			r := requisitosValidos()
			if c.muda != nil {
				c.muda(&r)
			}

			err := r.Validar()

			if c.queroErro == "" {
				if err != nil {
					t.Fatalf("erro inesperado: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("passou e devia ter sido recusado")
			}
			if !strings.Contains(err.Error(), c.queroErro) {
				t.Errorf("erro = %q, queria que nomeasse %q", err, c.queroErro)
			}
		})
	}
}

func TestVocabularioCanonicoEFechado(t *testing.T) {
	if !dominio.CampoCanonicoConhecido(dominio.CampoValorImovel) {
		t.Error("valor_imovel devia estar no vocabulário")
	}
	// ⚠️ A profissão está no vocabulário mas o Pedido não a transporta, e é
	// deliberado: vários simuladores pedem-na e várias APIs ignoram-na, e qual
	// é qual apura-se banco a banco, por captura.
	if !dominio.CampoCanonicoConhecido(dominio.CampoProfissao) {
		t.Error("profissao devia estar no vocabulário")
	}
	if dominio.CampoCanonicoConhecido("first_occupation_id") {
		t.Error("a chave do v1 não devia ser reconhecida")
	}
}

func TestInputsCanonicosDevolveCopia(t *testing.T) {
	roubado := dominio.InputsCanonicos()
	roubado[0].Rotulo = "apagado"

	if dominio.InputsCanonicos()[0].Rotulo == "apagado" {
		t.Error("mexer na cópia mexeu na tabela — o vocabulário não se altera de fora")
	}
}

func TestTodosOsInputsCanonicosTemRotuloETipo(t *testing.T) {
	for _, i := range dominio.InputsCanonicos() {
		if i.Campo == "" || i.Rotulo == "" || i.Tipo == "" {
			t.Errorf("entrada incompleta no vocabulário: %+v", i)
		}
	}
}
