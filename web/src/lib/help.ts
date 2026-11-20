// Help texts shared between screens, for the same reason the formatters live
// together in format.js: the Diagnostics column and the chip on the Cameras
// screen show the SAME quantity, and if each screen wrote its own explanation
// the two would diverge on the first change to either.

// The retained/fit pair is what makes the quota understandable: one is the past
// that exists, the other is what it still has room for. Alone, each one
// misleads.
export const HELP_RETAINED =
  'Tempo desde a gravação mais antiga em disco, incluídos os períodos sem gravar. Quando eles passam de 1h, aparece também o gravado: o vídeo que existe de fato.';

export const HELP_FIT =
  'Quanto tempo de gravação cabe na cota, pelo consumo médio do que a câmera já gravou. É estimativa: encolhe se ela passar a gastar mais por dia. Com a cota cheia, encosta no retido.';

// The motion detection sensitivity ladder. It lives here, and not on the
// screen, because the label and the cost appear in two places - the list chip
// and the form.
//
// `perHour` is the cost PER CAMERA, and repeats by hand
// internal/detect/params.go's OnsetsPerHour: with ten cameras at level 4 that
// is 500 inferences per hour, not 50.
//
// The levels go from 1 to 5, in order: level n is LEVELS[n - 1], and whoever
// only has the number asks level().
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
