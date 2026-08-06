import { animate } from "motion/mini";

/*
 * Page choreography, wired from data attributes so markup stays declarative.
 *
 *   data-typewriter  type this text out on load
 *   data-print       reveal instantly once the typing finishes, in document order
 *   data-rise        short fade when the element scrolls into view
 *   data-deal        deal a hand of cards in
 *
 * Two rules keep this feeling like a terminal rather than a marketing page:
 *
 *   1. The cursor does not blink while typing. A real terminal cursor is solid
 *      whenever characters are arriving and only blinks once it is idle waiting for
 *      you. Blinking throughout is the single biggest tell of a fake typing effect.
 *
 *   2. Output does not fade in, it appears. Nothing in a terminal cross-fades - a
 *      line is either printed or it is not. So the copy under the command switches
 *      from hidden to visible in one frame, with a beat between lines.
 *
 * Initial states are set here in JS, never in CSS, so a bundle that fails to load
 * leaves the page fully visible and merely un-animated.
 */

const reduced = matchMedia("(prefers-reduced-motion: reduce)").matches;

const OUT: [number, number, number, number] = [0.16, 1, 0.3, 1];
const BACK_OUT: [number, number, number, number] = [0.34, 1.56, 0.64, 1];

/** ~62ms lands between "fast typist" and "legible". Below ~40 it reads as a paste. */
const TYPE_MS = 62;

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

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

async function typeCommand(target: HTMLElement, text: string) {
  // Marks the whole command as mid-typing so CSS can hold the cursor solid.
  const line = target.closest("[data-ssh]");
  line?.setAttribute("data-typing", "");

  target.textContent = "";
  await sleep(300); // the beat before a hand starts moving

  for (const ch of text) {
    target.textContent += ch;
    // Uneven on purpose, and slower across a separator, the way a real hand is.
    const pause = ch === " " || ch === "." ? 90 : 0;
    await sleep(TYPE_MS + pause + Math.random() * 30);
  }

  await sleep(160);
  line?.removeAttribute("data-typing"); // idle now, so it may blink
}

async function setup() {
  const hidden: HTMLElement[] = [];

  // -------------------------------------------------------- hero: type it ---
  const typers = [
    ...document.querySelectorAll<HTMLElement>("[data-typewriter]"),
  ];
  const prints = [...document.querySelectorAll<HTMLElement>("[data-print]")];

  // visibility, not opacity: the element keeps its box, so revealing it later
  // cannot reflow anything and CLS stays at zero.
  for (const el of prints) el.style.visibility = "hidden";

  if (typers.length) {
    await Promise.all(
      typers.map((el) => typeCommand(el, el.dataset.typewriter || "")),
    );
  }

  // Print, do not fade.
  for (const el of prints) {
    el.style.visibility = "visible";
    await sleep(110);
  }

  // ------------------------------------------------------- rise on scroll ---
  // Kept deliberately plain: a short fade, no travel. Sliding panels around is the
  // thing that makes a page feel like an advert.
  for (const el of document.querySelectorAll<HTMLElement>("[data-rise]")) {
    el.style.opacity = "0";
    hidden.push(el);
    onView(el, () => {
      animate(el, { opacity: [0, 1] }, { duration: 0.32, ease: OUT });
      el.style.removeProperty("opacity");
    });
  }

  // --------------------------------------------------------------- cards ---
  for (const fan of document.querySelectorAll<HTMLElement>("[data-deal]")) {
    const cards = [...fan.children] as HTMLElement[];
    for (const c of cards) c.style.opacity = "0";
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
                "translate(-34px, -20px) rotate(-14deg)",
                `translate(0px, 0px) rotate(${rest}deg)`,
              ],
            },
            { duration: 0.42, delay: i * 0.07, ease: BACK_OUT },
          );
          card.style.removeProperty("opacity");
        });
      },
      0.4,
    );
  }

  // A hide the observer never reached would leave a section blank, which is worse
  // than an unanimated one.
  setTimeout(() => {
    for (const el of hidden) {
      if (el.style.opacity === "0") el.style.removeProperty("opacity");
    }
    for (const el of prints) el.style.removeProperty("visibility");
  }, 5000);
}

if (!reduced) setup();
