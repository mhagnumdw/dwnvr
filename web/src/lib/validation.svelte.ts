// Conferencia é a validação de um formulário de pessoa pelo servidor, campo a
// campo, no padrão "punir tarde, perdoar cedo":
//
// - um campo só mostra o resultado depois que a pessoa sai dele com algo
//   escrito, ou depois de um salvar recusado: ninguém leva vermelho no meio
//   da digitação;
// - a partir daí, ele é conferido de novo enquanto ela digita, depois de
//   ESPERA_AO_DIGITAR sem tecla, para o vermelho sumir assim que ela corrigir.
//
// A régua é a do servidor (o `pedir`), a mesma do salvar: a tela não a repete,
// porque não tem como (o e-mail passa pelo net/mail do Go, e o login e o
// e-mail de uma entrada ignorada do usuarios.json não vêm na listagem).
import type { PersonField, FieldErrors } from './api.ts';

// ESPERA_AO_DIGITAR é a pausa na digitação, em ms, antes de conferir de novo
// um campo que já mostra o resultado.
export const TYPING_WAIT = 300;

export type FieldState = 'ok' | 'erro' | null;

export class Validation {
  // Os campos que já podem mostrar o resultado.
  #mostrados = $state<PersonField[]>([]);
  // Os campos que a última resposta cobriu, e as recusas dela.
  #conferidos = $state<PersonField[]>([]);
  #recusas = $state<FieldErrors>({});

  // Só vale a resposta do pedido mais recente; digitar de novo também
  // descarta a que está a caminho, que é de valores que já não estão lá.
  #pedido = 0;
  #espera: ReturnType<typeof setTimeout> | undefined;

  readonly #campos: PersonField[];
  readonly #pedir: () => Promise<FieldErrors>;

  // `campos` são os do formulário, e `pedir` confere os valores de agora no
  // servidor e devolve as recusas.
  constructor(campos: PersonField[], request: () => Promise<FieldErrors>) {
    this.#campos = campos;
    this.#pedir = request;
  }

  // O resultado do campo: nulo enquanto ele não foi conferido.
  condition(field: PersonField): FieldState {
    if (!this.#conferidos.includes(field)) return null;
    return this.#recusas[field] ? 'erro' : 'ok';
  }

  // A mensagem da recusa do campo, ou vazio.
  message(field: PersonField): string {
    return this.#conferidos.includes(field) ? (this.#recusas[field] ?? '') : '';
  }

  // A pessoa saiu do campo (ou ele foi preenchido por ela, como o usuário
  // sugerido pelo nome). Vazio só entra se o campo já mostrava o resultado.
  leave(field: PersonField, value: string) {
    if (value.trim() !== '' && !this.#mostrados.includes(field)) {
      this.#mostrados = [...this.#mostrados, field];
    }
    if (this.#mostrados.length > 0) void this.#conferir();
  }

  // A pessoa digitou em algum campo. Os que já mostram o resultado são
  // conferidos de novo depois da pausa.
  typeIn() {
    this.#pedido++;
    clearTimeout(this.#espera);
    if (this.#mostrados.length === 0) return;
    this.#espera = setTimeout(() => void this.#conferir(), TYPING_WAIT);
  }

  // O salvar foi recusado com estas recusas: o servidor conferiu todos os
  // campos, e todos passam a mostrar o resultado.
  invalid(fieldErrors: FieldErrors) {
    this.stop();
    this.#mostrados = [...this.#campos];
    this.#conferidos = [...this.#campos];
    this.#recusas = fieldErrors;
  }

  // O formulário fechou: nada mais vai ao servidor.
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
      // Sem resposta, some o resultado de antes, que pode ser de um valor já
      // trocado (um verde que o servidor não deu). O salvar confere de novo.
      if (meu === this.#pedido) this.#conferidos = [];
      return;
    }
    if (meu !== this.#pedido) return;
    this.#conferidos = campos;
    this.#recusas = fieldErrors;
  }
}
