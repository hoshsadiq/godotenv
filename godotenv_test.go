package godotenv_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
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
				"OPTION_DOUBLE_D": "\n",
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

func TestOverloadWithNoArgsOverloadsDotEnv(t *testing.T) {
	t.Parallel()

	var pathError *os.PathError
	if err := godotenv.Overload(); !errors.As(err, &pathError) || pathError.Op != "open" || pathError.Path != ".env" {
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

func TestOverloadFileNotFound(t *testing.T) {
	t.Parallel()

	err := godotenv.Overload("somefilethatwillneverexistever.env")
	if err == nil {
		t.Error("File wasn't found but Overload didn't return an error")
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

	// ensure NO overload
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

func TestOverloadDoesOverride(t *testing.T) {
	envFileName := "fixtures/plain.env"

	// ensure NO overload
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

	err := godotenv.Overload(envFileName)
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
		{rawEnvLine: `FOO="bar\nbaz"`, expectedKey: "FOO", expectedValue: "bar\nbaz"},
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
		{rawEnvLine: `FOO="bar\n\ b\az"`, expectedKey: "FOO", expectedValue: "bar\n baz"},
		{rawEnvLine: `FOO="bar\\\n\ b\az"`, expectedKey: "FOO", expectedValue: "bar\\\n baz"},
		{rawEnvLine: `FOO="bar\\r\ b\az"`, expectedKey: "FOO", expectedValue: "bar\\r baz"},

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

func TestParameterExpansion(t *testing.T) {
	const unset = "GODOTENV_PE_UNSET"
	_ = os.Unsetenv(unset)

	tests := []struct {
		name    string
		input   string
		want    map[string]string
		wantErr bool
	}{
		{"length", "GODOTENV_PE_A=hello\nFOO=${#GODOTENV_PE_A}", map[string]string{"GODOTENV_PE_A": "hello", "FOO": "5"}, false},
		{"length of empty", "GODOTENV_PE_A=\nFOO=${#GODOTENV_PE_A}", map[string]string{"GODOTENV_PE_A": "", "FOO": "0"}, false},
		{"default recursive", "GODOTENV_PE_B=bla\nFOO=${" + unset + ":-$GODOTENV_PE_B}", map[string]string{"GODOTENV_PE_B": "bla", "FOO": "bla"}, false},
		{"default nested", "GODOTENV_PE_B=bla\nFOO=${" + unset + ":-${GODOTENV_PE_B}}", map[string]string{"GODOTENV_PE_B": "bla", "FOO": "bla"}, false},
		{"alt recursive", "GODOTENV_PE_A=1\nFOO=${GODOTENV_PE_A:+${GODOTENV_PE_A}x}", map[string]string{"GODOTENV_PE_A": "1", "FOO": "1x"}, false},
		{"default with spaces", "FOO=${" + unset + ":-a b}", map[string]string{"FOO": "a b"}, false},
		{"error op with value", "GODOTENV_PE_A=x\nFOO=${GODOTENV_PE_A:?boom}", map[string]string{"GODOTENV_PE_A": "x", "FOO": "x"}, false},
		{"error op unset", "FOO=${" + unset + ":?boom}", nil, true},
		{"error op empty", "GODOTENV_PE_A=\nFOO=${GODOTENV_PE_A:?boom}", nil, true},
		{"qmark op unset", "FOO=${" + unset + "?boom}", nil, true},
		{"qmark op empty is set", "GODOTENV_PE_A=\nFOO=${GODOTENV_PE_A?boom}", map[string]string{"GODOTENV_PE_A": "", "FOO": ""}, false},
		{"empty braces", "FOO=${}", nil, true},
		{"assign op unsupported colon", "FOO=${" + unset + ":=x}", nil, true},
		{"assign op unsupported", "FOO=${" + unset + "=x}", nil, true},
		{"substring trailing junk", "A=hello\nFOO=${A:1:2:3}", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := godotenv.Unmarshal(tt.input)
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

	input := "FOO=" + strings.Repeat("${GODOTENV_TEST_UNSET_VARIABLE:-", 2000) + "x" + strings.Repeat("}", 2000)

	if _, err := godotenv.Unmarshal(input); err == nil {
		t.Fatal("expected an error for deeply nested expansions")
	}
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
	}{
		{"strip prefix shortest", "A=hello\nFOO=${A#he}", "llo"},
		{"strip prefix longest", "A=hello\nFOO=${A##*l}", "o"},
		{"strip suffix shortest", "A=hello\nFOO=${A%lo}", "hel"},
		{"strip suffix longest", "A=hello\nFOO=${A%%l*}", "he"},
		{"strip no match", "A=hello\nFOO=${A#zzz}", "hello"},
		{"strip star shortest", "A=hello\nFOO=${A#*}", "hello"},
		{"strip star longest", "A=hello\nFOO=${A##*}", ""},
		{"strip one char", "A=hello\nFOO=${A#?}", "ello"},
		{"strip class", "A=hello\nFOO=${A#[a-z]}", "ello"},
		{"replace first", "A=hello\nFOO=${A/l/L}", "heLlo"},
		{"replace all", "A=hello\nFOO=${A//l/L}", "heLLo"},
		{"replace remove", "A=hello\nFOO=${A/l/}", "helo"},
		{"replace anchored start", "A=hello\nFOO=${A/#he/X}", "Xllo"},
		{"replace anchored end", "A=hello\nFOO=${A/%lo/X}", "helX"},
		{"path strip shortest", "A=a/b/c\nFOO=${A#*/}", "b/c"},
		{"path strip longest", "A=a/b/c\nFOO=${A##*/}", "c"},
		{"path suffix shortest", "A=a/b/c\nFOO=${A%/*}", "a/b"},
		{"path suffix longest", "A=a/b/c\nFOO=${A%%/*}", "a"},
		{"replace dot first", "A=a.b.c\nFOO=${A/./-}", "a-b.c"},
		{"replace dot all", "A=a.b.c\nFOO=${A//./-}", "a-b-c"},
		{"substring offset", "A=hello\nFOO=${A:1}", "ello"},
		{"substring offset len", "A=hello\nFOO=${A:1:3}", "ell"},
		{"substring negative", "A=hello\nFOO=${A: -2}", "lo"},
		{"substring negative len", "A=hello\nFOO=${A: -4:2}", "el"},
		{"substring negative end", "A=hello\nFOO=${A:2:-1}", "ll"},
		{"substring out of range", "A=hello\nFOO=${A:99}", ""},
		{"substring far negative", "A=hello\nFOO=${A: -99}", ""},
		{"strip one character", "A=é\nFOO=${A#?}", ""},
		{"length in characters", "A=é\nFOO=${#A}", "1"},
		{"substring by character", "A=héllo\nFOO=${A:1:2}", "él"},
		{"replace with wildcard", "A=héllo\nFOO=${A/h?llo/X}", "X"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := godotenv.Unmarshal(tt.input)
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

func TestWithPOSIX(t *testing.T) {
	tests := []struct {
		name    string
		opts    []godotenv.Option
		input   string
		want    string
		wantErr bool
	}{
		{"default double quote escapes", nil, `FOO="a\nb"`, "a\nb", false},
		{"posix double quote literal", []godotenv.Option{godotenv.WithPOSIX()}, `FOO="a\nb"`, `a\nb`, false},
		{"posix tab literal", []godotenv.Option{godotenv.WithPOSIX()}, `FOO="a\tb"`, `a\tb`, false},
		{"posix escaped quote", []godotenv.Option{godotenv.WithPOSIX()}, `FOO="a\"b"`, `a"b`, false},
		{"posix escaped backslash", []godotenv.Option{godotenv.WithPOSIX()}, `FOO="a\\b"`, `a\b`, false},
		{"posix escaped dollar", []godotenv.Option{godotenv.WithPOSIX()}, `FOO="a\$b"`, "a$b", false},
		{"posix unknown escape literal", []godotenv.Option{godotenv.WithPOSIX()}, `FOO="a\qb"`, `a\qb`, false},
		{"posix unicode literal", []godotenv.Option{godotenv.WithPOSIX()}, `FOO="\u0041"`, `\u0041`, false},
		{"posix hex literal", []godotenv.Option{godotenv.WithPOSIX()}, `FOO="\x41"`, `\x41`, false},
		{"posix astral literal", []godotenv.Option{godotenv.WithPOSIX()}, `FOO="\U0001F600"`, `\U0001F600`, false},
		{"posix line continuation", []godotenv.Option{godotenv.WithPOSIX()}, "FOO=\"a\\\nb\"", "ab", false},
		{"default single quote escape errors", nil, `FOO='a\'`, "", true},
		{"posix single quote backslash literal", []godotenv.Option{godotenv.WithPOSIX()}, `FOO='a\'`, `a\`, false},
		{"posix single quote backslash", []godotenv.Option{godotenv.WithPOSIX()}, `FOO='a\b'`, `a\b`, false},
		{"posix ansi-c still expands", []godotenv.Option{godotenv.WithPOSIX()}, `FOO=$'a\nb'`, "a\nb", false},
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
			if got["FOO"] != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got["FOO"])
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
	}{
		{"braced", "path=${HOME}/x", "path=/home/me/x", false},
		{"unbraced", "path=$HOME/x", "path=/home/me/x", false},
		{"default", "${MISSING:-fallback}", "fallback", false},
		{"dash default", "${MISSING-fallback}", "fallback", false},
		{"colon plus on empty", "${EMPTY:+set}", "", false},
		{"plus on empty", "${EMPTY+set}", "set", false},
		{"recursive", "${MISSING:-${HOME}}", "/home/me", false},
		{"braced unset errors", "${MISSING}", "", true},
		{"unbraced unset errors", "$MISSING", "", true},
		{"ansi-c stays literal", `$'a\nb'`, `$'a\nb'`, false},
		{"positional stays literal", "$1", "$1", false},
		{"special stays literal", "$?", "$?", false},
		{"command substitution stays literal", "$(echo hi)", "$(echo hi)", false},
		{"backticks stay literal", "`echo hi`", "`echo hi`", false},
		{"trailing dollar", "cost is $", "cost is $", false},
		{"plain text", "just a token", "just a token", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := godotenv.Expand(tt.in, lookup)
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
	}{
		{"unquoted", "FOO=a\\\r\nb", "ab"},
		{"double quoted", "FOO=\"a\\\r\nb\"", "ab"},
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

	loader := godotenv.New(godotenv.WithPOSIX())
	got, err := loader.Unmarshal(`FOO="$'a\nb'"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := `$'a\nb'`; got["FOO"] != want {
		t.Errorf("expected %q, got %q", want, got["FOO"])
	}

	word, err := godotenv.Unmarshal(`FOO=${GODOTENV_TEST_MISSING:-$'a\nb'}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "a\nb"; word["FOO"] != want {
		t.Errorf("expected %q, got %q", want, word["FOO"])
	}

	translated, err := loader.Unmarshal(`FOO=$"a$'a\nb'b"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := `a$'a\nb'b`; translated["FOO"] != want {
		t.Errorf("expected %q, got %q", want, translated["FOO"])
	}

	translatedEscapes, err := loader.Unmarshal(`FOO=$"a\n$'a\nb'b"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := `a\n$'a\nb'b`; translatedEscapes["FOO"] != want {
		t.Errorf("expected %q, got %q", want, translatedEscapes["FOO"])
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
	}{
		{"empty key", "=foo", "empty key"},
		{"key starting with a digit", "1FOO=1", "invalid character in key name"},
		{"key starting with a dot", ".FOO=1", "invalid character in key name"},
		{"caret points at the offending column", "A=1\nB C=2", "B C=2\n\t ^ Right here"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := godotenv.Unmarshal(tt.input)
			if err == nil {
				t.Fatalf("expected an error for %q", tt.input)
			}
			if msg := err.Error(); !strings.Contains(msg, tt.want) {
				t.Errorf("expected error to contain %q, got:\n%s", tt.want, msg)
			}
		})
	}

	t.Run("unbound variable in Expand", func(t *testing.T) {
		t.Parallel()

		_, err := godotenv.Expand("$GODOTENV_TEST_UNSET_VARIABLE", func([]byte) ([]byte, bool) {
			return nil, false
		})
		if err == nil {
			t.Fatal("expected an error")
		}
		if msg := err.Error(); !strings.Contains(msg, "unbound variable") {
			t.Errorf("expected error to mention the unbound variable, got:\n%s", msg)
		}
	})
}

// just test some single lines to show the general idea
func TestWrite(t *testing.T) {
	tests := []struct {
		env      string
		expected string
	}{
		// values are always double-quoted
		{env: `key="test\${hello}test"`, expected: `key="test${hello}test"`},
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
		{env: `foo="\n\r\\r!"`, expected: `foo="\n\r\\r!"`},
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

			envMap, _ := godotenv.Unmarshal(tt.env)
			actual, _ := godotenv.Marshal(envMap)
			if tt.expected != actual {
				t.Errorf("Expected '%v' (%v) to write as '%v', got '%v' instead.", tt.env, envMap, tt.expected, actual)
			}
		})
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

func TestRoundTrip(t *testing.T) {
	t.Parallel()

	fixtures := []string{"equals.env", "exported.env", "plain.env", "quoted.env"}
	for _, fixture := range fixtures {
		t.Run(fixture, func(t *testing.T) {
			t.Parallel()

			fixtureFilename := fmt.Sprintf("fixtures/%s", fixture)
			env, err := godotenv.Read(fixtureFilename)
			if err != nil {
				t.Errorf("Expected '%s' to read without error (%v)", fixtureFilename, err)
			}
			rep, err := godotenv.Marshal(env)
			if err != nil {
				t.Errorf("Expected '%s' to Marshal (%v)", fixtureFilename, err)
			}
			roundtripped, err := godotenv.Unmarshal(rep)
			if err != nil {
				t.Errorf("Expected '%s' to Mashal and Unmarshal (%v)", fixtureFilename, err)
			}
			if !reflect.DeepEqual(env, roundtripped) {
				t.Errorf("Expected '%s' to roundtrip as '%v', got '%v' instead", fixtureFilename, env, roundtripped)
			}
		})
	}
}
