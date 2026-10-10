// A foto da Minha conta, reduzida aqui no navegador: a original, de qualquer
// tamanho, não sai do aparelho. Vai ao servidor só o quadrado do centro, em
// JPEG, de até `lado` pixels. O canvas não copia o EXIF, e com ele fica para
// trás o GPS da foto.
//
// Os números (`lado`, `tetoBytes`, `qualidade`) vêm do GET /api/conta: moram no
// servidor, que recusa o que passar deles.

// Abaixo desta qualidade a foto já fica feia; se nem assim couber no teto, é
// melhor dizer que não deu do que mandar uma foto estragada. Na prática não
// acontece: ruído puro, o pior caso, coube na qualidade 80.
const QUALIDADE_MINIMA = 50;

function canvas(lado) {
  const c = document.createElement('canvas');
  c.width = c.height = lado;
  return c;
}

function jpeg(c, qualidade) {
  return new Promise((ok) => c.toBlob(ok, 'image/jpeg', qualidade / 100));
}

// reduzirFoto devolve o JPEG pronto para o PUT, ou lança um Error com a
// mensagem para a tela.
export async function reduzirFoto(arquivo, { lado, tetoBytes, qualidade }) {
  let img;
  try {
    // Já vem girada pela orientação do EXIF: é o padrão do createImageBitmap.
    img = await createImageBitmap(arquivo);
  } catch {
    throw new Error('este aparelho não abriu a imagem escolhida');
  }

  // O quadrado do centro, sem tela de recorte.
  const q = Math.min(img.width, img.height);
  let fonte = img;
  let x = (img.width - q) / 2;
  let y = (img.height - q) / 2;
  let l = q;

  // Em passos de metade: reduzir de uma vez só, de 4000 para 256, deixa o
  // resultado serrilhado no canvas de todo navegador. O primeiro passo também
  // é o que larga o resto da foto, fora do quadrado.
  while (l / 2 >= lado) {
    const n = Math.floor(l / 2);
    const c = canvas(n);
    const ctx = c.getContext('2d');
    ctx.imageSmoothingQuality = 'high';
    ctx.drawImage(fonte, x, y, l, l, 0, 0, n, n);
    fonte = c;
    x = y = 0;
    l = n;
  }
  // Foto menor que o lado fica do tamanho dela: ampliar não traz nitidez.
  const final = canvas(Math.min(lado, l));
  const ctx = final.getContext('2d');
  // O JPEG não tem transparência: o fundo de um PNG recortado sairia preto.
  ctx.fillStyle = '#fff';
  ctx.fillRect(0, 0, final.width, final.height);
  ctx.imageSmoothingQuality = 'high';
  ctx.drawImage(fonte, x, y, l, l, 0, 0, final.width, final.height);
  img.close();

  for (let qual = qualidade; qual >= QUALIDADE_MINIMA; qual -= 10) {
    const b = await jpeg(final, qual);
    if (b && b.size <= tetoBytes) return b;
  }
  throw new Error('esta foto não coube no tamanho do avatar: tente outra');
}
