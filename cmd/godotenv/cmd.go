package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/hoshsadiq/godotenv"
)

func main() {
	var showVersion, overload bool
	envFilenames := stringsFlag{".env"}

	flags := flag.NewFlagSet(projectName, flag.ContinueOnError)
	flags.BoolVar(&showVersion, "v", false, "Show version information.")
	flags.Var(&envFilenames, "f", "Comma separated paths to .env `files`. Repeat for multiple files.")
	flags.BoolVar(&overload, "o", false, "Override existing .env variables.")

	flags.Usage = func() {
		_, _ = fmt.Fprintf(flags.Output(), `Usage:
  %[1]s [ options ] command [ arg ... ]

Utility to run a process with an env setup from a .env file.

Options:
`, projectName)
		flags.PrintDefaults()

		_, _ = fmt.Fprintln(flags.Output())
		_, _ = fmt.Fprintln(flags.Output(), `Example:
	godotenv -f /path/to/something/.env -f /another/path/.env fortune
	godotenv -o -f /path/to/something/.env -f /another/path/.env fortune
	`)
		_, _ = fmt.Fprintf(flags.Output(), `For more information, see %s`, projectURL)
		_, _ = fmt.Fprintln(flags.Output())
	}

	err := flags.Parse(os.Args[1:])
	if errors.Is(err, flag.ErrHelp) {
		return
	}

	if showVersion {
		_, _ = fmt.Fprintf(flags.Output(), `%s %s+%s`, projectName, version, gitCommit)
		_, _ = fmt.Fprintln(flags.Output())
		return
	}

	// if no args or help requested
	// print usage and return
	args := flags.Args()
	if len(args) == 0 {
		flags.Usage()
		os.Exit(1)
	}

	loader := godotenv.Load
	if overload {
		loader = godotenv.Overload
	}

	err = loader(envFilenames...)
	if err != nil {
		log.Fatal(err)
		return
	}

	// take rest of args and "exec" them
	cmd := args[0]
	cmdArgs := args[1:]

	err = execv(cmd, cmdArgs)
	if err != nil {
		log.Fatal(err)
	}
}
