import { animate } from "motion/mini";

/*
 * Page choreography, wired from data attributes so markup stays declarative.
 *
 *   data-typewriter  type this text out, one character at a time, on load
 *   data-reveal      fade up once the typewriter above it has finished
 *   data-rise        fade up when the element scrolls into view
 *   data-deal        deal a hand of cards in
 *
 * From motion/mini (WAAPI-backed animate, ~2KB) rather than the full package. The
 * full entry costs ~20KB brotli and the only extras used here would be inView and
 * stagger - an IntersectionObserver wrapper and `i * n`, both of which are below.
 *
 * Initial states are set in JS, never CSS. If this bundle fails to load, everything
 * stays at its natural visible state and the page simply does not animate; opacity:0
 * in CSS would risk a permanently blank page instead.
 */

const reduced = matchMedia("(prefers-reduced-motion: reduce)").matches;

const OUT: [number, number, number, number] = [0.16, 1, 0.3, 1];
const BACK_OUT: [number, number, number, number] = [0.34, 1.56, 0.64, 1];

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

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
 * Anything hidden for a reveal must be guaranteed to come back. A display:none
 * ancestor or an element that never enters the viewport would otherwise stay
 * invisible forever, which is a worse outcome than not animating at all.
 */
function failsafe(els: HTMLElement[]) {
  setTimeout(() => {
    for (const el of els) {
      if (el.style.opacity === "0") el.style.removeProperty("opacity");
    }
  }, 5000);
}

function riseIn(els: HTMLElement[], step = 0) {
  els.forEach((el, i) => {
    animate(
      el,
      { opacity: [0, 1], transform: ["translateY(12px)", "translateY(0px)"] },
      { duration: 0.5, delay: i * step, ease: OUT },
    );
    el.style.removeProperty("opacity");
  });
}

/*
 * Types text with per-character jitter. A fixed interval reads as a machine
 * printing; a real person is uneven, pauses at a space, and hesitates before the
 * first keystroke. The jitter is what makes it feel typed rather than animated.
 */
async function typeOut(el: HTMLElement, text: string) {
  el.textContent = "";
  await sleep(320); // the beat before someone starts typing

  for (const ch of text) {
    el.textContent += ch;
    const base = ch === " " || ch === "." ? 105 : 52;
    await sleep(base + Math.random() * 55);
  }
}

async function setup() {
  const hidden: HTMLElement[] = [];

  // ------------------------------------------------------------ hero type ---
  const typers = [
    ...document.querySelectorAll<HTMLElement>("[data-typewriter]"),
  ];
  const reveals = [...document.querySelectorAll<HTMLElement>("[data-reveal]")];

  // Hide the follow-on copy first, so it cannot flash before the command lands.
  hide(reveals);
  hidden.push(...reveals);

  if (typers.length) {
    await Promise.all(
      typers.map((el) => typeOut(el, el.textContent?.trim() ?? "")),
    );
    await sleep(180);
  }

  riseIn(reveals, 0.09);

  // ------------------------------------------------------- rise on scroll ---
  for (const el of document.querySelectorAll<HTMLElement>("[data-rise]")) {
    hide([el]);
    hidden.push(el);
    onView(el, () => riseIn([el]));
  }

  // --------------------------------------------------------------- cards ---
  // A hand should arrive dealt, not fade in as a block: each card flies from
  // roughly where the deck sits and settles on its resting angle.
  for (const fan of document.querySelectorAll<HTMLElement>("[data-deal]")) {
    const cards = [...fan.children] as HTMLElement[];
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
                "translate(-38px, -22px) rotate(-16deg)",
                `translate(0px, 0px) rotate(${rest}deg)`,
              ],
            },
            { duration: 0.45, delay: i * 0.08, ease: BACK_OUT },
          );
          card.style.removeProperty("opacity");
        });
      },
      0.4,
    );
  }

  failsafe(hidden);
}

if (reduced) {
  // Nothing to stage: the markup already contains the final text and full opacity.
} else {
  setup();
}
