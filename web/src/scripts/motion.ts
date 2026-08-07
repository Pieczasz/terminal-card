import { animate } from "motion/mini";

/*
 * Page choreography, wired from data attributes so markup stays declarative.
 *
 *   data-typewriter  type this text out on load
 *   data-rise        short fade when the element scrolls into view
 *   data-deal        deal a hand of cards in
 *
 * The typing is hand-written for two reasons no library covers: nothing animates text
 * character by character, and a real terminal cursor stays solid while characters are
 * arriving and only blinks once it is idle. Blinking throughout is the tell of a fake
 * typing effect. Everything else here is Motion.
 *
 * The hero copy underneath is deliberately not animated at all. It is present at first
 * paint. The command typing is the one moment on the page; staging the paragraphs too
 * only added a queue of things waiting to appear.
 */

const reduced = matchMedia("(prefers-reduced-motion: reduce)").matches;

const OUT: [number, number, number, number] = [0.16, 1, 0.3, 1];
const BACK_OUT: [number, number, number, number] = [0.34, 1.56, 0.64, 1];

/** ~62ms sits between "fast typist" and "legible". Below ~40 it reads as a paste. */
const TYPE_MS = 62;

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

/**
 * True when the element is already on screen.
 *
 * Anything visible at first paint must never be hidden: this script runs after that
 * paint, so hiding it produces a visible flash of content that then fades back in.
 * Only elements below the fold get staged for a reveal.
 */
function onScreen(el: Element) {
  const r = el.getBoundingClientRect();
  return r.top < innerHeight * 0.9 && r.bottom > 0;
}

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
  // Marks the command as mid-typing so CSS can hold the cursor solid.
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

function setup() {
  const staged: HTMLElement[] = [];

  for (const el of document.querySelectorAll<HTMLElement>(
    "[data-typewriter]",
  )) {
    typeCommand(el, el.dataset.typewriter || "");
  }

  // A short fade, no travel. Sliding panels around is what makes a page feel like an
  // advert rather than a tool.
  for (const el of document.querySelectorAll<HTMLElement>("[data-rise]")) {
    if (onScreen(el)) continue;
    el.style.opacity = "0";
    staged.push(el);
    onView(el, () => {
      animate(el, { opacity: [0, 1] }, { duration: 0.32, ease: OUT });
      el.style.removeProperty("opacity");
    });
  }

  for (const fan of document.querySelectorAll<HTMLElement>("[data-deal]")) {
    if (onScreen(fan)) continue;
    const cards = [...fan.children] as HTMLElement[];
    for (const c of cards) c.style.opacity = "0";
    staged.push(...cards);

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

  // A stage the observer never reached would leave a section blank, which is worse
  // than an unanimated one.
  setTimeout(() => {
    for (const el of staged) {
      if (el.style.opacity === "0") el.style.removeProperty("opacity");
    }
  }, 5000);
}

if (!reduced) setup();
