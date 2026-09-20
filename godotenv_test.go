package godotenv_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hoshsadiq/godotenv"
)

func TestFileLoading(t *testing.T) {
	tests := []struct {
		name           string
		envFileName    string
		presets        map[string]string
		expectedValues map[string]string
	}{
		{
			name:        "equals",
			envFileName: "fixtures/equals.env",
			expectedValues: map[string]string{
				"OPTION_A": "postgres://localhost:5432/database?sslmode=disable",
			},
		},
		{
			name:        "quoted",
			envFileName: "fixtures/quoted.env",
			expectedValues: map[string]string{
				"OPTION_SINGLE_A": "1",
				"OPTION_SINGLE_B": "2",
				"OPTION_SINGLE_C": "",
				"OPTION_SINGLE_D": "\\n",
				"OPTION_SINGLE_E": `echo "asd"`,
				"OPTION_SINGLE_F": "echo asd",
				"OPTION_SINGLE_G": "'escaped'",
				"OPTION_SINGLE_H": "1\n2",
				"OPTION_SINGLE_I": "1\n2\n3 is \\'quoted\\'",

				"OPTION_DOUBLE_A": "1",
				"OPTION_DOUBLE_B": "2",
				"OPTION_DOUBLE_C": "",
				"OPTION_DOUBLE_D": "\\n",
				"OPTION_DOUBLE_E": "echo 'asd'",
				"OPTION_DOUBLE_F": "echo asd",
				"OPTION_DOUBLE_G": "\"escaped\"",
				"OPTION_DOUBLE_H": "1\n2",
				"OPTION_DOUBLE_I": "1\n2\n3 is \"quoted\"",
			},
		},
		{
			name:        "substitutions",
			envFileName: "fixtures/substitutions.env",
			expectedValues: map[string]string{
				"OPTION_A": "1",
				"OPTION_B": "1",
				"OPTION_C": "1",
				"OPTION_D": "11",
				"OPTION_E": "",
			},
		},
		{
			name:        "exported",
			envFileName: "fixtures/exported.env",
			expectedValues: map[string]string{
				"OPTION_A": "2",
				"OPTION_B": "\\n",
			},
		},
		{
			name:        "plain",
			envFileName: "fixtures/plain.env",
			expectedValues: map[string]string{
				"OPTION_A": "1",
				"OPTION_B": "2",
				"OPTION_C": "",
			},
		},
		{
			name:        "all",
			envFileName: "fixtures/all.env",
			expectedValues: map[string]string{
				"OPTION_A": "1",
				"OPTION_B": "1#realvalue",
				"OPTION_C": "1",
				"OPTION_D": "1",
				"OPTION_E": "1",
				"OPTION_F": "1#realvalue",
				"OPTION_G": "11#realvalue",
				"OPTION_H": "",
				"OPTION_I": "1",
				"OPTION_J": "${OPTION_A}",
				"OPTION_K": "${OPTION_NOT_DEFINED:-default}",
				"OPTION_L": "${OPTION_A:+default}",
				"OPTION_M": "1\n2",
				"OPTION_N": "1",
				"OPTION_O": "1\n2",
				"OPTION_P": "1",
				"OPTION_Q": "default",
				"OPTION_R": "default",
			},
		},
	}

	t.Parallel()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			file, err := os.Open(tt.envFileName)
			if err != nil {
				return
			}
			defer file.Close()

			envMap, err := godotenv.ParseWithLookup(file, func(name []byte) (value []byte, exists bool) {
				val, exists := tt.presets[string(name)]
				return []byte(val), exists
			})
			if err != nil {
				t.Fatalf("Error loading %v: %s", tt.envFileName, err)
			}

			// to avoid conflict between windows and non-windows.
			for k, v := range envMap {
				envMap[k] = strings.ReplaceAll(v, "\r", "")
			}

			if !reflect.DeepEqual(tt.expectedValues, envMap) {
				t.Errorf("Mismatch env vars")
				printDiff(t, tt.expectedValues, envMap)
			}
		})
	}
}

func printDiff(t *testing.T, expected, actual map[string]string) {
	t.Helper()
	for i, v := range expected {
		if actual[i] != v {
			t.Logf("- %q = %q", i, v)
			t.Logf("+ %q = %q", i, actual[i])
		}
	}
	for k, v := range actual {
		if expected[k] != v {
			t.Logf("- %q = %q", k, v)
			t.Logf("+ %q = %q", k, expected[k])
		}
	}
}

func TestLoadWithNoArgsLoadsDotEnv(t *testing.T) {
	t.Parallel()

	var pathError *os.PathError
	if err := godotenv.Load(); !errors.As(err, &pathError) || pathError.Op != "open" || pathError.Path != ".env" {
		t.Errorf("Didn't try and open .env by default")
	}
}

func TestWithOverrideWithNoArgsOverridesDotEnv(t *testing.T) {
	t.Parallel()

	var pathError *os.PathError
	if err := godotenv.New(godotenv.WithOverride()).Load(); !errors.As(err, &pathError) || pathError.Op != "open" || pathError.Path != ".env" {
		t.Errorf("Didn't try and open .env by default")
	}
}

func TestLoadFileNotFound(t *testing.T) {
	t.Parallel()

	err := godotenv.Load("somefilethatwillneverexistever.env")
	if err == nil {
		t.Error("File wasn't found but Load didn't return an error")
	}
}

func TestWithOverrideFileNotFound(t *testing.T) {
	t.Parallel()

	err := godotenv.New(godotenv.WithOverride()).Load("somefilethatwillneverexistever.env")
	if err == nil {
		t.Error("File wasn't found but Load with WithOverride didn't return an error")
	}
}

func TestReadPlainEnv(t *testing.T) {
	t.Parallel()

	envFileName := "fixtures/plain.env"
	expectedValues := map[string]string{
		"OPTION_A": "1",
		"OPTION_B": "2",
		"OPTION_C": "",
	}

	envMap, err := godotenv.Read(envFileName)
	if err != nil {
		t.Errorf("Error reading file: %s", err)
	}

	if len(envMap) != len(expectedValues) {
		t.Error("Didn't get the right size map back")
	}

	for key, value := range expectedValues {
		if envMap[key] != value {
			t.Error("Read got one of the keys wrong")
		}
	}
}

func TestParse(t *testing.T) {
	t.Parallel()

	envMap, err := godotenv.Parse(bytes.NewReader([]byte("ONE=1\nTWO='2'")))
	expectedValues := map[string]string{
		"ONE": "1",
		"TWO": "2",
	}
	if err != nil {
		t.Fatalf("error parsing env: %v", err)
	}
	for key, value := range expectedValues {
		if envMap[key] != value {
			t.Errorf("expected %s to be %s, got %s", key, value, envMap[key])
		}
	}
}

func TestLoadDoesNotOverride(t *testing.T) {
	envFileName := "fixtures/plain.env"

	// ensure NO override
	vars := map[string]string{
		"OPTION_A": "do_not_override",
		"OPTION_B": "",
	}

	// first up, clear the env
	os.Clearenv()

	for k, v := range vars {
		_ = os.Setenv(k, v)
	}

	err := godotenv.Load(envFileName)
	if err != nil {
		t.Fatalf("Error loading %v: %s", envFileName, err)
	}

	for k := range vars {
		envValue := os.Getenv(k)
		v := vars[k]
		if envValue != v {
			t.Errorf("Mismatch for key '%v': expected '%v' got '%v'", k, v, envValue)
		}
	}
}

func TestWithOverrideDoesOverride(t *testing.T) {
	envFileName := "fixtures/plain.env"

	// ensure NO override
	vars := map[string]string{
		"OPTION_A": "do_not_override",
	}

	expectedValues := map[string]string{
		"OPTION_A": "1",
	}

	// first up, clear the env
	os.Clearenv()

	for k, v := range vars {
		_ = os.Setenv(k, v)
	}

	err := godotenv.New(godotenv.WithOverride()).Load(envFileName)
	if err != nil {
		t.Fatalf("Error loading %v: %s", envFileName, err)
	}

	for k := range expectedValues {
		envValue := os.Getenv(k)
		v := expectedValues[k]
		if envValue != v {
			t.Errorf("Mismatch for key '%v': expected '%v' got '%v'", k, v, envValue)
		}
	}
}

func TestReadMultipleFilesLastWins(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	first := filepath.Join(dir, "first.env")
	second := filepath.Join(dir, "second.env")
	writeTestFile(t, first, "SHARED=first\nONLY_FIRST=1\n")
	writeTestFile(t, second, "SHARED=second\nONLY_SECOND=2\n")

	got, err := godotenv.Read(first, second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]string{
		"SHARED":      "second",
		"ONLY_FIRST":  "1",
		"ONLY_SECOND": "2",
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("expected %v, got %v", want, got)
	}
}

func TestLoadMultipleFilesLastWins(t *testing.T) {
	// Load mutates the process environment, so this test does not run in
	// parallel. Unique key names keep it clear of the other env tests.
	const (
		shared     = "GODOTENV_TEST_MULTIFILE_SHARED"
		onlyFirst  = "GODOTENV_TEST_MULTIFILE_FIRST"
		onlySecond = "GODOTENV_TEST_MULTIFILE_SECOND"
	)

	for _, key := range []string{shared, onlyFirst, onlySecond} {
		old, had := os.LookupEnv(key)
		_ = os.Unsetenv(key)
		t.Cleanup(func() {
			if had {
				_ = os.Setenv(key, old)
			} else {
				_ = os.Unsetenv(key)
			}
		})
	}

	dir := t.TempDir()
	first := filepath.Join(dir, "first.env")
	second := filepath.Join(dir, "second.env")
	writeTestFile(t, first, shared+"=first\n"+onlyFirst+"=1\n")
	writeTestFile(t, second, shared+"=second\n"+onlySecond+"=2\n")

	if err := godotenv.Load(first, second); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := os.Getenv(shared); got != "second" {
		t.Errorf("expected the later file to win, got %q", got)
	}
	if got := os.Getenv(onlyFirst); got != "1" {
		t.Errorf("expected %s=1, got %q", onlyFirst, got)
	}
	if got := os.Getenv(onlySecond); got != "2" {
		t.Errorf("expected %s=2, got %q", onlySecond, got)
	}
}

func writeTestFile(t *testing.T, name, content string) {
	t.Helper()

	if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
}

