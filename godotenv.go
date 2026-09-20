// Package godotenv is a go port of the ruby dotenv library (https://github.com/bkeepers/dotenv)
// Examples/readme can be found on the github page at https://github.com/hoshsadiq/godotenv
// The TL;DR is that you make a .env file that looks something like
//
//	SOME_ENV_VAR=somevalue
//
// and then in your go code you can call
//
//	godotenv.Load()
//
// and all the env vars declared in .env will be available through os.Getenv("SOME_ENV_VAR")
package godotenv

import (
	"bytes"
	"errors"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
)

// Loader loads and parses .env content using the options passed to New.
type Loader struct {
	cfg config
}

// New returns a Loader configured with the given options.
func New(opts ...Option) *Loader {
	l := &Loader{}
	for _, opt := range opts {
		opt(&l.cfg)
	}
	return l
}

// Load reads the given files and sets variables in the process environment.
// Variables already set are left alone unless the loader was created with
// WithOverride.
func (l *Loader) Load(filenames ...string) error {
	return loadFile(l.cfg, filenames, l.cfg.override)
}

// Decode reads env content from r and returns the variables as a map.
func (l *Loader) Decode(r io.Reader) (map[string]string, error) {
	return parseConfig(r, l.cfg)
}

// Unmarshal parses env content from data and returns the variables as a map.
func (l *Loader) Unmarshal(data []byte) (map[string]string, error) {
	return parseBytes(data, l.cfg)
}

// Expand resolves references in s using the loader's lookup and options.
func (l *Loader) Expand(s string) (string, error) {
	return expandString(s, l.cfg)
}

// Load will read your env file(s) and load them into ENV for this process.
// Call this function as close as possible to the start of your program (ideally in main)
// If you call Load without any args it will default to loading .env in the current path
// You can otherwise tell it which files to load (there can be more than one) like
//
//	godotenv.Load("fileone", "filetwo")
//
// It's important to note that it WILL NOT OVERRIDE an env variable that already exists - consider the .env file to set dev vars or sensible defaults
func Load(filenames ...string) (err error) {
	return loadFile(config{}, filenames, false)
}

// Decode reads an env file from io.Reader, returning a map of keys and values.
func Decode(r io.Reader) (envMap map[string]string, err error) {
	return parseConfig(r, config{})
}

// Unmarshal reads an env file from a byte slice, returning a map of keys and values.
func Unmarshal(data []byte) (envMap map[string]string, err error) {
	return parseBytes(data, config{})
}

// Expand resolves $VAR and ${VAR...} references in s using the process
// environment. Only $NAME and ${...} are interpreted. To use a custom lookup,
// or to enable WithBash or WithUnboundError, create a Loader with New.
func Expand(s string) (string, error) {
	return expandString(s, config{})
}

func expandString(s string, cfg config) (string, error) {
	cfg.restricted = true
	p := newParser([]byte(s), cfg)
	return p.expandString(s, cfg.lookupEnv())
}

func parseConfig(r io.Reader, cfg config) (envMap map[string]string, err error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	return parseBytes(data, cfg)
}

func parseBytes(d []byte, cfg config) (envMap map[string]string, err error) {
	envMap = make(map[string]string, bytes.Count(d, []byte("\n"))+1)

	lookupEnv := cfg.lookupEnv()
	expandEnv := func(s []byte) ([]byte, bool) {
		if val, exists := envMap[string(s)]; exists {
			return []byte(val), exists
		}

		return lookupEnv(s)
	}

	parser := newParser(d, cfg)

	err = parser.parse(envMap, expandEnv)

	return envMap, err
}

// Write serializes the given environment and writes it to a file
func Write(envMap map[string]string, filename string) (err error) {
	content, err := Marshal(envMap)
	if err != nil {
		return err
	}
	file, err := os.Create(filename) // #nosec G304 -- filename is provided by the caller
	if err != nil {
		return err
	}
	defer func() {
		if cerr := file.Close(); cerr != nil {
			err = errors.Join(err, cerr)
		}
	}()
	_, err = file.Write(append(content, '\n'))
	if err != nil {
		return err
	}
	return file.Sync()
}

// Marshal outputs the given environment as a dotenv-formatted environment file.
// Each line is in the format KEY=VALUE. Integer values are written unquoted;
// every other value is double-quoted and escaped, including $ and `, so the
// output is safe to source in a shell.
func Marshal(envMap map[string]string) ([]byte, error) {
	lines := make([]string, 0, len(envMap))
	for k, v := range envMap {
		lines = append(lines, k+"="+marshalValue(v))
	}
	sort.Strings(lines)
	return []byte(strings.Join(lines, "\n")), nil
}

// marshalValue renders v for a dotenv file. Integer-looking values stay
// unquoted; everything else is double-quoted with Go-style escapes and then
// has $ and ` backslash-escaped so a shell sourcing the file will not perform
// parameter expansion or command substitution.
func marshalValue(v string) string {
	if isInt(v) {
		return v
	}

	// strconv.Quote escapes backslashes, so a literal $ or ` in v survives as
	// a single byte here; adding a backslash in front cannot double-escape.
	quoted := strconv.Quote(v)
	quoted = strings.ReplaceAll(quoted, "$", `\$`)
	quoted = strings.ReplaceAll(quoted, "`", "\\`")
	return quoted
}

func isInt(s string) bool {
	s = strings.TrimPrefix(s, "-")
	if len(s) == 0 {
		return false
	}

	for i := range s {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}

	return true
}

func filenamesOrDefault(filenames []string) []string {
	if len(filenames) == 0 {
		return []string{".env"}
	}
	return filenames
}

func loadFile(cfg config, filenames []string, override bool) error {
	filenames = filenamesOrDefault(filenames)

	currentEnv := map[string]bool{}
	for _, envLine := range os.Environ() {
		key := strings.SplitN(envLine, "=", 2)[0]
		currentEnv[key] = true
	}

	for _, filename := range filenames {
		envMap, err := readFile(cfg, filename)
		if err != nil {
			return err
		}

		for key, value := range envMap {
			if !currentEnv[key] || override {
				_ = os.Setenv(key, value)
			}
		}
	}

	return nil
}

func readFile(cfg config, filename string) (envMap map[string]string, err error) {
	file, err := os.Open(filename) // #nosec G304 -- filename is provided by the caller
	if err != nil {
		return
	}
	defer func() {
		if cerr := file.Close(); cerr != nil {
			err = errors.Join(err, cerr)
		}
	}()

	return parseConfig(file, cfg)
}
