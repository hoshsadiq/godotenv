package godotenv

import "os"

// Option configures a Loader created with New.
type Option func(*config)

type config struct {
	unboundErr bool
	bash       bool
	restricted bool
	override   bool
	lookup     LookupEnvFunc
}

func (c config) lookupEnv() LookupEnvFunc {
	if c.lookup != nil {
		return c.lookup
	}

	return lookupProcessEnv
}

// WithUnboundError makes an unset variable reference ($VAR or ${VAR}) return an
// error instead of expanding to an empty string.
func WithUnboundError() Option {
	return func(c *config) {
		c.unboundErr = true
	}
}

// WithBash enables the bash extensions on top of the POSIX defaults: ANSI-C
// quoting ($'...', including \x{}, \u{} and \U{}), translation quoting ($"..."),
// the ${var/pat/rep} replacement family (/, //, /# and /%) and dotenv-style
// backslash escapes inside "..." such as \n and \t. Without this option the
// parser follows strict POSIX rules, where all of those are literal text.
func WithBash() Option {
	return func(c *config) {
		c.bash = true
	}
}

// WithOverride makes Load overwrite variables already set in the process
// environment, so the files win instead of the environment.
func WithOverride() Option {
	return func(c *config) {
		c.override = true
	}
}

// WithLookup sets the function used to resolve a reference to a variable that
// was not defined earlier in the parsed content. By default the process
// environment is used.
func WithLookup(lookup LookupEnvFunc) Option {
	return func(c *config) {
		c.lookup = lookup
	}
}

// lookupProcessEnv reports the value of the process environment variable named by name.
func lookupProcessEnv(name []byte) (value []byte, exists bool) {
	val, b := os.LookupEnv(string(name))
	return []byte(val), b
}
