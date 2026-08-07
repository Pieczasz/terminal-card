import { defineCollection } from "astro:content";
import { glob } from "astro/loaders";
// Astro 7 deprecates re-exporting `z` from astro:content; import Zod 4 directly.
import { z } from "zod";

// Astro 7: config lives at src/content.config.ts, loaders come from astro/loaders,
// and entries expose `id` rather than `slug`.
const blog = defineCollection({
  loader: glob({ base: "./src/content/blog", pattern: "**/*.md" }),
  schema: z.object({
    title: z.string(),
    description: z.string(),
    date: z.coerce.date(),
    draft: z.boolean().default(false),
  }),
});

export const collections = { blog };
