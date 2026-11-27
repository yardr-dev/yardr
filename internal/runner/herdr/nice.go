package herdr

import (
	"fmt"
	"syscall"

	"yardr.dev/yardr/internal/runner"
)

// niceShell sets the niceness of the shell in a pane to want, unless it has
// that or more already, so that the agent started in the pane and all the
// agent starts have it (handle names the shell, as shellOn put it there).
// herdr starts the pane and takes no priority for it, so the shell is
// reniced once it exists; what it ran before then, its own startup, kept the
// niceness it had. A renice that lands in the few milliseconds after a
// process starts is undone by the exec it is still finishing (seen on
// macOS), but yardr is past that: it has spawned herdr, written the store
// and run git since herdr started the shell. A group with no niceness asks
// nothing.
func niceShell(handle map[string]any, want int) error {
	if want <= 0 {
		return nil
	}
	pid, _ := handle["shell_pid"].(int)
	if pid <= 0 {
		return fmt.Errorf("herdr did not report the pane shell, so its niceness cannot be set to %d", want)
	}
	own, err := runner.NiceOf(pid)
	if err != nil {
		return fmt.Errorf("niceness of the pane shell %d: %w", pid, err)
	}
	if want <= own {
		return nil
	}
	if err := syscall.Setpriority(syscall.PRIO_PROCESS, pid, want); err != nil {
		return fmt.Errorf("nice %d for the pane shell %d: %w", want, pid, err)
	}
	return nil
}
