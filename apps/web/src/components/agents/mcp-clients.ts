/**
 * Per-client connection guides for the remote MCP endpoint.
 *
 * Agent platforms do not agree on where MCP servers are configured or what the
 * file looks like, so a bare URL leaves the reader to work it out. Each guide
 * names the exact file and renders the exact snippet, with the token as a
 * visible placeholder the reader swaps for one of the agent's own.
 */

/** Stand-in for the agent token in every snippet. Never a real token. */
export const MCP_TOKEN_PLACEHOLDER = "magt-your-token-here"

export type McpClientId =
  | "claude-code"
  | "cursor"
  | "vscode"
  | "codex"
  | "opencode"
  | "claude-web"

export interface McpClientGuide {
  id: McpClientId
  name: string
  /** Where the snippet goes. Null when the client is configured through its own UI. */
  file: string | null
  /** Rendered instead of a snippet when there is nothing to paste. */
  note?: string
  snippet?: (url: string, token: string) => string
}

export const MCP_CLIENT_GUIDES: readonly McpClientGuide[] = [
  {
    id: "claude-code",
    name: "Claude Code",
    file: ".mcp.json",
    snippet: (url, token) =>
      JSON.stringify(
        {
          mcpServers: {
            meshploy: { type: "http", url, headers: { Authorization: `Bearer ${token}` } },
          },
        },
        null,
        2
      ),
  },
  {
    id: "cursor",
    name: "Cursor",
    file: "~/.cursor/mcp.json",
    snippet: (url, token) =>
      JSON.stringify(
        {
          mcpServers: {
            meshploy: { url, headers: { Authorization: `Bearer ${token}` } },
          },
        },
        null,
        2
      ),
  },
  {
    id: "vscode",
    name: "VS Code",
    file: ".vscode/mcp.json",
    snippet: (url, token) =>
      JSON.stringify(
        {
          servers: {
            meshploy: { type: "http", url, headers: { Authorization: `Bearer ${token}` } },
          },
        },
        null,
        2
      ),
  },
  {
    id: "codex",
    name: "Codex CLI",
    file: "~/.codex/config.toml",
    // TOML, not JSON, and the table is `mcp_servers` with an underscore. The
    // header key is `http_headers`; a wrong shape here fails to parse the whole
    // config file rather than just this entry.
    snippet: (url, token) =>
      [
        "[mcp_servers.meshploy]",
        `url = "${url}"`,
        `http_headers = { Authorization = "Bearer ${token}" }`,
      ].join("\n"),
  },
  {
    id: "opencode",
    name: "OpenCode",
    file: "~/.config/opencode/opencode.json",
    snippet: (url, token) =>
      JSON.stringify(
        {
          mcp: {
            meshploy: {
              type: "remote",
              url,
              enabled: true,
              headers: { Authorization: `Bearer ${token}` },
            },
          },
        },
        null,
        2
      ),
  },
  {
    id: "claude-web",
    name: "Claude web and desktop",
    file: null,
    // Listed even though it cannot connect yet, because its absence reads as an
    // oversight and sends people hunting for a settings field that is not there.
    // Custom connectors authenticate by OAuth discovery against the server and
    // offer nowhere to paste a token, so a magt- token cannot be used here.
    note: "Custom connectors sign in through the server rather than taking a pasted token, so an agent token cannot be used here yet. Use one of the clients above, which accept a token directly.",
  },
]

export function getMcpClientGuide(id: McpClientId): McpClientGuide {
  return MCP_CLIENT_GUIDES.find((g) => g.id === id) ?? MCP_CLIENT_GUIDES[0]
}
