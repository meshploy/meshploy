/**
 * sync-docs.mjs
 *
 * Copies source markdown files from the monorepo into src/content/docs/,
 * prepending Starlight frontmatter. Run automatically before dev and build.
 *
 * Source of truth stays in the root of the monorepo — never edit the
 * generated files in src/content/docs/ directly.
 */

import { readFileSync, writeFileSync, mkdirSync, copyFileSync } from 'fs';
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
    src: 'README.md',
    dest: 'introduction.md',
    title: 'Introduction',
    description: 'What Meshploy is, who it\'s for, and what it can do.',
    // Extract only the intro — stop before Repository Structure
    extract: (content) => content.replace(/\n## Repository Structure[\s\S]*$/, ''),
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
    dest: 'architecture/concepts.md',
    title: 'Concepts & design decisions',
    description: 'Why each technical choice was made and what the alternatives were.',
  },
  {
    src: 'packages/db/README.md',
    dest: 'architecture/database.md',
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
  { src: `${webPublic}/favicon.svg`,              dest: resolve(__dirname, 'src/assets/logo.svg') },
];

for (const asset of staticAssets) {
  mkdirSync(dirname(asset.dest), { recursive: true });
  copyFileSync(asset.src, asset.dest);
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
