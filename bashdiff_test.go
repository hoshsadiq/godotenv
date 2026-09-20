//go:build bashdiff

package godotenv_test

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/hoshsadiq/godotenv"
	"github.com/hoshsadiq/godotenv/internal/bashdiff"
)

// The reference suites are fetched at run time and cached under
// os.UserCacheDir()/godotenv. Their test files are read from the cache and are
// never committed to this repository. GNU bash's suite is GPL; Oils is
// Apache-2.0; FreeBSD and NetBSD are BSD-2-Clause.
const (
	bashTarballURL = "https://ftp.gnu.org/gnu/bash/bash-5.3.tar.gz"
	bashTarballSHA = "0d5cd86965f869a26cf64f4b71be7b96f90a3ba8b3d74e27e8e9d9d5550f31ba"
	bashTarballDir = "bash-5.3/tests/"

	oilsRepo    = "oils-for-unix/oils"
	oilsSHA     = "08310e96b182cf1d2dd65161e07b1743d17f8936"
	freebsdRepo = "freebsd/freebsd-src"
	freebsdSHA  = "62c80d76f7dc960f430cb34f88c2a549612e3e93"
	netbsdRepo  = "NetBSD/src"
	netbsdSHA   = "7fc84c350b484a8af1a4abbfdaabc2baa029bfae"
)

var oilsPaths = []string{
	"spec/var-op-len.test.sh",
	"spec/var-op-test.test.sh",
	"spec/var-op-strip.test.sh",
	"spec/var-sub.test.sh",
	"spec/var-sub-quote.test.sh",
	"spec/quote.test.sh",
	"spec/posix.test.sh",
}

var freebsdPaths = []string{
	"bin/sh/tests/expansion/trim1.0",
	"bin/sh/tests/expansion/trim5.0",
	"bin/sh/tests/expansion/trim9.0",
	"bin/sh/tests/expansion/trim11.0",
	"bin/sh/tests/expansion/length1.0",
	"bin/sh/tests/expansion/length5.0",
	"bin/sh/tests/expansion/plus-minus1.0",
	"bin/sh/tests/expansion/plus-minus5.0",
	"bin/sh/tests/expansion/assign1.0",
	"bin/sh/tests/expansion/question1.0",
	"bin/sh/tests/expansion/question2.0",
	"bin/sh/tests/expansion/set-u1.0",
}

var netbsdPaths = []string{
	"tests/bin/sh/t_varquote.sh",
	"tests/bin/sh/t_expand.sh",
}

