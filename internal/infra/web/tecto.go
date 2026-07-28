package web

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// O tecto de pedidos por IP, e o bug de produção que ele existe para não repetir.
//
// ⚠️ **No v1, atrás de um proxy noutro contentor, o `X-Forwarded-For` não era
// aceite e o IP do cliente passava a ser o do proxy — igual para toda a gente.**
// O tecto de «10 pedidos por 10 minutos por IP» virava o tecto do SITE INTEIRO,
// e o primeiro visitante trancava todos os outros.
//
// ⚠️ E a correcção **não é confiar em toda a gente**. Aceitar o
// `X-Forwarded-For` de quem quer que seja deixa qualquer cliente escolher o seu
// IP num cabeçalho e contornar o tecto de vez — que é pior do que não ter tecto,
// porque parece que se tem. A correcção é uma lista **explícita** de proxies de
// confiança, com default **vazio**.
//
// ⚠️ A razão do tecto mudou a 2026-07-25 e ele fica. Existia porque cada
// submissão custava dezenas de segundos de scraping a partir do nosso IP; hoje
// custa uma consulta a Postgres. Mantém-se porque um endpoint público sem tecto
// é um convite — mas é uma medida contra abuso banal, e não o travão que protege
// a relação com os bancos. Esse é o do varrimento (§7.2).

// Contador é o que sabe quantos pedidos uma chave já fez na janela.
//
// ⚠️ Declara-se aqui e implementa-se na infra, sobre a tabela `limites`. O
// contador vive na BASE e não no processo pela mesma razão que o travão do
// varrimento: várias PaaS definem a concorrência sozinhas, e dois processos com
// contadores em memória não se travam um ao outro.
type Contador interface {
	ContarPedido(ctx context.Context, chave string, janela time.Duration, agora time.Time) (int, error)
}

// Tecto é a política de limite.
type Tecto struct {
	// Pedidos é quantos se aceitam por janela. Zero desliga o tecto.
	Pedidos int

	// Janela é o período fixo. Zero vale JanelaOmissao.
	Janela time.Duration

	// ProxiesDeConfianca são as redes de onde um `X-Forwarded-For` é aceite.
	//
	// ⚠️ **Vazio por omissão, e é a decisão central.** Um serviço que arranca
	// sem saber quem está à frente dele não deve acreditar em cabeçalhos: usa o
	// endereço da ligação, que ninguém pode forjar. Quem põe um proxy à frente
	// declara-o aqui.
	ProxiesDeConfianca []*net.IPNet
}

// Os valores de omissão. ⚠️ São generosos de propósito: isto trava abuso banal,
// e um tecto apertado sobre um endpoint que custa uma consulta só serve para
// irritar quem carrega duas vezes no botão.
const (
	PedidosOmissao = 60
	JanelaOmissao  = time.Minute
)

