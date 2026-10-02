// Cadastrar pela tela é por onde a gravação começa: o clique em "salvar" tem
// que pôr a câmera para gravar na hora, sem reiniciar nada.
import { test, expect } from '../../apoio/fixtures';
import { esperarTrechos } from '../../apoio/api';

test('cadastrar pela tela começa a gravar', async ({ page, limpa }) => {
  await page.goto(`${limpa.baseURL}/#cams`);

  await test.step('o stream do go2rtc aparece para cadastrar', async () => {
    await page.getByRole('button', { name: '+ cam_relogio', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Cadastrar cam_relogio' })).toBeVisible();
    // O nome nasce do id do stream, sem o prefixo cam_.
    await expect(page.getByLabel('Nome', { exact: true })).toHaveValue('Relogio');
  });

  await test.step('salvar fecha o formulário e mostra a câmera', async () => {
    await page.getByLabel('Duração do segmento (s)').fill('10');
    await page.getByRole('button', { name: 'salvar' }).click();
    await expect(page.getByRole('heading', { name: 'Cadastrar cam_relogio' })).toBeHidden();
    await expect(page.getByText('Relogio', { exact: true })).toBeVisible();
    // Cadastrada, ela sai da lista dos que faltam cadastrar.
    await expect(page.getByRole('button', { name: '+ cam_relogio', exact: true })).toBeHidden();
  });

  await test.step('a gravação começa: o ponto fica verde, o trecho entra no índice e a resolução aparece', async () => {
    // O ponto não tem texto: é a cor que diz "conectada".
    const card = page.locator('.card.cam').filter({ hasText: 'cam_relogio' });
    await expect(card.locator('.dot')).toHaveClass(/\bok\b/);
    await esperarTrechos(limpa.api, 'cam_relogio', 1, 45_000);
    await expect(card.getByText('640×360')).toBeVisible();
  });
});
