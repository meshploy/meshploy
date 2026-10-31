// Packs a folder from this computer the way `meshploy deploy` does, for a
// service built from an upload: a tar.gz of the folder's files, leaving out
// .git, installed dependencies, .env files (an example of one is kept), what
// the folder's .meshployignore names, and what its .gitignore files name,
// each for the folder it sits in.

import { detectVariables, type FoundVariable } from "./detect-variables"

/** The most the API takes, packed. */
export const MAX_PACKED_BYTES = 100 * 1024 * 1024

export interface PackedFolder {
  name: string
  archive: Blob
  files: number
  /** Left out by a rule rather than an ignore file: what a user may wonder about. */
  skipped: string[]
  /** What the folder's example env files name and its code reads. */
  variables: FoundVariable[]
}

interface FolderFile {
  /** Relative to the folder, slash-separated. */
  path: string
  file: File
}

const ALWAYS_SKIPPED = new Set([".git", "node_modules", ".venv", "venv", "__pycache__", ".DS_Store"])

export function isEnvFile(name: string): boolean {
  if (name !== ".env" && !name.startsWith(".env.")) return false
  return ![".example", ".sample", ".template"].some((k) => name.endsWith(k))
}

/** The part of a path a rule skips, or null: `node_modules` for `node_modules/a/b.js`. */
function skippedBy(path: string): string | null {
  const parts = path.split("/")
  for (let i = 0; i < parts.length; i++) {
    if (ALWAYS_SKIPPED.has(parts[i]) || (i === parts.length - 1 && isEnvFile(parts[i]))) {
      return parts.slice(0, i + 1).join("/")
    }
  }
  return null
}

// ─── Reading a folder ────────────────────────────────────────────────────────

/** A folder chosen with <input webkitdirectory>: every path starts with its name. */
export function filesFromInput(list: FileList): { name: string; files: FolderFile[] } {
  const files: FolderFile[] = []
  let name = "folder"
  for (const file of Array.from(list)) {
    const rel = file.webkitRelativePath || file.name
    const slash = rel.indexOf("/")
    if (slash < 0) {
      files.push({ path: rel, file })
      continue
    }
    name = rel.slice(0, slash)
    files.push({ path: rel.slice(slash + 1), file })
  }
  return { name, files }
}

/** A folder dropped on the page. What is skipped or ignored is not read at
 *  all, so a dropped app with its node_modules and build output stays quick.
 *  onFile counts the files read so far. */
export async function filesFromDrop(
  items: DataTransferItemList,
  onFile?: (read: number) => void,
): Promise<{ name: string; files: FolderFile[]; skipped: string[] }> {
  const entry = Array.from(items)
    .map((i) => (i.kind === "file" ? i.webkitGetAsEntry?.() : null))
    .find((e): e is FileSystemEntry => !!e)
  if (!entry) throw new Error("Your browser did not hand over the folder. Try Choose a folder instead.")
  if (!entry.isDirectory) throw new Error("Drop a folder, not a file.")
  const files: FolderFile[] = []
  const skipped: string[] = []
  const scopes = new IgnoreScopes()
  const fileOf = (e: FileSystemEntry) => new Promise<File>((res, rej) => (e as FileSystemFileEntry).file(res, rej))
  const walk = async (dir: FileSystemDirectoryEntry, prefix: string): Promise<void> => {
    const reader = dir.createReader()
    const entries: FileSystemEntry[] = []
    // readEntries answers in batches until it answers none.
    for (;;) {
      const batch = await new Promise<FileSystemEntry[]>((res, rej) => reader.readEntries(res, rej))
      if (batch.length === 0) break
      entries.push(...batch)
    }
    // This folder's ignore files first: they decide what in it is read.
    for (const name of prefix ? [".gitignore"] : [".gitignore", ".meshployignore"]) {
      const f = entries.find((e) => e.isFile && e.name === name)
      if (f) scopes.add(prefix, await (await fileOf(f)).text())
    }
    for (const e of entries) {
      const path = prefix ? `${prefix}/${e.name}` : e.name
      if (ALWAYS_SKIPPED.has(e.name) || (e.isFile && isEnvFile(e.name))) {
        skipped.push(path)
        continue
      }
      if (scopes.ignored(path, e.isDirectory)) continue
      if (e.isDirectory) await walk(e as FileSystemDirectoryEntry, path)
      else {
        files.push({ path, file: await fileOf(e) })
        onFile?.(files.length)
      }
    }
  }
  await walk(entry as FileSystemDirectoryEntry, "")
  if (files.length === 0 && skipped.length === 0) {
    throw new Error("Your browser handed over the folder empty. Try Choose a folder instead.")
  }
  return { name: entry.name, files, skipped }
}

