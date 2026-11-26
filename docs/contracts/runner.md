# Runner

A runner is how a session starts. What crosses the core's edge here is the `Runner` interface of `internal/dispatch`, the optional interfaces a runner may add to it, and the session handle: a `map[string]any` that a runner's `Start` returns and the store keeps on the session. The dispatch pass and the store sit on one side of it, and `internal/runner` on the other, with four runners: `manual` (a person or an outside agent works the bead), `exec` (a command), `herdr` (an agent program in a pane that herdr runs) and `peer` (another yard). The herdr runner's reading of a pane is how-and-what, and herdr, which it reaches by its command line and its socket, is an outside program that is no part of the core. The design is section 3.2.3 of [the design](../design/2026-11-24-yardr-h5wn-core-contracts.md).

## Form

**The interface.** `dispatch.Runner` (`internal/dispatch/dispatch.go:94`) has three methods. `Start(ctx, job)` starts the work for a `dispatch.Job` (`internal/dispatch/dispatch.go:26`) and returns the handle. `Alive(ctx, handle)` says whether the work still runs, and an error from it means unknown and never ends a session. `Stop(ctx, handle)` tears down what the runner made once the session has ended. A `Start` error may say whose fault it is: `dispatch.Runtime` marks the runtime's (herdr missing or down), `dispatch.Blocked` the session's own configuration's (an `env_file` that is missing or open to others), and `dispatch.BeadBlocked` the bead's own, for which a person must act (`internal/dispatch/runerrors.go:20`, `:44` and `:67`).

**The registry.** `runner.All` (`internal/runner/runner.go:41`) returns the runners by the name a group refers to them by: `manual`, `exec`, `herdr` and `store.PeerRunner`, which is `peer`. The daemon builds its runners with it (`internal/cli/ops.go:98`), and `yardr down` asks each about its agent with it (`internal/cli/updown.go:139`).

**The optional interfaces.** A runner implements any of the interfaces below that it can answer, and the dispatcher asks each question only of a runner that does; one that implements none is a complete runner. The interfaces are the exported interface types of the non-test files of `internal/dispatch`, 29 with `Runner`. The herdr runner implements all of them but `Waiter`, which the peer runner implements; `manual` and `exec` implement `Runner` alone.

| interface | declared in `internal/dispatch/` | the question it answers | implemented by |
|---|---|---|---|
| `Runner` | `dispatch.go:94` | How does the work start, does it still run, and how is it stopped? | manual, exec, herdr, peer |
| `CrewRunner` | `crew.go:18` | Start an always-on session that is tied to no bead? | herdr |
| `CrewFinder` | `crew.go:32` | Which agent of the runtime is already a crew member's, though the yard did not start it? | herdr |
| `CrewCloser` | `crew.go:46` | What did an ended crew session leave open in the runtime, so that the next start can close it? | herdr |
| `Nudger` | `crew.go:58` | Deliver a short message to a running session, and wait for the agent to start on it? | herdr |
| `DirectNudger` | `crew.go:67` | Submit a short message to a running session without waiting for the agent to start on it? | herdr |
| `Notifier` | `crewblocked.go:13` | Show a notification to a person at this machine? | herdr |
| `ContextReader` | `crewcontext.go:16` | How much context does the session's agent hold, in tokens? | herdr |
| `Statuser` | `dispatch.go:562` | Is the session's agent working, idle or blocked, which is more than alive or dead? | herdr |
| `Waiter` | `dispatch.go:574` | Is the session a wait for work done elsewhere, with what limit, and how is that work read? | peer |
| `Finisher` | `dispatch.go:583` | What did the session leave behind (a worktree), and remove it once the bead closes? | herdr |
| `Waker` | `dispatch.go:594` | Can the runtime push an event that wakes a pass? | herdr |
| `Forgetter` | `forget.go:59` | Make the agent's harness forget a session that ended? | herdr |
| `FirstPrompter` | `harness.go:125` | What was the session's agent first told? | herdr |
| `Versioner` | `harness_dialog.go:36` | Which version of the kind's harness is installed? | herdr |
| `OnboardingMarker` | `harnesslogin.go:955` | Mark the harness's own onboarding done where its sessions find it? | herdr |
| `Modeler` | `model.go:36` | Which model does the session's agent report it runs? | herdr |
| `SessionIDer` | `model.go:51` | Which session id does the agent keep in its own session file? | herdr |
| `PromptWatcher` | `prompt.go:45` | Did a prompt reach the session's agent? | herdr |
| `PromptSubmitter` | `prompt.go:53` | Is a prompt typed in the agent's input and not yet submitted, and if so, submit it? | herdr |
| `ScreenWatcher` | `prompt.go:65` | Has the harness drawn its own screen yet? | herdr |
| `TrustAcceptor` | `prompt.go:80` | Answer the folder-trust, API-key and onboarding dialogs that a harness draws at a start? | herdr |
| `InputReader` | `prompt.go:124` | What is typed in the agent's input and not sent? | herdr |
| `DialogReader` | `prompt.go:133` | Is the agent at a question the harness draws in place of its input, and which? | herdr |
| `LoginReader` | `prompt.go:141` | Is the agent at the harness's login screen, and at which line? | herdr |
| `InputMarker` | `prompt.go:318` | Does the runner know the mark this agent's harness begins its input with? | herdr |
| `Tailer` | `stall.go:49` | What did the agent last print? | herdr |
| `Worker` | `stall.go:56` | Has the session changed anything where it works since it started? | herdr |
| `UsageReader` | `usage.go:48` | What did a stopped session's agent use, in tokens? | herdr |

