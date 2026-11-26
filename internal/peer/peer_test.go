package peer

import (
	"bufio"
	"errors"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The peer of a unix connection is the connecting process, and its
// ancestors begin with it and its parent, unless that is pid 1.
func TestPIDAndAncestors(t *testing.T) {
	dir, err := os.MkdirTemp("", "yp") // t.TempDir is too long for a socket path
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	ln, err := net.Listen("unix", filepath.Join(dir, "s"))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			t.Error(err)
			close(accepted)
			return
		}
		accepted <- c
	}()
	client, err := net.Dial("unix", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server := <-accepted
	if server == nil {
		t.FailNow()
	}
	defer server.Close()
	pid, err := PID(server)
	if err != nil {
		t.Fatalf("PID: %v", err)
	}
	if pid != os.Getpid() {
		t.Fatalf("peer pid %d, want this process %d", pid, os.Getpid())
	}
	// Ancestors leaves out pid 1 and anything below it (Parent answers 0 for
	// them), so a test binary that is a direct child of pid 1, as when a
	// container's entrypoint runs it, has itself alone; any other has its
	// parent next. (yardr-t6ah: slicing two off the pid alone panicked.)
	anc := Ancestors(os.Getpid())
	if ppid := os.Getppid(); ppid <= 1 {
		if !slices.Equal(anc, []int{os.Getpid()}) {
			t.Fatalf("ancestors %v, want %d alone: its parent is pid %d, which Ancestors leaves out", anc, os.Getpid(), ppid)
		}
	} else if len(anc) < 2 || !slices.Equal(anc[:2], []int{os.Getpid(), ppid}) {
		t.Fatalf("ancestors %v, want to begin with %d, %d", anc, os.Getpid(), ppid)
	}
	// A pid no process has is nobody's child.
	if p, err := Parent(1<<22 - 7); err != nil || p != 0 {
		t.Fatalf("parent of a free pid = %d, %v", p, err)
	}
	if _, err := PID(fakeConn{}); err == nil {
		t.Fatal("a non-unix connection has a peer")
	}
}

// Alive tells a process from a later one with its pid by the start time,
// and knows a reaped one is gone.
func TestAlive(t *testing.T) {
	if !HaveStat() {
		t.Skip("this OS reports no process state")
	}
	me := os.Getpid()
	st, err := Describe(me)
	if err != nil {
		t.Fatal(err)
	}
	if !Alive(me, st.Start) {
		t.Fatal("this process is not alive")
	}
	if Alive(me, st.Start+1) {
		t.Fatal("another start time is the same process")
	}
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if Alive(cmd.Process.Pid, 0) {
		t.Fatal("a reaped process is alive")
	}
	if _, err := Describe(cmd.Process.Pid); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a reaped process: %v, want %v", err, fs.ErrNotExist)
	}
}

// A kill refused with EPERM is another user's process: not ours, not
// alive. (yardr-4wfs: it counted as alive.)
func TestAnotherUsersProcessIsNotOurs(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can signal any process")
	}
	if err := unixKill(1, 0); !errors.Is(err, syscall.EPERM) {
		t.Skipf("pid 1 answers %v, not EPERM; no foreign process to try", err)
	}
	if kill0(1) {
		t.Fatal("kill0(1) is true for another user's process")
	}
}

type fakeConn struct{ net.Conn }

// startOrphan starts a shell that leads a session of its own, as a command
// a harness runs does, has it start a command in the background from a
// subshell that ends at once (`( cmd & )`), and answers the command's pid
// and the leader's. The command's parent is then init's, or the nearest
// subreaper's, and it stays in the leader's session while the leader runs.
// endLeader ends the leader and waits for it.
func startOrphan(t *testing.T) (orphan, leader int, endLeader func()) {
	t.Helper()
	cmd := exec.Command("sh", "-c", "( sleep 300 >/dev/null 2>&1 </dev/null & echo $! ); echo ready; read _")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if _, err := cmd.StdinPipe(); err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	ended := false
	endLeader = func() {
		if !ended {
			ended = true
			cmd.Process.Kill()
			cmd.Wait()
		}
	}
	t.Cleanup(func() {
		endLeader()
		if orphan > 0 {
			syscall.Kill(orphan, syscall.SIGKILL)
		}
	})
	// The shell prints "ready" once the subshell has ended, so the command
	// has been given to init by the time it is read.
	r := bufio.NewReader(out)
	line, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("the orphan's pid: %v", err)
	}
	if orphan, err = strconv.Atoi(strings.TrimSpace(line)); err != nil {
		t.Fatalf("the orphan's pid %q: %v", line, err)
	}
	if ready, err := r.ReadString('\n'); err != nil || strings.TrimSpace(ready) != "ready" {
		t.Fatalf("the shell says %q, %v, want ready", ready, err)
	}
	return orphan, cmd.Process.Pid, endLeader
}

// A command left running in the background is no child of the process that
// started it, but it stays in that process's session: Lineage follows it to
// the session's leader while the leader runs, and to nobody once it is gone.
func TestLineageFollowsAnOrphanToItsSessionsLeader(t *testing.T) {
	if !HaveStat() {
		t.Skip("this OS does not tell a process's parent")
	}
	me := os.Getpid()
	if me <= 1 {
		t.Skip("this process is init: its children are never orphans of it")
	}
	orphan, leader, endLeader := startOrphan(t)
	if anc := Ancestors(orphan); slices.Contains(anc, me) || slices.Contains(anc, leader) {
		t.Fatalf("ancestors of the orphan %d are %v: it was not orphaned", orphan, anc)
	}
	// The process, then what is above it, then the leader and what is above
	// the leader: the nearest first.
	got := Lineage(orphan)
	if !slices.Equal(got[:len(Ancestors(orphan))], Ancestors(orphan)) {
		t.Fatalf("lineage %v does not begin with the ancestors %v", got, Ancestors(orphan))
	}
	i, j := slices.Index(got, leader), slices.Index(got, me)
	if i < 0 || j < i {
		t.Fatalf("lineage of the orphan %d is %v, want its session's leader %d, then this process %d above it", orphan, got, leader, me)
	}
	// No process has pid 0 or init's pid for a pane: getsid(0) is the
	// caller's own session, which must not be taken for the pid's.
	if got := Lineage(0); !slices.Equal(got, []int{0}) {
		t.Fatalf("lineage of pid 0 is %v, want it alone", got)
	}
	// Linux keeps an exited session leader's process record, including its
	// parent, until it is reaped. That dead shell must not keep the pane.
	if err := syscall.Kill(leader, syscall.SIGKILL); err != nil {
		t.Fatalf("kill the session leader: %v", err)
	}
	for fuse := time.Now().Add(time.Second); ; runtime.Gosched() {
		st, err := Describe(leader)
		if err != nil {
			t.Fatalf("describe the unreaped session leader: %v", err)
		}
		if st.State == 'Z' {
			break
		}
		if time.Now().After(fuse) {
			t.Fatalf("session leader %d did not become a zombie: %+v", leader, st)
		}
	}
	if got := Lineage(orphan); slices.Contains(got, me) || slices.Contains(got, leader) {
		t.Fatalf("lineage of the orphan %d is %v while its session leader %d is a zombie, want no process above it", orphan, got, leader)
	}
	// Without its leader nothing says where the orphan came from.
	endLeader()
	if got := Lineage(orphan); slices.Contains(got, me) || slices.Contains(got, leader) {
		t.Fatalf("lineage of the orphan %d is %v after its leader %d ended, want no process above it", orphan, got, leader)
	}
}
