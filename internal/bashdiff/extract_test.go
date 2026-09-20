package bashdiff

import (
	"strings"
	"testing"
)

// The sample text below is written for this test and is not derived from the
// GNU bash test suite.

func TestExtract_SourcesAndContext(t *testing.T) {
	t.Parallel()

	src := strings.Join([]string{
		"# a comment",
		"A=one",
		"B=\"two words\"",
		"recho \"$A\"",
		"recho $B",
		"recho token",
	}, "\n")

	cases, counts := Extract("sample.tests", []byte(src))

	if len(cases) != 5 {
		t.Fatalf("got %d cases, want 5: %+v", len(cases), cases)
	}

	want := []struct {
		id      string
		varName string
		expr    string
		source  Source
		context []string
	}{
		{"sample.tests:2:A", "A", "A=one", SourceBare, nil},
		{"sample.tests:3:B", "B", "B=\"two words\"", SourceBare, []string{"A=one"}},
		{"sample.tests:4:OUT", "OUT", "OUT=\"$A\"", SourceRechoQuoted, []string{"A=one"}},
		{"sample.tests:5:OUT", "OUT", "OUT=$B", SourceRechoToken, []string{"A=one"}},
		{"sample.tests:6:OUT", "OUT", "OUT=token", SourceRechoToken, []string{"A=one"}},
	}

	for i, w := range want {
		got := cases[i]
		if got.ID != w.id {
			t.Errorf("case %d: ID = %q, want %q", i, got.ID, w.id)
		}
		if got.Var != w.varName {
			t.Errorf("case %d: Var = %q, want %q", i, got.Var, w.varName)
		}
		if got.Expr != w.expr {
			t.Errorf("case %d: Expr = %q, want %q", i, got.Expr, w.expr)
		}
		if got.Source != w.source {
			t.Errorf("case %d: Source = %v, want %v", i, got.Source, w.source)
		}
		if strings.Join(got.Context, "|") != strings.Join(w.context, "|") {
			t.Errorf("case %d: Context = %v, want %v", i, got.Context, w.context)
		}
	}

	if counts.Bare != 2 || counts.Recho != 3 {
		t.Errorf("counts: Bare=%d Recho=%d, want 2/3", counts.Bare, counts.Recho)
	}
	if counts.Contexts != 1 {
		t.Errorf("counts.Contexts = %d, want 1 (only the plain literal A=one)", counts.Contexts)
	}
}

func TestExtract_Exclusions(t *testing.T) {
	t.Parallel()

	lines := []struct {
		name string
		line string
	}{
		{"command-substitution", "X=$(echo hi)"},
		{"backtick", "X=`echo hi`"},
		{"positional-token", "recho $1"},
		{"positional-brace", "X=${1}"},
		{"special-at", "recho $@"},
		{"special-star", "recho \"$*\""},
		{"special-hash", "recho $#"},
		{"special-question", "recho $?"},
		{"special-bang", "recho $!"},
		{"special-pid", "X=$$"},
		{"special-zero", "recho $0"},
		{"special-dash", "recho $-"},
		{"indirect", "X=${!y}"},
		{"transform-op", "X=${y@Q}"},
		{"case-tilde", "X=${y~~}"},
		{"case-mod-pattern", "M1=${S1^^[aeiou]}"},
		{"case-mod-double", "X=${y,,}"},
		{"special-hash-empty", "recho ${#}"},
		{"special-hash-modifier", "recho ${#:foo}"},
		{"special-hash-dash", "recho ${#-posparams}"},
		{"shell-pwd", "X=$PWD"},
		{"shell-bashpid", "X=$BASHPID"},
		{"shell-assign-random", "RANDOM=42"},
		{"shell-assign-path", "PATH=/x:/y"},
		{"control-byte", "X=\x01"},
		{"case-upper", "X=${y^}"},
		{"case-lower", "X=${y,}"},
		{"assign-colon", "X=${y:=1}"},
		{"assign-plain", "X=${y=1}"},
		{"arith-dollar", "X=$((1 + 2))"},
		{"arith-bracket", "X=$[1 + 2]"},
		{"array-subscript", "X=${arr[0]}"},
		{"dparen", "recho ((1 + 2))"},
		{"redir-out", "X=a>b"},
		{"redir-in", "X=a<b"},
		{"tilde", "recho ~"},
		{"ifs", "X=$IFS"},
		{"ifs-assign", "IFS=\"$IFS\""},
		{"unbalanced-quote", "X=\"abc"},
		{"unquoted-space", "X=a b"},
		{"multi-arg-recho", "recho a b"},
		{"multi-assignment", "foo=10 x=foo"},
		{"export", "export X=1"},
		{"declare", "declare -i X=1"},
		{"readonly", "readonly X=1"},
		{"local", "local X=1"},
		{"typeset", "typeset X=1"},
		{"case-stmt", "case $x in"},
		{"if-stmt", "if true; then"},
		{"for-stmt", "for i in a b; do"},
		{"while-stmt", "while true; do"},
		{"set-stmt", "set abc def"},
		{"unset-stmt", "unset xxx"},
		{"sub-invocation", "${THIS_SH} ./exp1.sub"},
	}

	for _, tc := range lines {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cases, _ := Extract("x.tests", []byte(tc.line))
			if len(cases) != 0 {
				t.Fatalf("line %q produced cases: %+v", tc.line, cases)
			}
		})
	}
}

