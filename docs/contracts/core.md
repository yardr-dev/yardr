# The core

This page is the norm for what the core is and what may import what. It
states the rule that puts a file inside or outside the core, the core's
types, the split of `store.API`, the layers of `internal/`, the import
rules, the line ceilings of the packages that hold core code and the names
the core may not name. The Rules table names the check of each rule.
`TestImportRules` and `TestLayersMatchCode` in `arch_test.go` (package main,
at the repository root) hold the code to the import rules and the layers,
`TestCorePackagesKeepUnderTheirCeilings` in `ceilings_test.go` holds each
package to its ceiling, and `TestCoreNamesNothingOnTheList` in
`corenames_test.go` holds the code to the list of names. The page and its
checks change together, so a new import that breaks the rules, a package
that outgrows its ceiling or a new hit of a listed name fails the gate. The
contracts at the core's edge each have a page beside this one, listed under
The contracts, with a Rules table of the same form.

## What is core

The core is the part of yardr that the contracts are written around. The
how-and-what is everything else. Two questions put a file on one side. They
are written for Go, and the end of this section says how they place every
other kind of file.

**Q1. Does it keep, deliver or move a record of the yard?** A record is a
bead, note, ask, answer, event, mail, session, flow, route, group, depot,
source record, peer record, user, device, timer or setting. Keeping means
storing it, migrating it and deciding who may change it. Delivering means a
subscriber, an inbox or a push learning of it. Moving means the flow engine
taking a bead from stage to stage, or the dispatch pass starting, watching
and ending a session through the runner interface. A subscription, a
preview and a delegation are not in the list of records, because each has a
part that is how-and-what. The two lists below sort them by part.

**Q2. Does it know something that changes when something outside yardr's
own model changes?** That means a harness program and its versions,
herdr's commands, a service's API, the words, flags or sentences of one
yardr command, or the layout of one page, window or terminal view.

A file is **core** when Q1 is yes and Q2 is no. It is **how-and-what** when
Q2 is yes. A file where both answers are no is a **library** (config,
version, otel, yardkey, the rpc transport). A library sits with whoever
imports it, and with the core when the core imports it. Device enrolment
(`internal/store/devicepair.go`) is a library, as the pairing wire between
yards is.

By this rule these are core:

- the store's transaction, bucket and event machinery
- beads and their gates
- notes, asks and answers as data (the `Ask` record, approve, put aside, decline, take back)
- the carry record that an approved command waits in (`internal/store/carry.go`)
- mail and notifications
- sessions and claims
- flows and their moves
- routes and groups as records
- the permission gates (`policy.go`)
- migrations
- the dispatch pass
- the runner interface
- peering as delivery
- the store API with its socket
- timers (`internal/store/timers.go`)
- subscriptions as generic delivery state
- the preview registry, its states and its two permission gates (`internal/store/previews.go`, `previewwatch.go`, `previewpolicy.go`)
- the delegation gate and its kept record (`internal/store/delegation.go`'s gate, `delegation_repo.go`, `delegation_view.go`)
- the device record (`internal/store/devices.go`)

These are how-and-what:

- harness knowledge in any package
- the herdr runner's pane reading
- every run-ask row's parser, card sentences and run
- pack content and the pack planner
- the provider logic of source declarations
- the web pages, chat and the wording of mcp's tools
- doctor
- onboarding (`init`, `setup`, `guide`)
- upgrade and release
- the daemon's housekeeping passes that know about the machine (`internal/dispatch/gocache.go`, `internal/diskkeep`, `internal/dispatch/telemetry.go`)
- the per-source parts of `internal/store/subscriptions.go` (who a crew member is in a source, the `--where` matching)
- the preview supervisor (`internal/preview/preview.go`)
- delegated pack imports (`internal/store/delegation_pack.go`, `delegation_pack_record.go`, `delegation_pack_target.go`)
- ETA cells (`internal/store/eta.go`)

Three of these stand on a side the code does not show yet:

- The per-source parts of subscriptions still sit in
  `internal/store/subscriptions.go`, beside the generic state, and moving
  them out is **planned**.
- The core still writes ETA cells inside a bead's move, and taking them out
  of it is **planned**.
- Device enrolment still sits in the store, and moving it out is
  **planned**.

### Where the rule cannot decide

Seven cases are mixed. Each holds both sides in one file, so the rule sorts
it by function and gives the file no single side.

1. **The run-ask machinery.** Proposing, approving, re-drawing and
   staleness are core, because an approval moves state. The rows are
   how-and-what, because each knows one command's words. Both are written
   into the same functions, and `approveAsk` calls a row's `card` and `run`
   inside the core's write.
2. **Routing by quota.** `Route.Choose` is core. The quotas it judges by
   come from a harness subscription's limits (`internal/limits`,
   `dispatch/quota.go`, `dispatch/harness_limits.go`), which is
   how-and-what. `Route.Quotaed` and `Route.Gated` depend on which groups
   start agents, a fact about the runner.
