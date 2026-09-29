// Packs a folder from this computer the way `meshploy deploy` does, for a
// service built from an upload: a tar.gz of the folder's files, leaving out
// .git, installed dependencies, .env files (an example of one is kept) and
// what the folder's .gitignore and .meshployignore name.

/** The most the API takes, packed. */
export const MAX_PACKED_BYTES = 100 * 1024 * 1024

export interface PackedFolder {
  name: string
  archive: Blob
  files: number
  /** Left out by a rule rather than an ignore file: what a user may wonder about. */
  skipped: string[]
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

/** A folder dropped on the page. Skipped folders are not read at all, so a
 *  dropped app with its node_modules stays quick. */
export async function filesFromDrop(items: DataTransferItemList): Promise<{ name: string; files: FolderFile[]; skipped: string[] } | null> {
  const entry = Array.from(items)
    .map((i) => (i.kind === "file" ? i.webkitGetAsEntry?.() : null))
    .find((e): e is FileSystemEntry => !!e)
  if (!entry || !entry.isDirectory) return null
  const files: FolderFile[] = []
  const skipped: string[] = []
  const walk = async (dir: FileSystemDirectoryEntry, prefix: string): Promise<void> => {
    const reader = dir.createReader()
    // readEntries answers in batches until it answers none.
    for (;;) {
      const batch = await new Promise<FileSystemEntry[]>((res, rej) => reader.readEntries(res, rej))
      if (batch.length === 0) break
      for (const e of batch) {
        const path = prefix ? `${prefix}/${e.name}` : e.name
        if (ALWAYS_SKIPPED.has(e.name) || (e.isFile && isEnvFile(e.name))) {
          skipped.push(path)
          continue
        }
        if (e.isDirectory) await walk(e as FileSystemDirectoryEntry, path)
        else files.push({ path, file: await new Promise<File>((res, rej) => (e as FileSystemFileEntry).file(res, rej)) })
      }
    }
  }
  await walk(entry as FileSystemDirectoryEntry, "")
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

/** Whether a file is ignored: the last rule that matches it, or a folder it is in, decides. */
export function ignored(rules: IgnoreRule[], path: string): boolean {
  let out = false
  for (const r of rules) {
    const target = r.dirOnly ? path.slice(0, Math.max(0, path.lastIndexOf("/"))) : path
    if (target && r.re.test(target)) out = !r.negate
  }
  return out
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
  const rules: IgnoreRule[] = []
  for (const name of [".gitignore", ".meshployignore"]) {
    const f = kept.find((k) => k.path === name)
    if (!f) continue
    for (const line of (await f.file.text()).split("\n")) {
      const r = parseIgnoreLine(line)
      if (r) rules.push(r)
    }
  }
  const files = kept.filter((f) => !ignored(rules, f.path)).sort((a, b) => (a.path < b.path ? -1 : 1))
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
  return { name, archive: new Blob([archive], { type: "application/gzip" }), files: files.length, skipped: [...skipped].sort() }
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
