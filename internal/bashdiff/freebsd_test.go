package bashdiff

import (
	"strings"
	"testing"
)

// The samples in this file are written for these tests and are not derived
// from the FreeBSD test suite.

const freebsdSample = `e= q='?' p='/et[c]/'

testcase() {
	code="$1"
	expected="$2"
}

testcase 'set -- ${e}' '0|'
testcase 'set -- ${p}' '1|/etc/'
testcase 'set -- a b' '2|a|b'
set -f
testcase 'set -- ${p}' '1|/et[c]/'
test "x$failures" = x
`

func TestExtractFreeBSD(t *testing.T) {
	t.Parallel()

	cases, counts := ExtractFreeBSD("trim.0", []byte(freebsdSample))

	if counts.Cases != 4 {
		t.Errorf("Cases = %d, want 4", counts.Cases)
	}
	if counts.Emitted != 4 || counts.Compared != 3 || counts.Skipped != 1 || counts.Rejected != 0 {
		t.Errorf("Emitted/Compared/Skipped/Rejected = %d/%d/%d/%d, want 4/3/1/0",
			counts.Emitted, counts.Compared, counts.Skipped, counts.Rejected)
	}

	wants := []struct {
		expr     string
		expected string
		skip     bool
		noglob   bool
	}{
		{"OUT=${e}", "", false, false},
		{"OUT=${p}", "/etc/", false, false},
		{"OUT=a b", "", true, false},
		{"OUT=${p}", "/et[c]/", false, true},
	}
	if len(cases) != len(wants) {
		t.Fatalf("got %d cases, want %d: %+v", len(cases), len(wants), cases)
	}
	for i, w := range wants {
		got := cases[i]
		if got.Expr != w.expr || got.Expected != w.expected || (got.Skip != "") != w.skip || got.NoGlob != w.noglob {
			t.Errorf("case %d = %+v, want %+v", i, got, w)
		}
	}
	if got := strings.Join(cases[0].Context, "|"); got != "e=|q='?'|p='/et[c]/'" {
		t.Errorf("context = %q", got)
	}
}

func TestExtractFreeBSD_Assertion(t *testing.T) {
	t.Parallel()

	src := strings.Join([]string{
		"v=abcd",
		`[ "${#v}" = 4 ] || echo wrong`,
		`[ "$b" = z ] || echo wrong`,
	}, "\n")

	cases, counts := ExtractFreeBSD("length.0", []byte(src))
	if len(cases) != 1 {
		t.Fatalf("got %d cases, want 1: %+v", len(cases), cases)
	}
	if cases[0].Expr != `OUT="${#v}"` || cases[0].Expected != "4" {
		t.Errorf("case = %+v", cases[0])
	}
	if counts.Rejected != 1 {
		t.Errorf("Rejected = %d, want 1 (undefined variable)", counts.Rejected)
	}
}
