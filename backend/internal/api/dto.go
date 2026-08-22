package api

import "time"

// createPasteRequest is the JSON body of POST /api/paste. Data is an opaque,
// already-encrypted blob produced by the client — the server never inspects
// its contents. PasswordProtected is purely a UX hint for later clients (so
// ViewPastePage knows to prompt for a password); the server has no way to
// verify it and doesn't need to.
type createPasteRequest struct {
	Data              string `json:"data"`
	ExpireSeconds     int64  `json:"expireSeconds"`
	BurnAfterRead     bool   `json:"burnAfterRead"`
	PasswordProtected bool   `json:"passwordProtected"`
}

type createPasteResponse struct {
	ID string `json:"id"`
}

type getPasteResponse struct {
	Data              string    `json:"data"`
	BurnAfterRead     bool      `json:"burnAfterRead"`
	PasswordProtected bool      `json:"passwordProtected"`
	CreatedAt         time.Time `json:"createdAt"`
}

type errorResponse struct {
	Error string `json:"error"`
}