func TestExpanding(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected map[string]string
	}{
		{
			"expands variables found in values",
			"FOO=test\nBAR=$FOO",
			map[string]string{"FOO": "test", "BAR": "test"},
		},
		{
			"parses variables wrapped in brackets",
			"FOO=test\nBAR=${FOO}bar",
			map[string]string{"FOO": "test", "BAR": "testbar"},
		},
		{
			"expands undefined variables to an empty string",
			"BAR=$FOO",
			map[string]string{"BAR": ""},
		},
		{
			"expands variables in double quoted strings",
			"FOO=test\nBAR=\"quote $FOO\"",
			map[string]string{"FOO": "test", "BAR": "quote test"},
		},
		{
			"does not expand variables in single quoted strings",
			"BAR='quote $FOO'",
			map[string]string{"BAR": "quote $FOO"},
		},
		{
			"does not expand escaped variables",
			`FOO="foo\$BAR"`,
			map[string]string{"FOO": "foo$BAR"},
		},
		{
			"does not expand escaped variables",
			`FOO="foo\${BAR}"`,
			map[string]string{"FOO": "foo${BAR}"},
		},
		{
			"does not expand escaped variables",
			"FOO=test\nBAR=\"foo\\${FOO} ${FOO}\"",
			map[string]string{"FOO": "test", "BAR": "foo${FOO} test"},
		},
	}

	t.Parallel()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env, err := godotenv.Parse(strings.NewReader(tt.input))
			if err != nil {
				t.Errorf("Error: %s", err.Error())
			}
			for k, v := range tt.expected {
				if strings.Compare(env[k], v) != 0 {
					t.Errorf("Expected: %q, Actual: %q", v, env[k])
				}
			}
		})
	}
}

func TestActualEnvVarsAreLeftAlone(t *testing.T) {
	os.Clearenv()
	os.Setenv("OPTION_A", "actualenv")
	_ = godotenv.Load("fixtures/plain.env")

	if os.Getenv("OPTION_A") != "actualenv" {
		t.Error("An ENV var set earlier was overwritten")
	}
}

func TestParsing(t *testing.T) {
	tests := []struct {
		rawEnvLine    string
		expectedKey   string
		expectedValue string
		env           map[string]string
	}{
		// unquoted values
		{rawEnvLine: "FOO=bar", expectedKey: "FOO", expectedValue: "bar"},

		// parses values with spaces around equal sign
		{rawEnvLine: "FOO =bar", expectedKey: "", expectedValue: ""},
		{rawEnvLine: "FOO= bar", expectedKey: "", expectedValue: ""},

		// parses double quoted values
		{rawEnvLine: `FOO="bar"`, expectedKey: "FOO", expectedValue: "bar"},

		// parses single quoted values
		{rawEnvLine: "FOO='bar'", expectedKey: "FOO", expectedValue: "bar"},

		// parses escaped double quotes
		{rawEnvLine: `FOO="escaped\"bar"`, expectedKey: "FOO", expectedValue: `escaped"bar`},

		// parses single quotes inside double quotes
		{rawEnvLine: `FOO="'d'"`, expectedKey: "FOO", expectedValue: `'d'`},

		// parses non-yaml options with colons
		{rawEnvLine: "OPTION_A=1:B", expectedKey: "OPTION_A", expectedValue: "1:B"},

		// parses export keyword
		{rawEnvLine: "export OPTION_A=2", expectedKey: "OPTION_A", expectedValue: "2"},
		{rawEnvLine: `export OPTION_B='\n'`, expectedKey: "OPTION_B", expectedValue: "\\n"},
		{rawEnvLine: "export exportFoo=2", expectedKey: "exportFoo", expectedValue: "2"},
		{rawEnvLine: "exportFOO=2", expectedKey: "exportFOO", expectedValue: "2"},
		{rawEnvLine: "export_FOO=2", expectedKey: "export_FOO", expectedValue: "2"},
		{rawEnvLine: "export.FOO=2", expectedKey: "", expectedValue: ""},
		{rawEnvLine: "export\tOPTION_A=2", expectedKey: "OPTION_A", expectedValue: "2"},
		{rawEnvLine: "  export OPTION_A=2", expectedKey: "OPTION_A", expectedValue: "2"},
		{rawEnvLine: "\texport OPTION_A=2", expectedKey: "OPTION_A", expectedValue: "2"},
		{rawEnvLine: `FOO="bar\nbaz"`, expectedKey: "FOO", expectedValue: "bar\\nbaz"},
		{rawEnvLine: "FOO.BAR=foobar", expectedKey: "", expectedValue: ""},

		// it 'parses varibales with several "=" in the value' do
		// expect(env('FOO=foobar=')).to eql('FOO' => 'foobar=')
		{rawEnvLine: "FOO=foobar=", expectedKey: "FOO", expectedValue: "foobar="},

		// it 'strips unquoted values' do
		// expect(env('foo=bar ')).to eql('foo' => 'bar') # not 'bar '
		{rawEnvLine: "FOO=bar ", expectedKey: "FOO", expectedValue: "bar"},

		// it 'ignores inline comments' do
		// expect(env("foo=bar # this is foo")).to eql('foo' => 'bar')
		{rawEnvLine: "FOO=bar # this is foo", expectedKey: "FOO", expectedValue: "bar"},

		// it 'allows # in quoted value' do
		// expect(env('foo="bar#baz" # comment')).to eql('foo' => 'bar#baz')
		{rawEnvLine: `FOO="bar#baz" # comment`, expectedKey: "FOO", expectedValue: "bar#baz"},
		{rawEnvLine: "FOO='bar#baz' # comment", expectedKey: "FOO", expectedValue: "bar#baz"},
		{rawEnvLine: `FOO="bar#baz#bang" # comment`, expectedKey: "FOO", expectedValue: "bar#baz#bang"},

		// it 'parses # in quoted values' do
		// expect(env('foo="ba#r"')).to eql('foo' => 'ba#r')
		// expect(env("foo='ba#r'")).to eql('foo' => 'ba#r')
		{rawEnvLine: `FOO="ba#r"`, expectedKey: "FOO", expectedValue: "ba#r"},
		{rawEnvLine: "FOO='ba#r'", expectedKey: "FOO", expectedValue: "ba#r"},

		// newlines and backslashes should be escaped
		{rawEnvLine: `FOO="bar\n\ b\az"`, expectedKey: "FOO", expectedValue: "bar\\n\\ b\\az"},
		{rawEnvLine: `FOO="bar\\\n\ b\az"`, expectedKey: "FOO", expectedValue: "bar\\\\n\\ b\\az"},
		{rawEnvLine: `FOO="bar\\r\ b\az"`, expectedKey: "FOO", expectedValue: "bar\\r\\ b\\az"},

		{rawEnvLine: `="value"`, expectedKey: "", expectedValue: ""},
		{rawEnvLine: `KEY="`, expectedKey: "", expectedValue: ""},
		{rawEnvLine: `KEY="value`, expectedKey: "", expectedValue: ""},

		// leading whitespace should be ignored
		{rawEnvLine: " KEY =value", expectedKey: "", expectedValue: ""},
		{rawEnvLine: "   KEY=value", expectedKey: "KEY", expectedValue: "value"},
		{rawEnvLine: "\tKEY=value", expectedKey: "KEY", expectedValue: "value"},

		// https://github.com/joho/godotenv/issues/153
		// Docker compose v2 requires parameter expansion
		{rawEnvLine: `FOO=${FOO:-FOO_ENV_DEFAULT}`, expectedKey: "FOO", expectedValue: "FOO_ENV_DEFAULT"},
		{rawEnvLine: `BAR="${BAR:-BAR_ENV_DEFAULT}"`, expectedKey: "BAR", expectedValue: "BAR_ENV_DEFAULT"},
		{rawEnvLine: `FOO=${FOO:-FOO_ENV_DEFAULT}`, env: map[string]string{"FOO": "bla"}, expectedKey: "FOO", expectedValue: "bla"},
		{rawEnvLine: `BAR="${BAR:-BAR_ENV_DEFAULT}"`, env: map[string]string{"BAR": "bla"}, expectedKey: "BAR", expectedValue: "bla"},

		// Additional shell expansions
		{rawEnvLine: `FOO=${FOO:+FOO_ENV_DEFAULT}`, expectedKey: "FOO", expectedValue: ""},
		{rawEnvLine: `BAR="${BAR:+BAR_ENV_DEFAULT}"`, expectedKey: "BAR", expectedValue: ""},
		{rawEnvLine: `FOO=${FOO:+FOO_ENV_DEFAULT}`, env: map[string]string{"FOO": "bla"}, expectedKey: "FOO", expectedValue: "FOO_ENV_DEFAULT"},
		{rawEnvLine: `BAR="${BAR:+BAR_ENV_DEFAULT}"`, env: map[string]string{"BAR": "bla"}, expectedKey: "BAR", expectedValue: "BAR_ENV_DEFAULT"},

		// Issue https://github.com/joho/godotenv/issues/155
		// Though the issue compares against ruby's dotenv. Because $1 (and others) is a special variable, it needs to be
		// handled as such. Therefore, the result of the issue is as below.
		{rawEnvLine: "VARIABLE_0=$a$0$12$_x", expectedKey: "VARIABLE_0", expectedValue: "2"},
		{rawEnvLine: `VARIABLE_1="$a$0$12$_x"`, expectedKey: "VARIABLE_1", expectedValue: "2"},
		{rawEnvLine: `VARIABLE_2='$a$0$12$_x'`, expectedKey: "VARIABLE_2", expectedValue: "$a$0$12$_x"},
		{rawEnvLine: `VARIABLE_3=       $a$0$12$_x`, expectedKey: "", expectedValue: ""},

		// https://github.com/joho/godotenv/issues/127
		// Hashes are comments if it's directly followed by whitespace
		{rawEnvLine: `FOO=asd#asd`, expectedKey: "FOO", expectedValue: "asd#asd"},
		{rawEnvLine: `FOO=asd #asd`, expectedKey: "FOO", expectedValue: "asd"},

		// an escaped space does not turn a following # into a comment
		{rawEnvLine: `FOO=a\ #b`, expectedKey: "FOO", expectedValue: "a #b"},
		{rawEnvLine: `FOO=a\#b`, expectedKey: "FOO", expectedValue: "a#b"},
		{rawEnvLine: `FOO=a\  #b`, expectedKey: "FOO", expectedValue: "a "},

		// a trailing backslash at EOF is literal
		{rawEnvLine: `FOO=abc\`, expectedKey: "FOO", expectedValue: `abc\`},

		// unquoted whitespace is preserved, not concatenated
		{rawEnvLine: "FOO=a b c", expectedKey: "FOO", expectedValue: "a b c"},
		{rawEnvLine: "FOO=a  b", expectedKey: "FOO", expectedValue: "a  b"},
		{rawEnvLine: "FOO=a\tb", expectedKey: "FOO", expectedValue: "a\tb"},

		// command substitution is preserved verbatim
		{rawEnvLine: `FOO=$(echo hi)`, expectedKey: "FOO", expectedValue: "$(echo hi)"},
		{rawEnvLine: "FOO=`echo hi`", expectedKey: "FOO", expectedValue: "`echo hi`"},
		{rawEnvLine: `FOO=$(echo $BAR)`, expectedKey: "FOO", expectedValue: "$(echo $BAR)"},

		// backslash-newline is a line continuation
		{rawEnvLine: "FOO=a\\\nb", expectedKey: "FOO", expectedValue: "ab"},
		{rawEnvLine: "FOO=\"a\\\nb\"", expectedKey: "FOO", expectedValue: "ab"},
	}

	t.Parallel()
	for _, tt := range tests {
		t.Run(tt.rawEnvLine, func(t *testing.T) {
			t.Parallel()

			if tt.env == nil {
				tt.env = make(map[string]string, 0)
			}
			newEnv := tt.env

			expandEnv := func(s []byte) (value []byte, exists bool) {
				var val string

				if val, exists = newEnv[string(s)]; exists {
					return []byte(val), exists
				}

				return []byte(val), false
			}

			newEnv, _ = godotenv.ParseWithLookup(strings.NewReader(tt.rawEnvLine), expandEnv)
			if tt.expectedKey == "" {
				if !reflect.DeepEqual(tt.env, newEnv) {
					t.Errorf("Expected '%v' to parse as '%v' => '%v', got %+v", tt.rawEnvLine, tt.expectedKey, tt.expectedValue, newEnv)
				}
			} else {
				value, ok := newEnv[tt.expectedKey]
				if !ok {
					t.Errorf("Expected '%v' to parse as '%v' => '%v', got %+v", tt.rawEnvLine, tt.expectedKey, tt.expectedValue, newEnv)
				}
				if value != tt.expectedValue {
					t.Errorf("Expected '%v' to parse as '%v' => '%v', got %+v", tt.rawEnvLine, tt.expectedKey, tt.expectedValue, newEnv)
				}
			}
		})
	}
}

func TestCommandSubstitution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"nested substitution", `FOO=$(echo "$(date)")`, `$(echo "$(date)")`},
		{"parenthesis inside quotes", `FOO=$(echo ")")`, `$(echo ")")`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := godotenv.Unmarshal(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got["FOO"] != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got["FOO"])
			}
		})
	}
}

func TestKeyValidation(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"F\u00aa=1", "F\u00bd=1", "F\u00d6=1", "F\u00e9=1"} {
		t.Run(input, func(t *testing.T) {
			t.Parallel()

			env, err := godotenv.Unmarshal(input)
			if err == nil {
				t.Errorf("expected %q to be rejected, got %v", input, env)
			}
		})
	}
}

func TestValueWhitespace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"no indentation before a value", "FOO= bar", "", true},
		{"whitespace-only value is empty", "FOO= ", "", false},
		{"trailing whitespace after unquoted value", "FOO=bar ", "bar", false},
		{"comment after whitespace is dropped", "FOO=bar # comment", "bar", false},
		{"trailing whitespace after an empty double quoted value", `FOO="" `, "", false},
		{"trailing whitespace after an empty single quoted value", `FOO='' `, "", false},
		{"trailing whitespace after an empty expansion", "FOO=$GODOTENV_TEST_UNSET_VARIABLE ", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := godotenv.ParseWithLookup(strings.NewReader(tt.input), func([]byte) ([]byte, bool) {
				return nil, false
			})
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got["FOO"] != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got["FOO"])
			}
		})
	}
}

func TestTrailingBackslashAtEOF(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"unquoted is literal", `FOO=abc\`, `abc\`, false},
		{"double quoted is unterminated", `FOO="abc\`, "", true},
		{"single quoted is unterminated", `FOO='abc\`, "", true},
		{"ansi-c quoted is unterminated", `FOO=$'abc\`, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := godotenv.Unmarshal(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got["FOO"] != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got["FOO"])
			}
		})
	}
}

