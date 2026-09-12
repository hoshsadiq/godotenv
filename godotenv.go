// Package godotenv is a go port of the ruby dotenv library (https://github.com/bkeepers/dotenv)
// Examples/readme can be found on the github page at https://github.com/hoshsadiq/godotenv
// The TL;DR is that you make a .env file that looks something like
//
// 		SOME_ENV_VAR=somevalue
//
// and then in your go code you can call
//
// 		godotenv.Load()
//
// and all the env vars declared in .env will be available through os.Getenv("SOME_ENV_VAR")
package godotenv

import (
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
)

// Option configures a Loader created with New.
type Option func(*config)

type config struct {
	unboundErr bool
	posix      bool
}

// WithUnboundError makes an unset variable reference ($VAR or ${VAR}) return an
// error instead of expanding to an empty string.
func WithUnboundError() Option {
	return func(c *config) {
		c.unboundErr = true
	}
}

// WithPOSIX applies POSIX shell quoting rules: inside double quotes only $, `,
// ", \ and newline are special, and backslash is literal inside single quotes.
func WithPOSIX() Option {
	return func(c *config) {
		c.posix = true
	}
}

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

// Load reads the given files and sets unset variables in the process
// environment.
func (l *Loader) Load(filenames ...string) error {
	return loadFile(l.cfg, filenames, false)
}

// Overload reads the given files and overrides variables already set in the
// process environment.
func (l *Loader) Overload(filenames ...string) error {
	return loadFile(l.cfg, filenames, true)
}

// Read returns the variables from the given files as a map.
func (l *Loader) Read(filenames ...string) (map[string]string, error) {
	return readFiles(l.cfg, filenames)
}

// Parse reads env content from r and returns the variables as a map.
func (l *Loader) Parse(r io.Reader) (map[string]string, error) {
	return parseConfig(r, l.cfg, LookupEnv)
}

// Unmarshal parses env content from a string and returns the variables as a map.
func (l *Loader) Unmarshal(str string) (map[string]string, error) {
	return l.Parse(strings.NewReader(str))
}

// Load will read your env file(s) and load them into ENV for this process.
// Call this function as close as possible to the start of your program (ideally in main)
// If you call Load without any args it will default to loading .env in the current path
// You can otherwise tell it which files to load (there can be more than one) like
//
//		godotenv.Load("fileone", "filetwo")
//
// It's important to note that it WILL NOT OVERRIDE an env variable that already exists - consider the .env file to set dev vars or sensible defaults
func Load(filenames ...string) (err error) {
	return loadFile(config{}, filenames, false)
}

// Overload will read your env file(s) and load them into ENV for this process.
// Call this function as close as possible to the start of your program (ideally in main)
// If you call Overload without any args it will default to loading .env in the current path
// You can otherwise tell it which files to load (there can be more than one) like
//
//		godotenv.Overload("fileone", "filetwo")
//
// It's important to note this WILL OVERRIDE an env variable that already exists - consider the .env file to forcefilly set all vars.
func Overload(filenames ...string) (err error) {
	return loadFile(config{}, filenames, true)
}

// Read all env (with same file loading semantics as Load) but return values as
// a map rather than automatically writing values into env
func Read(filenames ...string) (envMap map[string]string, err error) {
	return readFiles(config{}, filenames)
}

// ParseWithLookup reads an env file from io.Reader, returning a map of keys and values.
// It uses the lookupEnv to retrieve environment variables. Parse calls this function with
// LookupEnv as the lookupEnv argument.
func ParseWithLookup(r io.Reader, lookupEnv lookupEnvFunc) (envMap map[string]string, err error) {
	return parseConfig(r, config{}, lookupEnv)
}

func parseConfig(r io.Reader, cfg config, lookupEnv lookupEnvFunc) (envMap map[string]string, err error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	return parseWithLookup(data, cfg, lookupEnv)
}

func parseWithLookup(d []byte, cfg config, lookupEnv lookupEnvFunc) (envMap map[string]string, err error) {
	envMap = make(map[string]string)

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

// Parse reads an env file from io.Reader, returning a map of keys and values.
func Parse(r io.Reader) (envMap map[string]string, err error) {
	return ParseWithLookup(r, LookupEnv)
}

// Unmarshal reads an env file from a string, returning a map of keys and values.
func Unmarshal(str string) (envMap map[string]string, err error) {
	return Parse(strings.NewReader(str))
}

// Write serializes the given environment and writes it to a file
func Write(envMap map[string]string, filename string) error {
	content, err := Marshal(envMap)
	if err != nil {
		return err
	}
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.WriteString(content + "\n")
	if err != nil {
		return err
	}
	return file.Sync()
}

// Marshal outputs the given environment as a dotenv-formatted environment file.
// Each line is in the format: KEY="VALUE" where VALUE is backslash-escaped.
func Marshal(envMap map[string]string) (string, error) {
	lines := make([]string, 0, len(envMap))
	for k, v := range envMap {
		if isInt(v) {
			lines = append(lines, k+"="+v)
		} else {
			lines = append(lines, k+"="+strconv.Quote(v))
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n"), nil
}

func isInt(s string) bool {
	s = strings.TrimPrefix(s, "-")
	if len(s) == 0 {
		return false
	}

	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}

	return true
}

func LookupEnv(name []byte) (value []byte, exists bool) {
	val, b := os.LookupEnv(string(name))
	return []byte(val), b
}

func filenamesOrDefault(filenames []string) []string {
	if len(filenames) == 0 {
		return []string{".env"}
	}
	return filenames
}

func loadFile(cfg config, filenames []string, overload bool) error {
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
			if !currentEnv[key] || overload {
				_ = os.Setenv(key, value)
			}
		}
	}

	return nil
}

func readFiles(cfg config, filenames []string) (envMap map[string]string, err error) {
	filenames = filenamesOrDefault(filenames)
	envMap = make(map[string]string)

	for _, filename := range filenames {
		individualEnvMap, individualErr := readFile(cfg, filename)
		if individualErr != nil {
			err = individualErr
			return // return early on a spazout
		}

		for key, value := range individualEnvMap {
			envMap[key] = value
		}
	}

	return
}

func readFile(cfg config, filename string) (envMap map[string]string, err error) {
	file, err := os.Open(filename)
	if err != nil {
		return
	}
	defer file.Close()

	return parseConfig(file, cfg, LookupEnv)
}
