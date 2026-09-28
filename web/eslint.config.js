// ESLint do web/: bug e má prática no JavaScript e nos componentes Svelte.
// Roda pelo prek (.pre-commit-config.yaml); à mão: `npx eslint .`
import { defineConfig, globalIgnores } from 'eslint/config';
import js from '@eslint/js';
import svelte from 'eslint-plugin-svelte';
import globals from 'globals';
import svelteConfig from './svelte.config.js';

export default defineConfig([
  // Código do go2rtc, copiado como veio: não é nosso para corrigir.
  globalIgnores(['src/vendor/']),

  js.configs.recommended,
  svelte.configs.recommended,

  {
    languageOptions: { globals: globals.browser },
    rules: {
      // Tirar campos de um objeto com `...resto` não é variável esquecida,
      // como os campos internos que o Cameras.svelte tira antes de salvar.
      'no-unused-vars': ['error', { ignoreRestSiblings: true }],
      // O Svelte 5 apara o espaço no começo e no fim de um bloco: em
      // `{#if x}{' · '}...`, o mustache é o que segura o espaço. Aqui todo
      // mustache de string é esse caso, e o --fix da regra colaria o texto.
      'svelte/no-useless-mustaches': 'off',
    },
  },

  // O parser do Svelte lê o preprocess do svelte.config.js.
  {
    files: ['**/*.svelte', '**/*.svelte.js'],
    languageOptions: { parserOptions: { svelteConfig } },
  },

  // Só a config do Vite roda no Node.
  {
    files: ['vite.config.js'],
    languageOptions: { globals: globals.node },
  },
]);
