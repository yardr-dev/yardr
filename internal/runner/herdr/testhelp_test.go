package herdr

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"yardr.dev/yardr/internal/scripttest"
)

// The helpers the tests of this package and those of internal/runner both
// use. Each package has a copy of the small ones in its own testhelp_test.go;
// the git ones are scripttest's, and these are the calls the tests make of
// them.

func gitT(t *testing.T, dir string, args ...string) { t.Helper(); scripttest.Git(t, dir, args...) }

func testGitEnv() []string { return scripttest.GitEnv() }

func seedRepo(t *testing.T) string { t.Helper(); return scripttest.SeedRepo(t) }

func seedEmptyRepo(t *testing.T) string { t.Helper(); return scripttest.SeedEmptyRepo(t) }

func copyDir(t *testing.T, src, dst string) { t.Helper(); scripttest.CopyDir(t, src, dst) }

func mustT(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// psNice is the niceness ps says pid runs at: what an operator sees, which
// runner.NiceOf is held to.
func psNice(t *testing.T, pid int) int {
	t.Helper()
	out, err := exec.Command("ps", "-o", "nice=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		t.Fatalf("ps -o nice= -p %d: %v", pid, err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatalf("ps -o nice= -p %d said %q", pid, out)
	}
	return n
}

// above is own plus by, for a test to ask a group for a niceness above that of
// a process at own. A group's is 0 to store.MaxNice and 0 asks for none, so
// where the process started below 0, as every process of some CI runners does,
// it counts from 0: own+by would be one no group can ask for. It can pass
// store.MaxNice, which a test that cannot ask for that skips or caps.
func above(own, by int) int { return max(own, 0) + by }

const envSecret = "lin_api_s3cret"

func envFileT(t *testing.T, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "yardmaster.env")
	body := "# outbound Linear\nLINEAR_API_KEY=" + envSecret + "\nYARDR_ACTOR=someone-else\n"
	if err := os.WriteFile(p, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	return p
}

// serverSettingNames are the settings of the yard's server.
var serverSettingNames = []string{"YARDR_HOOK_ADDR", "YARDR_WEB_ADDR", "YARDR_BDP_ADDR", "YARDR_EVENT_RETENTION", "YARDR_HOOK_PAYLOAD_RETENTION"}

// waitFile is what an exec command wrote to path, waited for.
func waitFile(t *testing.T, path string) string {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if b, _ := os.ReadFile(path); len(b) > 0 {
			return string(b)
		}
	}
	t.Fatalf("%s was never written", path)
	return ""
}
