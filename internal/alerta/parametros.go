package alerta

import (
	"time"

	"github.com/mhagnumdw/dwnvr/internal/detect"
)

// Este arquivo guarda todos os números do pacote, cada um com o motivo ao
// lado.

// AlertasNaEspera é quantos alertas esperam a vez de sair pelos meios. Cada
// um leva o JPEG do quadro olhado, de dezenas de KB a 480 px, e 32 ficam
// abaixo de 2 MB. Com um meio travado, cada envio leva até o PrazoDoEnvio, e
// a goroutine de envio dá conta de 120 por hora. Na instalação de teste,
// `pessoa` nas 9 câmeras, na imagem inteira, deu ~629 alertas por dia (medido
// em 22/10/2026), uns 26 por hora: a espera só enche numa rajada, e o que não
// cabe é descartado e contado.
const AlertasNaEspera = 32

// PrazoDoEnvio é quanto um envio pode levar antes de contar como falha. Um
// e-mail com o quadro anexo leva poucos segundos mesmo num uplink doméstico
// lento; 30 s separa o lento do travado, sem que um meio travado segure os
// outros por muito tempo.
const PrazoDoEnvio = 30 * time.Second

// CaixaFolgaPx é quanto a caixa desenhada no quadro do alerta cresce para
// cada lado além da caixa do detector de objetos, o mesmo CAIXA_FOLGA_PX da
// tela (web/src/lib/caixas.ts): o traço fica por dentro da caixa e, sem
// folga, cobriria a beirada do objeto.
const CaixaFolgaPx = 5

// CaixaTracoPx é a espessura do traço da caixa, a da tela.
const CaixaTracoPx = 2

// AreaTracoPx é a espessura do contorno da área. A tela usa 1,5 px; no
// quadro, que só tem pixel inteiro, 1 px some na compressão do JPEG.
const AreaTracoPx = 2

// AreaTracoLigadoPx e AreaTracoDesligadoPx são o tracejado do contorno da
// área, o setLineDash([6, 4]) da tela (web/src/lib/area.ts): o tracejado é o
// que diz "área", e não objeto.
const (
	AreaTracoLigadoPx    = 6
	AreaTracoDesligadoPx = 4
)

// QualidadeDoQuadro é a do JPEG do quadro desenhado: a mesma do quadro olhado.
// Acima dela não volta detalhe que já se perdeu; medido em 27/10/2026, em 4
// quadros reais de 480×270, 85 deu ~25% a mais (de 24-43 KB para 30-54 KB)
// sem diferença visível no traço nem na imagem, ampliada 4 vezes.
const QualidadeDoQuadro = detect.QualidadeDoQuadro