**The session handle.** `Session.Handle` (`internal/store/routing.go:1730`) and `CrewSession.Handle` (`internal/store/crew.go:158`) hold the map. What is in it is the runner's, except for the few keys that the store and the dispatcher read or write besides (see Stored data).

**The herdr program.** Herdr is reached by its command line and its socket (`internal/runner/herdr_api.go`, `internal/runner/herdr_events.go`). What yardr asks of it is herdr 0.9's, and the oldest herdr yardr starts agents with is `herdrOldest` (`internal/cli/needs.go:459`), checked by `yardr doctor` and `yardr up` (`herdrTooOld`, `internal/cli/needs.go:475`), not by the runner.

## Owner

The core owns the `Runner` interface, the optional interfaces and the handle. Herdr's command line and socket belong to herdr.

## Version

None, since the interfaces are internal: a change updates every implementation in the same commit. The herdr program is the one outside part, and its floor is `herdrOldest`; a herdr older than that is refused with the version it needs (see Rules).

## Change rule

Change every implementation in the same commit. A new optional interface goes in `internal/dispatch` with a line in the table under Form, and a new handle key is listed under Stored data with the runner that writes it. Neither of these is held by a check yet.

## Stored data

Sessions and their handles. Live sessions survive a restart of the server and an upgrade, because the daemon replaces itself and takes up the sessions in the store, so a handle key that the old build wrote must still be read by the new one, or the upgrade must end the sessions it cannot read.

Each runner builds its handle in `Start`:

- `manual` (`internal/runner/notify.go:45`) writes `manual`, and `notify` and `session` when it shows a notification.
- `exec` (`internal/runner/exec.go:148`) writes `pid`, `log` and `start`, the start time that tells the process from a later one with its pid.
- `peer` (`internal/runner/peer.go:45`) writes `peer`, `bead` and, once the message is sent, `message`.
- `herdr` writes for a bead's session `agent`, `branch`, `repo`, `workspace`, `worktree`, `tab`, `pane`, `brief`, and `pull` and `pull_head` or `head` for a pull request (`internal/runner/herdr.go:212` to `:356`), and the pane's shell as `shell_pid`, `shell_start` and `herdr_session` (`internal/runner/herdr.go:650`). For a bead of a dir depot it writes `agent`, `dir`, `depot`, `bead`, `workspace`, `tab` and `pane` (`internal/runner/herdr_dir.go:36`), and for a crew member `agent`, `workspace`, `tab`, `pane`, `dir` and `for` (`internal/runner/herdr_crew.go:258`).

The store names the keys it reads or writes itself where it declares the record: `pending_prompt` (`internal/store/routing.go:1782`), `snooze_until` and `snooze_count` (`internal/store/routing.go:1790`), `count_due` (`internal/store/routing.go:1801`), `agent_session` and `kind` (`internal/store/crew.go:228`), `harness_login` and `harness_login_ask` (`internal/store/crew.go:241`) and `herdr_session` (`internal/store/settings.go:943`). The dispatcher names the keys it writes in `internal/dispatch/dispatch.go`: `prompt_unconfirmed`, `prompt_started` and `prompt_look` (`:525` to `:537`), and the keys of the harness passes that [harness.md](harness.md) lists.

## Rules

| # | rule | check |
|---|---|---|
| 1 | The handle that `Start` returns is stored on the session and handed back to `Alive` and `Stop`. | `TestBeadThroughStages`, `TestDeadSessionIsRetriedThenHeld` |
| 2 | One session is started for a bead in a stage, however many passes run; the session of a bead that moves to another group is stopped, and a bead that closes starts no more. | `TestBeadThroughStages` |
| 3 | A session whose runner says it is no longer alive ends and its bead is started again, and after `MaxAttempts` sessions that failed or died the bead is held for a person with a `gave_up` event. | `TestDeadSessionIsRetriedThenHeld` |
| 4 | A `Start` error marked `dispatch.Runtime` is the runtime's and not the bead's: no bead of the group counts a failure or is held, the group reports `runtime_down` once, and `runtime_up` when its beads start again. | `TestRuntimeDownHoldsNoBead` |
| 5 | A `Start` error marked `dispatch.BeadBlocked` holds the bead for a person with a `session_blocked` event that gives the reason, counts no failure and does not mark the group's runtime down. | `TestBeadBlockedStartHoldsBeadWithReason` |
| 6 | A runner that implements `Runner` alone is complete: the pass starts, supervises and stops its sessions, and every optional interface reads as none. | `TestBeadThroughStages` |
| 7 | A herdr older than `herdrOldest` is a fault in `yardr doctor` and stops `yardr up`, each naming the oldest herdr yardr starts agents with; one whose version cannot be read is not called old. | `TestHerdrTooOld`, `TestDoctorHerdrServer`, `TestUpWithHerdrTooOld` |
| 8 | An error from `Alive` means unknown and never ends a session. | unchecked |
| 9 | A new optional interface declared in `internal/dispatch` has a line in the table under Form, with the one question it answers. | unchecked |
| 10 | A new handle key is listed under Stored data with the runner that writes it. | unchecked |
| 11 | A handle key that the old build wrote is read by the new one, or the upgrade ends the sessions that hold it. | unchecked |

## Planned

- **planned** (design 3.2.3): the herdr version named and checked at the runner's start. The herdr runner names the herdr version it reads in one place and checks it when it starts, and refuses a herdr it does not read with the version it needs. Today the one check is in `internal/cli/needs.go`, in `yardr doctor` and `yardr up`, and not in the runner.
- **planned** (design 3.2.3): every handle key listed on this page with the runner that writes it. The lists under Stored data name what each runner builds in `Start` and where the store and the dispatcher declare theirs; a key that a runner adds to a running session's handle later is not yet listed.
