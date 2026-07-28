import { apiFetch } from "./core"

export interface TerminalTicket {
  ticket: string
  expires_at: string
}

/**
 * Mint a single-use ticket for a terminal WebSocket.
 *
 * The browser WebSocket API cannot set an Authorization header, so the
 * credential has to travel in the URL — where it is captured by request logs.
 * Sending the session token there meant every terminal session wrote a
 * full-privilege bearer token to the API's logs in cleartext. This ticket is
 * single-use and expires in seconds, so a logged copy is inert.
 *
 * Mint immediately before opening the socket; it is not worth caching.
 */
export function createTerminalTicket(token: string): Promise<TerminalTicket> {
  return apiFetch<TerminalTicket>("/api/v1/terminal/ticket", { method: "POST" }, token)
}