func TestCommentFirstLineAndEOF(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  map[string]string
	}{
		{"leading comment", "# leading comment\nFOO=1\n", map[string]string{"FOO": "1"}},
		{"leading comment without newline", "# only a comment", map[string]string{}},
		{"comment at EOF without newline", "FOO=1\n# trailing comment", map[string]string{"FOO": "1"}},
		{"comment between values", "FOO=1\n# comment\nBAR=2\n", map[string]string{"FOO": "1", "BAR": "2"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := godotenv.Parse(strings.NewReader(tt.input))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(tt.want, got) {
				t.Errorf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func TestReadWhitespaceEnv(t *testing.T) {
	t.Parallel()

	got, err := godotenv.Read("fixtures/whitespace.env")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]string{
		"KEY_A": "a b c",
		"KEY_B": "1 2 3",
		"KEY_C": "bar",
		"KEY_D": "1 KEY_E=2",
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("expected %v, got %v", want, got)
	}
}

func TestReadLineContinuationEnv(t *testing.T) {
	t.Parallel()

	got, err := godotenv.Read("fixtures/line_continuation.env")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]string{
		"KEY_A": "ab",
		"KEY_B": "cd",
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("expected %v, got %v", want, got)
	}
}

func TestUnboundVariableOption(t *testing.T) {
	const unset = "GODOTENV_TEST_UNSET_VARIABLE"
	_ = os.Unsetenv(unset)

	tests := []struct {
		name    string
		input   string
		opts    []godotenv.Option
		want    map[string]string
		wantErr bool
	}{
		{"unset expands empty by default", "FOO=$" + unset, nil, map[string]string{"FOO": ""}, false},
		{"unset braced expands empty by default", "FOO=${" + unset + "}", nil, map[string]string{"FOO": ""}, false},
		{"unset unbraced errors with option", "FOO=$" + unset, []godotenv.Option{godotenv.WithUnboundError()}, nil, true},
		{"unset braced errors with option", "FOO=${" + unset + "}", []godotenv.Option{godotenv.WithUnboundError()}, nil, true},
		{"length of unset errors with option", "FOO=${#" + unset + "}", []godotenv.Option{godotenv.WithUnboundError()}, nil, true},
		{"unset strip errors with option", "FOO=${" + unset + "#a}", []godotenv.Option{godotenv.WithUnboundError()}, nil, true},
		{"unset longest strip errors with option", "FOO=${" + unset + "##a}", []godotenv.Option{godotenv.WithUnboundError()}, nil, true},
		{"unset suffix errors with option", "FOO=${" + unset + "%a}", []godotenv.Option{godotenv.WithUnboundError()}, nil, true},
		{"unset longest suffix errors with option", "FOO=${" + unset + "%%a}", []godotenv.Option{godotenv.WithUnboundError()}, nil, true},
		{"unset replace errors with option", "FOO=${" + unset + "/a/b}", []godotenv.Option{godotenv.WithUnboundError()}, nil, true},
		{"unset replace all errors with option", "FOO=${" + unset + "//a/b}", []godotenv.Option{godotenv.WithUnboundError()}, nil, true},
		{"unset anchored replace errors with option", "FOO=${" + unset + "/#a/b}", []godotenv.Option{godotenv.WithUnboundError()}, nil, true},
		{"unset anchored suffix replace errors with option", "FOO=${" + unset + "/%a/b}", []godotenv.Option{godotenv.WithUnboundError()}, nil, true},
		{"unset substring errors with option", "FOO=${" + unset + ":1}", []godotenv.Option{godotenv.WithUnboundError()}, nil, true},
		{"unset substring length errors with option", "FOO=${" + unset + ":1:2}", []godotenv.Option{godotenv.WithUnboundError()}, nil, true},
		{"unset inside strip pattern errors with option", "A=abc\nFOO=${A#${" + unset + "}}", []godotenv.Option{godotenv.WithUnboundError()}, nil, true},
		{"unset inside replace pattern errors with option", "A=abc\nFOO=${A/${" + unset + "}/x}", []godotenv.Option{godotenv.WithUnboundError()}, nil, true},
		{"default word is not an unbound error", "FOO=${" + unset + ":-fallback}", []godotenv.Option{godotenv.WithUnboundError()}, map[string]string{"FOO": "fallback"}, false},
		{"set variable is fine", "BAR=1\nFOO=$BAR", []godotenv.Option{godotenv.WithUnboundError()}, map[string]string{"BAR": "1", "FOO": "1"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := godotenv.New(tt.opts...).Unmarshal(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(tt.want, got) {
				t.Errorf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func TestNewDefaultsMatchPackageFuncs(t *testing.T) {
	t.Parallel()

	input := "FOO=bar\nBAZ=${FOO}"
	want, err := godotenv.Unmarshal(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := godotenv.New().Unmarshal(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("New() default differs: expected %v, got %v", want, got)
	}
}

func TestLoaderOptions(t *testing.T) {
	t.Parallel()

	t.Run("unbound error through Read", func(t *testing.T) {
		t.Parallel()

		_, err := godotenv.New(godotenv.WithUnboundError()).Read("fixtures/substitutions.env")
		if err == nil {
			t.Fatal("expected an error for the unset variable")
		}
		if msg := err.Error(); !strings.Contains(msg, "unbound variable") {
			t.Errorf("expected an unbound variable error, got: %v", err)
		}
	})

	t.Run("unbound error through Load", func(t *testing.T) {
		t.Parallel()

		if err := godotenv.New(godotenv.WithUnboundError()).Load("fixtures/substitutions.env"); err == nil {
			t.Fatal("expected an error for the unset variable")
		}
	})

	t.Run("unbound error through WithOverride", func(t *testing.T) {
		t.Parallel()

		if err := godotenv.New(godotenv.WithUnboundError(), godotenv.WithOverride()).Load("fixtures/substitutions.env"); err == nil {
			t.Fatal("expected an error for the unset variable")
		}
	})

	t.Run("single quote backslash is literal in both modes", func(t *testing.T) {
		t.Parallel()

		for _, opts := range [][]godotenv.Option{nil, {godotenv.WithBash()}} {
			got, err := godotenv.New(opts...).Unmarshal(`FOO='\'`)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got["FOO"] != `\` {
				t.Fatalf("expected %q, got %q", `\`, got["FOO"])
			}
		}
	})

	t.Run("default reads quoted env", func(t *testing.T) {
		t.Parallel()

		if _, err := godotenv.New().Read("fixtures/quoted.env"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestParameterExpansion(t *testing.T) {
	const unset = "GODOTENV_PE_UNSET"
	_ = os.Unsetenv(unset)

	tests := []struct {
		name    string
		input   string
		want    map[string]string
		wantErr bool
		bash    bool
	}{
		{"length", "GODOTENV_PE_A=hello\nFOO=${#GODOTENV_PE_A}", map[string]string{"GODOTENV_PE_A": "hello", "FOO": "5"}, false, false},
		{"length of empty", "GODOTENV_PE_A=\nFOO=${#GODOTENV_PE_A}", map[string]string{"GODOTENV_PE_A": "", "FOO": "0"}, false, false},
		{"default recursive", "GODOTENV_PE_B=bla\nFOO=${" + unset + ":-$GODOTENV_PE_B}", map[string]string{"GODOTENV_PE_B": "bla", "FOO": "bla"}, false, false},
		{"default nested", "GODOTENV_PE_B=bla\nFOO=${" + unset + ":-${GODOTENV_PE_B}}", map[string]string{"GODOTENV_PE_B": "bla", "FOO": "bla"}, false, false},
		{"alt recursive", "GODOTENV_PE_A=1\nFOO=${GODOTENV_PE_A:+${GODOTENV_PE_A}x}", map[string]string{"GODOTENV_PE_A": "1", "FOO": "1x"}, false, false},
		{"default with spaces", "FOO=${" + unset + ":-a b}", map[string]string{"FOO": "a b"}, false, false},
		{"error op with value", "GODOTENV_PE_A=x\nFOO=${GODOTENV_PE_A:?boom}", map[string]string{"GODOTENV_PE_A": "x", "FOO": "x"}, false, false},
		{"error op unset", "FOO=${" + unset + ":?boom}", nil, true, false},
		{"error op empty", "GODOTENV_PE_A=\nFOO=${GODOTENV_PE_A:?boom}", nil, true, false},
		{"qmark op unset", "FOO=${" + unset + "?boom}", nil, true, false},
		{"qmark op empty is set", "GODOTENV_PE_A=\nFOO=${GODOTENV_PE_A?boom}", map[string]string{"GODOTENV_PE_A": "", "FOO": ""}, false, false},
		{"empty braces", "FOO=${}", nil, true, false},
		{"assign op unsupported colon", "FOO=${" + unset + ":=x}", nil, true, false},
		{"assign op unsupported", "FOO=${" + unset + "=x}", nil, true, false},
		{"substring trailing junk", "A=hello\nFOO=${A:1:2:3}", nil, true, false},
		{"word quote closes before brace", "A=xyz\nB=${A-'a\\'b}", map[string]string{"A": "xyz", "B": "xyz"}, false, false},
		{"word unterminated quote errors", "A=xyz\nB=${A-'a\\'b'}", nil, true, false},
		{"word slash inside quote errors", "A=xyz\nB=${A/'/x}", nil, true, false},
		{"strip pattern unterminated quote errors", "A=xyz\nB=${A#'a\\'b'}", nil, true, false},
		{"default word unterminated quote errors", "A=xyz\nB=${A:-'a\\'b'}", nil, true, false},
		{"replace pattern with brace", "A=xyz\nB=${A/a{b/c}", map[string]string{"A": "xyz", "B": "xyz"}, false, true},
		{"unset word backslash in quotes", "B=${" + unset + "-'a\\'b}", map[string]string{"B": "a\\b"}, false, false},
		{"unset word unterminated quote errors", "B=${" + unset + "-'a\\'b'}", nil, true, false},
		{"word brace is literal", "B=${" + unset + ":-a{b}", map[string]string{"B": "a{b"}, false, false},
		{"word brace does not nest", "B=${" + unset + ":-a{b}c}", map[string]string{"B": "a{bc}"}, false, false},
		{"word brace with value", "GODOTENV_PE_A=xyz\nFOO=${GODOTENV_PE_A:-a{b}c}", map[string]string{"GODOTENV_PE_A": "xyz", "FOO": "xyzc}"}, false, false},
		{"word empty braces stay literal", "B=${" + unset + ":-{}}", map[string]string{"B": "{}"}, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := []godotenv.Option{}
			if tt.bash {
				opts = append(opts, godotenv.WithBash())
			}
			got, err := godotenv.New(opts...).Unmarshal(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(tt.want, got) {
				t.Errorf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func TestExpansionNestingIsBounded(t *testing.T) {
	t.Parallel()

	deep := strings.Repeat("${GODOTENV_TEST_UNSET_VARIABLE:-", 2000) + "x" + strings.Repeat("}", 2000)

	t.Run("word", func(t *testing.T) {
		t.Parallel()

		_, err := godotenv.Unmarshal("FOO=" + deep)
		if err == nil {
			t.Fatal("expected an error for deeply nested expansions")
		}
		if !strings.Contains(err.Error(), "expansion nesting too deep") {
			t.Error("expected a nesting-too-deep error")
		}
	})

	t.Run("strip pattern", func(t *testing.T) {
		t.Parallel()

		deepPattern := strings.Repeat("${GODOTENV_TEST_UNSET_VARIABLE#", 2000) + "x" + strings.Repeat("}", 2000)

		_, err := godotenv.Unmarshal("FOO=" + deepPattern)
		if err == nil {
			t.Fatal("expected an error for a deeply nested pattern")
		}
		if !strings.Contains(err.Error(), "expansion nesting too deep") {
			t.Error("expected a nesting-too-deep error")
		}
	})

	t.Run("replace replacement", func(t *testing.T) {
		t.Parallel()

		_, err := godotenv.New(godotenv.WithBash()).Unmarshal("A=x\nFOO=${A/a/" + deep + "}")
		if err == nil {
			t.Fatal("expected an error for a deeply nested replacement")
		}
		if !strings.Contains(err.Error(), "expansion nesting too deep") {
			t.Error("expected a nesting-too-deep error")
		}
	})

	t.Run("replace replacement column", func(t *testing.T) {
		t.Parallel()

		_, err := godotenv.New(godotenv.WithBash()).Unmarshal("A=x\nFOO=${A/a/" + deep + "}")
		if err == nil {
			t.Fatal("expected an error for a deeply nested replacement")
		}

		if msg := err.Error(); !strings.Contains(msg, "expansion nesting too deep on line 2, column 32011") {
			t.Errorf("unexpected error message: %s", msg)
		}
	})
}

func TestQuotedCloseBraceInExpansion(t *testing.T) {
	t.Parallel()

	got, err := godotenv.Unmarshal(`FOO=${GODOTENV_TEST_UNSET_VARIABLE:-"}"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "}"; got["FOO"] != want {
		t.Errorf("expected %q, got %q", want, got["FOO"])
	}
}

func TestLengthOperatorWithModifierErrors(t *testing.T) {
	t.Parallel()

	if _, err := godotenv.Unmarshal(`FOO=${#GODOTENV_TEST_UNSET_VARIABLE:-xyz}`); err == nil {
		t.Fatal("expected an error")
	}
}

func TestParameterPatternsAndSubstrings(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
		bash  bool
	}{
		{"strip prefix shortest", "A=hello\nFOO=${A#he}", "llo", false},
		{"strip prefix longest", "A=hello\nFOO=${A##*l}", "o", false},
		{"strip suffix shortest", "A=hello\nFOO=${A%lo}", "hel", false},
		{"strip suffix longest", "A=hello\nFOO=${A%%l*}", "he", false},
		{"strip no match", "A=hello\nFOO=${A#zzz}", "hello", false},
		{"strip star shortest", "A=hello\nFOO=${A#*}", "hello", false},
		{"strip star longest", "A=hello\nFOO=${A##*}", "", false},
		{"strip one char", "A=hello\nFOO=${A#?}", "ello", false},
		{"strip class", "A=hello\nFOO=${A#[a-z]}", "ello", false},
		{"replace first", "A=hello\nFOO=${A/l/L}", "heLlo", true},
		{"replace all", "A=hello\nFOO=${A//l/L}", "heLLo", true},
		{"replace remove", "A=hello\nFOO=${A/l/}", "helo", true},
		{"replace anchored start", "A=hello\nFOO=${A/#he/X}", "Xllo", true},
		{"replace anchored end", "A=hello\nFOO=${A/%lo/X}", "helX", true},
		{"path strip shortest", "A=a/b/c\nFOO=${A#*/}", "b/c", false},
		{"path strip longest", "A=a/b/c\nFOO=${A##*/}", "c", false},
		{"path suffix shortest", "A=a/b/c\nFOO=${A%/*}", "a/b", false},
		{"path suffix longest", "A=a/b/c\nFOO=${A%%/*}", "a", false},
		{"replace dot first", "A=a.b.c\nFOO=${A/./-}", "a-b.c", true},
		{"replace dot all", "A=a.b.c\nFOO=${A//./-}", "a-b-c", true},
		{"substring offset", "A=hello\nFOO=${A:1}", "ello", false},
		{"substring offset len", "A=hello\nFOO=${A:1:3}", "ell", false},
		{"substring negative", "A=hello\nFOO=${A: -2}", "lo", false},
		{"substring negative len", "A=hello\nFOO=${A: -4:2}", "el", false},
		{"substring negative end", "A=hello\nFOO=${A:2:-1}", "ll", false},
		{"substring out of range", "A=hello\nFOO=${A:99}", "", false},
		{"substring far negative", "A=hello\nFOO=${A: -99}", "", false},
		{"strip one character", "A=é\nFOO=${A#?}", "", false},
		{"length in characters", "A=é\nFOO=${#A}", "1", false},
		{"substring by character", "A=héllo\nFOO=${A:1:2}", "él", false},
		{"replace with wildcard", "A=héllo\nFOO=${A/h?llo/X}", "X", true},
		{"strip pattern with brace", "A=a{bX\nFOO=${A#a{b}", "X", false},
		{"strip pattern brace wildcard", "A=a{bX\nFOO=${A##*{b}", "X", false},
		{"replace pattern with unbalanced brace", "A=xyz\nFOO=${A/a{b}", "xyz", true},
		{"replace pattern with brace and replacement", "A=a{bX\nFOO=${A/a{b/REP}", "REPX", true},

		// expansions inside pattern operands
		{"expand prefix pattern", "A=abcdef\nB=abc\nFOO=${A#${B}}", "def", false},
		{"expand replace pattern", "A=abc\nB=b\nFOO=${A/${B}/X}", "aXc", true},
		{"expanded metacharacters are active", "A=abc\nB=*\nFOO=${A//${B}/X}", "X", true},
		{"quoted expanded pattern is literal", "A=abc\nB=*\nFOO=${A//\"${B}\"/X}", "abc", true},
		{"ansi-c quoted pattern is literal", `A=abc` + "\n" + `FOO=${A#$'*'}`, "abc", true},
		{"escaped metacharacter is literal", `A=a*c` + "\n" + `FOO=${A#\*}`, "a*c", false},
		{"expanded question mark is active", "A=abc\nB=a?\nFOO=${A#$B}", "c", false},
		{"nested expanded pattern", "A=abc\nB=ab\nFOO=${A#${B}c}", "", false},

		// zero-width and empty patterns in the // form
		{"replace all star matches whole", "A=abc\nFOO=${A//*/X}", "X", true},
		{"replace all star matches empty value", "A=\nFOO=${A//*/X}", "X", true},
		{"replace all empty pattern is a no-op", "A=abc\nFOO=${A///X}", "abc", true},
		{"replace all double star matches whole", "A=abc\nFOO=${A//**/X}", "X", true},
		{"replace all question replaces each rune", "A=héllo\nFOO=${A//?/x}", "xxxxx", true},
		{"replace all star removes all", "A=abc\nFOO=${A//*/}", "", true},
		{"replace all greedy star", "A=axbxc\nFOO=${A//x*/X}", "aX", true},
		{"replace empty pattern is a no-op", "A=abc\nFOO=${A/}", "abc", true},
		{"replace all empty pattern no-op", "A=abc\nFOO=${A//}", "abc", true},

		// # and % are literal in the // form
		{"literal hash in double slash", "A=aab\nFOO=${A//#a/X}", "aab", true},
		{"literal percent in double slash", "A=aab\nFOO=${A//%b/X}", "aab", true},
		{"anchored empty pattern prepends", "A=abc\nFOO=${A/#/X}", "Xabc", true},
		{"anchored empty pattern appends", "A=abc\nFOO=${A/%/X}", "abcX", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := []godotenv.Option{}
			if tt.bash {
				opts = append(opts, godotenv.WithBash())
			}
			got, err := godotenv.New(opts...).Unmarshal(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got["FOO"] != tt.want {
				t.Errorf("expected %q, got %q (map %v)", tt.want, got["FOO"], got)
			}
		})
	}
}

func TestSubstringBounds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"offset beyond the value", "A=hello\nFOO=${A:99999999999999999999}", ""},
		{"length beyond the value", "A=hello\nFOO=${A:1:99999999999999999999}", "ello"},
		{"length overflows an int", "A=hello\nFOO=${A:1:9223372036854775807}", "ello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := godotenv.Unmarshal(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got["FOO"] != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got["FOO"])
			}
		})
	}
}

func TestSubstringArithmetic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"expanded length", "z=abcdef\nFOO=${z:0:${#z}}", "abcdef"},
		{"arithmetic offset and length", "z=abcdef\nFOO=${z:1+1:2*2}", "cdef"},
		{"additive length", "z=abcdef\nFOO=${z:0:3+1}", "abcd"},
		{"parenthesised", "z=abcdef\nFOO=${z:(1):(2)}", "bc"},
		{"ternary offset and length", "z=abcdef\nGODOTENV_SUB_J=1\nFOO=${z:GODOTENV_SUB_J?1:0:GODOTENV_SUB_J}", "b"},
		{"ternary in offset", "z=abcdef\nFOO=${z:1?2:0:2}", "cd"},
		{"ternary in length", "z=abcdef\nFOO=${z:1:1?2:0}", "bc"},
		{"subtraction and addition", "z=abcdef\nFOO=${z:2-1:1+1}", "bc"},
		{"length beyond end", "z=abcdef\nFOO=${z:0:100}", "abcdef"},
		{"negative offset", "z=abcdef\nFOO=${z: -2}", "ef"},
		{"negative offset with length", "z=abcdef\nFOO=${z: -2:1}", "e"},
		{"negative length", "z=abcdef\nFOO=${z:1: -1}", "bcde"},
		{"zero length", "z=abcdef\nFOO=${z:1:0}", ""},
		{"modulo", "z=abcdef\nFOO=${z:0:4%3}", "a"},
		{"logical not", "z=abcdef\nFOO=${z:0:!0}", "a"},
		{"logical and", "z=abcdef\nFOO=${z:0:1&&2}", "a"},
		{"logical or", "z=abcdef\nFOO=${z:0:1||0}", "a"},
		{"equality", "z=abcdef\nFOO=${z:0:1==1}", "a"},
		{"unset arithmetic variable is zero", "z=abcdef\nFOO=${z:GODOTENV_SUB_J?1:0:GODOTENV_SUB_J}", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := godotenv.Unmarshal(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got["FOO"] != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got["FOO"])
			}
		})
	}
}

func TestSubstringArithmeticRecursionIsBounded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{"self reference", "a=a\nFOO=${z:a:1}"},
		{"mutual reference", "a=b\nb=a\nFOO=${z:a:1}"},
		{"deep unary", "FOO=${z:" + strings.Repeat("!", 20000) + "0:1}"},
		{"deep parentheses", "FOO=${z:" + strings.Repeat("(", 20000) + "0" + strings.Repeat(")", 20000) + ":1}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := godotenv.Unmarshal(tt.input); err == nil {
				t.Fatal("expected an error, got none")
			}
		})
	}

	t.Run("moderate nesting still evaluates", func(t *testing.T) {
		t.Parallel()

		input := "z=abcdef\nFOO=${z:" + strings.Repeat("(", 500) + "0" + strings.Repeat(")", 500) + ":1}"

		got, err := godotenv.Unmarshal(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got["FOO"] != "a" {
			t.Errorf("expected %q, got %q", "a", got["FOO"])
		}
	})
}

