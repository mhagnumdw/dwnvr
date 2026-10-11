// Prettier do web/: a formatação do TypeScript, dos componentes Svelte, do CSS
// e do index.html. Roda pelo prek (.pre-commit-config.yaml); à mão:
// `npx prettier --write .`
//
// Indentação, fim de linha e newline no fim ele lê do .editorconfig da raiz.
// Aqui fica só o que o código já fazia: aspas simples e linha de até 100.
import type { Config } from 'prettier';

export default {
  // O caminho resolvido a partir daqui, e não só o nome: o Prettier procura o
  // plugin pela pasta de onde foi chamado, e o hook o chama da raiz.
  plugins: [import.meta.resolve('prettier-plugin-svelte')],
  singleQuote: true,
  printWidth: 100,
} satisfies Config;
