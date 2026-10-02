// Roda antes de todos os testes (o projeto `ambiente` do playwright.config.ts):
// confere que o ambiente do `make e2e-up` está no ar e que a cam_relogio já
// gravou o bastante para os testes de reprodução terem o que tocar.
import { test as setup, expect } from '@playwright/test';
import { esperarTrechos } from '../apoio/api';
import { GRAVANDO } from '../apoio/instancias';

setup('o ambiente está no ar e a cam_relogio já gravou', async ({ playwright }) => {
  const api = await playwright.request.newContext({ baseURL: GRAVANDO });
  const versao = await api.get('/api/version').catch(() => null);
  expect(versao?.ok(), `o dwnvr não responde em ${GRAVANDO}: o ambiente está no ar? (make e2e-up)`).toBe(true);

  // Dois trechos, e não um: a timeline já tem uma emenda, e o player, o que
  // pedir à frente.
  await esperarTrechos(api, 'cam_relogio', 2, 90_000);
  await api.dispose();
});