func TestUnterminatedGlobClass(t *testing.T) {
	t.Parallel()

	got, err := godotenv.Unmarshal("A=[abcx\nFOO=${A#[abc}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "x"; got["FOO"] != want {
		t.Errorf("expected %q, got %q", want, got["FOO"])
	}
}

func TestReplacePatternLeadingSlash(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"leading slash pattern replaces all", "a=/a\nOUT=${a///a/}", ""},
		{"leading slash pattern no match", "A=abc\nOUT=${A///X}", "abc"},
		{"leading slash pattern with trailing separator", "A=abc\nOUT=${A///X/}", "abc"},
		{"empty pattern is a slash", "A=a/b/c\nOUT=${A///}", "abc"},
		{"leading slash with replacement", "a=/a\nOUT=${a///a/x}", "x"},
		{"leading slash match leaves tail", "A=/a/b\nOUT=${A///a/}", "/b"},
		{"single slash replaces first", "A=abc\nOUT=${A//b}", "ac"},
		{"escaped slash pattern", `A=a/b/c` + "\n" + `OUT=${A//\//X}`, "aXbXc"},
		{"single escaped slash", `A=a/b/c` + "\n" + `OUT=${A/\//X}`, "aXb/c"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := godotenv.New(godotenv.WithBash()).Unmarshal(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got["OUT"] != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got["OUT"])
			}
		})
	}
}

