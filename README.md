# GoDotEnv [![Validate](https://github.com/hoshsadiq/godotenv/actions/workflows/validate.yml/badge.svg)](https://github.com/hoshsadiq/godotenv/actions/workflows/validate.yml) [![Go Report Card](https://goreportcard.com/badge/github.com/hoshsadiq/godotenv)](https://goreportcard.com/report/github.com/hoshsadiq/godotenv)

A Go (golang) port of the Ruby dotenv project (which loads env vars from a .env file)

From the original Library:

> Storing configuration in the environment is one of the tenets of a twelve-factor app. Anything that is likely to change between deployment environments–such as resource handles for databases or credentials for external services–should be extracted from the code into environment variables.
>
> But it is not always practical to set environment variables on development machines or continuous integration servers where multiple projects are run. Dotenv load variables from a .env file into ENV when the environment is bootstrapped.

It can be used as a library (for loading in env for your own daemons etc) or as a bin command.

There is test coverage and CI for both linuxish and windows environments, but I make no guarantees about the bin version working on windows.

This is a fork of [joho/godotenv](https://github.com/joho/godotenv) with an improved parser that allows for far fewer allocations, and, due to removal of regexp usage, it should be significantly faster.

## Installation

As a library
```shell
go get github.com/hoshsadiq/godotenv
```

or if you want to use it as a bin command
```shell
go install github.com/hoshsadiq/godotenv/cmd/godotenv@latest
```

or via GitHub releases:
```shell
os="linux" # linux, darwin, freebsd, windows
arch="amd64" # amd64, armv6, armv7, arm64

# latest version
curl -fsSL "https://github.com/hoshsadiq/godotenv/releases/latest/download/godotenv_linux_amd64.tar.gz" |
  tar -xzvf - -C $HOME/.local/bin

# or a specific version
curl -fsSL "https://github.com/hoshsadiq/godotenv/releases/download/v1.0.0/godotenv_linux_amd64.tar.gz" |
  tar -xzvf - -C $HOME/.local/bin
```

For a list of os/arch combinations, see the [releases page](https://github.com/hoshsadiq/godotenv/releases).

In the case of GitHub releases, the checksums file is signed with [cosign](https://github.com/sigstore/cosign) (keyless, via GitHub Actions OIDC):
```shell
os="linux" # linux, darwin, freebsd, or windows
arch="amd64" # amd64, armv6, armv7, or arm64

# this is for latest release, substitute URL as per above if you need a specific release
curl -fsSL "https://github.com/hoshsadiq/godotenv/releases/latest/download/godotenv_checksums.txt" -o godotenv_checksums.txt
curl -fsSL "https://github.com/hoshsadiq/godotenv/releases/latest/download/godotenv_checksums.txt.sigstore.json" -o godotenv_checksums.txt.sigstore.json

cosign verify-blob \
  --certificate-identity-regexp '^https://github.com/hoshsadiq/godotenv/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --bundle godotenv_checksums.txt.sigstore.json \
  godotenv_checksums.txt

grep "godotenv_${os}_${arch}" godotenv_checksums.txt | sha256sum -c
```

Lastly, you can use Docker or Podman to pull in godotenv. See the [releases page](https://github.com/hoshsadiq/godotenv/releases) for the URLs and tags. Note the images are based on [distroless](https://github.com/GoogleContainerTools/distroless) so they do not have a shell or anything else attached. Thus they're only useful for minimal images or copying the executable over into your own Docker image.

Container images are signed with cosign as well:
```shell
cosign verify \
  --certificate-identity-regexp '^https://github.com/hoshsadiq/godotenv/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  ghcr.io/hoshsadiq/godotenv:latest
```

## Usage

Add your application configuration to your `.env` file in the root of your project:

```shell
S3_BUCKET=YOURS3BUCKET
SECRET_KEY=YOURSECRETKEYGOESHERE
```

Then in your Go app you can do something like

```go
package main

import (
    "github.com/hoshsadiq/godotenv"
    "log"
    "os"
)

func main() {
  err := godotenv.Load()
  if err != nil {
    log.Fatal("Error loading .env file")
  }

  s3Bucket := os.Getenv("S3_BUCKET")
  secretKey := os.Getenv("SECRET_KEY")

  // now do something with s3 or whatever
}
```

While `.env` in the project root is the default, you don't have to be constrained, both examples below are 100% legit

```go
_ = godotenv.Load("somerandomfile")
_ = godotenv.Load("filenumberone.env", "filenumbertwo.env")
```

If you want to be really fancy with your env file you can do comments and exports (below is a valid env file)

```shell
# I am a comment and that is OK
SOME_VAR=someval
FOO=BAR # comments at line end are OK too
export BAR=BAZ
```

as a final aside, if you don't want godotenv munging your env you can just get a map back instead

```go
file, err := os.Open(".env")
if err != nil {
    log.Fatal(err)
}
defer file.Close()

myEnv, err := godotenv.Decode(file)

s3Bucket := myEnv["S3_BUCKET"]
```

... or from any `io.Reader`, not just a local file

```go
reader := getRemoteFile()
myEnv, err := godotenv.Decode(reader)
```

... or from a `[]byte` if you so desire

```go
data := getRemoteFileContent()
myEnv, err := godotenv.Unmarshal(data)
```

### Options

`godotenv.New` returns a `*Loader` that you configure once and reuse across
`Load`, `Decode`, `Unmarshal` and `Expand`:

```go
loader := godotenv.New(godotenv.WithBash())

file, err := os.Open(".env")
if err != nil {
    log.Fatal(err)
}
defer file.Close()

env, err := loader.Decode(file)
```

By default the parser follows strict POSIX shell rules with some exceptions.
There are four options:

- `WithUnboundError()`: an unset `$VAR` or `${VAR}` returns an error instead of
  expanding to an empty string. This is the equivalent of `set -u`.
- `WithBash()`: opt into the bash extensions. Without it, each of the following
  is literal text:
  - `$'...'` ANSI-C quoting, including `\xHH`, `\uHHHH`, `\UHHHHHHHH` and the
    braced `\x{}`, `\u{}`, `\U{}` forms.
  - `$"..."` translation quoting, which behaves as `"..."`.
  - dotenv-style backslash escapes inside `"..."`, so `"\n"` is a newline and
    `"\t"` is a tab.
- `WithOverride()`: `Load` overwrites variables already set in the process
  environment, so the files win instead of the environment.
- `WithLookup()`: the function used to resolve a reference to a variable not
  defined earlier in the parsed content. By default the process environment is
  used.

The package-level `Load`, `Decode`, `Unmarshal` and `Expand` functions use the
default (POSIX) behaviour and the process environment. Create a `Loader` with
`New` to change either.

### Shell Semantics

These forms are recognised in a value. `pat` is a shell glob (`*`, `?`,
`[...]`); `word` and `rep` are expanded recursively.

| form | meaning |
| --- | --- |
| `$VAR`, `${VAR}` | the value of `VAR`; empty when unset, or an error with `WithUnboundError()` |
| `${VAR:-word}`, `${VAR-word}` | `word` when `VAR` is unset; with `:` also when it is empty |
| `${VAR:+word}`, `${VAR+word}` | `word` when `VAR` is set; with `:` also when it is non-empty |
| `${VAR:?word}`, `${VAR?word}` | an error when `VAR` is unset; with `:` also when it is empty |
| `${VAR:=word}`, `${VAR=word}` | not supported — a parse error, since assigning inside a value makes no sense in an `.env` file |
| `${#VAR}` | the length of `VAR`, in characters |
| `${VAR#pat}`, `${VAR##pat}` | strip the shortest / longest matching prefix |
| `${VAR%pat}`, `${VAR%%pat}` | strip the shortest / longest matching suffix |
| `${VAR:offset}`, `${VAR:offset:length}` | substring; both are arithmetic and may be negative |
| `${VAR/pat/rep}`, `${VAR//pat/rep}` | replace the first / every match (ksh/bash extension, enabled in both modes) |
| `${VAR/#pat/rep}`, `${VAR/%pat/rep}` | replace a match anchored to the start / end (same extension) |
| `$((...))` | integer arithmetic, evaluated |
| `$(...)`, `` `...` `` * | kept verbatim and is not executed |

\* As mentioned in the table, command substitution is never executed: `$(...)`
and backticks are copied through as text, so loading a `.env` file cannot
run a command. I can't foresee it being needed in `.env` files, and feels
like it's rife for exploitation. I'm happy for someone to make the case
for adding support for this, please raise an issue.

Quoting:

| form | default (POSIX) | `WithBash()` |
| --- | --- | --- |
| `"..."` | backslash is special only before `$`, `` ` ``, `"`, `\` and newline, so `"\n"` stays `\n` | dotenv escapes are decoded: `"\n"` is a newline, `"\t"` a tab, plus `\xHH` and `\uHHHH` |
| `'...'` | literal; nothing is expanded | same |
| `$'...'` | literal text: the `$` stays and the quotes are dropped, so `$'a\tb'` is `$a\tb` | ANSI-C escapes: `$'a\tb'` is `a`, tab, `b`; also `\xHH`, `\x{}`, `\u{}`, `\U{}` |
| `$"..."` | a `$` followed by a normal double-quoted string | behaves exactly like `"..."` |

Inside a `${...}` word the quoting is decided by the word itself, not by a
surrounding `"..."`. So with `WithBash()`, `"${V:-$'a\tb'}"` is `a` + tab + `b`,
while `"$'a\tb'"` is the literal text `$'a\tb'`, in the second case the `$'`
sits directly inside the double quotes.

### Expanding Arbitrary Strings

`Expand` resolves references inside a single string, for configuration values
that are not `.env` syntax:

```go
out, err := godotenv.Expand("token=${API_KEY}")
```

Only `$NAME` and `${...}` are interpreted, against the process environment. For
a custom lookup or other options, use a `Loader`:

```go
loader := godotenv.New(godotenv.WithLookup(myLookup), godotenv.WithUnboundError())
out, err := loader.Expand("token=${API_KEY}")
```

`WithUnboundError()` makes an unset variable with no modifier an error instead
of the empty string, and `WithBash()` enables the `$'...'` and `$"..."` forms.
`$((...))` is left as written. `LookupEnvFunc` is the lookup signature taken by
`WithLookup`.

### Precedence & Conventions

`Load` reads the process's environment, then merges variables read from the
specified files. It leaves anything already set in the alone, so previously
environment variables stay. You can use `WithOverride()` to override this.

Within one call, files are read in the order you give them. If the same key
appears in more than one file, the value from the last file is used.

Key names must match `[A-Za-z_][A-Za-z0-9_]*`; in particular they cannot
contain `-` or `.`.

The [convention](https://github.com/bkeepers/dotenv#what-other-env-files-can-i-use)
for managing multiple environments (i.e. development, test, production)
is to create an env named `{YOURAPP}_ENV` and load envs in this order:

```go
env := os.Getenv("FOO_ENV")
if "" == env {
  env = "development"
}

godotenv.Load(".env." + env + ".local")
if "test" != env {
  godotenv.Load(".env.local")
}
godotenv.Load(".env." + env)
godotenv.Load() // The Original .env
```

If you need to, you can also use `godotenv.New(godotenv.WithOverride()).Load(...)`
to defy this convention and overwrite existing envs instead of only supplanting
them. Use with caution.

### Command Mode

Assuming you've installed the command as above, and you've got `$GOPATH/bin` in your `$PATH`

```
godotenv -f /some/path/to/.env some_command with some args
```

If you don't specify `-f` it will fall back on the default of loading `.env` in `PWD`

`-f` may be repeated to load several files (later files win) and replaces the
default `.env`. Pass `-o` to override existing environment variables.

### Writing Env Files

Godotenv can also write a map back to a correctly formatted and escaped file. Values are preserved as they are: integer values stay unquoted, leading zeros included, and everything else is double-quoted with `$` and `` ` `` backslash-escaped so the file is safe to `source`.

Because control characters are written with C-style escapes, reading such a file back needs `WithBash()`; the default POSIX parser keeps those backslashes literal.

```go
env, err := godotenv.Unmarshal([]byte("KEY=value"))
err = godotenv.Write(env, "./.env")
```

... or to bytes

```go
env, err := godotenv.Unmarshal([]byte("KEY=value"))
content, err := godotenv.Marshal(env)
```

## Development

Tools are pinned with [mise](https://mise.jdx.dev/) and the git hooks run through [prek](https://prek.j178.dev/):

```bash
mise install
mise run precommit:install
```

Useful tasks:

- `mise run test`: `go test -race -cover ./...`
- `mise run check`: gofumpt, goimports, golangci-lint and go vet
- `mise run precommit`: run every prek hook
- `mise run release:dry`: build a GoReleaser snapshot

## Contributing

Contributions are most welcome! The parser itself is pretty stupidly naive and I wouldn't be surprised if it breaks with edge cases.

*code changes without tests will not be accepted*

1. Fork it
2. Create your feature branch (`git checkout -b my-new-feature`)
3. Commit your changes (`git commit -am 'Added some feature'`)
4. Push to the branch (`git push origin my-new-feature`)
5. Create new Pull Request

## Releases

Releases should follow [Semver](http://semver.org/) though the first couple of releases are `v1` and `v1.1`.

Use [annotated tags for all releases](https://github.com/hoshsadiq/godotenv/issues/30). Example `git tag -a v1.2.1`

## Who?

The original library [dotenv](https://github.com/bkeepers/dotenv) was written by [Brandon Keepers](http://opensoul.org/), and this port was done by [John Barton](https://johnbarton.co/) based off the tests/fixtures in the original library.
