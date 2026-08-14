import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

// Para onde o proxy do dev manda /api. O default serve o caso de rodar o dwnvr
// na própria máquina; apontar para outra instalação é só exportar DWNVR_API.
const API = process.env.DWNVR_API || 'http://localhost:8080';

// O build sai direto para dentro do pacote Go, que o embute com embed.FS.
// É isso que mantém a promessa de um binário único: nada de copiar uma pasta
// de assets junto na instalação.
export default defineConfig(({ command }) => {
  // Sem esta linha, apontar para o servidor errado é indistinguível de tudo
  // certo: a tela abre, mostra dados plausíveis e o bug investigado é o de
  // outra instalação.
  if (command === 'serve') console.log(`  API do dwnvr: ${API}\n`);

  return {
    plugins: [svelte()],
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
  };
});
