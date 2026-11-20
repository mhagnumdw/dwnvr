import { defineConfig } from 'vite';
import type { Plugin } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

// Where the dev proxy sends /api. The default serves the case of running dwnvr
// on the same machine; pointing at another installation is just exporting
// DWNVR_API.
// eslint-disable-next-line @typescript-eslint/prefer-nullish-coalescing -- an empty DWNVR_API= also means "I didn't point anywhere"
const API = process.env['DWNVR_API'] || 'http://localhost:8080';

// Without this line, pointing at the wrong server is indistinguishable from
// everything being right: the screen opens, shows plausible data, and the bug
// being investigated is from another installation. It goes in the servers'
// hooks, and not loose in the config, because svelte-check also reads this
// file, and it starts no server.
const notifyAPI = () => {
  console.log(`  API do dwnvr: ${API}\n`);
};
const showAPI: Plugin = {
  name: 'dwnvr-mostra-api',
  configureServer: notifyAPI,
  configurePreviewServer: notifyAPI,
};

// The build goes straight into the Go package, which embeds it with embed.FS.
// That is what keeps the promise of a single binary: no copying an assets
// folder along at install time.
export default defineConfig({
  // No svelte.config: Svelte 5 understands the TypeScript in `<script
  // lang="ts">` without a preprocessor, and the vitePreprocess that was here
  // didn't change a byte of the build.
  plugins: [svelte({ configFile: false }), showAPI],
  build: {
    outDir: '../internal/api/dist',
    emptyOutDir: true,
    // No sourcemap and with short names: the target is modest hardware.
    sourcemap: false,
    target: 'es2022',
  },
  server: {
    // In development the API comes from the real server - so the screen is
    // built against real data from the first minute.
    proxy: {
      '/api': {
        target: API,
        changeOrigin: true,
        ws: true,
      },
    },
  },
});
