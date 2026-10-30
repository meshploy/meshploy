/**
 * The variables a folder's example env files name and its code reads, offered
 * when an app is made so it starts with what it needs. Nothing found is
 * required: code that reads a name may give it a default. The server finds a
 * repository's the same way (packages/server/service/detect_variables.go).
 */

export interface FoundVariable {
  name: string
  /** "example" for a name in an example env file, "code" for one the code reads. */
  from: "example" | "code"
}

const EXAMPLE_FILES = new Set([".env.example", ".env.sample", ".env.template", "example.env", ".env.dist"])
const EXAMPLE_LINE = /^\s*(?:export\s+)?([A-Z][A-Z0-9_]*)\s*=/gm
const READS_ENV = new RegExp(
  [
    String.raw`process\.env\.([A-Z][A-Z0-9_]*)`,
    String.raw`process\.env\[\s*['"]([A-Z][A-Z0-9_]*)['"]\s*\]`,
    String.raw`os\.environ\[\s*['"]([A-Z][A-Z0-9_]*)['"]\s*\]`,
    String.raw`os\.(?:environ\.get|getenv)\(\s*['"]([A-Z][A-Z0-9_]*)['"]`,
    String.raw`os\.(?:Getenv|LookupEnv)\(\s*"([A-Z][A-Z0-9_]*)"`,
    String.raw`ENV(?:\.fetch\(\s*|\[\s*)['"]([A-Z][A-Z0-9_]*)['"]`,
    String.raw`getenv\(\s*['"]([A-Z][A-Z0-9_]*)['"]`,
    String.raw`\$_ENV\[\s*['"]([A-Z][A-Z0-9_]*)['"]`,
    String.raw`env::var\(\s*"([A-Z][A-Z0-9_]*)"`,
    String.raw`System\.getenv\(\s*"([A-Z][A-Z0-9_]*)"`,
  ].join("|"),
  "g",
)
const CODE_EXT = new Set([".js", ".mjs", ".cjs", ".ts", ".mts", ".tsx", ".jsx", ".py", ".go", ".rb", ".php", ".rs", ".java", ".kt"])
/** Directories that are never an app's own code. */
const SKIP_DIRS = new Set([".git", "node_modules", ".venv", "venv", "__pycache__", "dist", "build", "tests", "test", "vendor", ".next"])
/** Set by the system or the runtime, never by the person deploying. */
const AMBIENT = new Set(["NODE_ENV", "PATH", "HOME", "HOSTNAME", "PWD", "USER", "SHELL", "TERM", "LANG", "TMPDIR", "TZ", "CI"])
const MAX_READ = 200 * 1024
const MAX_FOUND = 40

const base = (p: string) => p.slice(p.lastIndexOf("/") + 1)
const ext = (p: string) => { const b = base(p); const i = b.lastIndexOf("."); return i > 0 ? b.slice(i) : "" }
const skipped = (p: string) => p.split("/").slice(0, -1).some((d) => SKIP_DIRS.has(d))

/** Reads the files that can name a variable and lists them, the example's first. */
export async function detectVariables(files: { path: string; file: Blob }[]): Promise<FoundVariable[]> {
  const seen = new Set<string>()
  const example: string[] = []
  const code: string[] = []
  const add = (list: string[], name: string | undefined) => {
    if (!name || seen.has(name) || AMBIENT.has(name)) return
    seen.add(name)
    list.push(name)
  }
  const sorted = [...files].sort((a, b) => (a.path < b.path ? -1 : 1))
  for (const f of sorted) {
    if (!EXAMPLE_FILES.has(base(f.path)) || skipped(f.path)) continue
    for (const m of (await f.file.text()).matchAll(EXAMPLE_LINE)) add(example, m[1])
  }
  for (const f of sorted) {
    if (!CODE_EXT.has(ext(f.path)) || skipped(f.path) || f.file.size > MAX_READ || f.path.endsWith(".min.js")) continue
    for (const m of (await f.file.text()).matchAll(READS_ENV)) add(code, m.slice(1).find(Boolean))
  }
  code.sort()
  return [
    ...example.map((name) => ({ name, from: "example" as const })),
    ...code.map((name) => ({ name, from: "code" as const })),
  ].slice(0, MAX_FOUND)
}

/** A name whose value should not be shown as it is typed. */
export function looksSecret(name: string): boolean {
  return /(TOKEN|SECRET|PASSWORD|PASSWD|_KEY$|^KEY$|APIKEY|API_KEY|PRIVATE|CREDENTIAL|_DSN$|_URL$)/.test(name)
}
