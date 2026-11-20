// Prettier for web/: the formatting of the TypeScript, the Svelte components,
// the CSS and index.html. It runs through prek (.pre-commit-config.yaml); by
// hand: `npx prettier --write .`
//
// Indentation, line endings and the final newline come from the .editorconfig
// at the root. Only what the code already did stays here: single quotes and
// lines up to 100.
import type { Config } from 'prettier';

export default {
  // The path resolved from here, and not just the name: Prettier looks for the
  // plugin from the folder it was called from, and the hook calls it from the
  // root.
  plugins: [import.meta.resolve('prettier-plugin-svelte')],
  singleQuote: true,
  printWidth: 100,
} satisfies Config;