// ─── Ignore files ────────────────────────────────────────────────────────────

interface IgnoreRule { re: RegExp; negate: boolean; dirOnly: boolean }

/** A .gitignore line, as the CLI reads it: # comments, !, a trailing / for a
 *  folder, a leading or inner / anchoring it, and *, ? and **. */
export function parseIgnoreLine(raw: string): IgnoreRule | null {
  let line = raw.replace(/[ \t\r]+$/, "")
  if (!line || line.startsWith("#")) return null
  let negate = false
  let dirOnly = false
  if (line.startsWith("!")) { negate = true; line = line.slice(1) }
  if (line.endsWith("/")) { dirOnly = true; line = line.slice(0, -1) }
  const anchored = line.includes("/")
  line = line.replace(/^\//, "")
  if (!line) return null
  let re = anchored ? "^" : "(^|.*/)"
  for (let i = 0; i < line.length; i++) {
    const c = line[i]
    if (c === "*" && line[i + 1] === "*") {
      i++
      if (line[i + 1] === "/") { i++; re += "(.*/)?" } else re += ".*"
    } else if (c === "*") re += "[^/]*"
    else if (c === "?") re += "[^/]"
    else re += c.replace(/[.+^${}()|[\]\\]/g, "\\$&")
  }
  return { re: new RegExp(re + "(/.*)?$"), negate, dirOnly }
}

/** Whether path is ignored by rules, carrying on from what rules before
 *  them decided: the last rule that matches it, or a folder it is in, decides. */
function decide(rules: IgnoreRule[], path: string, isDir: boolean, was: boolean): boolean {
  let out = was
  for (const r of rules) {
    if (r.dirOnly && !isDir && !r.re.test(path.slice(0, Math.max(0, path.lastIndexOf("/"))))) continue
    if (r.re.test(path)) out = !r.negate
  }
  return out
}

/** Whether a path is ignored by one set of rules. */
export function ignored(rules: IgnoreRule[], path: string, isDir = false): boolean {
  return decide(rules, path, isDir, false)
}

/** The rules of every folder that has an ignore file. A path is matched
 *  against each of its folders' rules from the top down, relative to that
 *  folder, so a deeper .gitignore overrides one above it. */
export class IgnoreScopes {
  private scopes = new Map<string, IgnoreRule[]>()

  /** Adds an ignore file's text for the folder at base ("" for the top). */
  add(base: string, text: string) {
    const rules = text.split("\n").map(parseIgnoreLine).filter((r): r is IgnoreRule => !!r)
    if (rules.length > 0) this.scopes.set(base, [...(this.scopes.get(base) ?? []), ...rules])
  }

  ignored(path: string, isDir: boolean): boolean {
    let out = false
    // i is where the folder being looked at ends in path; -1 is the top.
    for (let i = -1; ; ) {
      const rules = this.scopes.get(i < 0 ? "" : path.slice(0, i))
      if (rules) out = decide(rules, i < 0 ? path : path.slice(i + 1), isDir, out)
      const j = path.indexOf("/", i + 1)
      if (j < 0) return out
      i = j
    }
  }
}

// ─── Packing ─────────────────────────────────────────────────────────────────

export async function packFolder(name: string, all: FolderFile[], preSkipped: string[] = []): Promise<PackedFolder> {
  const skipped = new Set(preSkipped)
  const kept: FolderFile[] = []
  for (const f of all) {
    const by = skippedBy(f.path)
    if (by) skipped.add(by)
    else kept.push(f)
  }
  // Each .gitignore not itself ignored applies to its folder, the shallowest
  // first, so a folder's rules are in place before those below it are judged.
  const scopes = new IgnoreScopes()
  const ignoreFiles = kept
    .filter((f) => f.path === ".meshployignore" || f.path === ".gitignore" || f.path.endsWith("/.gitignore"))
    .sort((a, b) => a.path.split("/").length - b.path.split("/").length)
  for (const f of ignoreFiles) {
    const base = f.path.includes("/") ? f.path.slice(0, f.path.lastIndexOf("/")) : ""
    if (base && scopes.ignored(base, true)) continue
    scopes.add(base, await f.file.text())
  }
  // A file in an ignored folder stays out whatever is said of the file, as
  // when the folder is walked and the ignored folder never opened.
  const out = (path: string) => {
    for (let i = path.indexOf("/"); i >= 0; i = path.indexOf("/", i + 1)) {
      if (scopes.ignored(path.slice(0, i), true)) return true
    }
    return scopes.ignored(path, false)
  }
  const files = kept.filter((f) => !out(f.path)).sort((a, b) => (a.path < b.path ? -1 : 1))
  if (files.length === 0) throw new Error("Nothing to upload: every file in the folder is left out.")
  const unpacked = files.reduce((n, f) => n + f.file.size, 0)
  if (unpacked > 4 * MAX_PACKED_BYTES) {
    throw new Error(`The folder is ${Math.round(unpacked / 1048576)} MB: leave out build output with a .meshployignore.`)
  }

  const parts: BlobPart[] = []
  for (const f of files) {
    parts.push(...(tarEntry(f.path, f.file.size) as BlobPart[]), f.file, padding(f.file.size) as BlobPart)
  }
  parts.push(new Uint8Array(1024))
  const tar = new Blob(parts)
  const archive = await new Response(tar.stream().pipeThrough(new CompressionStream("gzip"))).blob()
  if (archive.size > MAX_PACKED_BYTES) {
    throw new Error(`The folder is over ${MAX_PACKED_BYTES / 1048576} MB packed: leave out build output with a .meshployignore.`)
  }
  return {
    name, archive: new Blob([archive], { type: "application/gzip" }), files: files.length, skipped: [...skipped].sort(),
    variables: await detectVariables(files),
  }
}

const encoder = new TextEncoder()

function padding(size: number): Uint8Array {
  return new Uint8Array((512 - (size % 512)) % 512)
}

/** The header block(s) for one file. A path too long for the ustar fields
 *  goes in a PAX header before it. Every file is 0644 with no date, so the
 *  same files pack the same. */
function tarEntry(path: string, size: number): Uint8Array[] {
  const out: Uint8Array[] = []
  let name = path
  let prefix = ""
  if (encoder.encode(path).length > 100) {
    const cut = path.lastIndexOf("/", path.length - 1)
    const [p, n] = cut > 0 ? [path.slice(0, cut), path.slice(cut + 1)] : ["", path]
    if (encoder.encode(p).length <= 155 && encoder.encode(n).length <= 100 && cut > 0) {
      prefix = p
      name = n
    } else {
      const record = paxRecord("path", path)
      out.push(header("PaxHeader", record.length, "x", ""), record, padding(record.length))
      name = path.slice(-100)
    }
  }
  out.push(header(name, size, "0", prefix))
  return out
}

function paxRecord(key: string, value: string): Uint8Array {
  // The record's length counts its own digits.
  const body = encoder.encode(` ${key}=${value}\n`).length
  let len = body + String(body).length
  if (String(len).length > String(body).length) len = body + String(len).length
  return encoder.encode(`${len} ${key}=${value}\n`)
}

function header(name: string, size: number, type: string, prefix: string): Uint8Array {
  const h = new Uint8Array(512)
  const put = (s: string, at: number, len: number) => h.set(encoder.encode(s).slice(0, len), at)
  const octal = (n: number, len: number) => n.toString(8).padStart(len - 1, "0")
  put(name, 0, 100)
  put(octal(0o644, 8), 100, 8)
  put(octal(0, 8), 108, 8)
  put(octal(0, 8), 116, 8)
  put(octal(size, 12), 124, 12)
  put(octal(0, 12), 136, 12)
  put("        ", 148, 8)
  put(type, 156, 1)
  put("ustar\0", 257, 6)
  put("00", 263, 2)
  put(prefix, 345, 155)
  let sum = 0
  for (const b of h) sum += b
  put(octal(sum, 7) + "\0", 148, 8)
  return h
}
