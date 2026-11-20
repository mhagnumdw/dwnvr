// The snackbar: the short message that appears at the bottom of the screen and
// disappears on its own, saying what a tap just did - sometimes with an action
// to go back, like "desfazer". The name is Material Design's.
//
// One at a time, as there: the new one replaces whichever is visible. The state
// belongs to the whole app, and the `<Snackbar>` that draws it is mounted only
// once, in App; whoever notifies just calls `notify`.

// The default duration, for a notice that is only read. With an action, whoever
// notifies usually gives more time, enough to find the button.
const DURATION_MS = 2500;

export interface Notice {
  // One number per notice: the screen that leaves closes its own, and not
  // another's.
  id: number;
  // The bold part in front of the text, like the camera name in "Frente
  // oculta".
  forte?: string;
  texto: string;
  action?: { label: string; fazer: () => void };
}

export const snackbar = $state<{ current: Notice | null }>({ current: null });

let next = 0;
let timer: number | undefined;

export function notify(notice: Omit<Notice, 'id'>, durationMs = DURATION_MS) {
  const id = ++next;
  snackbar.current = { ...notice, id };
  clearTimeout(timer);
  timer = setTimeout(() => {
    closeNotice(id);
  }, durationMs);
  return id;
}

// Without `id`, it closes whichever is visible.
export function closeNotice(id?: number) {
  if (!snackbar.current || (id !== undefined && snackbar.current.id !== id)) return;
  clearTimeout(timer);
  snackbar.current = null;
}
