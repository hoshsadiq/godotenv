package bashdiff

import (
	"testing"
)

// The samples in this file are written for these tests and are not derived
// from the NetBSD test suite.

const netbsdSample = `foo="${x:-y}"
check "$foo" "y"
check 'x=1; echo ${x:-a}' 'a' 0
check 'set -- a b; echo ${x}' 'a' 0
check 'unset x; echo ${x:-$(echo a)}' 'a' 0
`

func TestExtractNetBSD(t *testing.T) {
	t.Parallel()

	cases, counts := ExtractNetBSD("t.0", []byte(netbsdSample))

	if counts.Cases != 4 {
		t.Errorf("Cases = %d, want 4", counts.Cases)
	}
	if counts.Emitted != 2 || counts.Compared != 2 || counts.Skipped != 0 || counts.Rejected != 2 {
		t.Errorf("Emitted/Compared/Skipped/Rejected = %d/%d/%d/%d, want 2/2/0/2",
			counts.Emitted, counts.Compared, counts.Skipped, counts.Rejected)
	}

	wants := []struct {
		expr     string
		expected string
	}{
		{`OUT="${x:-y}"`, "y"},
		{"OUT=${x:-a}", "a"},
	}
	if len(cases) != len(wants) {
		t.Fatalf("got %d cases, want %d: %+v", len(cases), len(wants), cases)
	}
	for i, w := range wants {
		if cases[i].Expr != w.expr || cases[i].Expected != w.expected {
			t.Errorf("case %d = %+v, want %+v", i, cases[i], w)
		}
	}
}

func TestExtractNetBSD_Continuations(t *testing.T) {
	t.Parallel()

	src := "check 'x=1; echo ${x:-a}' \\\n\t'a' 0\n"
	cases, _ := ExtractNetBSD("t.0", []byte(src))
	if len(cases) != 1 {
		t.Fatalf("got %d cases, want 1: %+v", len(cases), cases)
	}
	if cases[0].Expr != "OUT=${x:-a}" || cases[0].Expected != "a" {
		t.Errorf("case = %+v", cases[0])
	}
}
