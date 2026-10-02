// ESLint do e2e/: bug e má prática nos testes ponta a ponta. Roda pelo prek
// (.pre-commit-config.yaml); à mão, dentro de e2e/: `npx eslint .`
//
// O plugin do Playwright cobra como erro, e não como aviso, duas regras do
// docs/plano-testes-e2e.md: nada de espera fixa (`waitForTimeout`), e nada de
// teste pulado ou marcado para depois para a suíte ficar verde.
import { defineConfig, globalIgnores } from 'eslint/config';
import js from '@eslint/js';
import tseslint from 'typescript-eslint';
import playwright from 'eslint-plugin-playwright';

export default defineConfig([
  globalIgnores(['test-results/', 'playwright-report/', 'blob-report/', '.estado/']),

  js.configs.recommended,
  tseslint.configs.recommended,

  {
    files: ['testes/**/*.ts'],
    extends: [playwright.configs['flat/recommended']],
    settings: {
      // O projeto de preparação chama o `test` de `setup`.
      playwright: { globalAliases: { test: ['setup'] } },
    },
    rules: {
      'playwright/no-wait-for-timeout': 'error',
      'playwright/no-skipped-test': 'error',
      'playwright/no-focused-test': 'error',
    },
  },
]);
