import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

// O build sai direto para dentro do pacote Go, que o embute com embed.FS.
// É isso que mantém a promessa de um binário único: nada de copiar uma pasta
// de assets junto na instalação.
export default defineConfig({
  plugins: [svelte()],
  build: {
    outDir: '../internal/api/dist',
    emptyOutDir: true,
    // Sem sourcemap e com nomes curtos: o alvo é um hardware modesto servindo
    // por Wi-Fi.
    sourcemap: false,
    target: 'es2022',
  },
  server: {
    // No desenvolvimento a API vem do servidor de verdade - assim a tela é
    // construída contra dados reais desde o primeiro minuto.
    proxy: {
      '/api': {
        target: process.env.DWNVR_API || 'http://servidor.local:8080',
        changeOrigin: true,
        ws: true,
      },
    },
  },
});
