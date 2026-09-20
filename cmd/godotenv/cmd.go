package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"

	"github.com/hoshsadiq/godotenv"
)

// cliOptions is the parsed command line. It is a plain value so the parsing
// can be unit-tested without running the child process.
type cliOptions struct {
	filenames []string
	override  bool
	version   bool
	command   string
	cmdArgs   []string
}

func main() {
	opts, err := parseArgs(os.Args[1:])
	if errors.Is(err, flag.ErrHelp) {
		printUsage(os.Stdout)
		return
	}
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		printUsage(os.Stderr)
		os.Exit(2)
	}

	if opts.version {
		_, _ = fmt.Fprintf(os.Stdout, "%s %s+%s\n", projectName, version, gitCommit)
		return
	}

	if opts.command == "" {
		printUsage(os.Stderr)
		os.Exit(1)
	}

	var loadOpts []godotenv.Option
	if opts.override {
		loadOpts = append(loadOpts, godotenv.WithOverride())
	}

	if err = godotenv.New(loadOpts...).Load(opts.filenames...); err != nil {
		log.Fatal(err)
	}

	if err = execv(opts.command, opts.cmdArgs); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		log.Fatal(err)
	}
}

func newFlagSet(opts *cliOptions, filenames *stringsFlag) *flag.FlagSet {
	flags := flag.NewFlagSet(projectName, flag.ContinueOnError)
	flags.BoolVar(&opts.version, "v", false, "Show version information.")
	flags.Var(filenames, "f", "Path to a .env `file`. Repeat for multiple files.")
	flags.BoolVar(&opts.override, "o", false, "Override existing .env variables.")
	return flags
}

// parseArgs parses args without printing anything. It returns flag.ErrHelp when
// help was requested. When no -f is given, filenames defaults to .env.
func parseArgs(args []string) (*cliOptions, error) {
	var filenames stringsFlag
	opts := &cliOptions{}

	flags := newFlagSet(opts, &filenames)
	flags.SetOutput(io.Discard)

	if err := flags.Parse(args); err != nil {
		return nil, err
	}

	opts.filenames = filenames
	if len(opts.filenames) == 0 {
		opts.filenames = []string{".env"}
	}

	rest := flags.Args()
	if len(rest) > 0 {
		opts.command = rest[0]
	}
	if len(rest) > 1 {
		opts.cmdArgs = rest[1:]
	}

	return opts, nil
}

func printUsage(w io.Writer) {
	var filenames stringsFlag
	var opts cliOptions
	flags := newFlagSet(&opts, &filenames)
	flags.SetOutput(w)

	_, _ = fmt.Fprintf(w, `Usage:
  %[1]s [ options ] command [ arg ... ]

Utility to run a process with an env setup from a .env file.

Options:
`, projectName)
	flags.PrintDefaults()

	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, `Example:
	godotenv -f /path/to/something/.env -f /another/path/.env fortune
	godotenv -o -f /path/to/something/.env -f /another/path/.env fortune
	`)
	_, _ = fmt.Fprintf(w, `For more information, see %s`, projectURL)
	_, _ = fmt.Fprintln(w)
}
