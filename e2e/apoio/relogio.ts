// O relógio queimado no vídeo da cam_relogio (e2e/ambiente/cameras/relogio.sh),
// lido de volta pelo teste. É o oráculo de tudo que toca: o quadro na tela tem
// que ser o instante que a tela diz.
import { expect, type Locator, type Page } from '@playwright/test';

/** A faixa guarda o epoch em segundos módulo 2^20, que se repete a cada ~12 dias. */
export const MODULO = 2 ** 20;

/** O relógio do quadro e o do navegador, lidos na mesma chamada. */
export type Leitura = { quadro: number; agora: number };

/**
 * Lê a faixa de 20 blocos do quadro que o <video> mostra agora, e o relógio do
 * navegador no mesmo instante. Nulo enquanto o vídeo não tem quadro.
 *
 * O <video> das Gravações é alimentado por MSE com bytes da mesma origem, e o
 * do Ao vivo por WebRTC: nos dois o canvas não fica marcado, e o pixel pode
 * ser lido de volta (é o que a captura de imagem da interface também faz).
 */
export async function lerRelogio(video: Locator): Promise<Leitura | null> {
  return video.evaluate((v: HTMLVideoElement, modulo: number) => {
    if (!v.videoWidth || !v.videoHeight) return null;
    const c = document.createElement('canvas');
    c.width = v.videoWidth;
    c.height = v.videoHeight;
    const g = c.getContext('2d', { willReadFrequently: true });
    if (!g) return null;
    g.drawImage(v, 0, 0);
    // A faixa foi desenhada num quadro de 640 px de largura.
    const escala = v.videoWidth / 640;
    let quadro = 0;
    for (let i = 0; i < 20; i++) {
      const x = Math.round((32 * i + 16) * escala);
      const [r, gr, b] = g.getImageData(x, Math.round(12 * escala), 1, 1).data;
      if ((r + gr + b) / 3 > 127) quadro += 2 ** i;
    }
    return { quadro, agora: Math.floor(Date.now() / 1000) % modulo };
  }, MODULO);
}

/** Espera o vídeo ter quadro e devolve a leitura. */
export async function relogioDoVideo(video: Locator): Promise<Leitura> {
  let leitura: Leitura | null = null;
  await expect
    .poll(async () => (leitura = await lerRelogio(video)), { message: 'o vídeo ainda não tem quadro' })
    .not.toBeNull();
  return leitura!;
}

/** `a - b` em segundos, pelo caminho mais curto na volta do módulo. */
export function diferenca(a: number, b: number): number {
  return ((((a - b) % MODULO) + MODULO + MODULO / 2) % MODULO) - MODULO / 2;
}

/**
 * O instante que a tela de Gravações diz, em segundos módulo 2^20: o dia vem
 * do `day=` da URL e a hora do relógio do player. A conta da data é feita no
 * navegador, que está no fuso do teste, e não no Node, que está no da máquina.
 */
export async function instanteDaTela(page: Page): Promise<number> {
  await expect(page).toHaveURL(/[#&?]day=\d{4}-\d{2}-\d{2}/);
  const hora = await page.getByLabel('horário; digite outro para ir até ele').inputValue();
  expect(hora, 'o relógio do player ainda não tem horário').toMatch(/^\d{2}:\d{2}:\d{2}$/);
  const hash = new URL(page.url()).hash;
  const dia = new URLSearchParams(hash.slice(hash.indexOf('?') + 1)).get('day');
  const epoch = await page.evaluate(
    ([d, h]) => Math.floor(new Date(`${d}T${h}`).getTime() / 1000),
    [dia, hora] as const,
  );
  return epoch % MODULO;
}
