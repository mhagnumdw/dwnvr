// O teste-sentinela: o navegador que o Playwright baixou toca o que o dwnvr
// grava? O Chromium que o Playwright usava até a 1.56, e o que ele ainda usa no
// Linux arm64, não toca H.264 em nenhuma das três vias, e aí todo teste de
// vídeo falharia por um motivo que não é do dwnvr. Este falha primeiro, e diz
// por quê. É ele que avisa quando uma atualização do Playwright trocar o
// navegador.
import { test, expect } from '../apoio/fixtures';

test('o navegador toca H.264 por MSE, WebCodecs e WebRTC', async ({ page, gravando, browser }) => {
  test.info().annotations.push({ type: 'navegador', description: browser.version() });

  // Em localhost a página é contexto seguro, e só nele o WebCodecs existe.
  await page.goto(gravando.baseURL);
  const suporte = await page.evaluate(async () => {
    const mse = (codecs: string) => MediaSource.isTypeSupported(`video/mp4; codecs="${codecs}"`);
    let webcodecs = false;
    if (typeof VideoDecoder !== 'undefined') {
      webcodecs = !!(await VideoDecoder.isConfigSupported({ codec: 'avc1.640029' })).supported;
    }
    const webrtc =
      RTCRtpReceiver.getCapabilities('video')?.codecs.some((c) => /h264/i.test(c.mimeType)) ?? false;
    return {
      seguro: isSecureContext,
      mse: mse('avc1.640029'),
      mseComFlac: mse('avc1.640029,flac'),
      webcodecs,
      webrtc,
    };
  });

  const porque =
    'o navegador não tem H.264: provavelmente o Chromium sem codecs proprietários (Linux arm64, ou Playwright anterior à 1.57)';
  expect(suporte.seguro, 'localhost tinha que ser contexto seguro').toBe(true);
  expect(suporte.mse, `MSE: ${porque}`).toBe(true);
  expect(suporte.mseComFlac, `MSE com áudio FLAC: ${porque}`).toBe(true);
  expect(suporte.webcodecs, `WebCodecs: ${porque}`).toBe(true);
  expect(suporte.webrtc, `WebRTC: ${porque}`).toBe(true);
});
