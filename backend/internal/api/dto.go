package api

import "time"

// createPasteRequest is the JSON body of POST /api/paste. Data is an opaque,
// already-encrypted blob produced by the client — the server validates its
// envelope structure but never its contents. PasswordProtected is purely a
// UX hint for later clients (so ViewPastePage knows to prompt for a
// password); the server has no way to verify it and doesn't need to.
type createPasteRequest struct {
	Data              string `json:"data"`
	ExpireSeconds     int64  `json:"expireSeconds"`
	BurnAfterRead     bool   `json:"burnAfterRead"`
	PasswordProtected bool   `json:"passwordProtected"`

	// ReadToken is base64url(SHA-256(key fragment)), computed by the
	// client. Reads must present the same value, so possession of the paste
	// ID alone grants nothing — not even the ability to burn the paste. It
	// is a hash of the fragment, so it does not help the server decrypt.
	ReadToken string `json:"readToken"`
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
