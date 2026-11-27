package scripttest

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// gitConfig is what the tests' git reads in place of the developer's
// configuration, and it has git start no maintenance of its own. A commit, a
// push, a fetch and a fast-forward merge each end by running `git maintenance
// run --auto`, which by default detaches: it forks a daemon and returns, and
// the daemon takes the repository's objects/maintenance.lock a moment later
// and drops it a moment after that, a moment the scheduler stretches on a
// loaded box. A template is copied the instant its build returns (CopyDirErr),
// so a copy that lists the lock can find it gone when it reads it (yardr-03av,
// in contrib/assembly's copies). Nothing here wants a repository repacked, so
// no repository is given the chance. gc.auto is the same switch for a git
// older than maintenance, whose commands run gc --auto themselves.
const gitConfig = "[maintenance]\n\tauto = false\n[gc]\n\tauto = 0\n"

var (
	gitMu   sync.Mutex
	gitHome string // the private HOME of the tests' git, with gitConfig in it
	seedMu  sync.Mutex
	seedDir string // the repositories SeedRepo and SeedEmptyRepo copy
	seedErr error
)

// gitHomeDir is the private home of the tests' git, made on the first call
// and removed by Remove. No test that asks for it can go on without it, so a
// home that cannot be made is a panic.
func gitHomeDir() string {
	gitMu.Lock()
	defer gitMu.Unlock()
	if gitHome == "" {
		dir, err := os.MkdirTemp("", "yardr-git-home")
		if err == nil {
			err = os.WriteFile(filepath.Join(dir, "gitconfig"), []byte(gitConfig), 0o600)
		}
		if err != nil {
			panic(fmt.Sprintf("scripttest: make the git home of the tests: %v", err))
		}
		gitHome = dir
	}
	return gitHome
}

// GitEnv is the environment the tests' git runs in: a hermetic base, so what
// the developer or the session configured (notably a core.hooksPath from
// GIT_CONFIG_*, which would run hooks on every setup commit) reaches neither
// the test's git nor what it asserts. The private home also keeps Git's
// default global excludes file out of staged fixtures. What git reads in
// place of the developer's configuration is the gitconfig in that home, for
// the git that builds the templates the tests copy (SeedRepo) as for the
// rest.
func GitEnv() []string {
	home := gitHomeDir()
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "YARDR_") || strings.HasPrefix(k, "GIT_CONFIG") ||
			k == "GIT_EDITOR" || k == "VISUAL" || k == "EDITOR" ||
			k == "XDG_CONFIG_HOME" {
			continue
		}
		env = append(env, kv)
	}
	return append(env,
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+filepath.Join(home, "gitconfig"),
		"HOME="+home)
}

// Git runs git in dir, in GitEnv, and fails the test where it fails.
func Git(t testing.TB, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = GitEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// SeedRepo returns a fresh repository on branch main with one empty root
// commit: what `git init -b main` and a root commit make, without spawning
// git twice per test. The template is built once per run and every test
// gets a copy of its .git, so tests share no state; what a test commits on
// top stays its own.
func SeedRepo(t testing.TB) string {
	t.Helper()
	tpl := seedTemplate(t)
	repo := t.TempDir()
	CopyDir(t, filepath.Join(tpl, "full", ".git"), filepath.Join(repo, ".git"))
	return repo
}

// SeedEmptyRepo is SeedRepo without the root commit: `git init -b main`
// alone, for the tests whose repository holds nothing yet.
func SeedEmptyRepo(t testing.TB) string {
	t.Helper()
	tpl := seedTemplate(t)
	repo := t.TempDir()
	CopyDir(t, filepath.Join(tpl, "empty", ".git"), filepath.Join(repo, ".git"))
	return repo
}

// seedTemplate builds the repositories SeedRepo and SeedEmptyRepo copy:
// under dir, full/ holds init plus the root commit and empty/ holds init
// alone, each on branch main. It runs once per test run; copying a .git
// costs milliseconds where init and a commit cost two git processes.
func seedTemplate(t testing.TB) string {
	t.Helper()
	seedMu.Lock()
	defer seedMu.Unlock()
	if seedDir == "" && seedErr == nil {
		seedDir, seedErr = func() (string, error) {
			dir, err := os.MkdirTemp("", "yardr-seed")
			if err != nil {
				return "", err
			}
			run := func(d string, args ...string) error {
				cmd := exec.Command("git", append([]string{"-C", d}, args...)...)
				cmd.Env = GitEnv()
				if out, err := cmd.CombinedOutput(); err != nil {
					return fmt.Errorf("git %v: %v\n%s", args, err, out)
				}
				return nil
			}
			full, empty := filepath.Join(dir, "full"), filepath.Join(dir, "empty")
			if err := os.MkdirAll(full, 0o700); err != nil {
				return "", err
			}
			if err := run(full, "init", "-q", "-b", "main"); err != nil {
				return "", err
			}
			if err := CopyDirErr(filepath.Join(full, ".git"), filepath.Join(empty, ".git")); err != nil {
				return "", err
			}
			if err := run(full, "commit", "-q", "--allow-empty", "-m", "root"); err != nil {
				return "", err
			}
			return dir, nil
		}()
	}
	if seedErr != nil {
		t.Fatalf("seed repositories: %v", seedErr)
	}
	return seedDir
}

// removeGit removes the git home and the seed repositories: Remove calls it.
func removeGit() {
	gitMu.Lock()
	if gitHome != "" {
		os.RemoveAll(gitHome)
		gitHome = ""
	}
	gitMu.Unlock()
	seedMu.Lock()
	if seedDir != "" {
		os.RemoveAll(seedDir)
		seedDir = ""
	}
	seedMu.Unlock()
}

// CopyDir copies the tree at src to dst, which must not exist.
func CopyDir(t testing.TB, src, dst string) {
	t.Helper()
	if err := CopyDirErr(src, dst); err != nil {
		t.Fatal(err)
	}
}

// CopyDirErr is CopyDir for a caller with no test to fail.
func CopyDirErr(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("copy %s: not a regular file", path)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, raw, 0o600)
	})
}
