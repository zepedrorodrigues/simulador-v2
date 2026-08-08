// O caminho de «esta versão é demasiado antiga».
//
// ⚠️ **Existe agora porque depois é impossível.** Com uma app nas lojas não se
// controla quem actualiza (`docs/APP.md` §5): quando houver versões antigas no
// terreno, acrescentar este caminho não serve para nada — as versões que
// precisavam de o entender já foram publicadas sem ele. Custa pouco hoje, e hoje
// é a única altura em que se pode fazer.
//
// ⚠️ **Não substitui a regra de só mudar por acrescento** (`API.md` §4). É o
// travão de emergência para o dia em que ela não chegar — servir uma app velha
// que interpreta mal o que se lhe manda é pior do que lhe dizer que pare.

package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// CabecalhoDaVersao é onde a app diz qual é.
//
// ⚠️ Um cabeçalho e não um campo do corpo, e a escolha decide o âmbito: vale
// para **todas** as rotas, incluindo o `GET /api/v1/bancos`, e não obriga a
// mexer em schema nenhum. Um campo no corpo só chegaria às rotas que têm corpo,
// que são precisamente as que a app já não conseguiria montar se estivesse velha
// de mais.
const CabecalhoDaVersao = "X-App-Versao"

// Versao é uma versão da app, em três números.
type Versao struct{ Maior, Menor, Correccao int }

// VersaoDe lê "1.2.3".
//
// ⚠️ **Os três números lêem-se como NÚMEROS, e não é detalhe.** Comparadas como
// texto, "1.10.0" fica **antes** de "1.9.0" — e o modo de falhar é o pior que há
// para esta funcionalidade: no dia em que a versão menor passar de 9 para 10, o
// servidor começa a mandar parar exactamente as apps mais recentes.
//
// ⚠️ Um sufixo depois do terceiro número (`-rc1`, `+build`) é **recusado** em vez
// de ignorado. Aceitá-lo obrigava a decidir se `1.2.3-rc1` é anterior ou
// posterior a `1.2.3`, que é uma pergunta com resposta no semver e sem resposta
// nenhuma aqui — nunca se publicou uma pré-lançamento e não se vai inventar a
// ordem antes de haver uma.
func VersaoDe(s string) (Versao, error) {
	partes := strings.Split(strings.TrimSpace(s), ".")
	if len(partes) != 3 {
		return Versao{}, fmt.Errorf("uma versão são três números separados por pontos, e veio %q", s)
	}
	var v [3]int
	for i, parte := range partes {
		n, err := strconv.Atoi(parte)
		if err != nil || n < 0 {
			return Versao{}, fmt.Errorf("%q não é um número de versão", parte)
		}
		v[i] = n
	}
	return Versao{Maior: v[0], Menor: v[1], Correccao: v[2]}, nil
}

func (v Versao) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Maior, v.Menor, v.Correccao)
}

// Anterior diz se v é anterior a outra.
func (v Versao) Anterior(outra Versao) bool {
	if v.Maior != outra.Maior {
		return v.Maior < outra.Maior
	}
	if v.Menor != outra.Menor {
		return v.Menor < outra.Menor
	}
	return v.Correccao < outra.Correccao
}

// VersaoMinimaDe lê a definição APP_VERSAO_MINIMA.
//
// ⚠️ **Vazio desliga a verificação, e é o valor de partida.** Ligar um mínimo
// sem ter uma versão publicada no terreno não protege nada e arrisca mandar
// parar a única app que existe. Quem o ligar está a tomar uma decisão sobre
// clientes que já não controla, e essa decisão escreve-se na configuração.
func VersaoMinimaDe(s string) (*Versao, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	v, err := VersaoDe(s)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// exigirVersao recusa uma app anterior ao mínimo configurado.
//
// ⚠️ **Um pedido SEM o cabeçalho passa, e é decisão.** O alvo web, o `curl` de
// quem experimenta a API e a própria app de hoje não o mandam — exigi-lo era, em
// si, uma mudança que parte o `/api/v1`, que é exactamente o que este caminho
// existe para evitar. O que se recusa é uma versão **conhecida e velha**, nunca
// a ausência de informação.
//
// ⚠️ **Uma versão ilegível também passa.** Não se sabe que é velha, e tratar o
// que não se percebe como demasiado antigo transforma um defeito de escrita do
// cliente num bloqueio total — para o corrigir teria de sair uma versão nova, que
// é o que o bloqueio impede de instalar. Falha aberto, como o tecto.
//
// ⚠️ **426 e não 400.** O `Upgrade Required` existe para isto: o pedido está
// bem formado e o problema é o cliente. Um 400 dizia à app que o pedido dela
// estava errado e mandava-a corrigir o corpo, que é a acção errada.
func (s *Servidor) exigirVersao(seguinte http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		minima := s.versaoMinima
		if minima == nil {
			seguinte.ServeHTTP(w, r)
			return
		}

		bruto := r.Header.Get(CabecalhoDaVersao)
		if bruto == "" {
			seguinte.ServeHTTP(w, r)
			return
		}

		versao, err := VersaoDe(bruto)
		if err != nil {
			seguinte.ServeHTTP(w, r)
			return
		}

		if versao.Anterior(*minima) {
			erro(w, http.StatusUpgradeRequired, "versao_demasiado_antiga",
				fmt.Sprintf("Esta versão da aplicação (%s) já não é suportada. "+
					"Actualiza para a %s ou mais recente para continuar.", versao, minima))
			return
		}
		seguinte.ServeHTTP(w, r)
	})
}

// ComVersaoMinima liga a verificação.
func (s *Servidor) ComVersaoMinima(v *Versao) *Servidor {
	s.versaoMinima = v
	return s
}
