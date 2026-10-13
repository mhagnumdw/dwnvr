// Aba aberta antes de uma atualização do dwnvr. O servidor passa a servir a
// tela nova, e a aba segue com a antiga até alguém recarregar - às vezes por
// dias, no app instalado do celular. Pior: os chunks à parte da tela antiga
// (Detecções, Usuários, Conta) não existem no binário novo, e abrir uma delas
// pela primeira vez falha.
//
// Quem diz que a tela mudou é o próprio index.html: ele aponta para os
// arquivos de assets/, que têm hash no nome, e é servido com no-cache. Basta
// compará-lo com o que esta aba carregou. Mudança só no Go não troca nenhum
// arquivo e não acende nada; e nada aqui depende do servidor, nem do build.

// De quanto em quanto tempo perguntar com a aba à vista. Voltar para a aba
// também pergunta, e é esse o caso comum: o celular que sai do bolso.
const INTERVALO_MS = 30 * 60 * 1000;

export const telaNova = $state({ disponivel: false });

// Os arquivos de assets/ que o index.html carrega: o script de entrada e o CSS.
// O script muda sempre que qualquer chunk muda, porque leva o nome deles; o CSS
// principal vai à parte, só o index.html o cita.
function assinatura(doc: Document): string {
  return Array.from(
    doc.querySelectorAll('script[type="module"][src], link[rel="stylesheet"][href]'),
    (el) => el.getAttribute('src') ?? el.getAttribute('href') ?? '',
  )
    .filter((url) => url.includes('/assets/'))
    .sort()
    .join(' ');
}

// Lida agora, no carregamento do módulo: o Vite acrescenta ao <head> o CSS de
// cada chunk que baixa, e lida depois a assinatura já não seria a do
// index.html. No dev server não há assets/, a assinatura sai vazia e nada é
// vigiado: lá quem atualiza a tela é o próprio Vite.
const daAba = assinatura(document);

let perguntando = false;

async function perguntar() {
  if (!daAba || telaNova.disponivel || perguntando) return;
  perguntando = true;
  try {
    const r = await fetch(location.pathname, { cache: 'no-store' });
    if (!r.ok) return;
    const html = new DOMParser().parseFromString(await r.text(), 'text/html');
    const doServidor = assinatura(html);
    // Vazia é resposta que não é a tela - um proxy no meio, uma página de
    // erro -, e não uma versão nova.
    if (doServidor && doServidor !== daAba) telaNova.disponivel = true;
  } catch {
    // Sem rede ou servidor fora: fica para a próxima pergunta.
  } finally {
    perguntando = false;
  }
}

// Vigia enquanto a interface estiver aberta. Devolve a função que para.
export function vigiarVersaoDaTela(): () => void {
  const aoVoltar = () => {
    if (!document.hidden) void perguntar();
  };
  // Falha ao baixar um chunk, emitida pelo próprio Vite: é o sintoma de a tela
  // ter mudado, e a pergunta não espera a próxima volta.
  const aoFalharChunk = () => void perguntar();
  const timer = setInterval(aoVoltar, INTERVALO_MS);
  document.addEventListener('visibilitychange', aoVoltar);
  window.addEventListener('vite:preloadError', aoFalharChunk);
  return () => {
    clearInterval(timer);
    document.removeEventListener('visibilitychange', aoVoltar);
    window.removeEventListener('vite:preloadError', aoFalharChunk);
  };
}
