package herdr

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"yardr.dev/yardr/internal/config"
	"yardr.dev/yardr/internal/dispatch"
	"yardr.dev/yardr/internal/peer"
	"yardr.dev/yardr/internal/scripttest"
	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/storetest"
)

func TestAgentName(t *testing.T) {
	t.Parallel()
	cases := map[[2]string]string{
		{"web-12", "review"}:                     "web-12-review",
		{"Web-12", "Needs Review!"}:              "web-12-needs-review",
		{"12", "new"}:                            "y12-new",
		{"averyveryverylongprefix-123", "stage"}: "averyveryverylongprefix-123-stag",
	}
	for in, want := range cases {
		if got := AgentName(in[0], in[1]); got != want {
			t.Errorf("AgentName(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}

func TestCrewConversationAgentNameDisambiguatesUsers(t *testing.T) {
	t.Parallel()
	got := []string{
		CrewConversationAgentName("yard", "yardmaster", "ann.smith"),
		CrewConversationAgentName("yard", "yardmaster", "ann-smith"),
		CrewConversationAgentName("yard", "yardmaster", strings.Repeat("a", 80)+"x"),
		CrewConversationAgentName("yard", "yardmaster", strings.Repeat("a", 80)+"y"),
	}
	for i, name := range got {
		if len(name) > 32 || len(name) == 0 || name[0] < 'a' || name[0] > 'z' || nameUnsafe.MatchString(name) {
			t.Errorf("conversation agent name %q is not a herdr name of at most 32 characters", name)
		}
		for _, other := range got[:i] {
			if name == other {
				t.Errorf("conversation agent names collide: %q", name)
			}
		}
	}
}

// fakeHerdrBin writes a script that answers like herdr: results on stdout,
// error objects on stderr with exit status 1.
func fakeHerdrBin(t testing.TB, script string) string {
	t.Helper()
	// As a real pane: the prompt an agent was sent shows in it.
	return deafHerdrBin(t, `case "$1 $2" in
  "agent prompt") printf '%s\n' "$4" > "$0.pane" ;;
  "pane read") cat "$0.pane" 2>/dev/null ;;
esac
`+script)
}

// herdrAnswers is shell every fake herdr begins with. `answer <state>` says
// what herdr 0.9.0 says of an `agent prompt` it was given to wait on, in the
// words it was seen to use in an isolated yard: working or blocked for the
// state it saw the agent reach, refused for an agent that was at a dialog
// before anything was typed, timeout for the caller's --timeout running
// out, and anything else for a prompt the agent started on nothing for.
// Asked without the flags yardr waits by, it answers as herdr does a prompt
// it does not wait on: sent, the agent as it was. It ends the script either
// way: the answer is the last thing herdr does about a prompt.
const herdrAnswers = `ARGS="$*"
answer() {
  case "$ARGS" in
    *" --wait --until working --until blocked --timeout "[0-9]*) ;;
    *) echo '{"result":{"agent":{"agent_status":"idle"},"type":"agent_prompted"}}'; exit 0 ;;
  esac
  case "$1" in
    working|blocked) echo '{"result":{"agent":{"agent_status":"'"$1"'"},"type":"agent_prompted"}}'; exit 0 ;;
    refused) echo '{"error":{"code":"agent_blocked","message":"agent b1 is blocked and requires interactive input"},"id":"cli:agent:prompt"}' >&2; exit 1 ;;
    timeout) echo '{"error":{"code":"timeout","message":"timed out waiting for agent status"},"id":"cli:agent:prompt"}' >&2; exit 1 ;;
    *) echo '{"error":{"code":"agent_prompt_stalled","message":"agent prompt produced no observed working or blocked state within 5000 ms; current status is idle"},"id":"cli:agent:prompt"}' >&2; exit 1 ;;
  esac
}
`

// deafHerdrBin is fakeHerdrBin without a pane that shows what was sent: an
// agent that takes no keys, unless the script says otherwise.
func deafHerdrBin(t testing.TB, script string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "herdr")
	scripttest.Write(t, p, "#!/bin/sh\n"+herdrAnswers+script)
	return p
}

func TestHerdrAliveReadsErrorsFromStderr(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := &Herdr{Bin: fakeHerdrBin(t, `case "$3" in
  alive) echo '{"id":"x","result":{"type":"agent_info","agent":{"name":"alive"}}}' ;;
  gone)  echo '{"error":{"code":"agent_not_found","message":"agent target gone not found"},"id":"x"}' >&2; exit 1 ;;
  *)     echo '{"error":{"code":"server_not_running","message":"no herdr server"},"id":"x"}' >&2; exit 1 ;;
esac`)}
	if ok, err := h.Alive(ctx, map[string]any{"agent": "alive"}); !ok || err != nil {
		t.Fatalf("alive agent: %v %v", ok, err)
	}
	if ok, err := h.Alive(ctx, map[string]any{"agent": "gone"}); ok || err != nil {
		t.Fatalf("gone agent must be dead, not unknown: %v %v", ok, err)
	}
	if _, err := h.Alive(ctx, map[string]any{"agent": "x"}); err == nil {
		t.Fatal("herdr down must be unknown (error), not dead")
	}
}

func TestHerdrFinishWorktrees(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := seedRepo(t)
	root := t.TempDir()
	add := func(bead string) string {
		path := filepath.Join(root, bead)
		gitT(t, repo, "worktree", "add", "-q", "-b", "yard/"+bead, path)
		dir, err := BriefDir(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writeBrief(dir, "s", "brief"); err != nil {
			t.Fatal(err)
		}
		// a pre-yardr brief dir, ignored by its own .gitignore
		os.MkdirAll(filepath.Join(path, ".yard"), 0o700)
		os.WriteFile(filepath.Join(path, ".yard", ".gitignore"), []byte("*\n"), 0o600)
		os.WriteFile(filepath.Join(path, ".yard", "brief-s.md"), []byte("brief"), 0o600)
		os.WriteFile(filepath.Join(path, "tracked.txt"), []byte("tracked"), 0o600)
		gitT(t, path, "add", "tracked.txt")
		gitT(t, path, "commit", "-q", "-m", "tracked")
		return path
	}
	log := filepath.Join(t.TempDir(), "calls")
	clean, dirty, mod, staged := add("b1"), add("b2"), add("b3"), add("b4")
	// workspace w1 is b1's checkout; w9 belongs to someone else.
	h := &Herdr{Bin: fakeHerdrBin(t, `echo "$@" >> `+log+`
case "$1 $2 $3" in
  "workspace get w1") echo '{"result":{"type":"workspace_info","workspace":{"workspace_id":"w1","worktree":{"checkout_path":"`+clean+`"}}}}' ;;
  "workspace get w9") echo '{"result":{"type":"workspace_info","workspace":{"workspace_id":"w9","worktree":{"checkout_path":"/elsewhere"}}}}' ;;
  *) echo '{"result":{"type":"ok"}}' ;;
esac`)}

	if err := h.Finish(ctx, map[string]any{"worktree": clean, "repo": repo, "workspace": "w1"}); err != nil {
		t.Fatalf("clean worktree: %v", err)
	}
	if _, err := os.Stat(clean); !os.IsNotExist(err) {
		t.Fatal("clean worktree not removed")
	}
	gitT(t, repo, "rev-parse", "--verify", "yard/b1") // branch kept

	os.WriteFile(filepath.Join(dirty, "wip.txt"), []byte("unsaved"), 0o600)
	err := h.Finish(ctx, map[string]any{"worktree": dirty, "repo": repo, "workspace": "w9"})
	if err == nil || !strings.Contains(err.Error(), "wip.txt") {
		t.Fatalf("dirty worktree must be kept with a reason, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dirty, "wip.txt")); err != nil {
		t.Fatal("dirty worktree was touched")
	}

	os.WriteFile(filepath.Join(mod, "tracked.txt"), []byte("changed"), 0o600)
	if err := h.Finish(ctx, map[string]any{"worktree": mod, "repo": repo}); err == nil || !strings.Contains(err.Error(), "tracked.txt") {
		t.Fatalf("modified worktree must be kept with a reason, got %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(mod, "tracked.txt")); string(got) != "changed" {
		t.Fatal("modified worktree was touched")
	}

	// A staged deletion is deliberate, not a removal that died midway.
	gitT(t, staged, "rm", "-q", "tracked.txt")
	if err := h.Finish(ctx, map[string]any{"worktree": staged, "repo": repo}); err == nil || !strings.Contains(err.Error(), "uncommitted changes") {
		t.Fatalf("staged deletion must be kept with a reason, got %v", err)
	}

	if err := h.Finish(ctx, map[string]any{"worktree": clean, "repo": repo}); !errors.Is(err, dispatch.ErrNothingLeft) {
		t.Fatalf("already removed: %v", err)
	}
	calls, _ := os.ReadFile(log)
	if !strings.Contains(string(calls), "workspace close w1") || strings.Contains(string(calls), "close w9") {
		t.Fatalf("workspace close calls wrong:\n%s", calls)
	}
	// A removal interrupted after deleting tracked files leaves only ` D`
	// lines. The branch still holds those files, so Finish can remove the
	// remaining worktree while keeping its branch.
	half := add("b5")
	if err := os.Remove(filepath.Join(half, "tracked.txt")); err != nil {
		t.Fatal(err)
	}
	if err := h.Finish(ctx, map[string]any{"worktree": half, "repo": repo}); err != nil {
		t.Fatalf("half-removed worktree: %v", err)
	}
	if _, err := os.Stat(half); !os.IsNotExist(err) {
		t.Fatal("half-removed worktree not removed")
	}
	gitT(t, repo, "rev-parse", "--verify", "yard/b5") // branch kept
	if out := gitOutT(t, repo, "worktree", "list", "--porcelain"); strings.Contains(out, half) {
		t.Fatalf("worktree list still names %s:\n%s", half, out)
	}

	// Finish returns once the worktree is moved aside and pruned. A leftover
	// move-aside from a crashed cleanup is swept first, then the moved tree
	// disappears in the background.
	background := add("b6")
	stale := background + ".removing"
	os.MkdirAll(filepath.Join(stale, "sub"), 0o700)
	os.WriteFile(filepath.Join(stale, "sub", "junk"), []byte("junk"), 0o600)
	if err := h.Finish(ctx, map[string]any{"worktree": background, "repo": repo}); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if _, err := os.Stat(background); !os.IsNotExist(err) {
		t.Fatal("worktree path still exists after Finish returned")
	}
	if out := gitOutT(t, repo, "worktree", "list", "--porcelain"); strings.Contains(out, background) {
		t.Fatalf("worktree list still names %s:\n%s", background, out)
	}
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if _, err := os.Stat(stale); os.IsNotExist(err) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("moved worktree not deleted in the background")
		}
	}
}

func TestAgentArgsModel(t *testing.T) {
	t.Parallel()
	job := func(groupCfg, beadMeta map[string]any) dispatch.Job {
		return dispatch.Job{Group: store.Group{Config: groupCfg}, Bead: store.Bead{Metadata: beadMeta}}
	}
	args := []any{"--permission-mode", "auto", "--model", "sonnet"}
	cases := []struct {
		name string
		job  dispatch.Job
		want []string
	}{
		{"group args only", job(map[string]any{"args": args}, nil),
			[]string{"--permission-mode", "auto", "--model", "sonnet"}},
		{"bead overrides group args", job(map[string]any{"args": args}, map[string]any{"model": "opus"}),
			[]string{"--permission-mode", "auto", "--model", "opus"}},
		{"group model key", job(map[string]any{"model": "haiku"}, nil), []string{"--model", "haiku"}},
		{"unknown kind keeps model flag fallback", job(map[string]any{"kind": "cursor", "model": "auto"}, nil), []string{"--model", "auto"}},
		{"opencode's model goes in its environment, not its args", job(map[string]any{"kind": "opencode", "model": "opencode/big-pickle"}, nil), nil},
		{"opencode's bead model goes in its environment too", job(map[string]any{"kind": "opencode", "model": "opencode/big-pickle"}, map[string]any{"model": "other/model"}), nil},
		{"opencode drops a --model in its args", job(map[string]any{"kind": "opencode", "args": []any{"--auto", "--model", "sonnet"}, "model": "opencode/big-pickle"}, nil),
			[]string{"--auto"}},
		{"opencode drops a bare --model in its args", job(map[string]any{"kind": "opencode", "args": []any{"--auto", "--model"}, "model": "opencode/big-pickle"}, nil),
			[]string{"--auto"}},
		{"bead beats group key", job(map[string]any{"model": "haiku"}, map[string]any{"model": "opus"}),
			[]string{"--model", "opus"}},
		{"nothing", job(map[string]any{}, nil), nil},
		// yardr-jkcy: a group's effort is the kind's own words after the
		// group's args and the model's flag, and a kind with none passes none.
		{"claude effort", job(map[string]any{"kind": "claude", "effort": "max"}, nil), []string{"--effort", "max"}},
		{"effort after the args and the model", job(map[string]any{"kind": "claude", "args": args, "model": "haiku", "effort": "high"}, map[string]any{"model": "opus"}),
			[]string{"--permission-mode", "auto", "--model", "opus", "--effort", "high"}},
		{"effort with no model and args", job(map[string]any{"args": []any{"--permission-mode", "auto"}, "effort": "low"}, nil),
			[]string{"--permission-mode", "auto", "--effort", "low"}},
		{"muse effort", job(map[string]any{"kind": "muse", "args": []any{"--yolo"}, "model": "muse-spark-1.3", "effort": "xhigh"}, nil),
			[]string{"--yolo", "--model", "muse-spark-1.3", "--reasoning-effort", "xhigh"}},
		{"a kind that takes none", job(map[string]any{"kind": "pi", "effort": "max"}, nil), nil},
		{"an effort that is no string", job(map[string]any{"kind": "claude", "effort": 5}, nil), nil},
	}
	for _, c := range cases {
		if got := AgentArgs(c.job, "", "", ""); strings.Join(got, " ") != strings.Join(c.want, " ") {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// herdr reports an agent stuck at a startup question as agent_not_ready;
// the session must be kept with its first prompt pending, not failed.
func TestStartAgentKeepsNotReadyAgents(t *testing.T) {
	t.Parallel()
	log := filepath.Join(t.TempDir(), "calls")
	h := &Herdr{Bin: fakeHerdrBin(t, `echo "$@" >> `+log+`
case "$1 $2" in
  "agent start") echo '{"error":{"code":"agent_not_ready","message":"agent yardmaster is blocked during startup and is not ready for prompts"},"id":"x"}' >&2; exit 1 ;;
  *) echo '{"result":{"type":"ok"}}' ;;
esac`)}
	handle, err := h.startAgent(context.Background(), []string{"agent", "start", "yardmaster"}, "yardmaster", "prime yourself",
		map[string]any{"agent": "yardmaster"}, nil, nil, nil)
	if err != nil {
		t.Fatalf("not-ready agent treated as a failure: %v", err)
	}
	if handle[store.PendingPrompt] != "prime yourself" {
		t.Fatalf("handle = %v", handle)
	}
	calls, _ := os.ReadFile(log)
	if strings.Contains(string(calls), "agent prompt") {
		t.Fatalf("prompted an agent that was not ready:\n%s", calls)
	}
}

func TestHerdrLabels(t *testing.T) {
	t.Parallel()
	builders := store.Group{Name: "yard-builders"}
	codex := store.Group{Name: "yard-codex-review", Config: map[string]any{"label": "codex"}}
	cases := []struct {
		name               string
		depot              string
		group              store.Group
		stage, train, bead string
		workspace, tab     string
	}{
		{"child", "yard", builders, "new", "yard-sv6b", "yard-p4cv", "builders [sv6b] p4cv", "new [sv6b] p4cv"},
		{"plain bead", "yard", builders, "new", "", "yard-cbdc", "builders cbdc", "new cbdc"},
		{"group label set", "yard", codex, "review", "yard-sv6b", "yard-p4cv", "codex [sv6b] p4cv", "review [sv6b] p4cv"},
		{"group label set, plain bead", "yard", codex, "review", "", "yard-cbdc", "codex cbdc", "review cbdc"},
		// The label is used as it is, prefix or not.
		{"group label with the prefix", "yard", store.Group{Name: "yard-x", Config: map[string]any{"label": "yard-x"}}, "new", "", "yard-1", "yard-x 1", "new 1"},
		{"empty group label", "yard", store.Group{Name: "yard-builders", Config: map[string]any{"label": ""}}, "new", "", "yard-1", "builders 1", "new 1"},
		{"group without the depot prefix", "yard", store.Group{Name: "builders"}, "new", "yard-sv6b", "yard-p4cv", "builders [sv6b] p4cv", "new [sv6b] p4cv"},
		// Only a leading "<depot>-" goes: not the bare depot, not a longer word.
		{"group with a longer word", "yard", store.Group{Name: "yardstick"}, "new", "", "yard-29", "yardstick 29", "new 29"},
		{"ids without the depot prefix", "yard", builders, "new", "web-12", "web-13", "builders [web-12] web-13", "new [web-12] web-13"},
		{"id that is a longer word", "yard", builders, "new", "", "yardstick-1", "builders yardstick-1", "new yardstick-1"},
		{"another depot", "webapp", store.Group{Name: "yard-builders"}, "new", "", "webapp-1", "yard-builders 1", "new 1"},
		{"no depot", "", builders, "new", "yard-sv6b", "yard-p4cv", "yard-builders [yard-sv6b] yard-p4cv", "new [yard-sv6b] yard-p4cv"},
		{"no group", "yard", store.Group{}, "new", "yard-sv6b", "yard-p4cv", "[sv6b] p4cv", "new [sv6b] p4cv"},
	}
	for _, c := range cases {
		if got := WorkspaceLabel(c.depot, c.group, c.train, c.bead); got != c.workspace {
			t.Errorf("%s: WorkspaceLabel = %q, want %q", c.name, got, c.workspace)
		}
		if got := TabLabel(c.depot, c.stage, c.train, c.bead); got != c.tab {
			t.Errorf("%s: TabLabel = %q, want %q", c.name, got, c.tab)
		}
	}
}

// herdr shows workspace and tab labels: the workspace is the group and the
// bead, with the train between them for a child of one, and each stage's
// tab the same with the stage in place of the group, ids without the depot;
// on the first stage (worktree created) and on a later one (worktree
// reopened, and relabelled). A stage started again, after its session died
// early, reopens the same workspace: no second one is left beside it.
func TestHerdrStartLabels(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	wt := seedEmptyRepo(t)
	start := func(t *testing.T, createScript, stage string, group store.Group, train string) string {
		t.Helper()
		log := filepath.Join(t.TempDir(), "calls")
		h := &Herdr{Socket: fakeHerdrAPI(t, log), Bin: fakeHerdrBin(t, `printf '%s\n' "$*" >> `+log+`
case "$1 $2" in
  "worktree create") `+createScript+` ;;
  "worktree open") echo '{"result":{"workspace":{"workspace_id":"w1"},"tab":{"tab_id":"t1"},"root_pane":{"pane_id":"p1"},"worktree":{"path":"`+wt+`"},"already_open":true}}' ;;
  *) echo '{"result":{"type":"ok"}}' ;;
esac`)}
		job := dispatch.Job{
			Bead:    store.Bead{ID: "yard-13", Stage: stage},
			Group:   group,
			Depot:   &store.Depot{Name: "yard", Path: t.TempDir(), Base: "main"},
			Session: store.Session{ID: "s-" + stage},
			Train:   train,
		}
		if train != "" {
			// Start makes the train's branch from the base, so give it one.
			job.Depot.Path = seedRepo(t)
		}
		handle, err := h.Start(ctx, job)
		if err != nil {
			t.Fatal(err)
		}
		if handle["agent"] != AgentName("yard-13", stage) {
			t.Fatalf("agent name changed: %v", handle["agent"])
		}
		calls, _ := os.ReadFile(log)
		return string(calls)
	}

	builders := store.Group{Name: "yard-builders"}
	codex := store.Group{Name: "yard-codex-review", Config: map[string]any{"label": "codex"}}
	created := `echo '{"result":{"workspace":{"workspace_id":"w1"},"tab":{"tab_id":"t1"},"root_pane":{"pane_id":"p1"},"worktree":{"path":"` + wt + `"}}}'`
	calls := start(t, created, "new", builders, "")
	if !strings.Contains(calls, "worktree create ") || !strings.Contains(calls, "--label builders 13\n") {
		t.Fatalf("first stage workspace not labelled with the group and the bead:\n%s", calls)
	}
	if strings.Contains(calls, "workspace rename") {
		t.Fatalf("first stage relabelled:\n%s", calls)
	}
	// tab is the tab.create request's workspace, cwd and label.
	tab := func(calls string) string {
		p := apiCall(t, calls, "tab.create")
		return fmt.Sprint(p["workspace_id"], " ", p["cwd"], " ", p["label"])
	}
	if got := tab(calls); got != "w1 "+wt+" new 13" {
		t.Fatalf("first stage tab not labelled stage first: %q\n%s", got, calls)
	}

	exists := `echo '{"error":{"code":"worktree_exists","message":"branch exists"}}' >&2; exit 1`
	calls = start(t, exists, "review", codex, "")
	for _, want := range []string{
		"worktree open --cwd ",
		"workspace rename w1 codex 13\n",
	} {
		if !strings.Contains(calls, want) {
			t.Errorf("later stage missing %q:\n%s", want, calls)
		}
	}
	if got := tab(calls); got != "w1 "+wt+" review 13" {
		t.Errorf("later stage tab %q:\n%s", got, calls)
	}

	// The same stage started again reopens the dead session's workspace
	// rather than opening a second one beside it.
	calls = start(t, exists, "new", builders, "")
	if !strings.Contains(calls, "worktree open --cwd ") {
		t.Errorf("same-stage restart did not reopen the dead session's workspace:\n%s", calls)
	}
	if got := tab(calls); got != "w1 "+wt+" new 13" {
		t.Errorf("same-stage restart tab %q, want it in the reopened workspace:\n%s", got, calls)
	}

	// A child of a train: the workspace reads the train after the group, the
	// tab the same after the stage, and the agent name is as for any bead.
	calls = start(t, created, "new", builders, "yard-12")
	if !strings.Contains(calls, "worktree create ") || !strings.Contains(calls, "--label builders [12] 13\n") {
		t.Fatalf("child's workspace not labelled with its train:\n%s", calls)
	}
	if got := tab(calls); got != "w1 "+wt+" new [12] 13" {
		t.Fatalf("child's tab not labelled with its train: %q\n%s", got, calls)
	}
	calls = start(t, exists, "review", codex, "yard-12")
	if !strings.Contains(calls, "workspace rename w1 codex [12] 13\n") {
		t.Fatalf("child's reopened workspace not relabelled with its train:\n%s", calls)
	}
}

// The brief goes into the worktree's git dir, outside the tracked tree:
// nothing in the checkout changes, so .yardr/ there can be committed config,
// and the agent is pointed at the brief by its absolute path.
func TestHerdrBriefOutsideTheTree(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := seedRepo(t)
	wt := filepath.Join(t.TempDir(), "yard-13")
	gitT(t, repo, "worktree", "add", "-q", "-b", "yard/yard-13", wt)

	log := filepath.Join(t.TempDir(), "calls")
	h := &Herdr{Socket: fakeHerdrAPI(t, log), Bin: fakeHerdrBin(t, `printf '%s\n' "$*" >> `+log+`
case "$1 $2" in
  "worktree create") echo '{"result":{"workspace":{"workspace_id":"w1"},"tab":{"tab_id":"t1"},"root_pane":{"pane_id":"p1"},"worktree":{"path":"`+wt+`"}}}' ;;
  "pane read") echo "❯ " ;;
  *) echo '{"result":{"type":"ok"}}' ;;
esac`)}
	job := dispatch.Job{
		Bead:    store.Bead{ID: "yard-13", Title: "Shed", Stage: "new"},
		Group:   store.Group{Name: "builders"},
		Depot:   &store.Depot{Name: "yard", Path: repo, Base: "main"},
		Session: store.Session{ID: "s1"},
	}
	handle, err := h.Start(ctx, job)
	if err != nil {
		t.Fatal(err)
	}
	brief, _ := handle["brief"].(string)
	gitDir := gitOutT(t, wt, "rev-parse", "--absolute-git-dir")
	if !filepath.IsAbs(brief) || filepath.Dir(brief) != filepath.Join(gitDir, "yardr") {
		t.Fatalf("brief at %q, want it under %s/yardr", brief, gitDir)
	}
	if b, err := os.ReadFile(brief); err != nil || !strings.Contains(string(b), "Shed") {
		t.Fatalf("brief not written: %v\n%s", err, b)
	}
	if st := gitOutT(t, wt, "status", "--porcelain", "--ignored"); st != "" {
		t.Fatalf("brief left files in the worktree:\n%s", st)
	}
	if _, err := os.Stat(filepath.Join(wt, ".yard")); !os.IsNotExist(err) {
		t.Fatal("still writes .yard/ into the worktree")
	}
	calls, _ := os.ReadFile(log)
	if !strings.Contains(string(calls), "agent prompt yard-13-new Read "+brief+" and follow it. --wait ") {
		t.Fatalf("agent not pointed at the brief by its path:\n%s", calls)
	}
	// What the dispatcher gives again to a harness that was at capacity is
	// that same prompt, from the handle alone.
	if got, want := h.FirstPrompt(handle), "Read "+brief+" and follow it."; got != want {
		t.Fatalf("FirstPrompt = %q, want %q", got, want)
	}
	if got := h.FirstPrompt(map[string]any{"agent": "yardmaster"}); got != "" {
		t.Fatalf("FirstPrompt of a handle with no brief = %q, want none", got)
	}
}

// A bead agent's identity is in its environment, set by herdr on the tab
// it runs in, not in an export the prompt asks for: its yardr calls are
// attributed to it, reach this yard, and carry its session.
func TestHerdrStartPutsAgentEnvOnItsTab(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	wt := seedEmptyRepo(t)
	for _, open := range []bool{false, true} {
		log := filepath.Join(t.TempDir(), "calls")
		home := t.TempDir()
		h := &Herdr{YardHome: home, YardBin: "/bin/yardr", Socket: fakeHerdrAPI(t, log), Bin: fakeHerdrBin(t, `printf '%s\n' "$*" >> `+log+`
case "$1 $2" in
  "worktree create") echo '{"result":{"workspace":{"workspace_id":"w1"},"tab":{"tab_id":"t1"},"root_pane":{"pane_id":"p1"},"worktree":{"path":"`+wt+`"},"already_open":`+fmt.Sprint(open)+`}}' ;;
  "pane process-info") echo '{"result":{"process_info":{"pane_id":"p2","shell_pid":`+fmt.Sprint(os.Getpid())+`}}}' ;;
  *) echo '{"result":{"type":"ok"}}' ;;
esac`)}
		job := dispatch.Job{
			Bead:    store.Bead{ID: "yard-13", Stage: "new", Metadata: map[string]any{"model": "claude-test-model"}},
			Group:   store.Group{Name: "yard-builders"},
			Depot:   &store.Depot{Name: "yard", Path: t.TempDir(), Base: "main"},
			Session: store.Session{ID: "s1"},
		}
		handle, err := h.Start(ctx, job)
		if err != nil {
			t.Fatal(err)
		}
		if handle["tab"] != "t2" || handle["pane"] != "p2" {
			t.Fatalf("agent not in the tab created with its env: %v", handle)
		}
		// The tab's shell, with its start time, so the store knows the
		// session's calls by the process tree (store.Expect).
		if handle["shell_pid"] != os.Getpid() {
			t.Fatalf("shell_pid = %v, want this process %d", handle["shell_pid"], os.Getpid())
		}
		if st, err := peer.Describe(os.Getpid()); err == nil && handle["shell_start"] != st.Start {
			t.Fatalf("shell_start = %v, want %d", handle["shell_start"], st.Start)
		}
		if !strings.Contains(string(mustRead(t, log)), "pane process-info --pane p2") {
			t.Fatalf("the pane's process info was not asked for:\n%s", mustRead(t, log))
		}
		raw, _ := os.ReadFile(log)
		calls := string(raw)
		env := envOf(apiCall(t, calls, "tab.create"))
		// GIT_EDITOR: no editor opens in the tab, so a git rebase --continue
		// in it ends instead of parking.
		// YARDR_GROUP, YARDR_KIND and YARDR_MODEL are what the yard's commit-msg
		// hook names a commit with, and the core.hooksPath entries point the
		// agent's git at it: the tests of the hook (internal/runner) build the same
		// entries for the session they commit in.
		for k, v := range map[string]string{"YARDR_ACTOR": "yard-13-new", "YARDR_HOME": home, "YARDR_BEAD": "yard-13",
			"YARDR_SESSION": "s1", "YARDR_BIN": "/bin/yardr", "GIT_EDITOR": "true",
			"YARDR_GROUP": "yard-builders", "YARDR_KIND": "claude", "YARDR_MODEL": "claude-test-model",
			"GIT_CONFIG_KEY_0": "core.hooksPath", "GIT_CONFIG_VALUE_0": config.At(home).GitHooks()} {
			if env[k] != v {
				t.Errorf("already_open=%v: tab.create env lacks %s=%s: %v", open, k, v, env)
			}
		}
		if cli := cliCalls(calls); strings.Contains(cli, "--env") {
			t.Errorf("already_open=%v: the environment went on a command line:\n%s", open, cli)
		}
		if !strings.Contains(calls, "agent start yard-13-new --kind claude --pane p2") {
			t.Errorf("agent not started in the env tab's pane:\n%s", calls)
		}
		if strings.Contains(calls, "export") {
			t.Errorf("prompt still asks for an export:\n%s", calls)
		}
		// A workspace opened for this stage has an idle root tab, closed;
		// an already open one's root tab is someone else's and kept.
		if closed := strings.Contains(calls, "tab close t1\n"); closed == open {
			t.Errorf("already_open=%v: root tab closed=%v:\n%s", open, closed, calls)
		}
	}
}

func gitOutT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = testGitEnv()
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String())
}

// worktreeHerdr is a fake herdr whose worktree create really makes the
// worktree, from --base, as herdr does; every call is logged to log.
func worktreeHerdr(t *testing.T, log string) string {
	return fakeHerdrBin(t, `printf '%s\n' "$*" >> `+log+`
case "$1 $2" in
  "worktree create")
    shift 2
    while [ $# -gt 1 ]; do
      case "$1" in --cwd) cwd=$2 ;; --branch) br=$2 ;; --base) base=$2 ;; --path) p=$2 ;; esac
      shift 2
    done
    git -C "$cwd" worktree add -q -b "$br" "$p" "$base" >&2 || exit 1
    echo '{"result":{"workspace":{"workspace_id":"w1"},"tab":{"tab_id":"t1"},"root_pane":{"pane_id":"p1"},"worktree":{"path":"'"$p"'"}}}' ;;
  "pane process-info") echo '{"result":{"process_info":{"pane_id":"p1","shell_pid":`+fmt.Sprint(os.Getpid())+`}}}' ;;
  *) echo '{"result":{"type":"ok"}}' ;;
esac`)
}

// A child of a train branches from the train's branch, made from the depot's
// base the first time a child needs it and never moved after; YARDR_BASE
// and the brief name that branch. A bead with no train is as before.
func TestHerdrChildBranchesFromItsTrain(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := seedRepo(t)
	a := gitOutT(t, repo, "rev-parse", "main")

	s := storetest.New(t)
	depot := store.Depot{Name: "r", Path: repo, Base: "main"}
	if err := s.PutDepot(ctx, depot, "me"); err != nil {
		t.Fatal(err)
	}
	for _, nb := range []store.NewBead{
		{ID: "r-e", Title: "train", Type: store.TypeTrain, Depot: "r"},
		{ID: "r-1", Title: "child one", Depot: "r"},
		{ID: "r-2", Title: "child two", Depot: "r"},
		{ID: "r-3", Title: "plain", Depot: "r"},
	} {
		if _, err := s.CreateBead(ctx, nb, "me"); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []string{"r-1", "r-2"} {
		if err := s.AddDep(ctx, "r-e", c, store.DepParent, "", "", "me"); err != nil {
			t.Fatal(err)
		}
	}

	log := filepath.Join(t.TempDir(), "calls")
	h := &Herdr{Socket: fakeHerdrAPI(t, log), Bin: worktreeHerdr(t, log), WorktreeRoot: t.TempDir(),
		Store: &paneStore{API: s, t: t, calls: log}}
	// start runs bead id as the dispatcher would, and returns the agent's
	// environment (from the tab.create request) and the brief.
	start := func(id string) (env map[string]string, brief string) {
		t.Helper()
		b, err := s.GetBead(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		fam, err := s.Family(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		os.Remove(log)
		handle, err := h.Start(ctx, dispatch.Job{Bead: *b, Group: store.Group{Name: "builders"}, Depot: &depot,
			Session: store.Session{ID: "s-" + id}, Train: fam.Train, Base: fam.Base})
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := os.ReadFile(log)
		text, _ := os.ReadFile(handle["brief"].(string))
		return envOf(apiCall(t, string(raw), "tab.create")), string(text)
	}
	branched := func() []store.Event {
		t.Helper()
		evs, err := s.Events(ctx, "r-e", 0)
		if err != nil {
			t.Fatal(err)
		}
		var out []store.Event
		for _, e := range evs {
			if e.Kind == "train_branched" {
				out = append(out, e)
			}
		}
		return out
	}

	env, brief := start("r-1")
	if got := gitOutT(t, repo, "rev-parse", "yard/r-e"); got != a {
		t.Fatalf("train branch at %s, want main's tip %s", got, a)
	}
	if got := gitOutT(t, repo, "rev-parse", "yard/r-1"); got != a {
		t.Fatalf("child branched at %s, want %s", got, a)
	}
	if env["YARDR_BASE"] != "yard/r-e" {
		t.Errorf("child's env lacks YARDR_BASE=yard/r-e: %v", env)
	}
	if !strings.Contains(brief, "child of train r-e") || !strings.Contains(brief, "lands on that branch") {
		t.Errorf("brief does not say the bead is a child of r-e:\n%s", brief)
	}
	evs := branched()
	if len(evs) != 1 || evs[0].Data["commit"] != a || evs[0].Data["child"] != "r-1" {
		t.Fatalf("train_branched events = %+v, want one at %s for r-1", evs, a)
	}

	// main moves on and a child lands on the train branch: the next child
	// starts from the train branch as it is, which is neither recreated nor
	// moved to main.
	gitT(t, repo, "commit", "-q", "--allow-empty", "-m", "B")
	wt := filepath.Join(h.WorktreeRoot, "r", "r-1")
	gitT(t, wt, "commit", "-q", "--allow-empty", "-m", "C")
	gitT(t, repo, "branch", "-f", "yard/r-e", "yard/r-1")
	c := gitOutT(t, repo, "rev-parse", "yard/r-1")
	start("r-2")
	if got := gitOutT(t, repo, "rev-parse", "yard/r-e"); got != c {
		t.Fatalf("train branch moved to %s, want it kept at %s", got, c)
	}
	if got := gitOutT(t, repo, "rev-parse", "yard/r-2"); got != c {
		t.Fatalf("second child branched at %s, want the train's tip %s", got, c)
	}
	if n := len(branched()); n != 1 {
		t.Fatalf("%d train_branched events, want the first only", n)
	}

	// A bead with no train branches from main and is told so.
	b := gitOutT(t, repo, "rev-parse", "main")
	env, brief = start("r-3")
	if got := gitOutT(t, repo, "rev-parse", "yard/r-3"); got != b {
		t.Fatalf("plain bead branched at %s, want main %s", got, b)
	}
	if env["YARDR_BASE"] != "main" || strings.Contains(brief, "train") {
		t.Errorf("plain bead: env %v\nbrief:\n%s", env, brief)
	}
}

// With a remote base the train branch is made from the base as just
// fetched, not from a stale remote-tracking ref.
func TestHerdrTrainBranchFromFetchedBase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	origin := seedRepo(t)
	repo := filepath.Join(t.TempDir(), "repo")
	gitT(t, ".", "clone", "-q", origin, repo)
	gitT(t, origin, "commit", "-q", "--allow-empty", "-m", "B")
	b := gitOutT(t, origin, "rev-parse", "main")

	depot := store.Depot{Name: "r", Path: repo, Base: "origin/main"}
	log := filepath.Join(t.TempDir(), "calls")
	h := &Herdr{Socket: fakeHerdrAPI(t, log), Bin: worktreeHerdr(t, log), WorktreeRoot: t.TempDir()}
	_, err := h.Start(ctx, dispatch.Job{Bead: store.Bead{ID: "r-1", Stage: "new"}, Group: store.Group{Name: "g"},
		Depot: &depot, Session: store.Session{ID: "s"}, Train: "r-e", Base: "yard/r-e"})
	if err != nil {
		t.Fatal(err)
	}
	if got := gitOutT(t, repo, "rev-parse", "yard/r-e"); got != b {
		t.Fatalf("train branch at %s, want origin's fetched tip %s", got, b)
	}
	if up, err := gitOut(ctx, repo, "rev-parse", "--abbrev-ref", "yard/r-e@{upstream}"); err == nil {
		t.Fatalf("train branch tracks %s; it is not a copy of the base", up)
	}
}

// dirHerdr is a fake herdr for a dir depot's beads: workspace list and pane
// list answer with what the files ws and panes in state hold, workspace
// create opens w1, and every call is logged to log. The agent's tab is not
// its business: that goes over the socket (fakeHerdrAPI), with the env.
func dirHerdr(t *testing.T, log, state string) string {
	return fakeHerdrBin(t, `printf '%s\n' "$*" >> `+log+`
case "$1 $2" in
  "workspace list") cat `+filepath.Join(state, "ws")+` ;;
  "pane list") cat `+filepath.Join(state, "panes")+` ;;
  "workspace create") echo '{"result":{"workspace":{"workspace_id":"w1"},"tab":{"tab_id":"t1"},"root_pane":{"pane_id":"p1"}}}' ;;
  "pane process-info") echo '{"result":{"process_info":{"pane_id":"p2","shell_pid":`+fmt.Sprint(os.Getpid())+`}}}' ;;
  "worktree "*) echo '{"error":{"code":"not_a_repo","message":"no git here"}}' >&2; exit 1 ;;
  "pane read") echo "❯ " ;;
  *) echo '{"result":{"type":"ok"}}' ;;
esac`)
}

// A dir depot's bead is worked in the depot's directory itself: no worktree
// and no branch, a train's child included (no train_branched); the agent's
// workspace and tab open in the directory, its env names the directory and
// no base, and its brief goes under the directory's .yardr/briefs. A later
// stage reuses the bead's workspace while it is open, and Finish removes the
// brief and closes the workspace, leaving the directory's files alone.
func TestHerdrDirDepot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "invoice.pdf"), []byte("%PDF"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := storetest.New(t)
	depot := store.Depot{Name: "docs", Path: dir, Kind: store.DepotDir}
	if err := s.PutDepot(ctx, depot, "me"); err != nil {
		t.Fatal(err)
	}
	for _, nb := range []store.NewBead{
		{ID: "docs-e", Title: "September", Type: store.TypeTrain, Depot: "docs"},
		{ID: "docs-1", Title: "Sum the invoices", Body: "Write summary.md.", Depot: "docs"},
	} {
		if _, err := s.CreateBead(ctx, nb, "me"); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AddDep(ctx, "docs-e", "docs-1", store.DepParent, "", "", "me"); err != nil {
		t.Fatal(err)
	}
	state, log := t.TempDir(), filepath.Join(t.TempDir(), "calls")
	setState := func(ws, panes string) {
		t.Helper()
		for name, v := range map[string]string{"ws": ws, "panes": panes} {
			if err := os.WriteFile(filepath.Join(state, name), []byte(v), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	root := t.TempDir()
	// The sessions here are made up, not claimed: a store that only notes
	// the pane a start puts on record takes them.
	home := t.TempDir()
	h := &Herdr{Bin: dirHerdr(t, log, state), Socket: fakeHerdrAPI(t, log), WorktreeRoot: root,
		Store: &paneStore{API: s, t: t, calls: log}, YardHome: home, YardBin: "/bin/yardr"}
	start := func(stage string, group store.Group) (map[string]any, string) {
		t.Helper()
		b, err := s.GetBead(ctx, "docs-1")
		if err != nil {
			t.Fatal(err)
		}
		b.Stage = stage
		fam, err := s.Family(ctx, "docs-1")
		if err != nil {
			t.Fatal(err)
		}
		os.Remove(log)
		handle, err := h.Start(ctx, dispatch.Job{Bead: *b, Group: group, Depot: &depot,
			Session: store.Session{ID: "s-" + stage}, Train: fam.Train, Base: fam.Base})
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := os.ReadFile(log)
		return handle, string(raw)
	}
	// First stage: no workspace for the bead yet; one with its label in
	// another directory is someone else's.
	setState(`{"result":{"workspaces":[{"workspace_id":"w9","label":"builders [e] 1"},{"workspace_id":"w8","label":"builders 1","worktree":{"checkout_path":"`+dir+`"}}]}}`,
		`{"result":{"panes":[{"pane_id":"w9:p1","workspace_id":"w9","cwd":"/elsewhere"},{"pane_id":"w8:p1","workspace_id":"w8","cwd":"`+dir+`"}]}}`)
	handle, calls := start("new", store.Group{Name: "docs-builders"})
	if strings.Contains(calls, "worktree ") {
		t.Fatalf("a dir depot's bead made a worktree:\n%s", calls)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Fatalf("worktree root holds %v", entries)
	}
	if !strings.Contains(calls, "workspace create --cwd "+dir+" --label builders [e] 1 --no-focus\n") {
		t.Fatalf("workspace not opened in the directory:\n%s", calls)
	}
	tab := apiCall(t, calls, "tab.create")
	if tab["workspace_id"] != "w1" || tab["cwd"] != dir || tab["label"] != "new [e] 1" {
		t.Fatalf("agent's tab not in the directory: %v", tab)
	}
	env := envOf(tab)
	for k, v := range map[string]string{"YARDR_DEPOT_PATH": dir, "YARDR_DEPOT": "docs", "YARDR_ACTOR": "docs-1-new", "YARDR_HOME": home,
		"GIT_EDITOR": "true"} {
		if env[k] != v {
			t.Errorf("tab env lacks %s=%s: %v", k, v, env)
		}
	}
	for _, k := range []string{"YARDR_BASE", "YARDR_BRANCH", "YARDR_WORKTREE"} {
		if _, ok := env[k]; ok {
			t.Errorf("tab env has %s for a dir depot: %v", k, env)
		}
	}
	// As for a worktree's bead: the environment is on no command line.
	if cli := cliCalls(calls); strings.Contains(cli, "--env") || strings.Contains(cli, "YARDR_ACTOR") {
		t.Errorf("the environment went on a command line:\n%s", cli)
	}
	if !strings.Contains(calls, "tab close t1\n") {
		t.Errorf("new workspace's idle root tab kept:\n%s", calls)
	}
	// As in a worktree: the tab's shell is on the handle, so the store
	// knows the session's calls by the process tree (store.Expect).
	if handle["shell_pid"] != os.Getpid() || !strings.Contains(calls, "pane process-info --pane p2\n") {
		t.Fatalf("shell_pid = %v, want this process %d:\n%s", handle["shell_pid"], os.Getpid(), calls)
	}
	brief, _ := handle["brief"].(string)
	if want := filepath.Join(dir, ".yardr", "briefs", "brief-s-new.md"); brief != want {
		t.Fatalf("brief at %q, want %q", brief, want)
	}
	text, err := os.ReadFile(brief)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Work in `" + dir + "` directly", "Commit nothing", "what you produced, by path",
		"outside what the bead names", "child of train docs-e, which only groups it", "Write summary.md."} {
		if !strings.Contains(string(text), want) {
			t.Errorf("brief lacks %q:\n%s", want, text)
		}
	}
	for _, not := range []string{"branch `", "lands on that branch", "push"} {
		if strings.Contains(string(text), not) {
			t.Errorf("brief for a dir depot says %q:\n%s", not, text)
		}
	}
	if !strings.Contains(calls, "agent prompt docs-1-new Read "+brief+" and follow it.") {
		t.Errorf("agent not pointed at the brief:\n%s", calls)
	}
	if _, ok := handle["worktree"]; ok || handle["dir"] != dir || handle["workspace"] != "w1" {
		t.Fatalf("handle = %v", handle)
	}
	for _, id := range []string{"docs-e", "docs-1"} {
		evs, _ := s.Events(ctx, id, 0)
		for _, e := range evs {
			if e.Kind == "train_branched" {
				t.Fatalf("train_branched logged for a dir depot: %+v", e)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); !os.IsNotExist(err) {
		t.Fatal("a dir depot was made a repository")
	}

	// A later stage: the bead's workspace is open in the directory (a pane
	// of it there), so it is reused and relabelled, its root tab kept.
	setState(`{"result":{"workspaces":[{"workspace_id":"w9","label":"builders [e] 1"},{"workspace_id":"w1","label":"builders [e] 1"}]}}`,
		`{"result":{"panes":[{"pane_id":"w9:p1","workspace_id":"w9","cwd":"/elsewhere"},{"pane_id":"w1:p2","workspace_id":"w1","cwd":"`+dir+`"}]}}`)
	_, calls = start("review", store.Group{Name: "docs-review", Config: map[string]any{"label": "review"}})
	if strings.Contains(calls, "workspace create") || !strings.Contains(calls, "workspace rename w1 review [e] 1\n") {
		t.Fatalf("later stage did not reuse the bead's workspace:\n%s", calls)
	}
	if tab := apiCall(t, calls, "tab.create"); tab["workspace_id"] != "w1" || tab["cwd"] != dir || strings.Contains(calls, "tab close") {
		t.Fatalf("later stage's tab:\n%s", calls)
	}

	// Finish after close: the brief goes, the workspace closes, the
	// directory's own files stay; a second session finds nothing left.
	os.Remove(log)
	if err := h.Finish(ctx, handle); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(brief); !os.IsNotExist(err) {
		t.Fatal("brief not removed")
	}
	if _, err := os.Stat(filepath.Join(dir, "invoice.pdf")); err != nil {
		t.Fatal("Finish touched the directory's files")
	}
	raw, _ := os.ReadFile(log)
	if !strings.Contains(string(raw), "workspace close w1\n") || strings.Contains(string(raw), "close w9") {
		t.Fatalf("workspace close calls wrong:\n%s", raw)
	}
	if err := h.Finish(ctx, handle); !errors.Is(err, dispatch.ErrNothingLeft) {
		t.Fatalf("second finish: %v", err)
	}
}