func TestANSICQuoting(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"newline", `FOO=$'a\nb'`, "a\nb"},
		{"tab", `FOO=$'a\tb'`, "a\tb"},
		{"bell", `FOO=$'a\ab'`, "a\ab"},
		{"backslash", `FOO=$'a\\b'`, `a\b`},
		{"single quote", `FOO=$'it\'s'`, "it's"},
		{"double quote", `FOO=$'say \"hi\"'`, `say "hi"`},
		{"hex", `FOO=$'\x41'`, "A"},
		{"octal", `FOO=$'\101'`, "A"},
		{"unicode", `FOO=$'\u0041'`, "A"},
		{"unknown escape", `FOO=$'\q'`, `\q`},
		{"plain", `FOO=$'hello'`, "hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := godotenv.New(godotenv.WithBash()).Unmarshal(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got["FOO"] != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got["FOO"])
			}
		})
	}
}

func TestANSICBracedHexEscapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty braces truncates the value", `FOO=$'ab\x{}cd'`, "ab"},
		{"braced hex", `FOO=$'ab\x{41}cd'`, "abAcd"},
		{"unbraced hex", `FOO=$'ab\x41cd'`, "abAcd"},
		{"single braced digit", `FOO=$'ab\x{4}cd'`, "ab\x04cd"},
		{"single unbraced digit", `FOO=$'ab\x4cd'`, "abLd"},
		{"unterminated brace keeps low byte", `FOO=$'ab\x{cde'`, "ab\xde"},
		{"brace closes before trailing text", `FOO=$'ab\x{cd}e'`, "ab\xcde"},
		{"single digit brace", `FOO=$'ab\x{c}de'`, "ab\fde"},
		{"unterminated multi-digit", `FOO=$'\x{abcX'`, "\xbcX"},
		{"brace then close", `FOO=$'\x{ab}cX'`, "\xabcX"},
		{"empty braces only", `FOO=$'\x{}X'`, ""},
		{"open brace then text", `FOO=$'\x{X'`, ""},
		{"long run keeps low byte", `FOO=$'\x{01234567X'`, "gX"},
		{"braced hex then text", `FOO=$'\x{41}b'`, "Ab"},
		{"empty braces with tail", `FOO=$'\x{}bc'`, ""},
		{"single digit then text", `FOO=$'\x{1}bcd'`, "\x01bcd"},
		{"unterminated bde", `FOO=$'\x{bde'`, "\xde"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := godotenv.New(godotenv.WithBash()).Unmarshal(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got["FOO"] != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got["FOO"])
			}
		})
	}
}

func TestSingleQuoteInsideBracedExpansionInDoubleQuotes(t *testing.T) {
	t.Parallel()

	lookup := func([]byte) ([]byte, bool) { return nil, false }
	tests := []struct {
		name  string
		input string
		want  string
		bash  bool
	}{
		{"word keeps quotes and expands", `foo=bar` + "\n" + `OUT="${foo+'$foo'}"`, "'bar'", false},
		{"posix word quote does not hide the closing brace", `OUT="${GODOTENV_SQ_UNSET:-'}'}"`, "''}", false},
		{"posix two quoted words", `OUT="${GODOTENV_SQ_UNSET:-'}'}'}"`, "''}'}", false},
		{"bash word quote hides the closing brace", `OUT="${GODOTENV_SQ_UNSET:-'}'}"`, "'}'", true},
		{"bash two quoted words", `OUT="${GODOTENV_SQ_UNSET:-'}'}'}"`, "'}''}", true},
		{"pattern quote is removed", `x="}"` + "\n" + `OUT="${x##'}'}"`, "", false},
		{"replacement quote is removed", "x=ab\nOUT=\"${x/b/'Z'}\"", "aZ", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var (
				got map[string]string
				err error
			)
			if tt.bash {
				got, err = godotenv.New(godotenv.WithBash()).Parse(strings.NewReader(tt.input))
			} else {
				got, err = godotenv.ParseWithLookup(strings.NewReader(tt.input), lookup)
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got["OUT"] != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got["OUT"])
			}
		})
	}
}

