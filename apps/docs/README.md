# Meshploy documentation site

Astro Starlight generates the static site at `https://docs.meshploy.com`. Pagefind indexes the production build for search.

## Authoring sources

| Content | Edit here |
| --- | --- |
| Home | `src/content/docs/index.mdx` |
| Introduction | `content/introduction.md` |
| Workflow guides | `src/content/docs/guides/*.md` |
| Self-hosting | Root `README.md`, Self-Hosting section |
| Architecture and references | Source files listed in `sync-docs.mjs` |
| Navigation | `astro.config.mjs` |
| Theme | `src/styles/custom.css` and `src/components/` |

`npm run dev` and `npm run build` run `sync-docs.mjs` first. It copies selected repository documents, adds frontmatter, rewrites known source links, and copies shared font/icon assets. Generated documents are ignored by Git. Do not edit those output files directly; changes will be overwritten.

The introduction is a dedicated source processed by sync. Guides and the home page are tracked directly and are not overwritten. Rerun `npm run sync` after changing an external source while the dev server is running.

## Local development

Run these commands from `apps/docs`:

```bash
npm ci
npm run dev -- --port 4323
npm run build
npm run preview
```

Search requires a production build and preview. Build output is in `dist/`, including the search index and sitemap.

## Content review

Use the current implementation to verify steps and labels. Planning documents explain intent; a closed plan can still contain historical proposals, and an open plan can contain already-shipped work. Read status and verify the relevant code before documenting a feature as available.

Every workflow guide should explain prerequisites, the action, how to verify success, common failure paths, and any consequential limits. Keep future capabilities out of the operational steps. Use relative-to-site links with stable public slugs, and update navigation when adding a guide.

Build and check links after editing. A successful documentation build does not establish that commands or recovery procedures have been tested against a live cluster; report that validation separately.

See `CONTENT-REVIEW.md` for the content backlog and review findings.