// RedesDe lê a lista de proxies de confiança de uma definição separada por
// vírgulas: `10.0.0.0/8, 192.168.1.5`.
//
// ⚠️ Um endereço solto vale como /32 (ou /128), e não como «esta rede». Um
// engano aqui abre o cabeçalho a uma rede inteira, e é o género de engano que
// não dá erro nenhum.
func RedesDe(bruto string) ([]*net.IPNet, error) {
	var redes []*net.IPNet
	for _, parte := range strings.Split(bruto, ",") {
		parte = strings.TrimSpace(parte)
		if parte == "" {
			continue
		}
		if strings.Contains(parte, "/") {
			_, rede, err := net.ParseCIDR(parte)
			if err != nil {
				return nil, fmt.Errorf("proxy de confiança inválido %q: %w", parte, err)
			}
			redes = append(redes, rede)
			continue
		}
		ip := net.ParseIP(parte)
		if ip == nil {
			return nil, fmt.Errorf("proxy de confiança inválido %q: não é um IP nem uma rede", parte)
		}
		bits := 32
		if ip.To4() == nil {
			bits = 128
		}
		redes = append(redes, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
	}
	return redes, nil
}

// clienteDe devolve o IP a que o tecto se aplica.
//
// ⚠️ **O endereço da ligação manda, e só cede a um proxy declarado.** É a linha
// inteira desta issue: sem proxy de confiança, um `X-Forwarded-For` que chegue
// directamente à app é IGNORADO — quem o enviou está a tentar escolher o seu
// próprio identificador.
//
// Com proxy de confiança, lê-se o **último** endereço do `X-Forwarded-For` que
// não seja de um proxy nosso, percorrendo da direita para a esquerda: a direita
// é o que o nosso proxy acrescentou e em que podemos confiar; a esquerda é o que
// o cliente possa ter escrito antes de chegar cá.
func (t Tecto) clienteDe(r *http.Request) string {
	ligacao := enderecoDe(r.RemoteAddr)
	if !t.confiaEm(ligacao) {
		return ligacao
	}

	encaminhados := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(encaminhados) - 1; i >= 0; i-- {
		candidato := strings.TrimSpace(encaminhados[i])
		if candidato == "" {
			continue
		}
		if net.ParseIP(candidato) == nil {
			// Um endereço que não é um endereço: quem o pôs está a brincar, e a
			// cadeia deixa de valer a partir daqui.
			return ligacao
		}
		if !t.confiaEm(candidato) {
			return candidato
		}
	}
	return ligacao
}

func (t Tecto) confiaEm(ip string) bool {
	endereco := net.ParseIP(ip)
	if endereco == nil {
		return false
	}
	for _, rede := range t.ProxiesDeConfianca {
		if rede.Contains(endereco) {
			return true
		}
	}
	return false
}

// enderecoDe tira a porta do RemoteAddr.
func enderecoDe(remoto string) string {
	if ip, _, err := net.SplitHostPort(remoto); err == nil {
		return ip
	}
	return remoto
}

// limitar é o middleware do tecto.
//
// ⚠️ Falha ABERTO quando a base não responde: um contador indisponível não pode
// tornar o serviço inacessível. É uma escolha, e a alternativa — recusar tudo —
// transformava uma avaria da base numa negação de serviço completa. O tecto
// protege contra abuso banal; a base em baixo já é um problema maior e visível.
func (s *Servidor) limitar(seguinte http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.contador == nil || s.tecto.Pedidos < 1 {
			seguinte.ServeHTTP(w, r)
			return
		}

		// ⚠️ **O preflight não conta, e sem isto o CORS partia o tecto ao meio.**
		// Um browser manda um `OPTIONS` antes de cada `POST /api/v1/comparacoes`
		// — o `Content-Type: application/json` obriga-o —, portanto cada
		// comparação feita da app custaria **dois** do tecto, enquanto a mesma
		// comparação feita por `curl` custa um. O tecto passava a medir o cliente
		// e não o uso.
		//
		// E não abre buraco: um preflight não toca na base nem calcula nada,
		// responde 204 com cabeçalhos, e só é reconhecido como tal se trouxer o
		// `Access-Control-Request-Method` (ver `ehPreflight`). Quem quisesse usar
		// isto para escapar ao tecto teria de mandar pedidos que não fazem
		// trabalho nenhum.
		if ehPreflight(r) {
			seguinte.ServeHTTP(w, r)
			return
		}

		janela := s.tecto.Janela
		if janela <= 0 {
			janela = JanelaOmissao
		}

		cliente := s.tecto.clienteDe(r)
		contagem, err := s.contador.ContarPedido(r.Context(), "http:ip:"+cliente, janela, s.agora())
		if err != nil {
			seguinte.ServeHTTP(w, r)
			return
		}

		if contagem > s.tecto.Pedidos {
			// ⚠️ O Retry-After é a janela inteira e não o que falta dela: dizer
			// exactamente quando reabre convida a bater na porta ao segundo, e a
			// janela é fixa — o que falta dela não é informação que valha uma
			// consulta a mais.
			w.Header().Set("Retry-After", strconv.Itoa(int(janela.Seconds())))
			erro(w, http.StatusTooManyRequests, "tecto_excedido",
				fmt.Sprintf("Demasiados pedidos. Tenta daqui a %s.", janela))
			return
		}
		seguinte.ServeHTTP(w, r)
	})
}
