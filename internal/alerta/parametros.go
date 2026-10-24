package alerta

import "time"

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