3. **Reads shaped for one view.** `line.go`, `flowsboard.go`, `flowmap.go`
   and `decisions.go` in `internal/store` assemble records (core) in the
   shape of one footplate window or web page (Q2). They live in the store
   because the read must be consistent in one transaction.
4. **The prompt a session is given.** Choosing the stage file is core.
   Reading a brief for one harness is how-and-what. The lookup is
   `promptlook.go` in the store, which says in its first lines that it is
   there because the Line read is built in the store and the store cannot
   import the runner.
5. **Peers.** Peer records, the outbox and settlement are core, because
   they are delivery to another yard. The wire format, signing and sealing
   are a contract (`peer.md`).
6. **The dispatch harness passes** (`internal/dispatch/harness*.go`). They
   ride the core's pass and record on the core's session handle, but what
   they match is harness output.
7. **Housekeeping.** Backups and pruning of the store are core (Maintenance,
   under The split of `store.API`). GOCACHE rotation and the disk floor are
   how-and-what, although `dispatch` runs them.

### Files that are not product Go

The same two questions place every other file, with one rule for each kind.

- **A test file** (`*_test.go`) takes the side of the code it tests. A test
  of a core function is core, and a test of one harness, one command's card
  or one page is how-and-what. A test file that tests both sides is mixed,
  like the file it tests. The fixtures a test reads (`testdata/`) go with
  the test.
- **A file the core reads as data** is how-and-what when it holds knowledge
  of something outside (Q2): the harness files (`harnesses/*.yaml`), the
  packs under `packs/` with their flows, declarations and scripts, and the
  yard's own `.yardr/flows/` and `.yardr/roles/`. The format such a file is
  written in belongs to the core, and so does its norm page, but the file
  itself does not.
- **A page** is on the side of what it describes. The pages of
  `docs/contracts/` are core. A user page about one command, pack or service
  is how-and-what. A design document is neither, because it records a
  decision.
- **Scripts and tools** outside the binary (`contrib/`, `install.sh`,
  `.yardr/test`, the release workflow) are how-and-what. The gate's checks
  of the core's rules are the exception: they carry the core's norm and sit
  with it.

## Core types

Seven of the eight live in `internal/store`, where the records live:

| type | owning package | what it is |
|---|---|---|
| bead | `internal/store` | one durable piece of work: type, stage, notes, edges |
| flow | `internal/store` | the stages of a bead's life and the moves allowed |
| route | `internal/store` | which group takes the beads of a stage |
| group | `internal/store` | who works a stage, and its runner |
| runner | `internal/dispatch` | how the work starts: the `Runner` interface, implemented in `internal/runner` |
| event | `internal/store` | the ordered log of what happened |
| depot | `internal/store` | where a bead is worked: the repository and what tests it |
| source | `internal/store` | a service outside the yard: the `Source` record, declared and called through `internal/source` |

Other packages keep minimal shapes where they must (eta); the store's
are canonical.

