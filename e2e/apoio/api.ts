// Preparar e conferir estado pela API pública do dwnvr (docs/api.md), a mesma
// que a tela usa. Nada aqui existe só para o teste: zerar uma instância é
// remover cada câmera com as gravações e apagar cada órfã, como uma pessoa
// faria clicando.
import { expect, type APIRequestContext } from '@playwright/test';

/** A câmera como o POST /api/cameras recebe; o que faltar fica no padrão. */
export type Camera = {
  id: string;
  name?: string;
  enabled?: boolean;
  segmentSeconds?: number;
  quotaMB?: number;
  audio?: 'none' | 'flac' | 'aac';
};

type Listagem = {
  cameras?: { id: string }[];
  orphans?: { id: string }[];
};

type Dia = { day: string; count: number };

async function listagem(api: APIRequestContext): Promise<Listagem> {
  const r = await api.get('/api/cameras');
  expect(r.ok(), `GET /api/cameras: ${r.status()}`).toBe(true);
  return r.json();
}

/** Deixa a instância sem câmera, sem gravação e sem órfã. */
export async function zerar(api: APIRequestContext) {
  const antes = await listagem(api);
  for (const c of antes.cameras ?? []) {
    const r = await api.delete(`/api/cameras?id=${encodeURIComponent(c.id)}&recordings=1`);
    expect(r.ok(), `remover ${c.id}: ${await r.text()}`).toBe(true);
  }
  for (const o of antes.orphans ?? []) {
    const r = await api.delete(`/api/rec?cam=${encodeURIComponent(o.id)}`);
    expect(r.ok(), `apagar a órfã ${o.id}: ${await r.text()}`).toBe(true);
  }
  const depois = await listagem(api);
  expect(depois.cameras ?? [], 'câmeras depois de zerar').toEqual([]);
  expect(depois.orphans ?? [], 'órfãs depois de zerar').toEqual([]);
}

/** Cadastra pela API, como o "salvar" do formulário faz. */
export async function cadastrar(api: APIRequestContext, cam: Camera) {
  const r = await api.post('/api/cameras', { data: { name: cam.id, enabled: true, ...cam } });
  expect(r.ok(), `cadastrar ${cam.id}: ${await r.text()}`).toBe(true);
}

/** Quantos trechos da câmera já estão no índice, somando os dias. */
export async function trechos(api: APIRequestContext, cam: string): Promise<number> {
  const r = await api.get(`/api/rec/days?cam=${encodeURIComponent(cam)}`);
  if (!r.ok()) return 0;
  const { days = [] } = (await r.json()) as { days?: Dia[] };
  return days.reduce((soma, d) => soma + d.count, 0);
}

/**
 * Espera a câmera ter `n` trechos no índice. Um trecho só entra quando fecha:
 * com segmento de 10 s, o primeiro leva ~12 s desde que a câmera conecta.
 */
export async function esperarTrechos(api: APIRequestContext, cam: string, n: number, timeout = 60_000) {
  await expect
    .poll(() => trechos(api, cam), { timeout, message: `${cam} com ${n} trecho(s) no índice` })
    .toBeGreaterThanOrEqual(n);
}
