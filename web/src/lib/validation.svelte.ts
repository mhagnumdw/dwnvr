// Validation is the server's validation of a person form, field by field,
// following the "punish late, forgive early" pattern:
//
// - a field only shows the result after the person leaves it with something
//   written in it, or after a rejected save: nobody gets red in the middle of
//   typing;
// - from then on, it is checked again while the person types, after TYPING_WAIT
//   without a keystroke, so the red goes away as soon as they fix it.
//
// The ruler is the server's (the `request`), the same as the save's: the screen
// doesn't repeat it, because it can't (the email goes through Go's net/mail,
// and the login and email of an ignored users.json entry aren't in the
// listing).
import type { PersonField, FieldErrors } from './api.ts';

// TYPING_WAIT is the pause in typing, in ms, before checking again a field that
// already shows the result.
export const TYPING_WAIT = 300;

export type FieldState = 'ok' | 'erro' | null;

export class Validation {
  // The fields that can already show the result.
  #mostrados = $state<PersonField[]>([]);
  // The fields the last response covered, and its rejections.
  #conferidos = $state<PersonField[]>([]);
  #recusas = $state<FieldErrors>({});

  // Only the response to the most recent request counts; typing again also
  // discards the one on its way, which is for values that are no longer there.
  #pedido = 0;
  #espera: ReturnType<typeof setTimeout> | undefined;

  readonly #campos: PersonField[];
  readonly #pedir: () => Promise<FieldErrors>;

  // `fields` are the form's, and `request` checks the current values on the
  // server and returns the rejections.
  constructor(campos: PersonField[], request: () => Promise<FieldErrors>) {
    this.#campos = campos;
    this.#pedir = request;
  }

  // The field's result: null while it hasn't been checked.
  condition(field: PersonField): FieldState {
    if (!this.#conferidos.includes(field)) return null;
    return this.#recusas[field] ? 'erro' : 'ok';
  }

  // The field's rejection message, or empty.
  message(field: PersonField): string {
    return this.#conferidos.includes(field) ? (this.#recusas[field] ?? '') : '';
  }

  // The person left the field (or it was filled in for them, like the user
  // suggested from the name). Empty only gets in if the field already showed
  // the result.
  leave(field: PersonField, value: string) {
    if (value.trim() !== '' && !this.#mostrados.includes(field)) {
      this.#mostrados = [...this.#mostrados, field];
    }
    if (this.#mostrados.length > 0) void this.#conferir();
  }

  // The person typed in some field. The ones that already show the result are
  // checked again after the pause.
  typeIn() {
    this.#pedido++;
    clearTimeout(this.#espera);
    if (this.#mostrados.length === 0) return;
    this.#espera = setTimeout(() => void this.#conferir(), TYPING_WAIT);
  }

  // The save was rejected with these rejections: the server checked all the
  // fields, and all of them start showing the result.
  invalid(fieldErrors: FieldErrors) {
    this.stop();
    this.#mostrados = [...this.#campos];
    this.#conferidos = [...this.#campos];
    this.#recusas = fieldErrors;
  }

  // The form closed: nothing else goes to the server.
  stop() {
    this.#pedido++;
    clearTimeout(this.#espera);
  }

  async #conferir() {
    clearTimeout(this.#espera);
    const meu = ++this.#pedido;
    const campos = [...this.#mostrados];
    let fieldErrors: FieldErrors;
    try {
      fieldErrors = await this.#pedir();
    } catch {
      // With no response, the earlier result disappears, since it may be for a
      // value that has already changed (a green the server didn't give). The
      // save checks again.
      if (meu === this.#pedido) this.#conferidos = [];
      return;
    }
    if (meu !== this.#pedido) return;
    this.#conferidos = campos;
    this.#recusas = fieldErrors;
  }
}
