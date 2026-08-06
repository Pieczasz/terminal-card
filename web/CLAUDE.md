## Development

When starting the dev server, use background mode:

```
astro dev --background
```

Manage it with `astro dev stop`, `astro dev status`, and `astro dev logs`.

## Scripts

```
pnpm build     # astro build -> dist/
pnpm check     # astro check (needs typescript 6.x; 7.x lacks the API it uses)
pnpm lint      # biome check .
pnpm fmt       # biome check --write .
```

Run Biome from this directory, not the repo root. Invoked from above, it reports
`web/biome.json` as a nested config and exits.

## Conventions

- Design tokens live in `src/styles/global.css` (`@theme`) and are copied from
  `internal/tui/styles/theme.go` (dark branch). Change them there first.
- Spacing utilities are named, not numeric: `p-panel`, `gap-gap`, `max-w-shell`.
  The layout is column-based, so prefer these over `p-[3ch]`.
- Every section is wrapped in `TerminalWindow.astro`. Its frame is a CSS border
  with a notched title, not box-drawing glyphs - do not "fix" that back to
  characters. `╭───╮` is five glyph advances but a box-drawing glyph's advance is
  not `1ch` in every font, so glyph frames split apart. Same reason the 404 art and
  the terminal's `neofetch` use plain ASCII.
- Animation lives in `src/scripts/motion.ts`, driven by `data-typewriter`,
  `data-reveal`, `data-rise` and `data-deal` attributes. The hero types its command
  out, then `data-reveal` elements fade up behind it. Import from `motion/mini`, not
  `motion` - the full entry costs ~20KB brotli for `inView` and `stagger`, both of
  which are reimplemented in that file in a few lines.
- Reveals set `opacity: 0` from JS, never from CSS, so a failed bundle leaves the
  page visible rather than blank. There is a 4s failsafe that clears any hide the
  observer never got to.
- `output: 'static'`. No adapter, no server runtime, no API routes. Live numbers
  come from the Go server at runtime via `LiveStats.astro`, not at build time.
  `PUBLIC_API_BASE` points it somewhere else for local work.
- Prose uses `-`, never an em dash.
- Biome's `noUnusedVariables`/`noUnusedImports` are off for `.astro` because it
  only parses frontmatter and cannot see template usage.

## SEO

Owned by `src/layouts/Base.astro`: title, description, canonical, robots, OG and
Twitter cards, plus `WebSite` + `SoftwareApplication` JSON-LD on the home page.
Pages add their own `<head>` tags through the named `head` slot - `BlogPosting` and
`BreadcrumbList` on posts, `Blog` on the index.

- Pass `article={{ published }}` on posts; it switches `og:type` to `article`.
- Pass `noindex` on utility routes (404 does).
- `site` in `astro.config.mjs` is load-bearing: `@astrojs/sitemap` emits nothing
  without it, and canonical/OG URLs resolve against it.
- `public/og.png` is generated, not hand-drawn: edit `scripts/og-template.html`
  and re-run `scripts/make-og.mjs` (needs Playwright, see that file's header).

Verified with Lighthouse at 100 for SEO, accessibility and best practices on both
presets. Two notes if you change things:

- Inline links inside body text need a non-colour cue (underline), or `axe` flags
  WCAG 1.4.1. This is why prose links and the post breadcrumb are underlined.
- The remaining `render-blocking-insight` flag reports `FCP: 0, LCP: 0` savings.
  Inlining the stylesheet to clear it would cost cross-page cache reuse for no
  measured gain.
