// A tab opened before a dwnvr update. The server starts serving the new screen,
// and the tab stays on the old one until someone reloads - sometimes for days,
// in the installed phone app. Worse: the old screen's separate chunks
// (Detections, Users, Account) don't exist in the new binary, and opening one
// of them for the first time fails.
//
// What says the screen changed is index.html itself: it points to the files in
// assets/, which have a hash in the name, and it is served with no-cache. It is
// enough to compare it with what this tab loaded. A change only in Go doesn't
// swap any file and lights nothing; and nothing here depends on the server or
// on the build.

// How often to ask while the tab is visible. Coming back to the tab also asks,
// and that is the common case: the phone coming out of a pocket.
const INTERVAL_MS = 30 * 60 * 1000;

export const newScreen = $state({ available: false });

// The assets/ files that index.html loads: the entry script and the CSS. The
// script changes whenever any chunk changes, because it carries their names;
// the main CSS goes separately, only index.html mentions it.
function signature(doc: Document): string {
  return Array.from(
    doc.querySelectorAll('script[type="module"][src], link[rel="stylesheet"][href]'),
    (el) => el.getAttribute('src') ?? el.getAttribute('href') ?? '',
  )
    .filter((url) => url.includes('/assets/'))
    .sort()
    .join(' ');
}

// Read now, at module load: Vite adds each downloaded chunk's CSS to the
// <head>, and read later the signature would no longer be index.html's. On the
// dev server there is no assets/, the signature comes out empty and nothing is
// watched: there, Vite itself updates the screen.
const fromTab = signature(document);

let asking = false;

async function ask() {
  if (!fromTab || newScreen.available || asking) return;
  asking = true;
  try {
    const r = await fetch(location.pathname, { cache: 'no-store' });
    if (!r.ok) return;
    const html = new DOMParser().parseFromString(await r.text(), 'text/html');
    const fromServer = signature(html);
    // Empty means a response that isn't the screen - a proxy in the middle, an
    // error page -, and not a new version.
    if (fromServer && fromServer !== fromTab) newScreen.available = true;
  } catch {
    // No network or server down: it waits for the next check.
  } finally {
    asking = false;
  }
}

// Watches while the interface is open. Returns the function that stops it.
export function watchScreenVersion(): () => void {
  const onReturn = () => {
    if (!document.hidden) void ask();
  };
  // A chunk download failure, emitted by Vite itself: it is the symptom of the
  // screen having changed, and the check doesn't wait for the next round.
  const onChunkFail = () => void ask();
  const timer = setInterval(onReturn, INTERVAL_MS);
  document.addEventListener('visibilitychange', onReturn);
  window.addEventListener('vite:preloadError', onChunkFail);
  return () => {
    clearInterval(timer);
    document.removeEventListener('visibilitychange', onReturn);
    window.removeEventListener('vite:preloadError', onChunkFail);
  };
}