Three more words from the [core concepts](../../README.md#core-concepts)
are not types of their own:

- a pack is the files a depot's flows, groups, routes and sources arrive as; the store holds the records they become.
- the crew is the sessions that do the work, tied to no bead; runners start them, the store records them as sessions and events.
- a user is who mail goes to; identity is an actor string on beads, notes and events, not a type the core passes around.

## The split of `store.API`

One named interface per concept, for the future split of the 296-method
`store.API` (the split itself is later work; the names are decided
here). Runner has no interface: the store holds no runner records, and
sessions carry runner state. The full map of all 296 methods to these
interfaces is `docs/design/2026-10-29-yardr-x0gg-store-api-split.md`.

Beads (beads and everything bead-scoped: notes, deps, links, asks,
folds), Flows (flows, their pointers, flow-scoped reads), Routes,
Groups, Sessions (claims and the work's state), Events (the log and its
streams), Depots, Sources (declarations, keys, grants, calls), Users
(users, devices, teams), Mail (mail, inboxes, notifications, the push
queue), Crew, Peers (peers, pairing, beads across the link), Hooks
(hooks, deliveries, issue moves), Threads (participation in outside
threads), Subscriptions, Timers, Memories (remembered beads and crew
journals), Previews, Packs, Blobs, Settings (settings and the telemetry
switch), Permissions (`May`), Maintenance (snapshot, prune, backup,
close, cache rotation).

Each cross-concept verb is owned by exactly one interface:

- Beads: `Advance`, `AdvanceNote`, `Done` (a bead's stage moves,
  validated against flows); `Release` (frees the bead's session);
  `Ready`, `Dispatchable`, `Waiting`, `SidingBeads`, `FoldSidings`
  (routing reads over beads); `MoveLink`, `MoveLinkSource` (a bead's
  tracked issue); `GiveSecret` (a secret ask keys its source);
  `UserAsks` (asks awaiting a user); `Estimate`, `EstimateMany`,
  `Arrivals` (a bead's ETA); `Family` (a bead's train and base).
- Flows: `NextStages`, `FlowMap`, `Stranded` (a bead read through its
  flow); `SetFlowPointer`, `RepoFlows`, `DepotFlows` (a depot's flows);
  `Flows` (the travelling board).
- Sessions: `Claim` (a bead, group and stage become a session).
- Sources: `SourceCalls`, `SessionCalls` (what a session may call);
  `KeyUser`, `KeyUserCredential` (a user's credentials for a source);
  `MoveSourceRecords` (a source's rename).
- Users: `CheckFor` (`--for` names a user); `SetUser` (notify names a
  source); `Teammates`, `UninviteTeammate` (the mapping joined to users);
  the `DevicePair` methods (a user's device enrolment).
- Mail: `Conversation`; `UserInbox`, `AckUserInbox`, `UserNotifications`,
  `AckUserNotifications`; `Pushes`, `DropPushes`, `PushTried` (the
  outbound queue).
- Crew: the `*For` methods (a crew member serving a user);
  `RecordCrewUsage`, `CrewPrime`, `CrewPrimeFor`.
- Peers: `PeerBeadSend`, `SettlePeerBeads`, `PeerFetches`,
  `PeerFetched`, `PeerFetchFailed`, `PeerBases`, `PeerPulled` (beads
  across the link); `PeerRead`, `PeerReadKept`, `PeerNote` (reading and
  noting a peer's bead); `MailPeer`; `SetPeerIntake`,
  `RemovePeerIntake` (a peer's depots); `SetPeerIdentity`,
  `RemovePeerIdentity` (who a peer acts as).
- Hooks: the `IssueMove` methods (a hook's map moves the tracked issue).
- Threads: `FollowThread` through `StateSet` (a crew member's or user's
  participation in an outside thread).
- Subscriptions: `SetIdentity`; the `Add/RemoveSubscription` methods
  (names subscribed to sources).
- Timers: `FireTimers` (due timers fire, reading bead state).
- Memories: `Remember` (a memory is kept as a bead); `Journal`,
  `RecallJournal` (a crew member's journal).
- Maintenance: `Snapshot`, `Prune`, `Backup` (the whole store).
- Permissions: `May` (every record's gate).
- Unassigned: `LoginWait`, `LoginWaitReady` (an in-memory rendezvous
  between a leader and waiters; no record, so no interface).

## Layers

37 packages under `internal/` at 4bee454, each in one layer (38 at
f75abf88; `shared` and `udiff` folded into `store` since, `pprof` moved
out of `server`, `mcp` and `previewtest` added, `flowdraw` removed):

- leaves, importing no internal package (7): `pairing`, `peer`, `pprof`, `previewtest`, `rpc/clientgen`, `scripttest`, `version`.
- store (1): `store`. Measured: 220 non-test Go files, a 310-method `store.API`, 2 internal imports — the peer and source exceptions under the rules. Shared types the leaf splits move stay in store itself: there is no separate types package.
- services (25): `agents`, `bdp`, `chat`, `config`, `depotfile`, `diskkeep`, `edgelink`, `eta`, `hooks`, `limits`, `mcp`, `notify`, `otel`, `pack`, `preview`, `rpc`, `rpc/gen`, `source`, `standin`, `storetest`, `telemetry`, `testload`, `web`, `yardkey`, `yards`. (`storetest` and `testload` are test-only helpers; `rpc/gen` is a generator.)
- `dispatch`, `runner`, `server` (3): the control loop and the process owners.
- cli (1): `cli`. Measured: imports 28 of the 37 internal packages.
- main: the root package, the binary; the only importer of `cli`.

## Import rules

Three rules; the test holds them:

(a) `internal/store` imports no `internal` package, except
`internal/peer`, which stays: it is OS process code with no yard
types, so the edge carries none of the coupling the rule is there to
stop; and `internal/source`, which stays: the store executes run asks
through its functions, not only its types, so cutting the edge would
make the two import each other, while source itself imports only
`internal/config`.

(b) A leaf imports no internal package.

(c) No internal package imports `internal/cli`; only the root `main` may.

The rules cover `internal/` only; `contrib/` and the packs are outside
them, as is moving any code, which the split beads do.

## Line ceilings

The import rules stop a cycle and not a package's growth, and rule 3 caps
one file, so a package can still grow by adding files. So each package that
holds core code has a ceiling in non-test Go lines, counted over the whole
package. Its lines are every non-test `.go` file in its directory, not its
subdirectories, with each line counted as `wc -l` counts it. The table has a
row for each package under `internal/` where What is core puts some lines on
the core side. Its measured cell says how many lines the package held, and
at which commit, when the ceiling was set.

| package | ceiling | measured |
|---|---:|---|
| `internal/store` | 107,416 | 101,416 lines at 606fd3bf5 |
| `internal/dispatch` | 20,181 | 18,381 lines at 606fd3bf5 |
| `internal/rpc` | 7,545 | 7,045 lines at 606fd3bf5 |
| `internal/server` | 5,910 | 5,410 lines at 606fd3bf5 |
| `internal/runner` | 8,679 | 8,179 lines at 606fd3bf5 |
| `internal/hooks` | 4,264 | 3,764 lines at 606fd3bf5 |
| `internal/yards` | 1,749 | 1,249 lines at 606fd3bf5 |
| `internal/notify` | 889 | 389 lines at 606fd3bf5 |

A ceiling is a budget for the whole package, core and how-and-what alike,
and not a ceiling of the core lines in it, so it falls as how-and-what moves
out. A ceiling is first set at the package's lines plus a room of 6,000 for
`internal/store`, 1,800 for `internal/dispatch` and 500 for any other
package. New core work makes room by moving how-and-what out first; a
ceiling is lowered to the package's count by a bead of its own after a move
has landed, and raised by nobody but a bead that says why on a card to the
operator.

`TestCorePackagesKeepUnderTheirCeilings` fails when a package holds more
lines than its ceiling, and says the package, its count and its ceiling. It
fails when a row names a directory where no package stands, and when this
page names a package as core and the table has no row for it. The page names
a package as core by a file's path in the list of what is core, and by the
owning package of a type in Core types. A package under its ceiling by any
amount passes, and the test never raises, lowers or rewrites a ceiling.

## Names the core may not name

A name written in the core's code is knowledge of something that changes
without the core: the harness a session runs, the service a source calls, the
program that holds the panes. The table lists the names the core may not
write, and `TestCoreNamesNothingOnTheList` holds the code to it. The list is
written out here and not read from the names of directories under `packs/`,
which would also stop words that are the yard's own (`design`, `dir`,
`routine`, `starter`). For the same reason a word that is also a plain word of
the yard's own model stays off it: `herdr` is the name of a runner, and `pane`
is where a session runs and which one a call came from.

| group | names |
|---|---|
| harness kinds | `agy`, `claude`, `codex`, `grok`, `kimi`, `kiro`, `muse`, `opencode`, `pi` |
| providers | `github`, `google`, `linear`, `pushover`, `telegram` |
| herdr's command words | `tab`, `workspace` |

The word lists of yardr's own commands, and the names of the hosted edge, the
pack planner and a source's identity forms, are not on the list yet and wait
for the operator's choice of words.

`TestCoreNamesNothingOnTheList` does not replace four other tests, because
each holds more than it does. It reads the non-test Go of the packages in Line
ceilings and skips comments. `main_test.go:TestCoreComparesNoSourceName` keeps
`github` and `linear` out of all of `internal/` but `internal/hooks` and
`internal/standin`. `main_test.go:TestNoGoCodeNamesANotifyService` keeps
`telegram` and `pushover` out of the non-test Go of the whole repository,
comments included. `main_test.go:TestNoGoCodeNamesGitHub` keeps `github` out
of the same files, except those it lists, and keeps the names of GitHub's
credentials out of all of them. `main_test.go:TestNoGoCodeNamesARole` keeps
the file names of a pack's roles out of the non-test Go outside
`internal/pack` and `packs/`, and no row of the list holds them. Each has
exceptions of its own, named in its comment.

The harness kinds row holds the kind of each file in `harnesses/`, which is
named for it (`claude.yaml` is `claude`), so that a new kind joins the list in
the commit that adds its file (rule 7).

A group joins the list as a row, and a name as an entry in a row's cell: one
lowercase word of letters and digits. The test reads the table and knows
nothing of the groups but the harness kinds row.

The test reads the non-test Go of each package in Line ceilings, the files in
the package's own directory, less the files under Files the name scan does
not read, and skips comments. A name matches a whole string literal (`"pi"`)
and a whole part of an identifier, or several whole parts in a row, and never
the inside of a part: `kindPi` and `Pi` name `pi`, `OpenCode` names
`opencode`, and `pick` and `PID` do not name `pi`. A name's case is ignored.
A hit fails with its file, its line and the name.

Today's hits are the allow-list, `coreNamesAllowed` in `corenames_test.go`:
one row for a file and a name, with the number of hits the file may keep. A
file with more hits of a name than its row holds fails, and so does a row that
holds more hits than its file has, with the number to lower it to, so that a
count only falls as the code that names the name moves out and a hit that has
gone cannot come back. A row is added only in the commit that adds its name
to the list, at the hits that name has that day, and a file that moves or
splits takes its rows to its new paths with no more hits in all.

## Files the name scan does not read

A file that the how-and-what list in What is core names by its path is
how-and-what, so it may name the listed words, and the name scan does not
read it. The table gives each such file's path and the entry of that list
that names it. A file that the list names for part of it only, such as
`internal/store/subscriptions.go` for its per-source parts, stays in the scan,
because the rest of it is core. The test fails when a row names a file that
is not there or that stands in no package of Line ceilings.

| file | listed as |
|---|---|
| `internal/dispatch/gocache.go` | the daemon's housekeeping passes |
| `internal/dispatch/telemetry.go` | the daemon's housekeeping passes |
| `internal/store/delegation_pack.go` | delegated pack imports |
| `internal/store/delegation_pack_record.go` | delegated pack imports |
| `internal/store/delegation_pack_target.go` | delegated pack imports |
| `internal/store/eta.go` | ETA cells |

## The contracts

A contract is a form that crosses between the core and the how-and-what:
data, a file format, a command, an RPC call or a Go interface. It has an
owner, a version and a change rule, which says what a change to it must do.
Eight contracts cross the core's edge. Each has a norm page in
`docs/contracts/`, named on its line below, that states the contract's rules
as a table with a check for each rule. The line also names the section of
[the design](../design/2026-11-24-yardr-h5wn-core-contracts.md) that decides
the contract. A name that is not a link is a page not yet written
(**planned**).

- [`harness.md`](harness.md): what the core asks of an agent program (design 3.2.1)
- [`source.md`](source.md): calls and hooks, the declaration format, the command protocol and the hook envelope (design 3.2.2)
- [`runner.md`](runner.md): starting and reading a session, the `Runner` interface and the session handle (design 3.2.3)
- [`flow.md`](flow.md): flows and stages, the files the flow engine reads (design 3.2.4)

- [`pack.md`](pack.md): the pack API, the `api:` number of `pack.yaml` (design 3.2.5)
- [`card.md`](card.md): the card and the ask, the ask as the core's data and what one command supplies to be proposed (design 3.2.6)
- [`view.md`](view.md): what web, chat, mcp and footplate read (design 3.2.7)
- [`peer.md`](peer.md): the protocol between two yards, with the edge link (design 3.2.8)

## Rules

Every page in `docs/contracts/` has one Rules section holding one table. Each
row is one rule, numbered 1, 2 and so on in order, and its check cell says
what fails when the code breaks it. The header row is `| # | rule | check |`.
A check cell is one of these:

- The word `unchecked`, for a rule that no test or gate step holds yet. The
  gap shows and can be counted.
- One or more names in backticks, separated by `, `. A name is a Go test
  that a `*_test.go` file of the module declares (`TestImportRules`), or a
  step of the gate written `gate:<word>`, where the word occurs in
  `.yardr/test` (`gate:filesizes`).

The check of rule 4 reads each page and fails a row whose check does not
exist. It does not judge whether a named test holds the rule. The page's
author and its reviewer read the test for that.

A page cites the code it is about as file and symbol, in one span of
backticks: the path from the repository root, a colon and the symbol, as
`internal/store/asks.go:Ask`. The symbol is a func, type, var or const that
the file declares at its top level, or `Type.Method` for a method, as
`internal/store/flows.go:Store.PutFlow`. The path is always whole, never a
bare file name. Code inside a declaration, such as a line in the middle of a
function, is cited by the declaration. A line number is no citation, because
it moves with every edit above it and nothing goes red when it does. A
sentence that names the symbol in backticks keeps the name, and the citation
follows in parentheses.

The check of rule 8 reads each page and fails a citation whose file does not
exist or does not declare the symbol. It also fails a line number on any page
that `linePages` in `contracts_test.go` does not list. The list holds the
pages that still cite by line and wait to be converted. A listed page that
holds no line number fails until it comes off the list, so the list only
shrinks. The check does not judge whether the symbol is what the sentence
says. The page's author and its reviewer read that.

| # | rule | check |
|---|---|---|
| 1 | No import between `internal` packages breaks rule (a), (b) or (c) under Import rules. | `TestImportRules` |
| 2 | The Layers section names every `internal` package in its layer, and its counts are true. | `TestLayersMatchCode` |
| 3 | A non-test Go file over 1,000 lines has a row in the `filesizes` table and may not grow past the row's ceiling. | `gate:filesizes` |
| 4 | A page in `docs/contracts/` holds each of its rules to a check that exists. | `TestContractPagesNameTheirChecks` |
| 5 | A package with a row in Line ceilings holds no more non-test Go lines than its ceiling, each row names a package that exists, and each package this page names as core has a row. | `TestCorePackagesKeepUnderTheirCeilings` |
| 6 | A non-test Go file in a package of Line ceilings, less the files under Files the name scan does not read, names no word of the list under Names the core may not name as a whole string literal or as whole parts of an identifier, beyond what its row of the allow-list holds; a row holds no more hits than its file has. | `TestCoreNamesNothingOnTheList` |
| 7 | A file of `harnesses/` is named for a kind in the harness kinds row of the list under Names the core may not name. | `TestHarnessFilesNameOnlyListedKinds` |
| 8 | A page in `docs/contracts/` cites code as file and symbol, and the file declares the symbol. | `TestContractPagesCiteDeclaredSymbols` |
