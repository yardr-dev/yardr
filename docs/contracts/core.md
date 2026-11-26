# The core

This page is the norm for what the core is and what may import what. It
states the rule that puts a file inside or outside the core, the core's
types, the split of `store.API`, the layers of `internal/` and the import
rules. The Rules table names the check of each rule. `TestImportRules` and
`TestLayersMatchCode` in `arch_test.go` (package main, at the repository
root) hold the code to the import rules and the layers. The page and its
checks change together, so a new import that breaks the rules fails the
gate. The contracts at the core's edge each have a page beside this one,
listed under The contracts, with a Rules table of the same form.

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
- the daemon's housekeeping passes that know about the machine (`gocache.go`, `diskkeep`, `telemetry.go`)
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

- `harness.md`: what the core asks of an agent program (design 3.2.1)
- `source.md`: calls and hooks, the declaration format, the command protocol and the hook envelope (design 3.2.2)
- `runner.md`: starting and reading a session, the `Runner` interface and the session handle (design 3.2.3)
- `flow.md`: flows and stages, the files the flow engine reads (design 3.2.4)

- `pack.md`: the pack API, the `api:` number of `pack.yaml` (design 3.2.5)
- `card.md`: the card and the ask, the ask as the core's data and what one command supplies to be proposed (design 3.2.6)
- `view.md`: what web, chat, mcp and footplate read (design 3.2.7)
- `peer.md`: the protocol between two yards, with the edge link (design 3.2.8)

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

| # | rule | check |
|---|---|---|
| 1 | No import between `internal` packages breaks rule (a), (b) or (c) under Import rules. | `TestImportRules` |
| 2 | The Layers section names every `internal` package in its layer, and its counts are true. | `TestLayersMatchCode` |
| 3 | A non-test Go file over 1,000 lines has a row in the `filesizes` table and may not grow past the row's ceiling. | `gate:filesizes` |
| 4 | A page in `docs/contracts/` holds each of its rules to a check that exists. | `TestContractPagesNameTheirChecks` |

The line ceilings of the core's packages and the list of what the core may
not name (design 3.4) join this table later, each with its check.
