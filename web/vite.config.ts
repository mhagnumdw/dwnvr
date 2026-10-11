import { defineConfig } from 'vite';
import type { Plugin } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

// Para onde o proxy do dev manda /api. O default serve o caso de rodar o dwnvr
// na própria máquina; apontar para outra instalação é só exportar DWNVR_API.
// eslint-disable-next-line @typescript-eslint/prefer-nullish-coalescing -- DWNVR_API= vazio também é "não apontei"
const API = process.env['DWNVR_API'] || 'http://localhost:8080';

// Sem esta linha, apontar para o servidor errado é indistinguível de tudo
// certo: a tela abre, mostra dados plausíveis e o bug investigado é o de
// outra instalação. Vai nos hooks dos servidores, e não solta na config,
// porque o svelte-check também lê este arquivo, e não sobe servidor nenhum.
const avisaAPI = () => {
  console.log(`  API do dwnvr: ${API}\n`);
};
const mostraAPI: Plugin = {
  name: 'dwnvr-mostra-api',
  configureServer: avisaAPI,
  configurePreviewServer: avisaAPI,
};

// O build sai direto para dentro do pacote Go, que o embute com embed.FS.
// É isso que mantém a promessa de um binário único: nada de copiar uma pasta
// de assets junto na instalação.
export default defineConfig({
  // Sem svelte.config: o Svelte 5 entende o TypeScript do <script lang="ts">
  // sem preprocess, e o vitePreprocess que havia não mudava um byte do build.
  plugins: [svelte({ configFile: false }), mostraAPI],
  build: {
    outDir: '../internal/api/dist',
    emptyOutDir: true,
    // Sem sourcemap e com nomes curtos: o alvo é um hardware modesto.
    sourcemap: false,
    target: 'es2022',
  },
  server: {
    // No desenvolvimento a API vem do servidor de verdade - assim a tela é
    // construída contra dados reais desde o primeiro minuto.
    proxy: {
      '/api': {
        target: API,
        changeOrigin: true,
        ws: true,
      },
    },
  },
});
