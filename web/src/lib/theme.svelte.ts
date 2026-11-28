// The screen's theme: the person's choice, the palette applied, and the colors
// for whoever paints outside CSS.
//
// The choice is made before the first paint by the script in index.html, which
// also leaves `window.dwnvrTheme` behind: this module reuses its `apply`, so
// the rule that turns a choice into a palette lives in a single place. The
// choice is this browser's, in localStorage: two devices of the same person can
// have different themes.

// What the person picks. `system` follows the device: light gives the light
// palette, dark gives the dark one.
export type ThemeChoice = 'system' | 'light' | 'light-dim' | 'dark' | 'dark-dim';

// The palette on screen: the `data-theme` of <html>, or `dark` without it.
export type Theme = Exclude<ThemeChoice, 'system'>;

// What index.html leaves on the window.
interface Boot {
  key: string;
  // Applies the choice to <html> and to the theme-color and color-scheme metas,
  // and returns the palette applied. Anything it doesn't know is `system`.
  apply(choice: string | null): Theme;
}

declare global {
  interface Window {
    dwnvrTheme: Boot;
  }
}

// In the order the My account screen lists them.
export const THEME_CHOICES: readonly { value: ThemeChoice; label: string }[] = [
  { value: 'system', label: 'Sistema' },
  { value: 'light', label: 'Claro' },
  { value: 'light-dim', label: 'Pouco claro' },
  { value: 'dark', label: 'Escuro' },
  { value: 'dark-dim', label: 'Pouco escuro' },
];

const boot = window.dwnvrTheme;

function saved(): ThemeChoice {
  let v: string | null = null;
  try {
    v = localStorage.getItem(boot.key);
  } catch {
    // Blocked site data: the theme is the system's.
  }
  return THEME_CHOICES.find((c) => c.value === v)?.value ?? 'system';
}

export const theme = $state({
  choice: saved(),
  applied: (document.documentElement.dataset['theme'] ?? 'dark') as Theme,
});

export function setTheme(choice: ThemeChoice) {
  try {
    if (choice === 'system') localStorage.removeItem(boot.key);
    else localStorage.setItem(boot.key, choice);
  } catch {
    // Blocked site data: the theme changes until the screen is reloaded.
  }
  theme.choice = choice;
  theme.applied = boot.apply(choice);
}

// `system` follows the device while the screen is open, too: the phone that
// goes dark at night takes the screen with it.
matchMedia('(prefers-color-scheme: light)').addEventListener('change', () => {
  if (theme.choice === 'system') theme.applied = boot.apply('system');
});

// The theme's colors for whoever paints outside CSS: a canvas, or an icon
// turned into an image. The value is the CSS token from app.css, read on the
// element being painted: inside `.over-video` it is the dark palette, the same
// in every theme, and on the page it is the theme's.
//
// Read at drawing time, and not once at load: the theme can change with the
// screen open. Reading `theme.applied` here is what makes the effect or the
// template that called it run again when the theme changes: the timeline
// redraws and the family icon is made again in the new color.
export function tokenColor(el: Element, token: `--${string}`): string {
  // eslint-disable-next-line @typescript-eslint/no-unused-expressions -- only the read, so the caller's effect tracks it
  theme.applied;
  return getComputedStyle(el).getPropertyValue(token).trim();
}