func TestDoubleQuotedBracedWordOperand(t *testing.T) {
	t.Parallel()

	lookup := func([]byte) ([]byte, bool) { return nil, false }
	tests := []struct {
		name  string
		input string
		want  string
		bash  bool
	}{
		// POSIX mode follows dash: a single quote in the word is an ordinary
		// byte, and a backslash before a non-special byte stays literal.
		{"posix single quote is literal", `OUT="${GODOTENV_WORD_UNSET-'x'}"`, "'x'", false},
		{"posix backslash e is literal", `OUT="${GODOTENV_WORD_UNSET-\e}"`, `\e`, false},
		{"posix backslash q is literal", `OUT="${GODOTENV_WORD_UNSET-\q}"`, `\q`, false},
		{"posix backslash n is literal", `OUT="${GODOTENV_WORD_UNSET-\n}"`, `\n`, false},
		{"posix backslash t is literal", `OUT="${GODOTENV_WORD_UNSET-\t}"`, `\t`, false},
		{"posix colon single quote is literal", `OUT="${GODOTENV_WORD_UNSET:-'x'}"`, "'x'", false},
		{"posix colon backslash e is literal", `OUT="${GODOTENV_WORD_UNSET:-\e}"`, `\e`, false},
		{"posix single quote does not group", `OUT="${GODOTENV_WORD_UNSET-'}'}"`, "''}", false},
		{"posix colon single quote does not group", `OUT="${GODOTENV_WORD_UNSET:-'}'}"`, "''}", false},
		{"posix backslash escapes closing brace", `OUT="${GODOTENV_WORD_UNSET-\}}"`, "}", false},
		{"posix backslash open brace is literal", `OUT="${GODOTENV_WORD_UNSET-\{}"`, `\{`, false},
		{"posix double backslash", `OUT="${GODOTENV_WORD_UNSET-\\}"`, `\`, false},
		{"posix backslash dollar", `OUT="${GODOTENV_WORD_UNSET-\$}"`, "$", false},
		{"posix ansi quote stays literal", `OUT="${GODOTENV_WORD_UNSET-$'z'}"`, `$'z'`, false},
		{"posix skipped word ends at first brace", "v=abc\n" + `OUT="${v-'}'}"`, "abc'}", false},
		// bash mode keeps grouping the word's single quotes and expands $'...'.
		{"bash single quote is literal", `OUT="${GODOTENV_WORD_UNSET-'x'}"`, "'x'", true},
		{"bash backslash e is literal", `OUT="${GODOTENV_WORD_UNSET-\e}"`, `\e`, true},
		{"bash backslash q is literal", `OUT="${GODOTENV_WORD_UNSET-\q}"`, `\q`, true},
		{"bash backslash n is literal", `OUT="${GODOTENV_WORD_UNSET-\n}"`, `\n`, true},
		{"bash backslash t is literal", `OUT="${GODOTENV_WORD_UNSET-\t}"`, `\t`, true},
		{"bash colon single quote is literal", `OUT="${GODOTENV_WORD_UNSET:-'x'}"`, "'x'", true},
		{"bash colon backslash e is literal", `OUT="${GODOTENV_WORD_UNSET:-\e}"`, `\e`, true},
		{"bash single quote groups", `OUT="${GODOTENV_WORD_UNSET-'}'}"`, "'}'", true},
		{"bash colon single quote groups", `OUT="${GODOTENV_WORD_UNSET:-'}'}"`, "'}'", true},
		{"bash backslash escapes closing brace", `OUT="${GODOTENV_WORD_UNSET-\}}"`, "}", true},
		{"bash backslash open brace is literal", `OUT="${GODOTENV_WORD_UNSET-\{}"`, `\{`, true},
		{"bash ansi quote expands", `OUT="${GODOTENV_WORD_UNSET-$'z'}"`, "z", true},
		{"bash skipped word groups quotes", "v=abc\n" + `OUT="${v-'}'}"`, "abc", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var (
				got map[string]string
				err error
			)
			if tt.bash {
				got, err = godotenv.New(godotenv.WithBash()).Parse(strings.NewReader(tt.input))
			} else {
				got, err = godotenv.ParseWithLookup(strings.NewReader(tt.input), lookup)
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got["OUT"] != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got["OUT"])
			}
		})
	}
}

func TestUnicodeEscapeInDoubleQuotes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"ascii", `FOO="\u0041"`, "A"},
		{"latin", `FOO="caf\u00e9"`, "café"},
		{"hex", `FOO="\x41"`, "A"},
		{"astral", `FOO="\U0001F600"`, "😀"},
		{"malformed unicode keeps backslash", `FOO="\u"`, `\u`},
		{"malformed unicode keeps backslash and tail", `FOO="\uZZ"`, `\uZZ`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := godotenv.New(godotenv.WithBash()).Unmarshal(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got["FOO"] != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got["FOO"])
			}
		})
	}
}

func TestDoubleQuoteBellAndVerticalTab(t *testing.T) {
	tests := []struct {
		name  string
		opts  []godotenv.Option
		input string
		want  string
	}{
		{"posix bell literal", nil, `FOO="a\ab"`, `a\ab`},
		{"posix vertical tab literal", nil, `FOO="a\vb"`, `a\vb`},
		{"bash bell", []godotenv.Option{godotenv.WithBash()}, `FOO="a\ab"`, "a\ab"},
		{"bash vertical tab", []godotenv.Option{godotenv.WithBash()}, `FOO="a\vb"`, "a\vb"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := godotenv.New(tt.opts...).Unmarshal(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got["FOO"] != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got["FOO"])
			}
		})
	}
}

func TestMarshalRoundTripControlEscapes(t *testing.T) {
	t.Parallel()

	src := map[string]string{
		"BELL": "x\ay",
		"VT":   "x\vy",
		"MIX":  "\a\v\n\t\r",
	}
	out, err := godotenv.Marshal(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := godotenv.New(godotenv.WithBash()).Unmarshal(out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(src, got) {
		t.Errorf("expected round trip %v, got %v", src, got)
	}
}

func TestMarshalEscapesShellSpecials(t *testing.T) {
	t.Parallel()

	src := map[string]string{
		"DOLLAR":   "a$b",
		"BRACED":   "${X}",
		"CMDSUB":   "$(echo hi)",
		"BACKTICK": "`echo hi`",
	}
	out, err := godotenv.Marshal(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, special := range []string{"$", "`"} {
		if strings.Contains(strings.ReplaceAll(out, "\\"+special, ""), special) {
			t.Errorf("output leaves %q unescaped, so sourcing it would expand:\n%s", special, out)
		}
	}

	got, err := godotenv.Unmarshal(out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(src, got) {
		t.Errorf("expected round trip %v, got %v", src, got)
	}
}

func TestPOSIXDefaults(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"double quote escape stays literal", `FOO="a\nb"`, `a\nb`},
		{"tab stays literal", `FOO="a\tb"`, `a\tb`},
		{"escaped quote", `FOO="a\"b"`, `a"b`},
		{"escaped backslash", `FOO="a\\b"`, `a\b`},
		{"escaped dollar", `FOO="a\$b"`, "a$b"},
		{"unknown escape literal", `FOO="a\qb"`, `a\qb`},
		{"unicode literal", `FOO="\u0041"`, `\u0041`},
		{"hex literal", `FOO="\x41"`, `\x41`},
		{"astral literal", `FOO="\U0001F600"`, `\U0001F600`},
		{"line continuation", "FOO=\"a\\\nb\"", "ab"},
		{"single quote backslash literal", `FOO='a\'`, `a\`},
		{"single quote backslash", `FOO='a\b'`, `a\b`},
		{"ansi-c stays literal", `FOO=$'a\nb'`, `$a\nb`},
		{"dollar double quote stays literal", `FOO=$"a\nb"`, `$a\nb`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := godotenv.New().Unmarshal(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got["FOO"] != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got["FOO"])
			}
		})
	}
}

func TestWithBash(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"double quote escape", `FOO="a\nb"`, "a\nb"},
		{"tab", `FOO="a\tb"`, "a\tb"},
		{"escaped quote", `FOO="a\"b"`, `a"b`},
		{"escaped backslash", `FOO="a\\b"`, `a\b`},
		{"escaped dollar", `FOO="a\$b"`, "a$b"},
		{"unknown escape drops the backslash", `FOO="a\qb"`, `aqb`},
		{"unicode", `FOO="\u0041"`, "A"},
		{"hex", `FOO="\x41"`, "A"},
		{"astral", `FOO="\U0001F600"`, "😀"},
		{"line continuation", "FOO=\"a\\\nb\"", "ab"},
		{"ansi-c expands", `FOO=$'a\nb'`, "a\nb"},
		{"dollar double quote expands", `FOO=$"a\nb"`, "a\nb"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := godotenv.New(godotenv.WithBash()).Unmarshal(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got["FOO"] != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got["FOO"])
			}
		})
	}
}

func TestReplacementFamilyInDefaultMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"A=axb\nFOO=${A/x/y}", "ayb"},
		{"A=xaxb\nFOO=${A//x/y}", "yayb"},
		{"A=xaxb\nFOO=${A/#x/y}", "yaxb"},
		{"A=xaxb\nFOO=${A/%b/y}", "xaxy"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()

			for _, opts := range [][]godotenv.Option{nil, {godotenv.WithBash()}} {
				got, err := godotenv.New(opts...).Unmarshal(tt.input)
				if err != nil {
					t.Fatalf("Unmarshal(%q): %v", tt.input, err)
				}
				if got["FOO"] != tt.want {
					t.Errorf("Unmarshal(%q) = %q, want %q", tt.input, got["FOO"], tt.want)
				}
			}
		})
	}
}

func TestArithmeticExpansion(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"addition", "FOO=$((1+2))", "3", false},
		{"precedence", "FOO=$((1+2*3))", "7", false},
		{"nested parens", "FOO=$((1+(2*3)))", "7", false},
		{"variable", "A=4\nFOO=$((A+1))", "5", false},
		{"dollar variable", "A=4\nFOO=$((1+$A))", "5", false},
		{"hex", "FOO=$((0x10+1))", "17", false},
		{"inside double quotes", `FOO="$((2*3))"`, "6", false},
		{"inside word", "FOO=${GODOTENV_DIFF_NOPE:-$((2*3))}", "6", false},
		{"unset variable is zero", "FOO=$((GODOTENV_DIFF_NOPE+5))", "5", false},
		{"division by zero", "FOO=$((1/0))", "", true},
		{"modulo by zero", "FOO=$((1%0))", "", true},
		{"syntax error", "FOO=$((1+))", "", true},
		{"unclosed", "FOO=$((1+2)", "", true},
	}

	modes := []struct {
		name string
		opts []godotenv.Option
	}{
		{"posix", nil},
		{"bash", []godotenv.Option{godotenv.WithBash()}},
	}

	for _, mode := range modes {
		for _, tt := range tests {
			t.Run(mode.name+"/"+tt.name, func(t *testing.T) {
				t.Parallel()

				got, err := godotenv.New(mode.opts...).Unmarshal(tt.input)
				if tt.wantErr {
					if err == nil {
						t.Fatal("expected an error, got nil")
					}
					return
				}
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got["FOO"] != tt.want {
					t.Errorf("expected %q, got %q", tt.want, got["FOO"])
				}
			})
		}
	}
}

func TestArithmeticEmpty(t *testing.T) {
	t.Parallel()

	if _, err := godotenv.Unmarshal("FOO=$(( ))"); err == nil {
		t.Error("posix: expected an error for empty arithmetic")
	}

	got, err := godotenv.New(godotenv.WithBash()).Unmarshal("FOO=$(( ))")
	if err != nil {
		t.Fatalf("bash: unexpected error: %v", err)
	}
	if got["FOO"] != "0" {
		t.Errorf("bash: expected %q, got %q", "0", got["FOO"])
	}
}

// shellCase pairs a dotenv line with the equivalent shell statement, so a real
// shell can be asked for the expected value.
type shellCase struct {
	name   string
	input  string
	script string
}

func runShell(t *testing.T, shell, script string) (out string, ok bool) {
	t.Helper()

	cmd := exec.Command(shell, "-c", script)
	cmd.Env = []string{"LC_ALL=C"}

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &bytes.Buffer{}

	if err := cmd.Run(); err != nil {
		return "", false
	}

	return stdout.String(), true
}

func findShell(t *testing.T, candidates ...string) string {
	t.Helper()

	for _, candidate := range candidates {
		if path, err := exec.LookPath(candidate); err == nil {
			return path
		}
	}
	t.Skipf("none of %v found", candidates)

	return ""
}

