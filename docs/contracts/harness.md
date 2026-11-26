# Harness

A harness is the agent program that a herdr session runs (Claude Code, Codex, Kimi and the like), and its kind is the name herdr starts it by. What crosses the core's edge here is one file per kind, `harnesses/<kind>.yaml`: the marks, patterns, answers and argument lists by which the core reads a session's program. The core owns the format of these files and the questions it asks of them, in `internal/store` (the `Harness` type and its checks), `internal/agents` (the loader) and the passes of `internal/dispatch` that read the end of a pane. The files are how-and-what, since each holds what changes when one harness program changes, and so is the per-kind Go that still stands beside them (see Planned). The design is section 3.2.1 of [the design](../design/2026-11-24-yardr-h5wn-core-contracts.md).

## Form

A harness file is YAML. `harnesses/harnesses.go:FS` embeds every `*.yaml` of its directory in the binary as `harnesses.FS`, and `agents.Load(harnesses.FS)` (`internal/agents/harness.go:Harnesses`) is the one place the files are read, once, when the program starts. `agents.Load` (`internal/agents/harness.go:Load`) takes the kind from the file's name (`claude.yaml` is the kind `claude`), decodes the file into `store.Harness` (`internal/store/harness.go:Harness`) with every key that type does not name refused (`KnownFields(true)`, `internal/agents/harness.go:Load`), and holds the result to `Harness.Check` (`internal/store/harness.go:Harness.Check`). A kind with no file is the zero `Harness`, which knows nothing. [groups.md](../groups.md#what-yardr-knows-of-a-kind) says what each key means and which code reads it, and this page does not repeat it.

The questions the core asks of a session's program, and the keys that answer them today:

| the core asks | the file answers with | read by |
|---|---|---|
| Is the agent at its prompt? | `marks`, `background` | the herdr runner's pane reading (`ScreenWatcher`, `InputReader`, `PromptWatcher`), dispatch's idle pass |
| Did it stop on an error the yard knows, and which? | `errors` (a `reason` of `store.Reasons`, `internal/store/harness.go:Reasons`) and `marks.error` | `dispatch.HarnessError` (`internal/dispatch/harness.go:HarnessError`) |
| Which dialog is on the screen, and may the yard answer it? | `trust`, `api_key` and `onboarding` are answered; `dialogs` and `login` are told of | the herdr runner (`TrustAcceptor`, `DialogReader`, `LoginReader`), `dispatch.HarnessDialog` (`internal/dispatch/harness_dialog.go:HarnessDialog`) |
| Which model is it using, how much context, how many tokens did it use? | `model`, `context`, `usage` | the daemon, through the herdr runner (`Modeler`, `ContextReader`, `UsageReader`) |
| What arguments start it, unattended, with a model and an effort? | `flags` | the herdr runner at start, and the checks and forms that hold a group's kind, model, effort and permission preset (`internal/store`, `internal/cli`) |
| How does the yard make it forget a session it started? | `forget` | the daemon, through the herdr runner (`Forgetter`) |
| Which version is installed, and is the program the harness? | `flags.version`, `flags.version_contains` | `agents.HarnessVersion` (`internal/agents/harness.go:HarnessVersion`), `yardr doctor` |

The other keys (`label`, `provider`, `aliases`, `install`, `fetch`, `offers`, `login_recipe`, `login_env`, `hints`) say what a person is shown, offered or led through. The herdr runner sends Esc to two of the `dialogs`, the ones its code names `auto_mode_setup` and `rate_limit_switch` (`internal/runner/herdr_screen.go:CancelsDialog`), and sends no key for any other: `InputDialog` (`internal/runner/herdr_screen.go:Herdr.InputDialog`) gives its word and first line, and the daemon holds back what it would type there and says why (`dialogAt`, `internal/dispatch/prompt.go:Dispatcher.dialogAt`).

## Owner

The core owns the format and the questions, and so `store.Harness` and `Harness.Check`. The yardr repository owns each file of `harnesses/` today, and a file may later be kept in a repository of its own.

## Version

A file's `version` is the format's one number, `store.FileVersion` (`internal/store/harness.go:FileVersion`), 1 today. `Harness.Check` refuses a file at another number, or at none, with the number this build reads, as `version 2: this yardr reads version 1`. An optional key added to the format keeps the number. A key removed or given another meaning raises it, and every file the build carries moves in the same commit.