// TestBashDiff compares the parser in bash mode against the local bash for every
// parameter-expansion case extracted from bash's tests/*.tests files. It fails
// only on mismatches that are not covered by the documented allowlist.
func TestBashDiff(t *testing.T) {
	bashPath := findBash(t)
	bashVersion := firstLine(t, bashPath)

	tarball := ensureTarball(t)
	gotSHA := sha256File(t, tarball)
	if gotSHA != bashTarballSHA {
		t.Fatalf("tarball sha256 = %s, want %s", gotSHA, bashTarballSHA)
	}

	testsDir, extracted := extractTests(t, tarball)
	files, err := filepath.Glob(filepath.Join(testsDir, "*.tests"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	sort.Strings(files)

	var suites []fileCases
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		base := filepath.Base(file)
		cases, counts := bashdiff.Extract(base, src)
		suites = append(suites, fileCases{
			base: base,
			summary: fmt.Sprintf(
				"%-24s lines=%-4d cases=%-3d bare=%-3d recho=%-3d context=%-3d excluded=%-3d",
				base, counts.Lines, counts.Cases, counts.Bare, counts.Recho, counts.Contexts, counts.Excluded),
			cases: cases,
		})
	}

	t.Logf("local bash: %s", bashVersion)
	t.Logf("tarball: %s sha256=%s", tarball, gotSHA)
	t.Logf("extracted %d .tests files", extracted)

	saved := os.Environ()
	os.Clearenv()
	defer restoreEnv(saved)

	compareSuite(t, "bash", suites, bashExpected(bashPath), evaluateBash, nil, bashdiff.BashAllowlist())
}

// TestPOSIXDiff compares the parser in its default POSIX mode against the
// in-tree expectations of the Oils, FreeBSD and NetBSD sh suites. No shell is
// needed; /bin/dash is consulted only for cases with a dash-specific override.
func TestPOSIXDiff(t *testing.T) {
	dashPath := findDash()
	if dashPath == "" {
		t.Log("no /bin/dash found; cases with a dash-specific override will be skipped")
	}
	oilsBin := ensureOilsHelper(t)

	type suite struct {
		name  string
		repo  string
		sha   string
		paths []string
		ext   func(string, []byte) ([]bashdiff.Case, string)
	}
	suites := []suite{
		{"oils", oilsRepo, oilsSHA, oilsPaths, extractOils},
		{"freebsd", freebsdRepo, freebsdSHA, freebsdPaths, extractFreeBSD},
		{"netbsd", netbsdRepo, netbsdSHA, netbsdPaths, extractNetBSD},
	}

	for _, s := range suites {
		var files []fileCases
		for _, p := range s.paths {
			src := fetchRaw(t, s.repo, s.sha, p)
			base := filepath.Base(p)
			cases, summary := s.ext(base, src)
			files = append(files, fileCases{base: base, summary: summary, cases: cases})
		}
		compareSuite(t, s.name, files, dashExpected(dashPath, oilsBin), evaluatePOSIX, bashdiff.GlobSkip, bashdiff.POSIXAllowlist())
	}
}

type fileCases struct {
	base    string
	summary string
	cases   []bashdiff.Case
}

type (
	expectedFunc func(bashdiff.Case) (expected, skip string)
	evaluator    func(bashdiff.Case) (string, error)
)

// skipActualFunc lets a suite skip a case once its evaluated value is known,
// for differences only observable after evaluation (e.g. pathname expansion).
type skipActualFunc func(bashdiff.Case, string) string

// compareSuite runs one suite's cases through the given evaluator, printing
// per-file counts and a mismatch table. It fails the test only on mismatches
// that the allowlist does not cover.
func compareSuite(t *testing.T, name string, files []fileCases, expected expectedFunc, eval evaluator, skipActual skipActualFunc, entries []bashdiff.AllowlistEntry) {
	t.Helper()

	var (
		total, compared, matched, allowed, mismatched, skipped int
		mismatchRows, allowRows, skipRows                      []string
	)
	for _, f := range files {
		t.Logf("  %s", f.summary)
		for _, c := range f.cases {
			total++
			if c.Skip != "" {
				skipped++
				skipRows = append(skipRows, fmt.Sprintf("%-44s %s", c.ID, c.Skip))
				continue
			}
			exp, skip := expected(c)
			if skip != "" {
				skipped++
				skipRows = append(skipRows, fmt.Sprintf("%-44s %s", c.ID, skip))
				continue
			}
			actual, err := eval(c)
			if err != nil {
				actual = "<error: " + err.Error() + ">"
			}
			if skipActual != nil {
				if reason := skipActual(c, actual); reason != "" {
					skipped++
					skipRows = append(skipRows, fmt.Sprintf("%-44s %s", c.ID, reason))
					continue
				}
			}
			compared++
			if exp == actual {
				matched++
				continue
			}
			if reason, ok := bashdiff.Allowlisted(c, entries); ok {
				allowed++
				allowRows = append(allowRows, fmt.Sprintf("%-44s expected=%-22q godotenv=%-22q reason=%s", c.ID, exp, actual, reason))
				continue
			}
			mismatched++
			mismatchRows = append(mismatchRows, fmt.Sprintf("%-44s expected=%-22q godotenv=%-22q", c.ID, exp, actual))
		}
	}

	t.Logf("%s: cases=%d compared=%d matched=%d allowlisted=%d mismatched=%d skipped=%d",
		name, total, compared, matched, allowed, mismatched, skipped)
	if len(allowRows) > 0 {
		t.Logf("%s allowlisted (%d):\n%s", name, len(allowRows), strings.Join(allowRows, "\n"))
	}
	if len(skipRows) > 0 {
		t.Logf("%s skipped (%d):\n%s", name, len(skipRows), strings.Join(skipRows, "\n"))
	}
	if len(mismatchRows) > 0 {
		t.Logf("%s mismatches (%d):\n%s", name, len(mismatchRows), strings.Join(mismatchRows, "\n"))
		for _, m := range mismatchRows {
			t.Errorf("%s mismatch: %s", name, m)
		}
	}
}

// script renders a case as .env content: earlier assignments then the line under
// test.
func script(c bashdiff.Case) string {
	return strings.Join(append(append([]string(nil), c.Context...), c.Expr), "\n") + "\n"
}

// evaluatePOSIX runs the case through the default (POSIX) parser with no process
// environment, matching `env -i`.
func evaluatePOSIX(c bashdiff.Case) (string, error) {
	env, err := godotenv.New(godotenv.WithLookup(noLookup)).Decode(strings.NewReader(script(c)))
	if err != nil {
		return "", err
	}
	return env[c.Var], nil
}

// evaluateBash runs the case through the bash-mode parser. It uses the process
// environment for fallbacks; TestBashDiff clears it first so undefined
// references expand to nothing, matching the `env -i` bash side.
func evaluateBash(c bashdiff.Case) (string, error) {
	env, err := godotenv.New(godotenv.WithBash()).Unmarshal([]byte(script(c)))
	if err != nil {
		return "", err
	}
	return env[c.Var], nil
}

func noLookup([]byte) ([]byte, bool) { return nil, false }

// bashScript builds the script whose variable value is the expected result.
func bashScript(c bashdiff.Case) string {
	lines := append(append([]string(nil), c.Context...), c.Expr)
	return strings.Join(lines, "\n") + "\nprintf %s \"$" + c.Var + "\""
}

// dashScript builds the shell script used to adjudicate a dash-specific
// override: the context assignments followed by the echo of the value under
// test.
func dashScript(c bashdiff.Case) string {
	args := strings.TrimPrefix(c.Expr, c.Var+"=")
	lines := append(append([]string(nil), c.Context...), "echo "+args)
	return strings.Join(lines, "\n") + "\n"
}

func bashExpected(bashPath string) expectedFunc {
	return func(c bashdiff.Case) (string, string) {
		expected, code, _ := runBash(bashPath, bashScript(c))
		if code != 0 {
			return "", fmt.Sprintf("bash exit %d", code)
		}
		return expected, ""
	}
}

// dashExpected returns the recorded in-tree expectation, except for cases
// marked Adjudicate, which are confirmed by running /bin/dash. Those are
// skipped when dash is unavailable or cannot reproduce the case.
func dashExpected(dashPath, pathEnv string) expectedFunc {
	return func(c bashdiff.Case) (string, string) {
		if !c.Adjudicate {
			return c.Expected, ""
		}
		if dashPath == "" {
			return "", "dash-specific override; no dash to adjudicate"
		}
		out, code, err := runRefShell(dashPath, dashScript(c), pathEnv)
		if err != nil || code != 0 {
			return "", "dash could not reproduce the override"
		}
		return strings.TrimSuffix(out, "\n"), ""
	}
}

func findBash(t *testing.T) string {
	t.Helper()
	var candidates []string
	if p, err := exec.LookPath("bash"); err == nil {
		candidates = append(candidates, p)
	}
	candidates = append(candidates, "/opt/homebrew/bin/bash", "/usr/local/bin/bash")

	seen := make(map[string]bool)
	for _, c := range candidates {
		if seen[c] {
			continue
		}
		seen[c] = true
		info, err := os.Stat(c)
		if err != nil || info.IsDir() {
			continue
		}
		out, err := exec.Command(c, "--version").Output()
		if err != nil {
			continue
		}
		if bashMajor(string(out)) >= 5 {
			return c
		}
	}
	t.Skip("no bash >= 5 found; skipping bash differential")
	return ""
}

// findDash returns /bin/dash when present. It is optional: the POSIX suites
// carry in-tree expectations, so only dash-override cases need it.
func findDash() string {
	for _, p := range []string{"/bin/dash", "/usr/bin/dash"} {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	if p, err := exec.LookPath("dash"); err == nil {
		return p
	}
	return ""
}

func bashMajor(version string) int {
	const marker = "version "
	i := strings.Index(version, marker)
	if i < 0 {
		return 0
	}
	rest := version[i+len(marker):]
	j := strings.IndexByte(rest, '.')
	if j < 0 {
		return 0
	}
	n, err := strconv.Atoi(rest[:j])
	if err != nil {
		return 0
	}
	return n
}

func firstLine(t *testing.T, bashPath string) string {
	t.Helper()
	out, err := exec.Command(bashPath, "--version").Output()
	if err != nil {
		t.Fatalf("bash --version: %v", err)
	}
	line, _, _ := strings.Cut(string(out), "\n")
	return line
}

func restoreEnv(saved []string) {
	os.Clearenv()
	for _, kv := range saved {
		if k, v, ok := strings.Cut(kv, "="); ok {
			_ = os.Setenv(k, v)
		}
	}
}

func cacheDir(t *testing.T) string {
	t.Helper()
	dir, err := os.UserCacheDir()
	if err != nil {
		t.Fatalf("UserCacheDir: %v", err)
	}
	return filepath.Join(dir, "godotenv")
}

func cachePath(t *testing.T) string {
	t.Helper()
	return filepath.Join(cacheDir(t), "bash-5.3.tar.gz")
}

// ensureTarball reuses the cached tarball when its sha256 matches, downloading
// it only when missing or corrupt.
func ensureTarball(t *testing.T) string {
	t.Helper()
	path := cachePath(t)
	if _, err := os.Stat(path); err == nil && sha256File(t, path) == bashTarballSHA {
		return path
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	body := download(t, bashTarballURL)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		t.Fatalf("write %s: %v", tmp, err)
	}
	if got := sha256File(t, tmp); got != bashTarballSHA {
		os.Remove(tmp)
		t.Fatalf("download sha256 = %s, want %s", got, bashTarballSHA)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatalf("rename: %v", err)
	}
	return path
}

// fetchRaw downloads one pinned file, caching it under a directory keyed by the
// commit SHA so a pin change always refetches.
func fetchRaw(t *testing.T, repo, sha, path string) []byte {
	t.Helper()
	dir := filepath.Join(cacheDir(t), filepath.Base(repo)+"-"+sha[:8], filepath.Dir(path))
	cached := filepath.Join(dir, filepath.Base(path))
	if b, err := os.ReadFile(cached); err == nil {
		return b
	}
	url := "https://raw.githubusercontent.com/" + repo + "/" + sha + "/" + path
	body := download(t, url)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(cached, body, 0o644); err != nil {
		t.Fatalf("write %s: %v", cached, err)
	}
	return body
}

// ensureOilsHelper fetches Oils' argv.py helper and returns the directory that
// must be on PATH when a case is executed by a reference shell.
func ensureOilsHelper(t *testing.T) string {
	t.Helper()
	body := fetchRaw(t, oilsRepo, oilsSHA, "spec/bin/argv.py")
	dir := filepath.Join(cacheDir(t), filepath.Base(oilsRepo)+"-"+oilsSHA[:8], "spec", "bin")
	path := filepath.Join(dir, "argv.py")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(path, body, 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return dir
}

func download(t *testing.T, url string) []byte {
	t.Helper()
	t.Logf("downloading %s", url)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("download %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("download %s: status %s", url, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	return body
}

func sha256File(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		t.Fatalf("hash %s: %v", path, err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// extractTests unpacks every *.tests file from the tarball into a temp dir.
func extractTests(t *testing.T, tarball string) (string, int) {
	t.Helper()
	f, err := os.Open(tarball)
	if err != nil {
		t.Fatalf("open tarball: %v", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	defer gz.Close()

	dir := t.TempDir()
	tr := tar.NewReader(gz)
	count := 0
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("tar: %v", err)
		}
		if !strings.HasPrefix(hdr.Name, bashTarballDir) || !strings.HasSuffix(hdr.Name, ".tests") {
			continue
		}
		out := filepath.Join(dir, filepath.Base(hdr.Name))
		w, err := os.Create(out)
		if err != nil {
			t.Fatalf("create %s: %v", out, err)
		}
		if _, err := io.Copy(w, tr); err != nil {
			w.Close()
			t.Fatalf("write %s: %v", out, err)
		}
		if err := w.Close(); err != nil {
			t.Fatalf("close %s: %v", out, err)
		}
		count++
	}
	return dir, count
}

// runBash runs script in a clean environment and returns stdout and exit code.
func runBash(bashPath, script string) (string, int, error) {
	return runRefShell(bashPath, script, "")
}

func runRefShell(shellPath, script, pathEnv string) (string, int, error) {
	cmd := exec.Command(shellPath, "--noprofile", "--norc", "-c", script)
	cmd.Env = []string{}
	if pathEnv != "" {
		cmd.Env = append(cmd.Env, "PATH="+pathEnv+":/usr/bin:/bin")
	}

	var stdout strings.Builder
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return stdout.String(), ee.ExitCode(), err
		}
		return stdout.String(), -1, err
	}
	return stdout.String(), 0, nil
}

func extractOils(name string, src []byte) ([]bashdiff.Case, string) {
	cases, counts := bashdiff.ExtractOils(name, src)
	return cases, fmt.Sprintf("%-28s cases=%-3d emitted=%-3d compared=%-3d skipped=%-3d rejected=%-3d",
		name, counts.Cases, counts.Emitted, counts.Compared, counts.Skipped, counts.Rejected)
}

func extractFreeBSD(name string, src []byte) ([]bashdiff.Case, string) {
	cases, counts := bashdiff.ExtractFreeBSD(name, src)
	return cases, fmt.Sprintf("%-28s candidates=%-3d emitted=%-3d compared=%-3d skipped=%-3d rejected=%-3d",
		name, counts.Cases, counts.Emitted, counts.Compared, counts.Skipped, counts.Rejected)
}

func extractNetBSD(name string, src []byte) ([]bashdiff.Case, string) {
	cases, counts := bashdiff.ExtractNetBSD(name, src)
	return cases, fmt.Sprintf("%-28s checks=%-3d emitted=%-3d compared=%-3d skipped=%-3d rejected=%-3d",
		name, counts.Cases, counts.Emitted, counts.Compared, counts.Skipped, counts.Rejected)
}
