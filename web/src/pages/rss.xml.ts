import { getCollection } from "astro:content";
import rss from "@astrojs/rss";
import type { APIContext } from "astro";

export async function GET(context: APIContext) {
  const posts = (await getCollection("blog", ({ data }) => !data.draft)).sort(
    (a, b) => b.data.date.valueOf() - a.data.date.valueOf(),
  );

  return rss({
    title: "tty.cards — engineering notes",
    description:
      "Notes from building an SSH server for multiplayer card games in Go.",
    // context.site comes from `site` in astro.config.mjs; rss() throws without it.
    site: context.site ?? "https://tty.cards",
    items: posts.map((p) => ({
      title: p.data.title,
      description: p.data.description,
      pubDate: p.data.date,
      link: `/blog/${p.id}/`,
    })),
    customData: "<language>en</language>",
  });
}
