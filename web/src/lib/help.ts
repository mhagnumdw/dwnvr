// Textos de ajuda compartilhados entre telas, pela mesma razão que os
// formatadores moram juntos em format.js: a coluna do Diagnóstico e o chip da
// tela de Câmeras mostram a MESMA grandeza, e se cada tela escrevesse a sua
// explicação as duas divergiriam na primeira alteração de uma delas.

// O par retido/cabem é o que torna a cota compreensível: um é o passado que
// existe, o outro é o que ela ainda comporta. Sozinho, cada um engana.
export const HELP_RETAINED =
  'Tempo desde a gravação mais antiga em disco, incluídos os períodos sem gravar. Quando eles passam de 1h, aparece também o gravado: o vídeo que existe de fato.';

export const HELP_FIT =
  'Quanto tempo de gravação cabe na cota, pelo consumo médio do que a câmera já gravou. É estimativa: encolhe se ela passar a gastar mais por dia. Com a cota cheia, encosta no retido.';

// A escada de sensibilidade da detecção de movimento. Ela vive aqui, e não na
// tela, porque o rótulo e o custo aparecem em dois lugares - o chip da lista e
// o formulário.
//
// `porHora` é o custo POR CÂMERA, e repete à mão o OnsetsPerHour de
// internal/detect/params.go: com dez câmeras no nível 4 são 500 olhadas
// por hora, não 50.
//
// Os níveis vão de 1 a 5, na ordem: o de número n é o NIVEIS[n - 1], e quem
// tem só o número pergunta ao nivel().
export const LEVELS = [
  { label: 'muito baixa', perHour: 6 },
  { label: 'baixa', perHour: 12 },
  { label: 'média', perHour: 25 },
  { label: 'alta', perHour: 50 },
  { label: 'muito alta', perHour: 100 },
];

export const level = (n: number | undefined) => (n === undefined ? undefined : LEVELS[n - 1]);

export const HELP_SENSITIVITY =
  'Quantas vezes por hora, nesta câmera, o dwnvr para para olhar a imagem. Subir o nível pega mais movimento e custa mais CPU.';