func TestPOSIXMatchesDash(t *testing.T) {
	t.Parallel()

	shell := findShell(t, "/bin/dash")

	cases := []shellCase{
		{"dq tab stays literal", `FOO="a\tb"`, `FOO="a\tb"; printf '%s' "$FOO"`},
		{"dq escaped dollar", "X=hello\nFOO=\"a\\$X\"", `X=hello; FOO="a\$X"; printf '%s' "$FOO"`},
		{"dq escaped backslash", `FOO="a\\b"`, `FOO="a\\b"; printf '%s' "$FOO"`},
		{"ansi-c is literal", `FOO=$'a\tb'`, `FOO=$'a\tb'; printf '%s' "$FOO"`},
		{"dollar double quote is literal", `FOO=$"a\nb"`, `FOO=$"a\nb"; printf '%s' "$FOO"`},
		{"arithmetic", `FOO=$((1+2*3))`, `FOO=$((1+2*3)); printf '%s' "$FOO"`},
		{"strip prefix", "A=hello\nFOO=${A#he}", `A=hello; FOO=${A#he}; printf '%s' "$FOO"`},
		{"strip suffix", "A=abc\nFOO=${A%bc}", `A=abc; FOO=${A%bc}; printf '%s' "$FOO"`},
		{"length", "A=hello\nFOO=${#A}", `A=hello; FOO=${#A}; printf '%s' "$FOO"`},
		{"default word", `FOO=${GODOTENV_DIFF_NOPE:-fallback}`, `FOO=${GODOTENV_DIFF_NOPE:-fallback}; printf '%s' "$FOO"`},
	}

	lookup := func([]byte) ([]byte, bool) { return nil, false }

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			want, ok := runShell(t, shell, tc.script)
			if !ok {
				t.Fatalf("dash failed: %s", tc.script)
			}
			got, err := godotenv.ParseWithLookup(strings.NewReader(tc.input), lookup)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got["FOO"] != want {
				t.Errorf("dash %q, parser %q", want, got["FOO"])
			}
		})
	}

	t.Run("replacement is a deliberate extension", func(t *testing.T) {
		t.Parallel()

		if _, ok := runShell(t, shell, `A=abc; FOO=${A/b/X}; printf '%s' "$FOO"`); ok {
			t.Fatal("dash unexpectedly accepted a replacement")
		}

		got, err := godotenv.ParseWithLookup(strings.NewReader("A=abc\nFOO=${A/b/X}"), lookup)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got["FOO"] != "aXc" {
			t.Errorf("parser %q, want %q", got["FOO"], "aXc")
		}
	})
}

func TestBashMatchesBash(t *testing.T) {
	t.Parallel()

	shell := findShell(t, "/opt/homebrew/bin/bash", "bash")

	cases := []shellCase{
		{"ansi-c tab", `FOO=$'a\tb'`, `FOO=$'a\tb'; printf '%s' "$FOO"`},
		{"ansi-c hex", `FOO=$'a\x41b'`, `FOO=$'a\x41b'; printf '%s' "$FOO"`},
		{"ansi-c newline", `FOO=$'a\nb'`, `FOO=$'a\nb'; printf '%s' "$FOO"`},
		{"dollar double quote", `FOO=$"hello"`, `FOO=$"hello"; printf '%s' "$FOO"`},
		{"replace first", "A=hello\nFOO=${A/l/L}", `A=hello; FOO=${A/l/L}; printf '%s' "$FOO"`},
		{"replace all", "A=hello\nFOO=${A//l/L}", `A=hello; FOO=${A//l/L}; printf '%s' "$FOO"`},
		{"replace anchored start", "A=hello\nFOO=${A/#he/X}", `A=hello; FOO=${A/#he/X}; printf '%s' "$FOO"`},
		{"replace anchored end", "A=hello\nFOO=${A/%lo/X}", `A=hello; FOO=${A/%lo/X}; printf '%s' "$FOO"`},
		{"strip single-quoted pattern", "A=abcd\nFOO=${A#'ab'}", `A=abcd; FOO=${A#'ab'}; printf '%s' "$FOO"`},
		{"strip double-quoted pattern", "A=abcd\nFOO=\"${A#\"ab\"}\"", `A=abcd; FOO="${A#"ab"}"; printf '%s' "$FOO"`},
		{"brace pattern", "A=}abc\nFOO=\"${A#'}'}\"", `A=}abc; FOO="${A#'}'}"; printf '%s' "$FOO"`},
		{"longest brace pattern", "A=}}abc\nFOO=\"${A##'}'}\"", `A=}}abc; FOO="${A##'}'}"; printf '%s' "$FOO"`},
		{"arithmetic", `FOO=$((1+2*3))`, `FOO=$((1+2*3)); printf '%s' "$FOO"`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			want, ok := runShell(t, shell, tc.script)
			if !ok {
				t.Fatalf("bash failed: %s", tc.script)
			}
			got, err := godotenv.New(godotenv.WithBash()).Unmarshal(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got["FOO"] != want {
				t.Errorf("bash %q, parser %q", want, got["FOO"])
			}
		})
	}
}

func TestExpand(t *testing.T) {
	lookup := func(name []byte) ([]byte, bool) {
		switch string(name) {
		case "HOME":
			return []byte("/home/me"), true
		case "EMPTY":
			return []byte(""), true
		}
		return nil, false
	}

	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
		opts    []godotenv.Option
	}{
		{"braced", "path=${HOME}/x", "path=/home/me/x", false, nil},
		{"unbraced", "path=$HOME/x", "path=/home/me/x", false, nil},
		{"default", "${MISSING:-fallback}", "fallback", false, nil},
		{"dash default", "${MISSING-fallback}", "fallback", false, nil},
		{"colon plus on empty", "${EMPTY:+set}", "", false, nil},
		{"plus on empty", "${EMPTY+set}", "set", false, nil},
		{"recursive", "${MISSING:-${HOME}}", "/home/me", false, nil},
		{"braced unset is empty", "${MISSING}", "", false, nil},
		{"unbraced unset is empty", "$MISSING", "", false, nil},
		{"braced unset errors", "${MISSING}", "", true, []godotenv.Option{godotenv.WithUnboundError()}},
		{"unbraced unset errors", "$MISSING", "", true, []godotenv.Option{godotenv.WithUnboundError()}},
		{"ansi-c stays literal", `$'a\nb'`, `$'a\nb'`, false, nil},
		{"ansi-c with WithBash", `$'a\tb'`, "a\tb", false, []godotenv.Option{godotenv.WithBash()}},
		{"positional stays literal", "$1", "$1", false, nil},
		{"special stays literal", "$?", "$?", false, nil},
		{"command substitution stays literal", "$(echo hi)", "$(echo hi)", false, nil},
		{"backticks stay literal", "`echo hi`", "`echo hi`", false, nil},
		{"trailing dollar", "cost is $", "cost is $", false, nil},
		{"plain text", "just a token", "just a token", false, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := godotenv.Expand(tt.in, lookup, tt.opts...)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestLineContinuationWithCRLF(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
		opts  []godotenv.Option
	}{
		{"unquoted", "FOO=a\\\r\nb", "ab", nil},
		{"posix double quoted keeps the backslash", "FOO=\"a\\\r\nb\"", "a\\\r\nb", nil},
		{"bash double quoted is a continuation", "FOO=\"a\\\r\nb\"", "ab", []godotenv.Option{godotenv.WithBash()}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := godotenv.New(tt.opts...).Unmarshal(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got["FOO"] != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got["FOO"])
			}
		})
	}
}

func TestLineEndings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  map[string]string
	}{
		{"LF terminates the line", "FOO=a\nBAR=2", map[string]string{"FOO": "a", "BAR": "2"}},
		{"CRLF terminates the line", "FOO=a\r\nBAR=2", map[string]string{"FOO": "a", "BAR": "2"}},
		{"bare CR is data", "FOO=a\rBAR=2", map[string]string{"FOO": "a\rBAR=2"}},
		{"bare CR keeps the rest of the line", "FOO=a\rb", map[string]string{"FOO": "a\rb"}},
		{"bare CR is data at end of input", "FOO=a\r", map[string]string{"FOO": "a\r"}},
		{"bare CR is data for an empty value", "FOO=\r", map[string]string{"FOO": "\r"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := godotenv.Unmarshal(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(tt.want, got) {
				t.Errorf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func TestANSICQuotingContext(t *testing.T) {
	t.Parallel()

	posix := []struct {
		name  string
		input string
		want  string
	}{
		{"ansi-c inside double quotes is literal", `FOO="$'a\nb'"`, `$'a\nb'`},
		{"ansi-c in an unquoted word is literal", `FOO=${GODOTENV_TEST_MISSING:-$'a\nb'}`, `$a\nb`},
		{"dollar double quote keeps the dollar", `FOO=$"a$'a\nb'b"`, `$a$'a\nb'b`},
		{"dollar double quote keeps escapes", `FOO=$"a\n$'a\nb'b"`, `$a\n$'a\nb'b`},
	}

	for _, tt := range posix {
		t.Run("posix "+tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := godotenv.Unmarshal(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got["FOO"] != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got["FOO"])
			}
		})
	}

	bash := []struct {
		name  string
		input string
		want  string
	}{
		{"ansi-c inside double quotes is not decoded", `FOO="$'a\nb'"`, "$'a\nb'"},
		{"ansi-c in an unquoted word expands", `FOO=${GODOTENV_TEST_MISSING:-$'a\nb'}`, "a\nb"},
		{"dollar double quote behaves as double quotes", `FOO=$"a$'a\nb'b"`, "a$'a\nb'b"},
		{"dollar double quote keeps escapes", `FOO=$"a\n$'a\nb'b"`, "a\n$'a\nb'b"},
	}

	for _, tt := range bash {
		t.Run("bash "+tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := godotenv.New(godotenv.WithBash()).Unmarshal(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got["FOO"] != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got["FOO"])
			}
		})
	}
}

func TestErrorReadDirectory(t *testing.T) {
	t.Parallel()

	envFileName := "fixtures/"
	envMap, err := godotenv.Read(envFileName)
	if err == nil {
		t.Errorf("Expected error, got %v: %s", envMap, err)
	}
}

func TestErrorParsing(t *testing.T) {
	t.Parallel()

	envFileName := "fixtures/invalid1.env"
	envMap, err := godotenv.Read(envFileName)
	if err == nil {
		t.Errorf("Expected error, got %v: %s", envMap, err)
	}
}

func TestParseErrorMessages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
		bash  bool
	}{
		{"empty key", "=foo", "empty key", false},
		{"key starting with a digit", "1FOO=1", "invalid character in key name", false},
		{"key starting with a dot", ".FOO=1", "invalid character in key name", false},
		{"column of the offending key character", "A=1\nB C=2", "unexpected whitespace in key on line 2, column 2", false},
		{"unclosed expansion", "A=${FOO", "unexpected EOF while looking for matching '}'", false},
		{"unclosed expansion before a newline", "A=${FOO\nB=1", "unexpected EOF while looking for matching '}'", false},
		{"unclosed expansion after a colon", "A=${FOO:", "unexpected EOF while looking for matching '}'", false},
		{"column of the substring operator", "FOO=hello\nA=${FOO:1+}", "bad substitution: invalid substring on line 2, column 9", false},
		{"column of the unmatched quote", "A=$'x", "unmatched single quote on line 1, column 4", true},
		{"column of the substring length", "A=hello\nFOO=${A:1:2:3}", "bad substitution: invalid substring length on line 2, column 9", false},
		{"unclosed expansion with assignment", "A=${FOO=", "unexpected EOF while looking for matching '}' on line 1, column 4", false},
		{"closed assignment operator", "A=${FOO=}", "bad substitution: assignment is not supported", false},
		{"column of the unsupported operator", "FOO=${BAR@x}", "bad substitution: unsupported operator on line 1, column 10", false},
		{"column of the assignment operator", "FOO=${BAR=}", "bad substitution: assignment is not supported on line 1, column 10", false},
		{"column of the missing modifier", "FOO=${BAR:}", "bad substitution: no modifier on line 1, column 10", false},
		{"column of the empty nested expansion", "FOO=${BAR:-${}}", "bad substitution: empty on line 1, column 13", false},
		{"unclosed length operator", "A=${#FOO\nB=1", "unexpected EOF while looking for matching '}'", false},
		{"unclosed expansion with empty name", "FOO=${ ", "unexpected EOF while looking for matching '}'", false},
		{"unclosed expansion starting with a quote", "A=${'000", "unexpected EOF while looking for matching '}'", false},
		{"closed expansion with invalid name start", "FOO=${ x}", "bad substitution", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := []godotenv.Option{}
			if tt.bash {
				opts = append(opts, godotenv.WithBash())
			}
			_, err := godotenv.New(opts...).Unmarshal(tt.input)
			if err == nil {
				t.Fatalf("expected an error for %q", tt.input)
			}
			if msg := err.Error(); !strings.Contains(msg, tt.want) {
				t.Errorf("expected error to contain %q, got:\n%s", tt.want, msg)
			}
		})
	}

	t.Run("error format is a single line", func(t *testing.T) {
		t.Parallel()

		_, err := godotenv.Unmarshal("A=1\nB C=2")
		if err == nil {
			t.Fatal("expected an error")
		}
		if want := "godotenv: unexpected whitespace in key on line 2, column 2"; err.Error() != want {
			t.Errorf("expected %q, got %q", want, err.Error())
		}
	})

	t.Run("unbound variable in Expand", func(t *testing.T) {
		t.Parallel()

		_, err := godotenv.Expand("$GODOTENV_TEST_UNSET_VARIABLE", func([]byte) ([]byte, bool) {
			return nil, false
		}, godotenv.WithUnboundError())
		if err == nil {
			t.Fatal("expected an error")
		}
		if msg := err.Error(); !strings.Contains(msg, "unbound variable") {
			t.Errorf("expected error to mention the unbound variable, got:\n%s", msg)
		}
	})

	t.Run("unclosed word reports EOF before an inner unbound error", func(t *testing.T) {
		t.Parallel()

		const unset = "GODOTENV_TEST_UNSET_VARIABLE"

		_, err := godotenv.New(godotenv.WithUnboundError()).Unmarshal("A=${" + unset + ":-${" + unset + "}")
		if err == nil {
			t.Fatal("expected an error")
		}
		if msg := err.Error(); !strings.Contains(msg, "unexpected EOF while looking for matching '}'") {
			t.Errorf("expected an EOF error, got:\n%s", msg)
		}
	})
}

