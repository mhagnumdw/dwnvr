# TODO: the number in the "dropped" slice of the funnel has low contrast

Found while writing the contrast script of the themes (step 2 of
[themes](v1.0.0/temas.md), 27/11/2026), and measured on the tokens of
`web/src/app.css`.

- In the Diagnostics funnel bar, the "descartada: fila da câmera cheia" slice
  has a `--track-strong` background and carries its number in `--dim`
  (`Diagnostics.svelte`, `OUTCOMES`, `dark: false`).
- That pair gives about 3:1 in every palette, the dark one of today included
  (dark 3.20, dark-dim 2.70, light-dim 3.23, light 3.29). The text is 12px
  bold, so WCAG AA asks for 4.5:1.
- The pair is not in `web/scripts/contrast.mjs`, so nothing fails today; adding
  it there makes the hook fail until the colors change.

## Options

1. Put the number in `--fg` on that slice: `--fg` on `--track-strong` gives
   8.33 in dark, 5.75 in light-dim and 8.50 in light, but 4.31 in dark-dim,
   whose `--track-strong` would need to go a little darker.
2. Make `--track-strong` lighter in the dark palettes and darker in the light
   ones. It is also the discarded-bar color elsewhere in Diagnostics, so the
   change is wider than the funnel.
3. Leave the number out of the slice and show it only in the legend, as
   already happens when it does not fit.

## Verdict

Low priority: the slice is usually thin and its number often goes to the
legend. Option 1 is the smallest change (one line, plus the dark-dim track):
do it with any later work on Diagnostics, and add the pair to the script in the
same commit.
