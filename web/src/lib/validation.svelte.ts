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

export type FieldState = 'ok' | 'error' | null;

export class Validation {
  // The fields that can already show the result.
  #shown = $state<PersonField[]>([]);
  // The fields the last response covered, and its rejections.
  #checked = $state<PersonField[]>([]);
  #rejections = $state<FieldErrors>({});

  // Only the response to the most recent request counts; typing again also
  // discards the one on its way, which is for values that are no longer there.
  #sequence = 0;
  #wait: ReturnType<typeof setTimeout> | undefined;

  readonly #fields: PersonField[];
  readonly #ask: () => Promise<FieldErrors>;

  // `fields` are the form's, and `request` checks the current values on the
  // server and returns the rejections.
  constructor(fields: PersonField[], request: () => Promise<FieldErrors>) {
    this.#fields = fields;
    this.#ask = request;
  }

  // The field's result: null while it hasn't been checked.
  condition(field: PersonField): FieldState {
    if (!this.#checked.includes(field)) return null;
    return this.#rejections[field] ? 'error' : 'ok';
  }

  // The field's rejection message, or empty.
  message(field: PersonField): string {
    return this.#checked.includes(field) ? (this.#rejections[field] ?? '') : '';
  }

  // The person left the field (or it was filled in for them, like the user
  // suggested from the name). Empty only gets in if the field already showed
  // the result.
  leave(field: PersonField, value: string) {
    if (value.trim() !== '' && !this.#shown.includes(field)) {
      this.#shown = [...this.#shown, field];
    }
    if (this.#shown.length > 0) void this.#check();
  }

  // The person typed in some field. The ones that already show the result are
  // checked again after the pause.
  typeIn() {
    this.#sequence++;
    clearTimeout(this.#wait);
    if (this.#shown.length === 0) return;
    this.#wait = setTimeout(() => void this.#check(), TYPING_WAIT);
  }

  // The save was rejected with these rejections: the server checked all the
  // fields, and all of them start showing the result.
  invalid(fieldErrors: FieldErrors) {
    this.stop();
    this.#shown = [...this.#fields];
    this.#checked = [...this.#fields];
    this.#rejections = fieldErrors;
  }

  // The form closed: nothing else goes to the server.
  stop() {
    this.#sequence++;
    clearTimeout(this.#wait);
  }

  async #check() {
    clearTimeout(this.#wait);
    const mine = ++this.#sequence;
    const fields = [...this.#shown];
    let fieldErrors: FieldErrors;
    try {
      fieldErrors = await this.#ask();
    } catch {
      // With no response, the earlier result disappears, since it may be for a
      // value that has already changed (a green the server didn't give). The
      // save checks again.
      if (mine === this.#sequence) this.#checked = [];
      return;
    }
    if (mine !== this.#sequence) return;
    this.#checked = fields;
    this.#rejections = fieldErrors;
  }
}
