// O `test` dos testes ponta a ponta: o do Playwright, mais as instâncias do
// dwnvr prontas para usar. Os testes importam daqui, e não do
// @playwright/test.
//
//   gravando  só leitura: a instância que grava a cam_relogio desde a subida
//   limpa     a instância do worker, zerada antes do teste: para quem muda estado
import { test as base, expect, type APIRequestContext } from '@playwright/test';
import { zerar } from './api';
import { DO_WORKER, GRAVANDO } from './instancias';

/** Uma instância do dwnvr: onde abrir a tela, e a API dela para preparar estado. */
export type Instancia = { baseURL: string; api: APIRequestContext };

type DoTeste = { limpa: Instancia };
type DoWorker = { gravando: Instancia; doWorker: Instancia };

export const test = base.extend<DoTeste, DoWorker>({
  gravando: [
    async ({ playwright }, use) => {
      const api = await playwright.request.newContext({ baseURL: GRAVANDO });
      await use({ baseURL: GRAVANDO, api });
      await api.dispose();
    },
    { scope: 'worker' },
  ],

  // Um dwnvr por worker. O parallelIndex vai de 0 a workers-1 e continua o
  // mesmo quando um worker morre e outro assume o lugar: dois workers nunca
  // mexem no mesmo cameras.json.
  doWorker: [
    async ({ playwright }, use, workerInfo) => {
      const baseURL = DO_WORKER(workerInfo.parallelIndex);
      const api = await playwright.request.newContext({ baseURL });
      await use({ baseURL, api });
      await api.dispose();
    },
    { scope: 'worker' },
  ],

  // O que muda estado começa do zero, e não depende de outro teste ter
  // limpado antes.
  limpa: async ({ doWorker }, use) => {
    await zerar(doWorker.api);
    await use(doWorker);
  },
});

export { expect };
