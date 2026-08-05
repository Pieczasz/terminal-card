import { animate } from "motion/mini";

/*
 * All page choreography, wired from data attributes so markup stays declarative.
 *
 *   data-hero        one-time staggered entrance on load
 *   data-rise        fade + lift when the element scrolls into view
 *   data-rise-group  same, staggered across the element's children
 *   data-deal        deal a hand of cards in, each springing to its resting angle
 *
 * Imported from motion/mini (WAAPI-backed animate, ~2KB) rather than the full
 * package. The full entry costs ~20KB brotli, and the only things it adds that are
 * used here are inView and stagger - an IntersectionObserver wrapper and `i * n`.
 * Both are below, so the 18KB buys nothing.
 *
 * Initial states are set in JS, never in CSS. If this bundle fails to load, every
 * element stays at its natural visible state and the page simply does not animate;
 * opacity:0 in CSS would risk a permanently blank page instead.
 */

const reduced = matchMedia("(prefers-reduced-motion: reduce)").matches;

// Bezier control points, not CSS strings: Motion's Easing type takes a 4-tuple.
/** Standard ease-out. */
const OUT: [number, number, number, number] = [0.16, 1, 0.3, 1];
/** Overshoots slightly then settles - a spring without the spring solver. */
const BACK_OUT: [number, number, number, number] = [0.34, 1.56, 0.64, 1];

/** Fires once, the first time `el` is at least `amount` visible. */
function onView(el: Element, run: () => void, amount = 0.15) {
  const io = new IntersectionObserver(
    (entries) => {
      for (const e of entries) {
        if (!e.isIntersecting) continue;
        io.disconnect();
        run();
      }
    },
    { threshold: amount },
  );
  io.observe(el);
}

function hide(els: Iterable<HTMLElement>) {
  for (const el of els) el.style.opacity = "0";
}

/*
 * Anything hidden for a reveal must be guaranteed to come back. IntersectionObserver
 * is reliable, but a display:none ancestor, a zero-height scroll container or an
 * element that never enters the viewport would leave a section invisible forever -
 * and an invisible section is a worse outcome than an unanimated one. This clears
 * every remaining inline hide well after the page has settled.
 */
function failsafe(els: HTMLElement[]) {
  setTimeout(() => {
    for (const el of els) {
      if (el.style.opacity === "0") el.style.removeProperty("opacity");
    }
  }, 4000);
}

function riseIn(els: HTMLElement[], step = 0) {
  els.forEach((el, i) => {
    animate(
      el,
      { opacity: [0, 1], transform: ["translateY(14px)", "translateY(0px)"] },
      { duration: 0.55, delay: i * step, ease: OUT },
    );
    // Clear the inline hide so a later re-render cannot leave it invisible.
    el.style.removeProperty("opacity");
  });
}

function setup() {
  const hidden: HTMLElement[] = [];

  // ---------------------------------------------------------------- hero ---
  for (const hero of document.querySelectorAll<HTMLElement>("[data-hero]")) {
    const kids = Array.from(hero.children) as HTMLElement[];
    hide(kids);
    hidden.push(...kids);
    riseIn(kids, 0.07);
  }

  // -------------------------------------------------------- rise on view ---
  for (const el of document.querySelectorAll<HTMLElement>("[data-rise]")) {
    hide([el]);
    hidden.push(el);
    onView(el, () => riseIn([el]));
  }

  // ------------------------------------------------------ staggered group ---
  for (const group of document.querySelectorAll<HTMLElement>(
    "[data-rise-group]",
  )) {
    const kids = Array.from(group.children) as HTMLElement[];
    hide(kids);
    hidden.push(...kids);
    onView(group, () => riseIn(kids, 0.08), 0.2);
  }

  // --------------------------------------------------------------- cards ---
  // A hand should arrive dealt rather than fade in as a block: each card flies
  // from roughly where the deck sits and settles on its resting angle.
  for (const fan of document.querySelectorAll<HTMLElement>("[data-deal]")) {
    const cards = Array.from(fan.children) as HTMLElement[];
    hide(cards);
    hidden.push(...cards);

    onView(
      fan,
      () => {
        cards.forEach((card, i) => {
          const rest = Number(card.dataset.rot ?? 0);
          animate(
            card,
            {
              opacity: [0, 1],
              transform: [
                "translate(-42px, -26px) rotate(-18deg) scale(0.9)",
                `translate(0px, 0px) rotate(${rest}deg) scale(1)`,
              ],
            },
            { duration: 0.5, delay: i * 0.09, ease: BACK_OUT },
          );
          card.style.removeProperty("opacity");
        });
      },
      0.4,
    );
  }

  failsafe(hidden);
}

if (!reduced) setup();