## Change rule

A change to the format is made in `store.Harness` and `Harness.Check` and in every file of `harnesses/` it touches, in one commit. A new kind needs, today, its file `harnesses/<kind>.yaml`; the kind in `store.HerdrKinds` (`internal/store/harness_kinds.go:HerdrKinds`), the list of kinds herdr starts, which a file's kind must be among; a model lister for it in `modelLists` (`internal/agents/harness.go:modelLists`); and its entries in the tables of `internal/store/harness_kinds.go` that copy what its file says. `TestHarnessFiles` and `TestStoreTablesMatchCatalogue` fail until they agree. A kind is also a name the core may not name, so it joins the harness kinds row of the list in [core.md](core.md#names-the-core-may-not-name) in the same commit, and `TestHarnessFilesNameOnlyListedKinds` fails until it does; each file that already names the kind then gets a row in that page's allow-list, at the hits it has that day. A new kind as a file with fixtures and no Go is planned.

## Stored data

A group's config keys that a file bounds (`kind`, `model`, `effort`, `args`), the kind and model a session was started with (`Session.Kind` and `Session.Model`, `internal/store/routing.go:Session`), and the handle keys below, which the daemon keeps on a session's or a crew member's handle so that a daemon that replaces itself neither tells again nor forgets a wait. No harness file is stored: a file is read from the build at start.

| key | declared at | what it holds |
|---|---|---|
| `harness_error` | `internal/dispatch/harness.go:HarnessErrorKey` | the reason of the `session_harness_error` raised in the current idle spell |
| `harness_dialog` | `internal/dispatch/harness_dialog.go:HarnessDialogKey` | the kind of the `session_harness_dialog` raised in the current idle spell |
| `harness_login`, `harness_login_ask` | `internal/store/crew.go:HarnessLoginKey`, `internal/store/crew.go:HarnessLoginAskKey` | the line of the harness's login screen the agent sits at, and the ask of a login the yard leads for it |
| `harness_retries`, `harness_retry_at` | `internal/dispatch/harness.go:harnessRetries`, `internal/dispatch/harness.go:harnessRetryAt` | how often the yard prompted the agent again, and when its pane is next looked at |
| `harness_reset_at`, `harness_reset_window`, `harness_reset_model`, `harness_prompted` | `internal/dispatch/harness.go:harnessResetAt`, `internal/dispatch/harness.go:harnessResetWindow`, `internal/dispatch/harness.go:harnessResetModel`, `internal/dispatch/harness.go:harnessPrompted` | the wait for a usage limit's window to reset: when it is asked about again, the window, the model that waits and what was typed at the reset |
| `harness_fallback_model`, `harness_fallback_return_at` | `internal/dispatch/harness.go:harnessFallbackModel`, `internal/dispatch/harness.go:harnessFallbackReturnAt` | the fallback model a crew member runs on while its own model's window is used up, and when it goes back |
| `snooze_until`, `snooze_count` | `internal/store/routing.go:SnoozeUntil`, `internal/store/routing.go:SnoozeCount` | when a snooze the agent asked for ends, and how many it took in a row |
| `trust_accepted`, `key_accepted`, `onboarding_accepted_<n>` | `internal/dispatch/dispatch.go:TrustAccepted`, `internal/dispatch/dispatch.go:KeyAccepted`, `internal/dispatch/dispatch.go:OnboardingAccepted` | that the yard answered the folder-trust dialog, the API-key dialog and the nth onboarding screen |

## Rules

