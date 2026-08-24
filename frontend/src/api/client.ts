// Thin, typed fetch wrapper for the clipper REST API. Mirrors the Go DTOs in
// backend/internal/api/dto.go.

export interface CreatePasteRequest {
  data: string
  expireSeconds: number
  burnAfterRead: boolean
  passwordProtected: boolean
  /** base64url(SHA-256(key fragment)); required on every later read. */
  readToken: string
}

export interface CreatePasteResponse {
  id: string
}

export interface GetPasteResponse {
  data: string
  burnAfterRead: boolean
  passwordProtected: boolean
  createdAt: string
}

// Carrying the read token in a header rather than the query string keeps it
// out of proxy access logs, and makes the request non-simple so a
// cross-origin read is stopped at the browser's preflight.
const READ_TOKEN_HEADER = "X-Paste-Read-Token"

export class ApiError extends Error {
  status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = "ApiError"
    this.status = status
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    ...init,
    // The API uses no cookies or ambient credentials at all; saying so
    // explicitly keeps a future same-site cookie from silently becoming an
    // authentication signal this app never intended to have.
    credentials: "omit",
    referrerPolicy: "no-referrer",
    headers: { "Content-Type": "application/json", ...init?.headers },
  })

  if (!res.ok) {
    let message = res.statusText
    try {
      const body = (await res.json()) as { error?: string }
      if (body.error) message = body.error
    } catch {
      // non-JSON error body; fall back to statusText
    }
    throw new ApiError(res.status, message)
  }

  return res.json() as Promise<T>
}

export interface ClientConfig {
  /** false on a read-only instance (MODE=read): hide the create form. */
  createEnabled: boolean
  /** false on a write-only instance (MODE=write): retrieval is disabled. */
  readEnabled: boolean
}

export function getClientConfig(): Promise<ClientConfig> {
  return request<ClientConfig>("/api/config")
}

export function createPaste(req: CreatePasteRequest): Promise<CreatePasteResponse> {
  return request<CreatePasteResponse>("/api/paste", {
    method: "POST",
    body: JSON.stringify(req),
  })
}

export function getPaste(id: string, readToken: string): Promise<GetPasteResponse> {
  return request<GetPasteResponse>(`/api/paste/${encodeURIComponent(id)}`, {
    headers: { [READ_TOKEN_HEADER]: readToken },
  })
}
