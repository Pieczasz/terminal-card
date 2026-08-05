/*
 * Renders scripts/og-template.html to public/og.png (1200x630) and rasterises
 * public/favicon.svg to public/apple-touch-icon.png (180x180).
 *
 * Playwright is not a dependency of this project - the OG image is a static asset
 * that changes about never, so paying for a browser download on every install is
 * not worth it. Run with:
 *
 *   pnpm dlx playwright@latest install chromium
 *   pnpm dlx --package=playwright@latest node scripts/make-og.mjs
 */
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const web = resolve(here, "..");

let chromium;
try {
  ({ chromium } = await import("playwright"));
} catch {
  console.error(
    "playwright is not available. See the header of this file for the command.",
  );
  process.exit(1);
}

const browser = await chromium.launch();

const og = await browser.newPage({
  viewport: { width: 1200, height: 630 },
  deviceScaleFactor: 1,
});
await og.goto(`file://${web}/scripts/og-template.html`, { waitUntil: "load" });
// Let the webfont fallback settle before capturing, or glyph metrics can shift.
await og.waitForTimeout(400);
await og.screenshot({ path: `${web}/public/og.png` });

const icon = await browser.newPage({
  viewport: { width: 180, height: 180 },
  deviceScaleFactor: 1,
});
await icon.goto(`file://${web}/public/favicon.svg`, { waitUntil: "load" });
await icon.screenshot({ path: `${web}/public/apple-touch-icon.png` });

await browser.close();
console.log("wrote public/og.png and public/apple-touch-icon.png");
