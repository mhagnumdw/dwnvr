// ESLint for web/: bugs and bad practices in the TypeScript and in the Svelte
// components. It runs through prek (.pre-commit-config.yaml); by hand: `npx
// eslint .`
//
// The type-aware rules (typeChecked) read types through the projectService,
// which finds each file's tsconfig on its own: tsconfig.app.json for the screen
// and tsconfig.node.json for the configs.
import { defineConfig, globalIgnores } from 'eslint/config';
import js from '@eslint/js';
import svelte from 'eslint-plugin-svelte';
import ts from 'typescript-eslint';
import globals from 'globals';

export default defineConfig([
  // go2rtc code, copied as is: it is not ours to fix. The type declaration next
  // to it is ours, and it goes through the lint.
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
      // Dropping fields from an object with `...rest` is not a forgotten
      // variable, like the internal fields Cameras.svelte drops before saving.
      '@typescript-eslint/no-unused-vars': ['error', { ignoreRestSiblings: true }],
      // A number inside a template string is the common case (`${n} câmeras`)
      // and hides nothing: what the rule really catches, objects and null
      // turning into text, is still blocked.
      '@typescript-eslint/restrict-template-expressions': ['error', { allowNumber: true }],
      // Svelte 5 trims whitespace at the start and end of a block: in `{#if
      // x}{' · '}...`, the mustache is what holds the space. Here every string
      // mustache is that case, and the rule's --fix would glue the text
      // together.
      'svelte/no-useless-mustaches': 'off',
    },
  },

  {
    files: ['**/*.svelte'],
    rules: {
      // A component without `lang="ts"` would be JavaScript with no type
      // checking.
      'svelte/block-lang': ['error', { script: 'ts' }],
      // False positive on every `{@render x()}` of the component's own snippet:
      // the Svelte parser turns `{#snippet}` into a function that returns void,
      // and `{@render}` into a bare expression. The component's `<script>`
      // loses the rule along with it; the .ts files keep it.
      '@typescript-eslint/no-confusing-void-expression': 'off',
    },
  },

  // The <script> of a .svelte file and the .svelte.ts files go through the
  // TypeScript parser.
  {
    files: ['**/*.svelte', '**/*.svelte.ts'],
    languageOptions: {
      parserOptions: { parser: ts.parser, extraFileExtensions: ['.svelte'] },
    },
  },

  // Only the configs run on Node.
  {
    files: ['vite.config.ts', 'eslint.config.ts', 'prettier.config.ts'],
    languageOptions: { globals: globals.node },
  },
]);
