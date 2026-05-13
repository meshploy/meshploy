declare global {
  interface Window {
    __MESHPLOY_CONFIG__?: { apiUrl: string }
  }
}

// In production: BASE is "" — all paths (/api/v1/...) are relative, Caddy routes /api/* to port 4000.
// In dev: BASE is "http://localhost:4000" so the full path becomes http://localhost:4000/api/v1/...
const BASE =
  window.__MESHPLOY_CONFIG__?.apiUrl ??
  import.meta.env.VITE_API_URL ??
  "http://localhost:4000"

export class ApiError extends Error {
  constructor(
    public status: number,
    public detail: string,
    public title?: string
  ) {
    super(detail)
    this.name = "ApiError"
  }
}

export async function apiFetch<T>(
  path: string,
  options: RequestInit = {},
  token?: string | null
): Promise<T> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    ...(options.headers as Record<string, string>),
  }
  if (token) headers["Authorization"] = `Bearer ${token}`

  const res = await fetch(`${BASE}${path}`, { ...options, headers })

  if (!res.ok) {
    // Huma returns RFC 7807 problem details
    let detail = res.statusText
    let title: string | undefined
    try {
      const body = await res.json()
      detail = body.detail ?? body.message ?? detail
      title = body.title
    } catch {}
    throw new ApiError(res.status, detail, title)
  }

  // 204 No Content
  if (res.status === 204) return undefined as T

  return res.json() as Promise<T>
}
