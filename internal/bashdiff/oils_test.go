package bashdiff

import (
	"strings"
	"testing"
)

// The samples in this file are written for these tests and are not derived
// from the Oils test suite.

const oilsSample = `## compare_shells: dash bash mksh

#### length
v=foo
echo ${#v}
## stdout: 3

#### block
v=abcd
echo ${v%d}
echo ${v%%cd}
## STDOUT:
abc
ab
## END

#### not-implemented
echo ${x:-y}
## stdout: y
## N-I dash stdout: z

#### dash-override
echo ${x:-y}
## stdout: y
## OK dash stdout: Y

#### not-echo-only
for i in a b; do echo $i; done
## STDOUT:
a
b
## END
`

func TestExtractOils(t *testing.T) {
	t.Parallel()

	cases, counts := ExtractOils("sample.test.sh", []byte(oilsSample))

	if counts.Cases != 5 {
		t.Errorf("Cases = %d, want 5", counts.Cases)
	}
	if counts.Emitted != 5 {
		t.Errorf("Emitted = %d, want 5", counts.Emitted)
	}
	if counts.Compared != 4 || counts.Skipped != 1 || counts.Rejected != 1 {
		t.Errorf("Compared/Skipped/Rejected = %d/%d/%d, want 4/1/1",
			counts.Compared, counts.Skipped, counts.Rejected)
	}

	type want struct {
		expr       string
		expected   string
		skip       bool
		adjudicate bool
	}
	wants := []want{
		{"OUT=${#v}", "3", false, false},
		{"OUT=${v%d}", "abc", false, false},
		{"OUT=${v%%cd}", "ab", false, false},
		{"", "", true, false},
		{"OUT=${x:-y}", "Y", false, true},
	}
	if len(cases) != len(wants) {
		t.Fatalf("got %d cases, want %d: %+v", len(cases), len(wants), cases)
	}
	for i, w := range wants {
		got := cases[i]
		if got.Expr != w.expr {
			t.Errorf("case %d: Expr = %q, want %q", i, got.Expr, w.expr)
		}
		if got.Expected != w.expected {
			t.Errorf("case %d: Expected = %q, want %q", i, got.Expected, w.expected)
		}
		if (got.Skip != "") != w.skip {
			t.Errorf("case %d: Skip = %q, want skip=%v", i, got.Skip, w.skip)
		}
		if got.Adjudicate != w.adjudicate {
			t.Errorf("case %d: Adjudicate = %v, want %v", i, got.Adjudicate, w.adjudicate)
		}
	}
	if got := strings.Join(cases[1].Context, "|"); got != "v=abcd" {
		t.Errorf("block context = %q, want v=abcd", got)
	}
}

func TestExtractOils_CodeLine(t *testing.T) {
	t.Parallel()

	src := strings.Join([]string{
		"#### metadata code",
		"## code: echo ${x:-y}",
		"## stdout: y",
	}, "\n")

	cases, _ := ExtractOils("code.test.sh", []byte(src))
	if len(cases) != 1 {
		t.Fatalf("got %d cases, want 1: %+v", len(cases), cases)
	}
	if cases[0].Expr != "OUT=${x:-y}" || cases[0].Expected != "y" {
		t.Errorf("case = %+v", cases[0])
	}
}

func TestExtractOils_StdoutJSON(t *testing.T) {
	t.Parallel()

	src := strings.Join([]string{
		"#### empty json",
		"echo ${x:-}",
		`## stdout-json: ""`,
	}, "\n")

	cases, _ := ExtractOils("json.test.sh", []byte(src))
	if len(cases) != 1 {
		t.Fatalf("got %d cases, want 1: %+v", len(cases), cases)
	}
	if cases[0].Expected != "" {
		t.Errorf("Expected = %q, want empty", cases[0].Expected)
	}
}
