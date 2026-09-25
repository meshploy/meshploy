/**
 * sync-docs.mjs
 *
 * Copies source markdown files from the monorepo into src/content/docs/,
 * prepending Starlight frontmatter. Run automatically before dev and build.
 *
 * Source of truth stays in the root of the monorepo — never edit the
 * generated files in src/content/docs/ directly.
 */

import { readFileSync, writeFileSync, mkdirSync, copyFileSync, readdirSync, rmSync } from 'fs';
import { dirname, resolve } from 'path';
import { fileURLToPath } from 'url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const root = resolve(__dirname, '../..');
const out = resolve(__dirname, 'src/content/docs');

/**
 * Each entry: { src, dest, title, description }
 * src  — path relative to monorepo root
 * dest — path relative to src/content/docs (without leading slash)
 */
const docs = [
  // ── Getting Started ──────────────────────────────────────────────────────────
  {
    src: 'apps/docs/content/introduction.md',
    dest: 'introduction.md',
    title: 'Introduction',
    description: 'What Meshploy is, who it\'s for, and what it can do.',
  },
  {
    src: 'README.md',
    dest: 'self-hosting.md',
    title: 'Self-hosting',
    description: 'Install Meshploy on your own servers — DNS setup, supported distros, managing your installation.',
    extract: (content) => {
      const match = content.match(/(?=## Self-Hosting)([\s\S]*?)(?=\n## API)/);
      return match ? match[0] : '';
    },
  },

  // ── Architecture ─────────────────────────────────────────────────────────────
  {
    src: 'HOW_IT_WORKS.md',
    dest: 'architecture/how-it-works.md',
    title: 'How it works',
    description: 'Why NS delegation, why dark workers, how TLS works, the mesh architecture explained.',
  },
  {
    src: 'CONCEPTS.md',
    dest: 'architecture/design-decisions.md',
    title: 'Design decisions',
    description: 'How Meshploy is built, why each technical choice was made, and what the alternatives were.',
  },

  // ── Reference ────────────────────────────────────────────────────────────────
  {
    src: 'packages/db/README.md',
    dest: 'reference/database.md',
    title: 'Database schema',
    description: 'Shared GORM models, migrations, encryption, and the CE/EE open-core boundary.',
  },

  // ── CLI ───────────────────────────────────────────────────────────────────────
  {
    src: 'apps/cli/README.md',
    dest: 'cli/reference.md',
    title: 'CLI Reference',
    description: 'All meshploy CLI commands, flags, config file, and node workflows.',
  },

  // ── API ───────────────────────────────────────────────────────────────────────
  {
    src: 'apps/api/README.md',
    dest: 'api/reference.md',
    title: 'API Reference',
    description: 'All REST routes, authentication, and request/response shapes.',
  },

  // ── Contributing ──────────────────────────────────────────────────────────────
  {
    src: 'CONTRIBUTING.md',
    dest: 'contributing/guide.md',
    title: 'Contributing guide',
    description: 'Dev setup, coding guidelines, local vs VPS testing, PR process.',
  },
  {
    src: 'SECURITY.md',
    dest: 'contributing/security.md',
    title: 'Security policy',
    description: 'How to report vulnerabilities and what\'s in scope.',
  },
  {
    src: 'TODO.md',
    dest: 'contributing/roadmap.md',
    title: 'Roadmap',
    description: 'Planned features and upcoming work.',
  },
];

// ── Static assets synced from apps/web ───────────────────────────────────────
const webPublic = resolve(root, 'apps/web/public');
const staticAssets = [
  { src: `${webPublic}/favicon.svg`,              dest: resolve(__dirname, 'public/favicon.svg') },
  { src: `${webPublic}/favicon.ico`,              dest: resolve(__dirname, 'public/favicon.ico') },
  { src: `${webPublic}/apple-touch-icon.png`,     dest: resolve(__dirname, 'public/apple-touch-icon.png') },
  { src: `${webPublic}/fonts/GeistMono-Regular.woff2`, dest: resolve(__dirname, 'public/fonts/GeistMono-Regular.woff2') },
  { src: `${webPublic}/fonts/GeistMono-Medium.woff2`,  dest: resolve(__dirname, 'public/fonts/GeistMono-Medium.woff2') },
  { src: `${webPublic}/fonts/Manrope-400.ttf`,         dest: resolve(__dirname, 'public/fonts/Manrope-400.ttf') },
  { src: `${webPublic}/fonts/Manrope-500.ttf`,         dest: resolve(__dirname, 'public/fonts/Manrope-500.ttf') },
  { src: `${webPublic}/fonts/Manrope-600.ttf`,         dest: resolve(__dirname, 'public/fonts/Manrope-600.ttf') },
  { src: `${webPublic}/fonts/Manrope-700.ttf`,         dest: resolve(__dirname, 'public/fonts/Manrope-700.ttf') },
  // src/assets/logo.svg is deliberately NOT synced from favicon.svg. It used to
  // be, back when the two were the same file — but the favicon now draws its
  // diagonals at weight 4 so the mark survives 16px, while the header logo
  // renders at 24px+ and uses the full weight 7. Copying one onto the other
  // would put the small-size variant in a logo slot.
];

for (const asset of staticAssets) {
  mkdirSync(dirname(asset.dest), { recursive: true });
  copyFileSync(asset.src, asset.dest);
}

// ── Link rewriting ────────────────────────────────────────────────────────────
// Maps source-relative .md paths → Starlight slugs (or GitHub URLs for files
// that have no corresponding docs page).
const GITHUB_BASE = 'https://github.com/meshploy/meshploy/blob/main';
const linkMap = {
  './CONCEPTS.md':           '/architecture/design-decisions',
  './CONTRIBUTING.md':       '/contributing/guide',
  './HOW_IT_WORKS.md':       '/architecture/how-it-works',
  './packages/db/README.md': '/reference/database',
  './SECURITY.md':           '/contributing/security',
  './TODO.md':               '/contributing/roadmap',
  './apps/cli/README.md':    '/cli/reference',
  './apps/api/README.md':    '/api/reference',
  // No docs page — point to GitHub
  './apps/proxy/README.md':  `${GITHUB_BASE}/apps/proxy/README.md`,
  './apps/web/AGENTS.md':    `${GITHUB_BASE}/apps/web/AGENTS.md`,
  './CLAUDE.md':             `${GITHUB_BASE}/CLAUDE.md`,
};

// Anchor-only links that were section headings in the source README but are
// now separate pages in the docs site.
const anchorMap = {
  '#self-hosting': '/self-hosting',
};

function rewriteLinks(content) {
  // Rewrite .md file links
  content = content.replace(/\]\(([^)]+\.md)(#[^)]*)?\)/g, (match, path, anchor = '') => {
    const resolved = linkMap[path];
    if (!resolved) return match;
    return `](${resolved}${anchor})`;
  });
  // Rewrite anchor-only links that correspond to split-out pages
  content = content.replace(/\]\((#[^)]+)\)/g, (match, anchor) => {
    const resolved = anchorMap[anchor];
    if (!resolved) return match;
    return `](${resolved})`;
  });
  return content;
}

let synced = 0;

for (const doc of docs) {
  const srcPath = resolve(root, doc.src);
  const destPath = resolve(out, doc.dest);

  let content = readFileSync(srcPath, 'utf8');

  if (doc.extract) {
    content = doc.extract(content);
  }

  // Strip any existing frontmatter from the source file (--- ... ---)
  content = content.replace(/^---[\s\S]*?---\n/, '');

  // Strip the leading H1 — Starlight renders its own from the frontmatter title
  content = content.trimStart().replace(/^#[^\n]*\r?\n?/, '');

  // Rewrite internal .md links to Starlight slugs
  content = rewriteLinks(content);

  const frontmatter = [
    '---',
    `title: ${doc.title}`,
    `description: ${doc.description}`,
    '---',
    '',
    '',
  ].join('\n');

  mkdirSync(dirname(destPath), { recursive: true });
  writeFileSync(destPath, frontmatter + content, 'utf8');
  console.log(`  synced  ${doc.src} → apps/docs/src/content/docs/${doc.dest}`);
  synced++;
}

console.log(`\n✔  ${synced} docs synced.\n`);

// ── Concepts: the help topics ─────────────────────────────────────────────────
// packages/help/topics/*.md is one text read in three places: the console's
// help drawer, the MCP server, and here. Its own markup is turned into
// Starlight's: a heading's {#id} becomes an anchor that keeps the console's
// links working, a diagram becomes its text form, and the Terms section a
// glossary pointing at the sections behind each term.
const helpDir = resolve(root, 'packages/help/topics');
const conceptsOut = resolve(out, 'concepts');
rmSync(conceptsOut, { recursive: true, force: true });
mkdirSync(conceptsOut, { recursive: true });
for (const file of readdirSync(helpDir).filter((f) => f.endsWith('.md')).sort()) {
  const raw = readFileSync(resolve(helpDir, file), 'utf8');
  const header = raw.match(/^---\n([\s\S]*?)\n---\n/);
  if (!header) throw new Error(`packages/help/topics/${file}: no header`);
  const field = (name) => (header[1].match(new RegExp(`^${name}:\\s*(.*)$`, 'm')) || [])[1] || '';
  const id = field('id');
  let body = raw.slice(header[0].length);
  body = body.replace(/^## (.+?) \{#([a-z0-9-]+)\}\s*$/gm, '<span id="$2"></span>\n\n## $1');
  body = body.replace(/^::diagram [a-z-]+ \| (.+)$/gm, '```text\n$1\n```');
  body = body.replace(/^- \*\*(.+?)\*\* \{#([a-z0-9-]+) -> ([a-z0-9-]+)\}: (.+)$/gm, '- <span id="term-$2"></span>**$1**: $4 [More](#$3)');
  // Quoted: a summary with a colon in it is not valid YAML bare.
  const frontmatter = ['---', `title: ${JSON.stringify(field('title'))}`, `description: ${JSON.stringify(field('summary'))}`, '---', '', ''].join('\n');
  writeFileSync(resolve(conceptsOut, `${id}.md`), frontmatter + body, 'utf8');
  console.log(`  synced  packages/help/topics/${file} → apps/docs/src/content/docs/concepts/${id}.md`);
  synced++;
}
