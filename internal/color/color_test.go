package color

import "testing"

func TestPainter_On(t *testing.T) {
	p := NewPainter(true)
	cases := map[string]struct{ got, want string }{
		"error":   {p.Error("x"), "\x1b[31mx\x1b[0m"},
		"warn":    {p.Warn("x"), "\x1b[38;5;208mx\x1b[0m"},
		"success": {p.Success("x"), "\x1b[32mx\x1b[0m"},
	}
	for name, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: got %q, want %q", name, c.got, c.want)
		}
	}
}

func TestPainter_OffIsIdentity(t *testing.T) {
	p := NewPainter(false)
	for _, got := range []string{p.Error("config error:"), p.Warn("config error:"), p.Success("config error:")} {
		if got != "config error:" {
			t.Errorf("expected text unchanged when off, got %q", got)
		}
	}
}

func TestDecide(t *testing.T) {
	tests := []struct {
		mode, noColor string
		isTTY         bool
		want          bool
	}{
		{"auto", "", true, true},
		{"auto", "", false, false},
		{"auto", "1", true, false},
		{"always", "", false, true},
		{"always", "1", false, true},
		{"never", "", true, false},
		{"never", "1", true, false},
	}
	for _, tt := range tests {
		got, err := Decide(tt.mode, tt.noColor, tt.isTTY)
		if err != nil {
			t.Fatalf("Decide(%q, %q, %v): unexpected error %v", tt.mode, tt.noColor, tt.isTTY, err)
		}
		if got != tt.want {
			t.Errorf("Decide(%q, %q, %v) = %v, want %v", tt.mode, tt.noColor, tt.isTTY, got, tt.want)
		}
	}
}

func TestDecide_InvalidMode(t *testing.T) {
	if _, err := Decide("sometimes", "", true); err == nil {
		t.Fatal("expected an error for an invalid mode")
	}
}
