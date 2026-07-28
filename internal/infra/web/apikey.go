package web

import (
	"crypto/subtle"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// A credencial de MÁQUINA do `/api/rate-catalog`.
//
// ⚠️ Isto não é autenticação de utilizador — este serviço não tem contas, não
// tem sessões e não vai ter (§4, «o que não é uma tabela»). É um portão
// partilhado para a fronteira que outra app consome, e uma app sem cabeça não
// faz login interactivo: daí a chave estável.
//
// ⚠️ E viaja num CABEÇALHO, nunca na query string. É a mesma razão por que o
// Logger do chi não entra: uma chave no URL fica nos logs, no histórico do
// browser e nos `Referer`.

// CabecalhoDaChave é onde a chave viaja.
const CabecalhoDaChave = "X-API-Key"

// ComprimentoMinimoDaChave: uma chave curta é força-brutável contra um endpoint
// exposto. 32 caracteres url-safe são ~190 bits de entropia, o que põe isso fora
// de alcance.
//
// ⚠️ É validado no ARRANQUE e não a cada pedido: uma chave curta configurada é
// erro de quem a configurou, e descobri-lo ao primeiro pedido de produção é
// tarde. Ver `ChavesDe`.
const ComprimentoMinimoDaChave = 32

// ChavesDe lê as chaves de uma definição separada por vírgulas.
//
// Devolve erro para uma chave curta de mais — e não a ignora em silêncio, que
// deixaria o serviço a correr com menos portas fechadas do que quem o configurou
// pensa. Uma lista vazia não é erro: é o modo de desenvolvimento, e quem serve
// avisa alto.
func ChavesDe(bruto string) ([]string, error) {
	var chaves []string
	for _, chave := range strings.Split(bruto, ",") {
		chave = strings.TrimSpace(chave)
		if chave == "" {
			continue
		}
		if len(chave) < ComprimentoMinimoDaChave {
			return nil, fmt.Errorf(
				"chave de API com %d caracteres: o mínimo são %d, e uma chave curta é força-brutável "+
					"contra um endpoint exposto", len(chave), ComprimentoMinimoDaChave)
		}
		chaves = append(chaves, chave)
	}
	return chaves, nil
}

// exigirChave é o portão do `/api/rate-catalog`.
//
// ⚠️ Sem chaves configuradas o endpoint fica ABERTO, e isso é deliberado: é o
// modo de desenvolvimento, para quem clona o repositório poder correr sem
// configurar nada. Quem serve avisa alto no arranque — ver `avisoDeChaveAberta`.
// A alternativa, fechar por omissão, fazia toda a gente descobrir a existência
// da chave por um 401 sem explicação.
func (s *Servidor) exigirChave(seguinte http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if len(s.chaves) == 0 {
			seguinte(w, r)
			return
		}
		if !chaveValida(r.Header.Get(CabecalhoDaChave), s.chaves) {
			// ⚠️ A mensagem não diz se a chave estava errada ou ausente, e não
			// nomeia nada da configuração: quem não tem chave não precisa de
			// saber mais, e quem a tem não precisa de nada disto.
			erro(w, http.StatusUnauthorized, "credencial_invalida",
				"Esta fronteira exige uma credencial de máquina válida no cabeçalho X-API-Key.")
			return
		}
		seguinte(w, r)
	}
}

// chaveValida compara em tempo constante.
//
// ⚠️ `subtle.ConstantTimeCompare` e não `==`: a comparação de strings sai no
// primeiro byte diferente, e esse tempo revela quantos bytes do prefixo estão
// certos — o suficiente para reconstruir a chave byte a byte contra um endpoint
// exposto. E percorrem-se TODAS as chaves mesmo depois de uma bater, pela mesma
// razão: parar cedo revela em que posição da lista ela está.
func chaveValida(apresentada string, validas []string) bool {
	if apresentada == "" {
		return false
	}
	bate := false
	for _, valida := range validas {
		if subtle.ConstantTimeCompare([]byte(apresentada), []byte(valida)) == 1 {
			bate = true
		}
	}
	return bate
}

// avisoDeChaveAberta escreve o aviso de que a fronteira está sem portão.
//
// ⚠️ Alto e no arranque, e não uma linha de log entre outras: um serviço a
// publicar a série de mercado sem credencial nenhuma é uma decisão, e tem de ser
// visível a quem o arranca sem a ter tomado.
func avisoDeChaveAberta(saida io.Writer) {
	_, _ = fmt.Fprintln(saida,
		"⚠️  ATENÇÃO: /api/rate-catalog está ABERTO — nenhuma X-API-Key configurada.\n"+
			"    É o modo de desenvolvimento. Em produção, define API_KEYS com pelo menos uma chave\n"+
			fmt.Sprintf("    de %d caracteres ou mais.", ComprimentoMinimoDaChave))
}