| # | rule | check |
|---|---|---|
| 1 | A harness file's `version` is `store.FileVersion`: a file at another number, or at none, is refused with the number this build reads, and every file the build carries is at it. | `TestLoadRefuses`, `TestHarnessFiles` |
| 2 | A key the format does not name is refused, at the root of a file and inside the keys that hold others. | `TestLoadRefuses` |
| 3 | A file is named for a kind herdr starts (`store.HerdrKinds`), and a file's alias is not another file's kind. | `TestLoadRefuses` |
| 4 | A file that says what the core cannot go by (an error line whose reason is none of `store.Reasons`, a pattern that does not compile, a trust dialog with no cursor, a model file glob that names no session) is refused whole, and the error names the file and the reason. | `TestLoadRefuses` |
| 5 | A kind with a file has a model lister in `agents.modelLists`, and a kind with a lister has a file. | `TestHarnessFiles` |
| 6 | The store's tables that copy the files (labels, login variables, permission presets, offered models, efforts, model flags and model variables) say what the files say. | `TestStoreTablesMatchCatalogue` |
| 7 | A permission preset is marked `Strong` in `store.HarnessPresets` exactly when its flags skip the agent's permission prompts. | `TestHarnessPresetsAreStrongWhenTheirFlagsAskNothing` |
| 8 | A permission preset is found by its label in either spelling, and only among the presets the kind's file names. | `TestPreset` |
| 9 | A kind with no file is the zero harness: no marks, error lines, dialogs, login screen, model source, flags or answers. | `TestKindWithNoFile` |
| 10 | A group config that names no kind is of the kind `agents.DefaultKind`. | `TestKindConfigDefault` |
| 11 | The installed version of a harness is the first non-blank line its file's `flags.version` makes it print; a kind with no file, a program that is not installed, one that fails and one that does not answer in time have none. | `TestHarnessVersion` |
| 12 | The end of an idle agent's pane is read for the error lines of its kind's file, in the file's order and from the last lines only: the first line that matches gives the reason, an error above a message the agent took since is history, and a kind whose file lists no errors is never reported. | `TestHarnessErrorLines`, `TestHarnessErrorOnlyForAKnownLineOfTheKind` |
| 13 | An error is told once in an idle spell as `session_harness_error`, with its reason, line and the end of the pane; its mark on the handle outlives a restart of the daemon and is taken off when the agent is seen working. | `TestHarnessErrorIsReportedAtTheFirstIdlePass` |
| 14 | The yard answers a dialog only when its file names it: the text that marks the dialog and an answer that accepts it must both show, and any other question, a line that only looks like one, a group that says `trust=false` and a kind whose file names no dialog get no key. | `TestStartAgentLeavesOtherQuestions`, `TestHarnessDialogOnlyForAnUnknownDialogAtAStart` |
| 15 | A trust dialog that shows with none of the accepting answers its file names is told once in an idle spell as `session_harness_dialog` and is not typed into. | `TestHarnessDialog`, `TestHarnessDialogIsReportedAtTheFirstIdlePass`, `TestStartAgentLeavesRenamedTrustAnswer` |
| 16 | The tables of `docs/groups.md` that are made from the files (each kind's error lines and pane marks) say what the files say. | `TestGroupsDocTables` |
| 17 | The fields of a harness file that become arguments of the program, answer a dialog on its screen or name a command the yard runs come only from reviewed bytes: the files built into the binary. | unchecked |
| 18 | Every handle key that dispatch or the store keeps for a harness is listed under Stored data, and a new one is listed there with the code that writes it. | unchecked |

## Planned

- **planned** (design 3.2.1): the harness interface as data. The core's few questions, as the table under Form puts them, become a defined interface that a file answers by matchers, patterns, answers and argument lists, with the core reading the answers and never a harness name. Today the questions are answered by the keys above and interpreted by Go in `internal/store`, `internal/agents`, `internal/runner`, `internal/dispatch` and `internal/cli`.
- **planned** (design 3.2.1): matchers and fixtures in place of per-kind Go. What is code for one kind today (the model listers in `internal/agents/models.go`, `agents.MemoryOff` and `agents.MemoryDir`, the trust seeding of `internal/runner/trust.go`, `internal/cli/codextrust.go` and `internal/cli/kimihooks.go`, the Esc for two named dialogs) becomes a matcher in the file or a command the file names, run as a source command is run, and each kind is tested by fixture files beside its file, a recorded pane and the answer expected, and not by Go per kind. The greps of design section 2.3 find the per-kind Go. A new kind is then a file plus fixtures, with no Go, and a new question lands with its key in the format, its answer in every file the repository carries and a fixture per kind.
- **planned** (design 3.2.1, decision 6): a yard's own harness file for matchers and models, never for what starts a program or answers its dialogs. A file in the yard's home beside the built-in ones may name a kind the build carries or a new one. A key that becomes an argument of the program, answers a dialog on its screen or names a command the yard runs is refused in it, with the key's name and the reason, and is taken only from the build's file of that kind. This page then lists the guarded keys, and a test loads a yard file that carries each one and expects the refusal. A yard's file is read at the format's one version, as the built-in ones are.
