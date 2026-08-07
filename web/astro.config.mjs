// @ts-check

import sitemap from "@astrojs/sitemap";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "astro/config";

/*
 * Syntax highlighting in the palette from internal/tui/styles/theme.go.
 *
 * Not cosmetic: every bundled Shiki dark theme assumes an editor background around
 * #1e1e1e and colours comments near #6A737D, which lands at ~3.4:1 on this page's
 * #0b0b0b and fails WCAG AA. Every colour below clears 5.5:1 on that background.
 */
const codeTheme = {
  name: "tty-cards",
  // Cast, or ts-check widens this to `string` and Shiki's theme type rejects it.
  type: /** @type {const} */ ("dark"),
  colors: {
    "editor.background": "#0b0b0b",
    "editor.foreground": "#b4b4b4",
  },
  settings: [
    {
      scope: ["comment", "punctuation.definition.comment"],
      settings: { foreground: "#949494" },
    },
    {
      scope: ["keyword", "storage", "keyword.control"],
      settings: { foreground: "#ffb454" },
    },
    { scope: ["string", "string.quoted"], settings: { foreground: "#6fd48a" } },
    {
      scope: ["constant.numeric", "constant.language"],
      settings: { foreground: "#6be3e3" },
    },
    {
      scope: ["entity.name.function", "support.function"],
      settings: { foreground: "#ffd700" },
    },
    {
      scope: ["entity.name.type", "support.type", "storage.type"],
      settings: { foreground: "#6be3e3" },
    },
    {
      scope: ["variable", "meta.definition.variable"],
      settings: { foreground: "#b4b4b4" },
    },
  ],
};

// https://astro.build/config
export default defineConfig({
  markdown: { shikiConfig: { theme: codeTheme } },

  // @astrojs/sitemap emits nothing at all without this, and the canonical/OG URLs
  // in Base.astro are resolved against it.
  site: "https://www.tty.cards",

  output: "static",
  integrations: [sitemap()],

  // Astro 7 defaults this to 'jsx', which stops collapsing newlines between inline
  // elements. This layout is monospace and column-aligned, so a newline that turns
  // into a rendered space shifts a box-drawing rail by a cell. Keep v6 behaviour.
  compressHTML: true,

  build: {
    // /blog/index.html rather than /blog.html, so the same output serves correctly
    // from nginx and from GitHub Pages.
    format: "directory",
  },

  vite: {
    plugins: [tailwindcss()],
  },
});
