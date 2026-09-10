// Tags a build or deploy log line by level, so the console can mark errors in a
// long log instead of showing a wall of plain text. Heuristic by nature: build
// tools do not agree on a format, so this reads the words and symbols they use.
// No imports, so it can be exercised with plain Node.

export type LogLevel = "error" | "warning" | "success" | "info"

// ANSI escape sequences. The builder script colours its own lines, and the
// console printed the codes as text ("[1;34m[meshploy-build][0m").
// eslint-disable-next-line no-control-regex
const ANSI = /\x1b\[[0-9;?]*[ -/]*[@-~]/g

export function stripAnsi(line: string): string {
  return line.replace(ANSI, "")
}

// Reports of nothing wrong, which would otherwise read as errors or warnings.
const NOT_A_PROBLEM = /\b(0|no|zero) (errors?|warnings?|vulnerabilities)\b/i

const ERROR =
  /\berrors?\b(?!-)|\berr!|\bERR_[A-Z_]+|\bfatal\b|\bfail(ed|ure)?\b|\bpanic\b|\bexception\b|traceback|exit (code:?|status) ?[1-9]|❌|✘|\bbackoff\b|crashloop|oomkilled|\bevicted\b/i

const WARNING = /\bwarn(ing)?s?\b|\bdeprecat|⚠/i

// Meshploy's own milestones. Deliberately narrow: every "#12 DONE 0.3s" from
// BuildKit would otherwise turn the whole log green.
const SUCCESS =
  /^\s*(✔|✓)|\bbuild complete\b|\brollout complete\b|\bcloned successfully\b|\bsuccessfully (built|pushed|deployed)\b/i

export function classifyLogLine(line: string): LogLevel {
  if (NOT_A_PROBLEM.test(line)) return "info"
  if (ERROR.test(line)) return "error"
  if (WARNING.test(line)) return "warning"
  if (SUCCESS.test(line)) return "success"
  return "info"
}