func TestExtract_Included(t *testing.T) {
	t.Parallel()

	lines := []struct {
		name string
		line string
		expr string
	}{
		{"double-quoted-spaces", "recho \"a b\"", "OUT=\"a b\""},
		{"empty-double-quotes", "recho \"\"", "OUT=\"\""},
		{"empty-single-quotes", "recho ''", "OUT=''"},
		{"single-quoted-dollar", "recho '$x'", "OUT='$x'"},
		{"single-quoted-tilde", "recho '~'", "OUT='~'"},
		{"single-quoted-paren", "recho '$('", "OUT='$('"},
		{"length-op", "recho ${#X}", "OUT=${#X}"},
		{"pattern-removal", "recho ${x%.c}", "OUT=${x%.c}"},
		{"quoted-bare-spaces", "a=\"a b c\"", "a=\"a b c\""},
		{"dollar-only", "recho \"\\$\"", "OUT=\"\\$\""},
		{"quote-escape", "recho \"\\\"\"", "OUT=\"\\\"\""},
	}

	for _, tc := range lines {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cases, _ := Extract("x.tests", []byte(tc.line))
			if len(cases) != 1 {
				t.Fatalf("line %q produced %d cases, want 1", tc.line, len(cases))
			}
			if cases[0].Expr != tc.expr {
				t.Errorf("Expr = %q, want %q", cases[0].Expr, tc.expr)
			}
		})
	}
}

func TestExtract_Heredoc(t *testing.T) {
	t.Parallel()

	src := strings.Join([]string{
		"cat <<EOF",
		"X=inside",
		"EOF",
		"Y=outside",
	}, "\n")

	cases, _ := Extract("x.tests", []byte(src))
	if len(cases) != 1 {
		t.Fatalf("got %d cases, want 1: %+v", len(cases), cases)
	}
	if cases[0].Expr != "Y=outside" {
		t.Errorf("Expr = %q, want Y=outside", cases[0].Expr)
	}
}

func TestExtract_UnbalancedQuoteIsNotContext(t *testing.T) {
	t.Parallel()

	src := strings.Join([]string{
		"X='abc",
		"recho $X",
	}, "\n")

	cases, _ := Extract("x.tests", []byte(src))
	if len(cases) != 1 {
		t.Fatalf("got %d cases, want 1: %+v", len(cases), cases)
	}
	if len(cases[0].Context) != 0 {
		t.Errorf("Context = %v, want empty (unbalanced assignment must not leak)", cases[0].Context)
	}
}

func TestExtract_Counts(t *testing.T) {
	t.Parallel()

	src := strings.Join([]string{
		"A=1",
		"X=$(bad)",
		"recho $A",
		"",
		"# comment",
	}, "\n")

	_, counts := Extract("counts.tests", []byte(src))
	if counts.File != "counts.tests" {
		t.Errorf("File = %q", counts.File)
	}
	if counts.Lines != 5 {
		t.Errorf("Lines = %d, want 5", counts.Lines)
	}
	if counts.Cases != 2 || counts.Bare != 1 || counts.Recho != 1 {
		t.Errorf("Cases/Bare/Recho = %d/%d/%d, want 2/1/1", counts.Cases, counts.Bare, counts.Recho)
	}
	if counts.Excluded != 1 {
		t.Errorf("Excluded = %d, want 1", counts.Excluded)
	}
	if counts.Contexts != 1 {
		t.Errorf("Contexts = %d, want 1", counts.Contexts)
	}
}

func TestScanQuotes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		in       string
		balanced bool
	}{
		{"plain", "abc", true},
		{"double", "\"a b\"", true},
		{"single", "'a b'", true},
		{"mixed", "\"a\"'b'", true},
		{"unbalanced-double", "\"abc", false},
		{"unbalanced-single", "'abc", false},
		{"escaped-double", "\"a\\\"b\"", true},
		{"single-holds-quote", "'it''s'", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, balanced := scanQuotes(tc.in)
			if balanced != tc.balanced {
				t.Errorf("scanQuotes(%q) balanced = %v, want %v", tc.in, balanced, tc.balanced)
			}
		})
	}
}
