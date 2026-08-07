package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// versaoDoResumo entra no que se resume, e sobe sempre que a forma abaixo muda.
//
// ⚠️ **Sem ela, mudar a serialização fazia linhas antigas responderem a pedidos
// novos** — dois pedidos diferentes com a mesma chave, servidos um pelo outro.
// É o único erro grave que esta cache pode causar, e é silencioso: a resposta
// tem o aspecto certo. Com a versão lá dentro, uma mudança de formato produz
// chaves novas e as linhas velhas expiram sozinhas.
const versaoDoResumo = "v1"

// resumoDoPedido é a forma canónica de que sai a chave.
//
// ⚠️ **Campos exportados e em ordem fixa, de propósito.** O `encoding/json`
// escreve os campos de uma struct pela ordem de declaração, o que torna esta
// serialização determinística sem ordenar nada em runtime. Um `map` aqui seria
// ordenado pelo próprio JSON; não há nenhum, e não deve passar a haver.
type resumoDoPedido struct {
	Versao      string
	BancoID     string
	ValorImovel string
	Montante    string
	PrazoAnos   int
	TipoTaxa    string

	// PeriodoFixoAnos nulo é «sem preferência», e é diferente de zero: o banco
	// aplica o período mais longo que tiver. Vai como ponteiro para que os dois
	// casos não colapsem na mesma chave.
	PeriodoFixoAnos *int

	Indexante       string
	Titulares       []titularResumido
	Finalidade      string
	Localizacao     string
	GarantiaPublica bool
	JaCliente       bool

	// ⚠️ **Pela ordem em que vieram, e não ordenados.** Tratar os produtos como
	// conjunto era decidir por conta própria que o banco os considera iguais em
	// qualquer ordem — uma normalização, e as normalizações são o que matou a
	// KAN-15. Um acerto a menos custa um pedido; um acerto a mais custa um preço
	// errado.
	Produtos []string
}

type titularResumido struct {
	DataNascimento   string
	RendimentoMensal string
}

// Chave deriva a chave da cache a partir do banco e do pedido.
//
// ⚠️ **É aqui que o pedido em claro pára.** Um pedido traz data de nascimento e
// rendimento de alguém, e a §4 do ARQUITETURA.md recusa gravar isso. O que segue
// para a base é este resumo e mais nada — determinístico, para que dois clientes
// com o mesmo pedido partilhem o acerto, e sem volta, para que a linha não diga
// quem eles são.
//
// ⚠️ **Limite conhecido, e escrito porque é real:** um SHA-256 de um registo de
// baixa entropia confirma-se por tentativa. Quem já tiver a base pode testar um
// candidato (montante, data, rendimento) e ver se a chave bate — o resumo esconde
// o pedido de quem lê a tabela, não de quem o adivinha. Um HMAC com segredo
// fechava isso, e traz gestão de chaves que este repositório ainda não tem. O que
// limita a exposição hoje é a validade ser curta e a linha ser apagada.
//
// Os números vão pelo `String()` do Dinheiro, que é a forma da biblioteca decimal
// e corta zeros à direita — logo 100 e 100,00 dão a mesma chave, que é o que se
// quer: são o mesmo dinheiro.
func Chave(bancoID string, p dominio.Pedido) (string, error) {
	r := resumoDoPedido{
		Versao:          versaoDoResumo,
		BancoID:         bancoID,
		ValorImovel:     p.ValorImovel.String(),
		Montante:        p.Montante.String(),
		PrazoAnos:       p.PrazoAnos,
		TipoTaxa:        string(p.TipoTaxa),
		PeriodoFixoAnos: p.PeriodoFixoAnos,
		Indexante:       string(p.Indexante),
		Titulares:       make([]titularResumido, 0, len(p.Titulares)),
		Finalidade:      string(p.Finalidade),
		Localizacao:     string(p.Localizacao),
		GarantiaPublica: p.GarantiaPublica,
		JaCliente:       p.JaCliente,
		Produtos:        p.Produtos,
	}
	for _, t := range p.Titulares {
		r.Titulares = append(r.Titulares, titularResumido{
			DataNascimento:   t.DataNascimento.String(),
			RendimentoMensal: t.RendimentoMensal.String(),
		})
	}

	bruto, err := json.Marshal(r)
	if err != nil {
		return "", fmt.Errorf("resumir o pedido ao %q: %w", bancoID, err)
	}
	soma := sha256.Sum256(bruto)
	return hex.EncodeToString(soma[:]), nil
}
