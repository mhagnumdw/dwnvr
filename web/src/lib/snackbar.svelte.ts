// A snackbar: a mensagem curta que aparece embaixo da tela e some sozinha,
// dizendo o que um toque acabou de fazer - às vezes com uma ação para voltar
// atrás, como o "desfazer". O nome é o do Material Design.
//
// Uma de cada vez, como lá: a nova substitui a que estiver à vista. O estado é
// do app inteiro, e o `<Snackbar>` que o desenha fica montado uma vez só, no
// App; quem avisa só chama `avisar`.

// O tempo padrão, para um aviso que só se lê. Com uma ação, quem avisa costuma
// dar mais tempo, o de achar o botão.
const DURACAO_MS = 2500;

export interface Aviso {
  // Um número por aviso: a tela que sai fecha o seu, e não o de outra.
  id: number;
  // O trecho em negrito na frente do texto, como o nome da câmera em
  // "Frente oculta".
  forte?: string;
  texto: string;
  acao?: { rotulo: string; fazer: () => void };
}

export const snackbar = $state<{ atual: Aviso | null }>({ atual: null });

let proximo = 0;
let timer: number | undefined;

export function avisar(aviso: Omit<Aviso, 'id'>, duracaoMs = DURACAO_MS) {
  const id = ++proximo;
  snackbar.atual = { ...aviso, id };
  clearTimeout(timer);
  timer = setTimeout(() => {
    fecharAviso(id);
  }, duracaoMs);
  return id;
}

// Sem `id`, fecha o que estiver à vista.
export function fecharAviso(id?: number) {
  if (!snackbar.atual || (id !== undefined && snackbar.atual.id !== id)) return;
  clearTimeout(timer);
  snackbar.atual = null;
}
