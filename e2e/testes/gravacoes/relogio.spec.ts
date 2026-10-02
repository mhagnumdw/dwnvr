// A função de um NVR: o que foi gravado toca, e no instante que a tela diz.
// Tocar não basta; o relógio queimado no vídeo (apoio/relogio.ts) é o que
// prova que o quadro na tela é o daquele horário.
import { test, expect } from '../../apoio/fixtures';
import { diferenca, instanteDaTela, relogioDoVideo } from '../../apoio/relogio';

test('o quadro na tela é o instante que a tela diz', async ({ page, gravando }) => {
  await page.goto(`${gravando.baseURL}/#rec?cam=cam_relogio`);
  const video = page.locator('.stage video');

  await test.step('toca o trecho mais recente, sem aviso de erro', async () => {
    await expect.poll(() => video.evaluate((v: HTMLVideoElement) => v.currentTime)).toBeGreaterThan(1);
    await expect(page.locator('.avisos .bad')).toHaveCount(0);
  });

  await test.step('o relógio queimado no vídeo bate com o relógio da tela', async () => {
    // Pausado, o quadro e o relógio da tela param juntos: a comparação não
    // corre contra a reprodução.
    await page.getByRole('button', { name: 'tocar ou pausar' }).click();
    await expect.poll(() => video.evaluate((v: HTMLVideoElement) => v.paused)).toBe(true);
    await expect(page).toHaveURL(/paused=1/);

    const tela = await instanteDaTela(page);
    const { quadro } = await relogioDoVideo(video);
    const erro = diferenca(tela, quadro);
    test.info().annotations.push({ type: 'tela - vídeo (s)', description: String(erro) });
    expect(Math.abs(erro), `a tela diz ${tela} e o quadro mostra ${quadro}`).toBeLessThanOrEqual(1);
  });
});
