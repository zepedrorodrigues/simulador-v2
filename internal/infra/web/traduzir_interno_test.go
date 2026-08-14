// ⚠️ **O único teste interno do pacote, e a razão é a alcançabilidade.** Todos
// os outros são `package web_test` e entram pelas rotas, que é onde se afirma o
// que a app vê. A guarda do `CapturadoEm` não tem entrada: o `aovivo.Pedir`
// carimba sempre o instante antes de devolver, portanto por HTTP não há forma de
// lá chegar sem primeiro partir o caso de uso. Chamar a tradução directamente é
// o que permite afirmar o que ela emite **enquanto ela continua a não disparar**.
package web

import (
	"testing"

	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// TestUmaOfertaSemInstanteDeCapturaCulpaNosENaoOBanco (KAN-60).
//
// ⚠️ **A guarda está certa e o rótulo estava errado.** Uma oferta com preço e sem
// `CapturadoEm` não é servida — um preço sem data apresenta-se como se fosse de
// agora, e num acerto de cache pode ser de há cinco minutos. Isso mantém-se. O
// que muda é de quem é a culpa: saía `resposta_ilegivel`, que afirma que **o
// banco respondeu e não se percebeu**, e o `CapturadoEm` é carimbado por nós.
//
// ⚠️ **E não é hipotético: já disparou, e mentiu.** A 2026-08-07 o `aovivo`
// nasceu sem o carimbo. O efeito não foi um preço sem data — foi **nenhuma oferta
// servida**, porque a guarda recusa todas, e cada uma saiu a dizer, banco a
// banco, que a resposta do banco estava ilegível. Cinco bancos com fama de
// avariados por um defeito nosso de uma linha.
func TestUmaOfertaSemInstanteDeCapturaCulpaNosENaoOBanco(t *testing.T) {
	t.Parallel()

	tan, err := dominio.TaxaDeTexto("3.250")
	if err != nil {
		t.Fatalf("TaxaDeTexto: %v", err)
	}
	// Uma oferta boa em tudo menos no carimbo: é exactamente o que o caso de uso
	// devolvia quando lhe faltava a última linha.
	semCarimbo := dominio.Oferta{BancoID: "cgd", BancoNome: "CGD", TAN: &tan}

	servida := ofertaDe(semCarimbo)

	if servida.Sucesso {
		t.Fatal("uma oferta sem instante de captura foi servida como boa")
	}
	if servida.Erro == nil {
		t.Fatal("a oferta desceu a falha sem dizer porquê")
	}
	if servida.Erro.Codigo != string(dominio.ErroInterno) {
		t.Errorf("código %q, esperava %q — o %s pode ter respondido perfeitamente; quem não carimbou fomos nós",
			servida.Erro.Codigo, dominio.ErroInterno, semCarimbo.BancoNome)
	}
}
