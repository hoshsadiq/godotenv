package godotenv

import (
	"bytes"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
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
				"OPTION_SINGLE_G": "1\n2",
				"OPTION_SINGLE_H": "1\n2\n3 is \\'quoted\\'",

				"OPTION_DOUBLE_A": "1",
				"OPTION_DOUBLE_B": "2",
				"OPTION_DOUBLE_C": "",
				"OPTION_DOUBLE_D": "\n",
				"OPTION_DOUBLE_E": "echo 'asd'",
				"OPTION_DOUBLE_F": "echo asd",
				"OPTION_DOUBLE_G": "1\n2",
				"OPTION_DOUBLE_H": "1\n2\n3 is \"quoted\"",
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
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			file, err := os.Open(tt.envFileName)
			if err != nil {
				return
			}
			defer file.Close()

			envMap, err := ParseWithLookup(file, func(name []byte) (value []byte, exists bool) {
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
				t.Errorf("Mismatch env vars: expected '%v' got '%v'", tt.expectedValues, envMap)
			}
		})
	}
}

func TestLoadWithNoArgsLoadsDotEnv(t *testing.T) {
	t.Parallel()

	err := Load()
	pathError := err.(*os.PathError)
	if pathError == nil || pathError.Op != "open" || pathError.Path != ".env" {
		t.Errorf("Didn't try and open .env by default")
	}
}

func TestOverloadWithNoArgsOverloadsDotEnv(t *testing.T) {
	t.Parallel()

	err := Overload()
	pathError := err.(*os.PathError)
	if pathError == nil || pathError.Op != "open" || pathError.Path != ".env" {
		t.Errorf("Didn't try and open .env by default")
	}
}

func TestLoadFileNotFound(t *testing.T) {
	t.Parallel()

	err := Load("somefilethatwillneverexistever.env")
	if err == nil {
		t.Error("File wasn't found but Load didn't return an error")
	}
}

func TestOverloadFileNotFound(t *testing.T) {
	t.Parallel()

	err := Overload("somefilethatwillneverexistever.env")
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

	envMap, err := Read(envFileName)
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

	envMap, err := Parse(bytes.NewReader([]byte("ONE=1\nTWO='2'")))
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

	err := Load(envFileName)
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

	err := Overload(envFileName)
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
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env, err := Parse(strings.NewReader(tt.input))
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
	_ = Load("fixtures/plain.env")

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
	}

	t.Parallel()
	for _, tt := range tests {
		tt := tt
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

				return LookupEnv(s)
			}

			p := newParser([]byte(tt.rawEnvLine))

			_ = p.parse(newEnv, expandEnv)
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

func TestErrorReadDirectory(t *testing.T) {
	t.Parallel()

	envFileName := "fixtures/"
	envMap, err := Read(envFileName)
	if err == nil {
		t.Errorf("Expected error, got %v: %s", envMap, err)
	}
}

func TestErrorParsing(t *testing.T) {
	t.Parallel()

	envFileName := "fixtures/invalid1.env"
	envMap, err := Read(envFileName)
	if err == nil {
		t.Errorf("Expected error, got %v: %s", envMap, err)
	}
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
	}

	t.Parallel()
	for _, tt := range tests {
		tt := tt
		t.Run(tt.env, func(t *testing.T) {
			t.Parallel()

			envMap, _ := Unmarshal(tt.env)
			actual, _ := Marshal(envMap)
			if tt.expected != actual {
				t.Errorf("Expected '%v' (%v) to write as '%v', got '%v' instead.", tt.env, envMap, tt.expected, actual)
			}
		})
	}
}

func TestRoundTrip(t *testing.T) {
	t.Parallel()

	fixtures := []string{"equals.env", "exported.env", "plain.env", "quoted.env"}
	for _, fixture := range fixtures {
		fixtureFilename := fmt.Sprintf("fixtures/%s", fixture)
		env, err := readFile(fixtureFilename)
		if err != nil {
			t.Errorf("Expected '%s' to read without error (%v)", fixtureFilename, err)
		}
		rep, err := Marshal(env)
		if err != nil {
			t.Errorf("Expected '%s' to Marshal (%v)", fixtureFilename, err)
		}
		roundtripped, err := Unmarshal(rep)
		if err != nil {
			t.Errorf("Expected '%s' to Mashal and Unmarshal (%v)", fixtureFilename, err)
		}
		if !reflect.DeepEqual(env, roundtripped) {
			t.Errorf("Expected '%s' to roundtrip as '%v', got '%v' instead", fixtureFilename, env, roundtripped)
		}
	}
}
