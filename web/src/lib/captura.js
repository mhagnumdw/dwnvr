// Captura do quadro mostrado no player, entregue como arquivo ao usuário.
//
// Não há nada disso no servidor de propósito: o dwnvr nunca decodifica vídeo, e
// aqui não precisa mesmo. O <video> das gravações é alimentado por MSE com
// bytes buscados da mesma origem, então o canvas não fica marcado e o quadro já
// decodificado pela placa pode ser lido de volta em pixels.

// baixarQuadro recorta o quadro corrente do <video> e o entrega ao usuário:
// folha de compartilhamento onde houver, download onde não houver. Joga se não
// der para capturar, para quem chamou avisar na tela.
export async function baixarQuadro(video, nome) {
  // Antes do primeiro quadro decodificado o <video> ainda não tem tamanho, e
  // capturar aí produziria uma imagem 0x0 sem erro nenhum.
  if (!video?.videoWidth || !video.videoHeight) {
    throw new Error('ainda não há quadro para capturar');
  }

  // O tamanho é o NATIVO da câmera, não o da tela: o `max-height: 56dvh` do CSS
  // encolhe o vídeo para a timeline caber junto, e uma captura no tamanho
  // exibido devolveria menos pixels do que a gravação tem.
  const canvas = document.createElement('canvas');
  canvas.width = video.videoWidth;
  canvas.height = video.videoHeight;
  canvas.getContext('2d').drawImage(video, 0, 0);

  // JPEG porque o quadro veio de um H.264/H.265 já comprimido: o PNG guardaria
  // fielmente os artefatos da compressão anterior por várias vezes o tamanho.
  const blob = await new Promise((resolve) =>
    canvas.toBlob(resolve, 'image/jpeg', 0.92),
  );
  if (!blob) throw new Error('o navegador não conseguiu gerar a imagem');

  const file = new File([blob], nome, { type: 'image/jpeg' });

  // No celular o download cai numa pasta que ninguém abre; a folha do sistema
  // leva a imagem para a galeria ou para a conversa em que ela é útil.
  //
  // Onde ela não existir, o download é o caminho - e ela não existe em mais
  // lugares do que parece: a Web Share API é [SecureContext], então num dwnvr
  // servido em http:// nem `navigator.canShare` chega a ser definido, e o
  // celular baixa igual ao desktop. Não é defeito, é o endereço; ver
  // docs/TODO/TODO_compartilhar-exige-contexto-seguro.md.
  if (navigator.canShare?.({ files: [file] })) {
    try {
      await navigator.share({ files: [file] });
      return;
    } catch (e) {
      // Fechar a folha sem escolher nada é decisão do usuário, não falha: a
      // captura acabou aqui e a tela não deve acusar erro.
      if (e.name === 'AbortError') return;
      // Qualquer outra coisa - permissão negada, tipo recusado - ainda tem o
      // download como saída.
    }
  }

  const url = URL.createObjectURL(blob);
  try {
    const a = document.createElement('a');
    a.href = url;
    a.download = nome;
    a.click();
  } finally {
    // Sem isto o blob fica na memória até a aba fechar, e num celular olhando
    // câmera de 4 MP isso pesa.
    URL.revokeObjectURL(url);
  }
}
