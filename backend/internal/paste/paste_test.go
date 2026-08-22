package paste

import "testing"

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		data    string
		maxSize int64
		wantErr error
	}{
		{"empty", "", 10, ErrEmpty},
		{"too large", "abcdefghijk", 10, ErrTooLarge},
		{"ok", "abcde", 10, nil},
		{"exact limit ok", "abcdefghij", 10, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.data, tc.maxSize)
			if err != tc.wantErr {
				t.Fatalf("Validate(%q, %d) = %v, want %v", tc.data, tc.maxSize, err, tc.wantErr)
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
		if id == "" {
			t.Fatal("NewID() returned empty string")
		}
		if seen[id] {
			t.Fatalf("NewID() produced duplicate id %q", id)
		}
		seen[id] = true
	}
}
