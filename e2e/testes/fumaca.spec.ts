// O mínimo de toda rodada: a interface abre, as abas estão lá, e cada tela
// cabe na largura, com a navegação no lugar dela. Roda no desktop e no
// celular (os projetos do playwright.config.ts).
import { test, expect } from '../apoio/fixtures';
import { navegacaoEsperada, QUEBRA_DA_NAVEGACAO } from '../apoio/layout';

test('a interface abre com as abas e mostra a versão do servidor', async ({ page, gravando }) => {
  const versao = ((await (await gravando.api.get('/api/version')).json()) as { version: string }).version;

  await page.goto(`${gravando.baseURL}/#health`);
  // Duas barras existem no HTML, a de cima e a de baixo; o CSS esconde a que
  // não é da largura, e o papel só enxerga a visível.
  const navegacao = page.getByRole('navigation');
  for (const aba of ['Ao vivo', 'Gravações', 'Câmeras', 'Diagnóstico']) {
    await expect(navegacao.getByRole('link', { name: aba })).toBeVisible();
  }
  // Sem detector de objetos configurado, não há o que listar em Detecções.
  await expect(navegacao.getByRole('link', { name: 'Detecções' })).toHaveCount(0);

  // O Diagnóstico é o lugar onde se procura a versão em uso.
  await expect(page.getByText(`dwnvr ${versao}`)).toBeVisible();
});

// Cada tela, e o que dela precisa aparecer antes de medir a largura.
const telas = [
  { hash: '#live?cams=cam_relogio', nome: 'Ao vivo', pronta: 'video-stream' },
  { hash: '#rec?cam=cam_relogio', nome: 'Gravações', pronta: '.stage video' },
  { hash: '#cams', nome: 'Câmeras', pronta: 'text=Disponíveis no go2rtc' },
  { hash: '#health', nome: 'Diagnóstico', pronta: 'text=Disco' },
];

for (const tela of telas) {
  test(`${tela.nome}: cabe na largura, e a navegação fica no lugar`, async ({ page, gravando, isMobile }) => {
    await page.goto(`${gravando.baseURL}/${tela.hash}`);
    await expect(page.locator(tela.pronta).first()).toBeVisible();

    // A promessa do fase3-resultados.md: sem rolagem horizontal, em nenhuma
    // largura. Um elemento largo demais aparece aqui como sobra.
    const sobra = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
    expect(sobra, 'a página rola na horizontal').toBeLessThanOrEqual(0);

    // Abaixo de 720 px a navegação mora embaixo, no alcance do polegar; acima,
    // sobe para o topo (apoio/layout.ts).
    const viewport = page.viewportSize()!;
    expect(isMobile, 'só o projeto de celular fica abaixo da quebra').toBe(viewport.width < QUEBRA_DA_NAVEGACAO);
    const caixa = (await page.getByRole('navigation').boundingBox())!;
    const esperada = navegacaoEsperada(viewport, caixa.height);
    expect(Math.abs(caixa.y - esperada.y), `a navegação fica ${esperada.onde}`).toBeLessThanOrEqual(esperada.folga);
  });
}
