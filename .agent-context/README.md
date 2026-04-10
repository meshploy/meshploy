# .agent-context/

Temporary context files for work that has been discussed and decided but not yet implemented.

Each file describes a specific pending improvement: the problem, the decision made,
and exactly what to change. Once a file's described work is implemented and merged,
**delete that file**.

When this directory is empty, delete the directory itself.

## Files

| File | What it tracks | Delete when |
|---|---|---|
| `headscale-id-storage.md` | Store `headscale_id` on the Node model | Column added, `enrichNode` and `DeleteNode` updated |
| `cli-tool.md` | Build a `meshploy` CLI binary replacing the bash scripts | CLI is implemented, built in CI, and `get.sh` downloads it |
