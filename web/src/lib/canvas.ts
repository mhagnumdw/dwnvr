// contexto2d devolve o contexto 2D de um canvas. O navegador só o nega quando
// o canvas já tem um contexto de outro tipo, o que nenhum canvas desta tela
// tem: virar exceção é melhor que espalhar `?.` por todo desenho.
export function contexto2d(c: HTMLCanvasElement): CanvasRenderingContext2D {
  const g = c.getContext('2d');
  if (!g) throw new Error('o navegador não deu o contexto 2D do canvas');
  return g;
}
