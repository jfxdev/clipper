package paste

import (
	"errors"
	"strings"
	"testing"
)

func testEnvelope(ctLen int) string {
	return `{"v":1,"iter":0,"iv":"` + strings.Repeat("A", 16) + `","ct":"` + strings.Repeat("A", ctLen) + `"}`
}

func TestValidate(t *testing.T) {
	small := testEnvelope(22)
	cases := []struct {
		name    string
		data    string
		maxSize int64
		wantErr error
	}{
		{"empty", "", 10, ErrEmpty},
		{"too large", small, 10, ErrTooLarge},
		{"ok", small, 1024, nil},
		{"exact limit ok", small, int64(len(small)), nil},
		{"not json", "hello", 1024, ErrMalformed},
		{"wrong version", `{"v":9,"iter":0,"iv":"AAAAAAAAAAAAAAAA","ct":"AAAAAAAAAAAAAAAAAAAAAA"}`, 1024, ErrMalformed},
		{"negative iterations", `{"v":1,"iter":-1,"iv":"AAAAAAAAAAAAAAAA","ct":"AAAAAAAAAAAAAAAAAAAAAA"}`, 1024, ErrMalformed},
		{"iterations too high", `{"v":1,"iter":100000000,"iv":"AAAAAAAAAAAAAAAA","ct":"AAAAAAAAAAAAAAAAAAAAAA"}`, 1024, ErrMalformed},
		{"short iv", `{"v":1,"iter":0,"iv":"AAAA","ct":"AAAAAAAAAAAAAAAAAAAAAA"}`, 1024, ErrMalformed},
		{"short ct", `{"v":1,"iter":0,"iv":"AAAAAAAAAAAAAAAA","ct":"AAAA"}`, 1024, ErrMalformed},
		{"non base64 ct", `{"v":1,"iter":0,"iv":"AAAAAAAAAAAAAAAA","ct":"AAAA/AAAAAAAAAAAAAAAAA"}`, 1024, ErrMalformed},
		{"unknown field", `{"v":1,"iter":0,"iv":"AAAAAAAAAAAAAAAA","ct":"AAAAAAAAAAAAAAAAAAAAAA","z":1}`, 1024, ErrMalformed},
		{"trailing content", testEnvelope(22) + `{"v":1}`, 1024, ErrMalformed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.data, tc.maxSize)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Validate(...) = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestNewID(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		id, err := NewID()
		if err != nil {
			t.Fatalf("NewID() error = %v", err)
		}
		if err := ValidateID(id); err != nil {
			t.Fatalf("ValidateID(%q) = %v, want nil", id, err)
		}
		if seen[id] {
			t.Fatalf("NewID() produced duplicate id %q", id)
		}
		seen[id] = true
	}
}

func TestValidateID(t *testing.T) {
	bad := []string{
		"",
		"short",
		strings.Repeat("A", 21),
		strings.Repeat("A", 23),
		"AAAAAAAAAAAAAAAAAAAA/A", // base64 std alphabet, not url-safe
		"AAAAAAAAAAAAAAAAAAAA+A",
		"../../etc/passwd......",
		"clipper:paste:*ss*ss*",
	}
	for _, id := range bad {
		if err := ValidateID(id); !errors.Is(err, ErrInvalidID) {
			t.Errorf("ValidateID(%q) = %v, want ErrInvalidID", id, err)
		}
	}
}

func TestValidateReadToken(t *testing.T) {
	if err := ValidateReadToken(strings.Repeat("A", 43)); err != nil {
		t.Fatalf("ValidateReadToken(valid) = %v, want nil", err)
	}
	for _, token := range []string{"", "abc", strings.Repeat("A", 42), strings.Repeat("A", 44), strings.Repeat("*", 43)} {
		if err := ValidateReadToken(token); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("ValidateReadToken(%q) = %v, want ErrInvalidToken", token, err)
		}
	}
}

func TestTokensMatch(t *testing.T) {
	if !TokensMatch("abc", "abc") {
		t.Error("TokensMatch(equal) = false")
	}
	if TokensMatch("abc", "abd") || TokensMatch("abc", "") || TokensMatch("", "abc") {
		t.Error("TokensMatch(different) = true")
	}
}

// FuzzValidate makes sure no input shape can panic the validator or slip
// past it: whatever comes back must either be an error or a payload that
// really is a well-formed envelope.
func FuzzValidate(f *testing.F) {
	f.Add(testEnvelope(22))
	f.Add("")
	f.Add("{}")
	f.Add(`{"v":1,"iter":0,"iv":"","ct":""}`)
	f.Add(`{"v":1,"iter":1e309,"iv":"AAAAAAAAAAAAAAAA","ct":"AAAAAAAAAAAAAAAAAAAAAA"}`)
	f.Fuzz(func(t *testing.T, data string) {
		if err := Validate(data, 1<<20); err != nil {
			return
		}
		if len(data) == 0 || int64(len(data)) > 1<<20 {
			t.Fatalf("accepted data of length %d", len(data))
		}
	})
}

// FuzzValidateID pins down that nothing outside the exact ID alphabet and
// length is ever accepted — these strings become datastore keys verbatim.
func FuzzValidateID(f *testing.F) {
	f.Add("AAAAAAAAAAAAAAAAAAAAAA")
	f.Add("../../../etc/passwd")
	f.Add("")
	f.Fuzz(func(t *testing.T, id string) {
		if err := ValidateID(id); err != nil {
			return
		}
		if len(id) != idLen {
			t.Fatalf("accepted id of length %d: %q", len(id), id)
		}
		for i := 0; i < len(id); i++ {
			c := id[i]
			ok := (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_'
			if !ok {
				t.Fatalf("accepted id with byte %q: %q", c, id)
			}
		}
	})
}
