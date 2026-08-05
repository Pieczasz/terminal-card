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

## Conventions

- Design tokens live in `src/styles/global.css` and are copied from
  `internal/tui/styles/theme.go` (dark branch). Change them there first.
- Every section is wrapped in `TerminalWindow.astro`. Its rails are CSS borders,
  not repeated `─`/`│` glyphs — do not "fix" that back to characters.
- `output: 'static'`. No adapter, no server runtime, no API routes.
- Biome's `noUnusedVariables`/`noUnusedImports` are off for `.astro` because it
  only parses frontmatter and cannot see template usage.
