// The height an element can have to end together with the window: from its top
// to the window's bottom, minus what is reserved below it - each parent's
// breathing room up to `main`, and the space `main` keeps so the bottom
// navigation doesn't cover the content. That `main` padding is zero on desktop,
// where the navigation moves up: reading it avoids repeating here, in JS, the
// breakpoint that is already in the CSS.
//
// Measured in JS because CSS alone can't get there: `#app` only has a minimum
// height, and the header, with or without the new-version strip, has no fixed
// height.
export function freeHeight(el: Element, minimum = 80) {
  let reserved = 0;
  const main = el.closest('main');
  for (let p = el.parentElement; p; p = p.parentElement) {
    reserved += parseFloat(getComputedStyle(p).paddingBottom) || 0;
    if (p === main) break;
  }
  return Math.max(minimum, window.innerHeight - el.getBoundingClientRect().top - reserved);
}
