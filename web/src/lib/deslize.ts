// O deslize para o lado que troca de câmera: na tela cheia do Ao vivo e no
// vídeo das Gravações. Os números são os mesmos nas duas telas, para o dedo
// não precisar aprender dois gestos.

// Quanto dura a câmera saindo ou entrando de lado, e a que volta para o lugar
// num deslize curto demais.
export const TROCA_MS = 220;
// O deslize troca de câmera ao passar de DESLIZE_PX, ou de DESLIZE_FRACAO da
// largura se ela for estreita. Antes de EIXO_PX o dedo ainda não disse se vai
// para o lado ou para cima.
export const DESLIZE_PX = 80;
export const DESLIZE_FRACAO = 0.2;
export const EIXO_PX = 8;

// A partir de quantos px, numa área desta largura, o deslize troca de câmera.
export function limiteDoDeslize(largura: number) {
  return Math.min(DESLIZE_PX, largura * DESLIZE_FRACAO);
}
