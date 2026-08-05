// @ts-check

import sitemap from "@astrojs/sitemap";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "astro/config";

// https://astro.build/config
export default defineConfig({
  // @astrojs/sitemap emits nothing at all without this, and the canonical/OG URLs
  // in Base.astro are resolved against it.
  site: "https://tty.cards",

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
