/**
 * The versions a managed database is offered at, newest first. A version is
 * the engine image's tag (postgres:18, mysql:9.7, ...), so each one here is a
 * tag its registry publishes. `recommended` is what a new database starts at:
 * the newest release with long support, which is not always the newest.
 *
 * The API's default when no version is sent is the same
 * (packages/server/service/workload.go, defaultDBVersion).
 */
export type DbEngine = "postgres" | "mysql" | "redis" | "mongodb" | "dragonfly" | "clickhouse"

export interface DbVersion {
  tag: string
  /** Shown beside the tag in the version picker. */
  note?: string
}

export const DB_VERSIONS: Record<DbEngine, { versions: DbVersion[]; recommended: string }> = {
  postgres: {
    recommended: "18",
    versions: [
      { tag: "18", note: "newest" },
      { tag: "17" },
      { tag: "16" },
      { tag: "15" },
      { tag: "14" },
      { tag: "13", note: "end of life" },
    ],
  },
  mysql: {
    recommended: "9.7",
    versions: [
      { tag: "26", note: "newest, innovation release with short support" },
      { tag: "9.7", note: "long-term support" },
      { tag: "8.4", note: "long-term support" },
      { tag: "8.0", note: "end of life" },
      { tag: "5.7", note: "end of life" },
    ],
  },
  redis: {
    recommended: "8",
    versions: [
      { tag: "8", note: "newest" },
      { tag: "7" },
      { tag: "6" },
    ],
  },
  mongodb: {
    recommended: "8",
    versions: [
      { tag: "9", note: "newest" },
      { tag: "8" },
      { tag: "7" },
      { tag: "6", note: "end of life" },
    ],
  },
  dragonfly: {
    recommended: "v1.40.2",
    versions: [
      { tag: "v1.40.2", note: "newest" },
      { tag: "v1.39.0" },
      { tag: "latest", note: "moves to each new release" },
    ],
  },
  clickhouse: {
    recommended: "26.8",
    versions: [
      { tag: "26.10", note: "newest" },
      { tag: "26.8", note: "long-term support" },
      { tag: "25.8", note: "long-term support" },
      { tag: "24.8", note: "end of life" },
      { tag: "23.8", note: "end of life" },
    ],
  },
}

/** The tags an engine is offered at, newest first. */
export function dbVersionTags(engine: string): string[] {
  return DB_VERSIONS[engine as DbEngine]?.versions.map((v) => v.tag) ?? ["latest"]
}

/** What a new database of this engine starts at. */
export function recommendedDbVersion(engine: string): string {
  return DB_VERSIONS[engine as DbEngine]?.recommended ?? "latest"
}

/** The note shown beside a tag, if it has one. */
export function dbVersionNote(engine: string, tag: string): string | undefined {
  return DB_VERSIONS[engine as DbEngine]?.versions.find((v) => v.tag === tag)?.note
}
