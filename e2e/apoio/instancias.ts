// Onde cada instância do dwnvr do ambiente responde. As portas são as que o
// e2e/ambiente/compose.yml publica: mudou lá, muda aqui.

/** A que grava a cam_relogio desde a subida. Os testes só a leem. */
export const GRAVANDO = 'http://localhost:18080';

/**
 * A instância do worker de índice `i` (o parallelIndex do Playwright, de 0 a
 * workers-1). É a única que o teste daquele worker pode mudar.
 */
export const DO_WORKER = (i: number) => `http://localhost:${18081 + i}`;
