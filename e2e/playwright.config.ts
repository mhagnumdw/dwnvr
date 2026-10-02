// Os testes ponta a ponta do dwnvr. O ambiente (o go2rtc e as instâncias do
// dwnvr) sobe à parte, com `make e2e-up`; aqui é só o Playwright. O desenho
// inteiro, e o porquê de cada escolha, está em docs/plano-testes-e2e.md.
import { defineConfig, devices, type PlaywrightWorkerOptions } from '@playwright/test';

const CI = !!process.env.CI;

// Vídeo de todo teste, do tamanho da viewport: sem `size`, o Playwright o
// encolhe para caber em 800x800, e o texto da tela fica ilegível no vídeo de
// um desktop. O `show` desenha no próprio vídeo o teste e o passo (no alto, à
// esquerda) e cada ação (à direita), o que deixa entender uma falha da CI sem
// rodar de novo. A fonte da ação acompanha a largura: com os 24 px do padrão,
// no celular ela cobria o passo.
function video(width: number, height: number): PlaywrightWorkerOptions['video'] {
  return {
    mode: 'on',
    size: { width, height },
    show: {
      actions: { position: 'top-right', fontSize: width < 720 ? 13 : 20 },
      test: { level: 'step', position: 'top-left', fontSize: width < 720 ? 11 : 14 },
    },
  };
}

export default defineConfig({
  testDir: './testes',
  // Vídeo leva tempo: um trecho só fecha a cada 10 s.
  timeout: 90_000,
  expect: { timeout: 15_000 },
  // Um worker por instância dwnvr-w0..w2 do compose: o parallelIndex escolhe
  // a dele (apoio/fixtures.ts). Mais workers que instâncias, nunca.
  workers: 3,
  retries: CI ? 1 : 0,
  forbidOnly: CI,
  reporter: CI
    ? [['github'], ['list'], ['html', { open: 'never' }]]
    : [['list'], ['html', { open: 'never' }]],
  use: {
    locale: 'pt-BR',
    // O mesmo fuso dos containers (ambiente/compose.yml): a timeline vira o
    // dia na hora local, e o relógio queimado no vídeo é desenhado nela.
    timezoneId: 'America/Fortaleza',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [
    // Antes de tudo: o ambiente no ar, e a cam_relogio já com o que tocar.
    { name: 'ambiente', testMatch: /ambiente\.setup\.ts/ },
    {
      name: 'desktop',
      dependencies: ['ambiente'],
      use: {
        ...devices['Desktop Chrome'],
        viewport: { width: 1366, height: 768 },
        video: video(1366, 768),
      },
    },
    {
      name: 'celular',
      dependencies: ['ambiente'],
      use: { ...devices['Pixel 7'], video: video(412, 839) },
    },
  ],
});
