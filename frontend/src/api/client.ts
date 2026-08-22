// Thin, typed fetch wrapper for the clipper REST API. Mirrors the Go DTOs in
// backend/internal/api/dto.go.

export interface CreatePasteRequest {
  data: string
  expireSeconds: number
  burnAfterRead: boolean
  passwordProtected: boolean
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

export function createPaste(req: CreatePasteRequest): Promise<CreatePasteResponse> {
  return request<CreatePasteResponse>("/api/paste", {
    method: "POST",
    body: JSON.stringify(req),
  })
}

export function getPaste(id: string): Promise<GetPasteResponse> {
  return request<GetPasteResponse>(`/api/paste/${encodeURIComponent(id)}`)
}
