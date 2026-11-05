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
import type { CampoDaPessoa, Recusas } from './api.ts';

// ESPERA_AO_DIGITAR é a pausa na digitação, em ms, antes de conferir de novo
// um campo que já mostra o resultado.
export const ESPERA_AO_DIGITAR = 300;

export type EstadoDoCampo = 'ok' | 'erro' | null;

export class Conferencia {
  // Os campos que já podem mostrar o resultado.
  #mostrados = $state<CampoDaPessoa[]>([]);
  // Os campos que a última resposta cobriu, e as recusas dela.
  #conferidos = $state<CampoDaPessoa[]>([]);
  #recusas = $state<Recusas>({});

  // Só vale a resposta do pedido mais recente; digitar de novo também
  // descarta a que está a caminho, que é de valores que já não estão lá.
  #pedido = 0;
  #espera: ReturnType<typeof setTimeout> | undefined;

  readonly #campos: CampoDaPessoa[];
  readonly #pedir: () => Promise<Recusas>;

  // `campos` são os do formulário, e `pedir` confere os valores de agora no
  // servidor e devolve as recusas.
  constructor(campos: CampoDaPessoa[], pedir: () => Promise<Recusas>) {
    this.#campos = campos;
    this.#pedir = pedir;
  }

  // O resultado do campo: nulo enquanto ele não foi conferido.
  estado(campo: CampoDaPessoa): EstadoDoCampo {
    if (!this.#conferidos.includes(campo)) return null;
    return this.#recusas[campo] ? 'erro' : 'ok';
  }

  // A mensagem da recusa do campo, ou vazio.
  mensagem(campo: CampoDaPessoa): string {
    return this.#conferidos.includes(campo) ? (this.#recusas[campo] ?? '') : '';
  }

  // A pessoa saiu do campo (ou ele foi preenchido por ela, como o usuário
  // sugerido pelo nome). Vazio só entra se o campo já mostrava o resultado.
  sair(campo: CampoDaPessoa, valor: string) {
    if (valor.trim() !== '' && !this.#mostrados.includes(campo)) {
      this.#mostrados = [...this.#mostrados, campo];
    }
    if (this.#mostrados.length > 0) void this.#conferir();
  }

  // A pessoa digitou em algum campo. Os que já mostram o resultado são
  // conferidos de novo depois da pausa.
  digitar() {
    this.#pedido++;
    clearTimeout(this.#espera);
    if (this.#mostrados.length === 0) return;
    this.#espera = setTimeout(() => void this.#conferir(), ESPERA_AO_DIGITAR);
  }

  // O salvar foi recusado com estas recusas: o servidor conferiu todos os
  // campos, e todos passam a mostrar o resultado.
  recusado(recusas: Recusas) {
    this.parar();
    this.#mostrados = [...this.#campos];
    this.#conferidos = [...this.#campos];
    this.#recusas = recusas;
  }

  // O formulário fechou: nada mais vai ao servidor.
  parar() {
    this.#pedido++;
    clearTimeout(this.#espera);
  }

  async #conferir() {
    clearTimeout(this.#espera);
    const meu = ++this.#pedido;
    const campos = [...this.#mostrados];
    let recusas: Recusas;
    try {
      recusas = await this.#pedir();
    } catch {
      // Sem resposta, fica o resultado de antes: o salvar confere de novo.
      return;
    }
    if (meu !== this.#pedido) return;
    this.#conferidos = campos;
    this.#recusas = recusas;
  }
}