func TestParseErrorControlCharacter(t *testing.T) {
	t.Parallel()

	_, err := godotenv.Unmarshal("FOO=a\x01b")
	if err == nil {
		t.Fatal("expected an error for the control character")
	}
	if want := "godotenv: invalid value character: 0x01 on line 1, column 6"; err.Error() != want {
		t.Errorf("expected %q, got %q", want, err.Error())
	}
}

// just test some single lines to show the general idea
func TestWrite(t *testing.T) {
	tests := []struct {
		env      string
		expected string
		bash     bool
	}{
		// values are always double-quoted, and $ is escaped so sourcing is safe
		{env: `key="test\${hello}test"`, expected: `key="test\${hello}test"`},
		// values are always double-quoted
		{env: `key=value`, expected: `key="value"`},
		// non-nested double-quotes are seen as strings
		{env: `key=va"lu"e`, expected: `key="value"`},
		// same with single quotes
		{env: `key=va'lu'e`, expected: `key="value"`},
		// nested double quoted variables are escaped
		{env: `key='va"lu"e'`, expected: `key="va\"lu\"e"`},
		// nested single quoted variables are left alone
		{env: `key="va'lu'e"`, expected: `key="va'lu'e"`},
		// newlines, backslashes, and some other special chars are escaped
		{env: `foo="\n\r\\r!"`, expected: `foo="\n\r\\r!"`, bash: true},
		// lines should be sorted
		{env: "foo=bar\nbaz=buzz", expected: "baz=\"buzz\"\nfoo=\"bar\""},
		// integers should not be quoted
		{env: `key="10"`, expected: `key=10`},
		// integers keep their original representation
		{env: `key=007`, expected: `key=007`},
		{env: `key=-5`, expected: `key=-5`},
		{env: `key="+5"`, expected: `key="+5"`},
	}

	t.Parallel()
	for _, tt := range tests {
		t.Run(tt.env, func(t *testing.T) {
			t.Parallel()

			opts := []godotenv.Option{}
			if tt.bash {
				opts = append(opts, godotenv.WithBash())
			}
			envMap, _ := godotenv.New(opts...).Unmarshal(tt.env)
			actual, _ := godotenv.Marshal(envMap)
			if tt.expected != actual {
				t.Errorf("Expected '%v' (%v) to write as '%v', got '%v' instead.", tt.env, envMap, tt.expected, actual)
			}
		})
	}
}

func TestWriteReadRoundTrip(t *testing.T) {
	t.Parallel()

	src := map[string]string{
		"PLAIN":     "value",
		"INTEGER":   "007",
		"DOLLAR":    "a$b",
		"BRACED":    "${X}",
		"CMDSUB":    "$(echo hi)",
		"BACKTICK":  "`echo hi`",
		"CONTROL":   "x\ay\v",
		"NEWLINE":   "1\n2",
		"QUOTE":     `va"lu"e`,
		"BACKSLASH": `a\b`,
	}

	dir := t.TempDir()
	name := filepath.Join(dir, ".env")
	if err := godotenv.Write(src, name); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, special := range []string{"$", "`"} {
		if strings.Contains(strings.ReplaceAll(string(content), "\\"+special, ""), special) {
			t.Errorf("written file leaves %q unescaped:\n%s", special, content)
		}
	}

	got, err := godotenv.New(godotenv.WithBash()).Read(name)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(src, got) {
		t.Errorf("expected round trip %v, got %v", src, got)
	}
}

func BenchmarkParse(b *testing.B) {
	var bld strings.Builder
	for i := range 10000 {
		fmt.Fprintf(&bld, "KEY_%d=value_%d\n", i, i)
	}
	data := bld.String()

	b.ReportAllocs()
	b.SetBytes(int64(len(data)))

	for b.Loop() {
		if _, err := godotenv.Unmarshal(data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReplaceAll(b *testing.B) {
	for _, size := range []int{1 << 10, 1 << 13, 1 << 15} {
		b.Run(fmt.Sprintf("value=%d", size), func(b *testing.B) {
			input := "A=" + strings.Repeat("ab", size/2) + "\nFOO=${A//b/c}"

			b.ReportAllocs()
			b.SetBytes(int64(size))

			for b.Loop() {
				if _, err := godotenv.Unmarshal(input); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkWildcardNoMatch(b *testing.B) {
	cases := []struct {
		name  string
		input string
	}{
		{"replace-star-z-star-4k", "A=" + strings.Repeat("ab", 2048) + "\nFOO=${A//*z*/Q}"},
		{"strip-prefix-star-z-star-50k", "A=" + strings.Repeat("ab", 25000) + "\nFOO=${A#*z*}"},
		{"strip-suffix-star-z-star-50k", "A=" + strings.Repeat("ab", 25000) + "\nFOO=${A%*z*}"},
		{"replace-all-star-4k", "A=" + strings.Repeat("ab", 2048) + "\nFOO=${A//*/Q}"},
		{"replace-all-question-4k", "A=" + strings.Repeat("ab", 2048) + "\nFOO=${A//?/x}"},
		{"strip-literal-prefix-50k", "A=" + strings.Repeat("ab", 25000) + "\nFOO=${A#ab*}"},
		{"replace-star-literal-4k", "A=" + strings.Repeat("ab", 2048) + "\nFOO=${A//*ab/Q}"},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			for b.Loop() {
				if _, err := godotenv.Unmarshal(tc.input); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func FuzzUnmarshal(f *testing.F) {
	seeds := []string{
		"FOO=bar",
		"FOO='bar baz'",
		`FOO="bar baz"`,
		"FOO=$BAR",
		"FOO=${BAR:-default}",
		"export FOO=bar\n# comment\nBAZ=qux",
		"FOO=$(echo hi)",
		"FOO=a\\\nb",
		"FOO=a\rBAR=2",
		"FOO=\r",
		"=foo",
		"FOO=",
		"FOO=${V:-${W}}",
		"FOO=é",
		`FOO="\x41\U0001F600"`,
		"\xff\xfe",
		"\x00",
		"A=abc\nB=b\nFOO=${A#${B}}",
		"A=abc\nFOO=${A//*/X}",
		"A=abc\nFOO=${A///X}",
		"A=aab\nFOO=${A//#a/X}",
		"FOO=abc\\",
		`FOO=a\ #b`,
		`FOO="\u"`,
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		env, err := godotenv.Unmarshal(input)
		if err != nil {
			_ = err.Error()
			return
		}

		for key := range env {
			if key == "" {
				t.Errorf("empty key parsed from %q", input)
			}
		}
	})
}

func FuzzExpand(f *testing.F) {
	seeds := []string{
		"",
		"plain text",
		"$HOME",
		"${HOME}",
		"${MISSING:-fallback}",
		"${#HOME}",
		"${MISSING:?boom}",
		"${A//x/y}",
		"cost is $",
		"$'literal'",
		"$(echo hi)",
		"\xff",
		"${A//*/X}",
		"${A///X}",
		`${A#$'*'}`,
		"${A#${B}}",
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	lookup := func(name []byte) ([]byte, bool) {
		if string(name) == "HOME" {
			return []byte("/home/me"), true
		}
		return nil, false
	}

	f.Fuzz(func(t *testing.T, input string) {
		if _, err := godotenv.Expand(input, lookup); err != nil {
			_ = err.Error()
		}
	})
}

func TestRoundTrip(t *testing.T) {
	t.Parallel()

	fixtures := []string{"equals.env", "exported.env", "plain.env", "quoted.env"}
	for _, fixture := range fixtures {
		t.Run(fixture, func(t *testing.T) {
			t.Parallel()

			loader := godotenv.New(godotenv.WithBash())
			fixtureFilename := fmt.Sprintf("fixtures/%s", fixture)
			env, err := loader.Read(fixtureFilename)
			if err != nil {
				t.Errorf("Expected '%s' to read without error (%v)", fixtureFilename, err)
			}
			rep, err := godotenv.Marshal(env)
			if err != nil {
				t.Errorf("Expected '%s' to Marshal (%v)", fixtureFilename, err)
			}
			roundtripped, err := loader.Unmarshal(rep)
			if err != nil {
				t.Errorf("Expected '%s' to Mashal and Unmarshal (%v)", fixtureFilename, err)
			}
			if !reflect.DeepEqual(env, roundtripped) {
				t.Errorf("Expected '%s' to roundtrip as '%v', got '%v' instead", fixtureFilename, env, roundtripped)
			}
		})
	}
}
