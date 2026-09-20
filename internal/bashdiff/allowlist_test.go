package bashdiff

import "testing"

func TestBashAllowlist(t *testing.T) {
	t.Parallel()

	entries := BashAllowlist()
	if len(entries) == 0 {
		t.Fatal("bash allowlist is empty")
	}
	for _, e := range entries {
		if e.Reason == "" {
			t.Errorf("allowlist entry %q has no reason", e.Key)
		}
	}

	tests := []struct {
		name string
		c    Case
		want bool
	}{
		{"matches-expression", Case{ID: "x.tests:1:OUT", Expr: "OUT=${x:=1}"}, true},
		{"matches-id", Case{ID: "tilde.tests:1:~", Expr: "X=1"}, true},
		{"quote-difference-not-allowed", Case{ID: "exp.tests:1:OUT", Expr: `OUT="\.\."`}, false},
		{"backslash-not-allowed", Case{ID: "exp.tests:2:OUT", Expr: `OUT="\\a"`}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reason, got := Allowlisted(tc.c, entries)
			if got != tc.want {
				t.Fatalf("Allowlisted = %v (%q), want %v", got, reason, tc.want)
			}
			if got && reason == "" {
				t.Error("matched but reason is empty")
			}
		})
	}
}

func TestPOSIXAllowlist(t *testing.T) {
	t.Parallel()

	for _, e := range POSIXAllowlist() {
		if e.Reason == "" {
			t.Errorf("allowlist entry %q has no reason", e.Key)
		}
	}
}
