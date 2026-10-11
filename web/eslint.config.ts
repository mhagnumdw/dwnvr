// ESLint do web/: bug e má prática no TypeScript e nos componentes Svelte.
// Roda pelo prek (.pre-commit-config.yaml); à mão: `npx eslint .`
//
// As regras de tipo (typeChecked) leem os tipos pelo projectService, que acha o
// tsconfig de cada arquivo sozinho: o tsconfig.app.json para a tela e o
// tsconfig.node.json para as configs.
import { defineConfig, globalIgnores } from 'eslint/config';
import js from '@eslint/js';
import svelte from 'eslint-plugin-svelte';
import ts from 'typescript-eslint';
import globals from 'globals';

export default defineConfig([
  // Código do go2rtc, copiado como veio: não é nosso para corrigir. A
  // declaração dos tipos dele, ao lado, é nossa, e passa pelo lint.
  globalIgnores(['src/vendor/*.js']),

  js.configs.recommended,
  ts.configs.strictTypeChecked,
  ts.configs.stylisticTypeChecked,
  svelte.configs.recommended,

  {
    languageOptions: {
      globals: globals.browser,
      parserOptions: { projectService: true, tsconfigRootDir: import.meta.dirname },
    },
    rules: {
      // Tirar campos de um objeto com `...resto` não é variável esquecida,
      // como os campos internos que o Cameras.svelte tira antes de salvar.
      '@typescript-eslint/no-unused-vars': ['error', { ignoreRestSiblings: true }],
      // Número dentro de template string é o uso comum (`${n} câmeras`), e
      // não esconde nada: o que a regra pega de verdade, objeto e nulo virando
      // texto, continua barrado.
      '@typescript-eslint/restrict-template-expressions': ['error', { allowNumber: true }],
      // O Svelte 5 apara o espaço no começo e no fim de um bloco: em
      // `{#if x}{' · '}...`, o mustache é o que segura o espaço. Aqui todo
      // mustache de string é esse caso, e o --fix da regra colaria o texto.
      'svelte/no-useless-mustaches': 'off',
    },
  },

  {
    files: ['**/*.svelte'],
    rules: {
      // Componente sem `lang="ts"` seria JavaScript sem conferência de tipo.
      'svelte/block-lang': ['error', { script: 'ts' }],
      // Falso positivo em todo `{@render x()}` de snippet do próprio
      // componente: o parser do Svelte transforma o `{#snippet}` numa função
      // que devolve void, e o `{@render}` numa expressão solta. O `<script>`
      // do componente perde a regra junto; o .ts continua com ela.
      '@typescript-eslint/no-confusing-void-expression': 'off',
    },
  },

  // O <script> do .svelte e o .svelte.ts passam pelo parser do TypeScript.
  {
    files: ['**/*.svelte', '**/*.svelte.ts'],
    languageOptions: {
      parserOptions: { parser: ts.parser, extraFileExtensions: ['.svelte'] },
    },
  },

  // Só as configs rodam no Node.
  {
    files: ['vite.config.ts', 'eslint.config.ts', 'prettier.config.ts'],
    languageOptions: { globals: globals.node },
  },
]);
