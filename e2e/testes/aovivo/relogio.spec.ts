// O Ao vivo mostra a câmera de agora, e não uma de alguns segundos atrás: o
// atraso é medido pelo relógio queimado no vídeo contra o do navegador, que
// roda na mesma máquina que o go2rtc.
import { test, expect } from '../../apoio/fixtures';
import { diferenca, relogioDoVideo } from '../../apoio/relogio';

test('o Ao vivo mostra a câmera por WebRTC, com pouco atraso', async ({ page, gravando }) => {
  await page.goto(`${gravando.baseURL}/#live?cams=cam_relogio&view=1`);
  const video = page.locator('video-stream video');

  await test.step('o player negocia WebRTC e toca', async () => {
    // O player do go2rtc escreve o modo negociado no canto: RTC é WebRTC.
    await expect(page.locator('video-stream .mode')).toHaveText('RTC', { timeout: 30_000 });
    await expect.poll(() => video.evaluate((v: HTMLVideoElement) => v.currentTime)).toBeGreaterThan(1);
  });

  await test.step('o atraso é de poucos segundos', async () => {
    const { quadro, agora } = await relogioDoVideo(video);
    const atraso = diferenca(agora, quadro);
    test.info().annotations.push({ type: 'atraso do ao vivo (s)', description: String(atraso) });
    // A faixa tem resolução de 1 s: um atraso real de 0,9 s pode ler 1 ou 2.
    expect(atraso, 'o quadro é do futuro: relógios diferentes?').toBeGreaterThanOrEqual(0);
    expect(atraso, 'o Ao vivo está atrasado').toBeLessThanOrEqual(3);
  });
});
