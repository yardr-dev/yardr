# Packs become connectors and lines, and a line is copied once and then belongs to the yard

Design yardr-j22s, proposed 2026-11-28. A proposal as it was decided on, not a description of what the code does today; the decision is on the bead.

A *pack* is a directory a yard imports: `pack.yaml` and the files it names ([packs.md](../packs.md)). `yardr pack apply` imports one into a depot, or into the yard itself when the pack is of kind `source`. The yard keeps a *record* of each import, with the sha256 of every file and flow it wrote. The *plan* is what `yardr pack check` prints and what the card of a pack apply shows: each object the import would create, change or leave. A *card* is an ask that a person approves. A *run ask* is a card that carries one yardr command, and the *general card* is the run ask that carries any yardr command whole. Footplate is yardr's app for an operator, a desktop app and an iOS and an Android app, each in a repository of its own (section 2.11). *Decide* is its list of the cards that wait for the operator. Two words come from the direction this design answers. A *connector* is a service the yard calls or hears, which today is a pack of kind `source`. A *line* is a way of working, with its flow, stage files, roles, scripts and groups. Written to a folder it is still a line, and copied into a depot it is that depot's line. Today a line is a pack that carries a way of working and is not of kind `source`. One pack is neither. `pageframe` carries a page frame and nothing else, so it is a *frame alone*, and the default frame that yardr reads from its binary stays what it is (section 3.4). A *tool* is a program on the machine the yard's agents run on. The yard as an MCP server for a person's own client is *the yard's MCP*, and is never called a connector.

Every size below was measured at 0629160f0 (main, 28 Nov 2026). Section 2.1 gives the commands and says what was read and what was only counted by name.

## 1. The question

On 28 Nov 2026 mckean and the yardmaster agreed by mail (mails 264336 to 264394) to change packs by updating them, not reinventing them, and mckean picked "Design bead now" on ask 32411. The direction gives a service and a way of working two names, a connector and a line. A line names what it needs, connectors and tools, and installs none of it. A missing tool is installed by a card that shows the command word for word and runs it on the operator's yes (mckean's idea, mail 264377). Every operator step is a card in Footplate, so that no step of applying a line, seeing how it differs from its original, getting the original again or installing a tool may need a terminal (mckean, mail 264394). The install card needs no security design, because the machine is the operator's (264439). Drift is gone: "when applying we don't keep the original. we apply a line, great. now this can evolve, make it ours. a line is not installed, it's used, cut and extended. you can then reuse the same line or get the original line again" (264456). An improvement to the original is copied in again the way a skill is, and where the yard has edited a file no code merges: the yardmaster reads what changed and proposes it on top of the yard's version, as a card for each stage file or role (264465). A line in the yard may carry a label of where it came from, which a person reads and the compare uses, and nothing acts on it (264473). The label stands beside what the line needs, in one place in the line's file, and the line has one list, `needs`, for connectors and tools alike (264481, 264509). `line` is the word, for the folder and for what runs in a depot (264488 to 264502). Lines should travel whole, between depots and to other yards (264525). This document says how to build that, what it removes and what it adds, what it costs, and where the direction is wrong. mckean reads it and its critique and decides, and nothing is built before.

mckean then answered the open questions of the first draft (mails 264643 to 264732, relayed in the yardmaster's note 32449), and this draft follows each answer.

1. No second copy. Applying a line again writes only what the yard lacks. Where the yard has a flow, role or file of that name that reads otherwise, apply writes nothing and says `differs`. No sibling lands. The compare shows the yard's text beside the original, read from the place the label names, so no copy of the original sits in the yard. Taking a part of the newer original is a card per file. One yes that replaces a whole changed flow is left out (mails 264679, 264688).
2. Scripts follow one rule with the stage files. A script a line carries is copied once, is the yard's, shows in the compare and is changed by a card, with no fingerprint per file and no record of what was applied. Two differences stay: the card shows the script whole and is weighed `code`, and an old script can fail a stage. For the second, the line holds the yardr version it was built for, and after an upgrade a script that differs from the one this yardr carries is named at once with the card that takes it (mails 264696 to 264725).
3. Five steps first, then mckean looks: the words, apply again, the compare, needs and the install card, with only the Footplate work their cards need to be drawn right. Everything else is left out for now and named in section 4.2 (mail 264732).
4. `connector` means only a service the yard calls or hears (mails 264643 to 264657).
5. A tool's `check` runs only for a line that is applied, and never when a line is only looked at (the yardmaster's settlement, mail 264664, which mckean did not object to).

The other questions stand as the first draft recommended, where these leave them standing. Section 5 lists each.

## 2. What exists today

### 2.1 The sizes, measured again

The bead's count reproduces. Product Go whose path holds "pack" is 21,466 lines and its tests are 45,373:

```sh
find . -path ./.git -prune -o -name '*.go' ! -name '*_test.go' -print | grep -i pack | sort -u | xargs cat | wc -l
find . -path ./.git -prune -o -name '*_test.go' -print | grep -i pack | sort -u | xargs cat | wc -l
```

| place | product lines | test lines |
|---|---:|---:|
| `internal/pack`: the reader of `pack.yaml`, the planner, apply, export | 11,197 | 15,612 |
| `internal/store`: the cards of a pack apply, the delegated apply, the record | 5,107 | 4,818 |
| `internal/cli`: `yardr pack` | 3,858 | 8,096 |
| `internal/server`: reading packs for cards, carrying them out | 971 | 1,853 |
| `internal/web`: a page for each file of a card | 234 | 575 |
| `packs`: the embed | 99 | 9,657 |
| `internal/hooks`, `notify`, `dispatch`: tests named for a pack | 0 | 4,762 |
| total | 21,466 | 45,373 |

The count is by path, not by what a file does. 14,257 of the test lines (the tests inside `packs/` and the pack tests of `hooks` and `notify`) test what the connectors contain, and nothing in this design touches them. The cards of a pack apply, `runask_pack*.go` and `delegation_pack*.go` in `internal/store`, are 4,204 product lines and 4,444 test lines, the largest part after the plan. The plan, `internal/pack/plan*.go`, is 4,273 lines.

The five files the bead names for keeping, dropping and taking are 1,172 lines: `keep.go` 182, `plan_drop.go` 421, `runask_packtake.go` 185, `plan_files.go` 215 and `digest.go` 169. Reading them shortens that list. `digest.go` is the pin digest that `--sha256` holds a pack to, and a line keeps it. `plan_drop.go` plans removals but also holds `depotRole`, 55 lines about a depot's own role files. `plan_files.go` is mostly the copy of files. The lines that do nothing but decide keep, take, prune or edited by hand are about 440 across all the pack code, two percent of 21,466. That is all the bead's first idea, one compare in place of those rows, would remove. The direction that replaced it removes more, because with nothing compared against an original `change` goes too (section 3.10). A scan by function (every top-level function that names one of those words, counted whole) gives an upper bound of 3,936 product lines in 67 functions and 4,668 test lines in 79. Most of the lines in those functions do other things.

What I read whole for this draft: `keep.go`, `plan_files.go`, `runask_packtake.go`, `runask_packyes.go`, the filing in `runask_general.go`, `RecordPack` and `ImportRecord`, `stalePackNotes`, `upgradeStalePackLine`, `resolvePack`, and the group planner where it keeps or sets a prompt, a command or a limit. The earlier drafts read `plan_drop.go`, `runask_packinstall.go`, `internal/source/install.go` and the prune and remove functions of `apply.go`. The other planner, card and server files I know by their function lists and sizes (`awk` over the `^func` lines) and by the branches that name the words above. A removal figure in section 3.10 is that reading, with a range, and not a call graph.

Docs: `docs/packs.md` is 2,402 lines and `docs/pack-api.md` 523. About 160 lines of packs.md are the passage "A pack is a starting point" and the section "Pruning", plus six rows of two tables.

Packs: `packs/` has 22 directories. `template` is a skeleton that is not embedded and `testdata` holds fixtures, so 20 packs are embedded: 13 connectors, 6 lines (`starter`, `dir`, `contributors`, `design`, `ci-red`, `website`) and `pageframe`, a frame alone. It has no `kind` and no flow, group, crew member or source (`packs/pageframe/pack.yaml`), and `packs/packs.go` says it is of no kind and is read from the binary as it stands. The packs repository has 17: 14 connectors and 3 lines (`tdd`, `visuals`, `webdesign`). The bead's 14 of kind `source` counts the template. In all, 27 connectors, 9 lines and one frame alone. The starter is 16 files and 151 KB. The largest line is `contributors`, 23 files and 342 KB, which is a third of the 1 MiB that an ask holds. Four of the six embedded lines carry scripts (`starter`, `design`, `website`, `contributors`).

Limits in force. `internal/store` stands at its ceiling, 101,675 lines of 101,675 (`docs/contracts/core.md`, Line ceilings). `internal/rpc` has 7,045 of 7,545 and `internal/server` 5,412 of 5,910. A ceiling does not fall when lines leave a package. The page says a ceiling is lowered by a bead of its own after a move has landed, and raised by nobody but a bead that says why on a card to the operator. These files are at their own ceiling and cannot grow by a line: `internal/cli/pack.go` 1,866, `internal/cli/doctor.go` 1,243, `internal/pack/pack.go` 1,776 and `internal/pack/export.go` 1,047. `internal/store/runask_pack.go` has 999 of 1,000, `internal/store/delegation_pack.go` 994 of 1,000 and `internal/pack/plan.go` 992 of 1,000. `internal/pack`, `internal/cli`, `internal/source` and `internal/web` have no package ceiling. The yardmaster's base role has a hard ceiling of its own, 8,192 bytes (`yardmasterCeiling` in `internal/pack/pack_test.go`), and holds 8,191.

### 2.2 What an apply does

An apply copies the roles and scripts of the pack's groups to `<home>/packs/<pack>/`, the frame beside them, and the stage files of the flows it uses to `<home>/flows/<pack>.<flow>/`. It creates the flows as store flows named `<pack>.<flow>`, the depot's groups as `<depot>-<group>`, routes, flow pointers, crew members and their subscriptions, hook lines and maps, and the declared sources of the pack. It asks the operator what a pack cannot say (agent kind, model, arguments, members, grants). "Everything is matched by name, so importing twice changes nothing the second time" ([packs.md](../packs.md#how-a-pack-maps-to-the-yard)). The copies of roles, scripts and stage files are shared by every depot that applies the pack, which is why packs.md tells an operator to import a pack with the same `--crew` in every depot: the second depot rewrites the files the first reads.

### 2.3 How an apply keeps, drops and takes

A plan compares three things for a flow, a stage file or a role: what the yard has, what the pack has now, and what the last import recorded (`shapeOf` in `internal/pack/keep.go`). It finds one of four shapes. The pack moved and the yard did not, so the pack's version is brought. The yard changed it and the pack did not, so the yard's stays (`kept`). Both changed (`DIVERGED`). Or no record can say (`kept`, since the yard's version is the one that can be lost). A script or the frame the yard changed stops the import until `--yes`. An object the pack dropped shows as `no longer in it` and goes only with `--prune`. `--take <kind>/<name>` sets the pack's version of a kept object. On the wire a plan's actions are seven, `create`, `change`, `unchanged`, `not_in_pack`, `dropped`, `takes` and `kept`, with `shadows`, `keeps` and `edited` as fields of an object. Printed, they come to ten words: `create`, `change`, `unchanged`, `not in the pack`, `no longer in it`, `takes`, `kept` (three shapes), `shadows`, `kept:` under an object (five reasons), and `edited by hand` (two forms).

A card carries `--yes`, `--take`, `--sha256`, the depot and the mappings that answer the plan's questions (`packApplyFlags` in `internal/store/packmappings.go`). It does not carry `--prune`, so removing what a pack dropped is terminal-only, and it does not carry `--depot-roles` or `--pack-roles`. A card can read an embedded pack, a directory inside a registered depot's main checkout, and the packs repository's URL pinned with `--sha256`. A directory elsewhere, a recorded source, any other URL and a plan that still asks after the mappings are the operator's in a terminal ([packs.md](../packs.md#an-integration-pack-kind-source)). The yardmaster's delegated apply on the operator's mail takes nothing and removes nothing.

### 2.4 Editing in place, and copies, today

- A stage file is saved at once, on every device, by the operator: `yardr flow set-stage`, `PutStageFile` from a paired device, Footplate's Line window (decided 25 Nov, yd-kedn). The text replaced is kept in `<home>/stage-history/<sha256>`. A save does not touch the record, so the next plan calls the file `kept`. A crew member proposes the same save as a card, `flow set-stage`, weighty `code`, which shows the text that stands and the text proposed.
- A role is edited by a card that writes a new file named by the text's digest under `<home>/roles/` and points the group at it (`internal/store/runask_promptfile.go`). The pack's copy under `<home>/packs/<pack>/roles/` is never edited, so it stays as the pack wrote it, and the next plan finds that the group's prompt is not the pack's file and keeps it. A flow's stages and outcomes are replaced by `flow set`, which has a card too.
- A flow that governs a depot from its repository is copied into the store by `yardr flow copy-repo` before it is edited, as `<depot>.default` and so on (decided on ask 31114).
- A depot follows a flow through a pointer, and every apply points the depot at the pack's flow (`internal/pack/plan_flows.go`). A depot that follows a copy of its own is moved back.
- yardr-68s3, parked on 12 Oct (ask 2750), asks for a yard-local copy marked customised that apply keeps and names, "needed before anything edits configuration in a browser".

### 2.5 The record, and who reads it

The record is one `store.PackImport` for each pack and depot. It holds where the pack was read from (`Source`, `Commit`, `Dirty`, `Auth`), when and by whom, the names it mapped (`Groups`, `Crew`, `Flow`, `DepotRoles`), the answers about opening threads, calls and exposure to peers, the chosen region bases, the frame (`Frame`, `FrameActive`), whether the pack has setup notes and a yardmaster section, and `Objects`, each object with the sha256 of what the pack carried. Apply is not its only reader:

| reader | what it reads |
|---|---|
| the planner (`internal/pack/plan*.go`) | `Objects` for keep and drop, the mappings and answers so that a second apply asks nothing |
| `pack apply <name>`, `pack show <name>`, `pack list` | `Source` and `Auth`, to read the pack again from where it came, and the whole record |
| prime (`internal/cli/prime_sections.go`) | `Setup`, to name `pack show`, and `Section`, the yardmaster section that prime appends, read from the pack's source |
| page frames (`internal/pack/framefor.go`, `internal/store/framedpage.go`) | `Frame` and `FrameActive`, which frame a scope uses |
| `pack export` | which pack each governing flow came from, which groups were bound as another (`binds_as`), and which frame is active |
| `depot sources` and `depot source` | which flows and roles come from a pack, so a depot can switch between its repository's and the pack's |
| the Line read (`internal/store/line.go`) | which pack a flow came from, drawn as `pack <name>` on the flow |
| the setup steps, doctor | which imports exist, and the connectors' needs |
| `yardr upgrade` and doctor | the `packscripts` line and the note "files behind yardr" (section 2.6) |
| the setup card of a connector (`internal/server/appliedpack.go`) | the recorded directory, held to the commit the apply found |

`RecordPack` writes the record only when the import wrote something or the record differs from the stored one. It compares without the source, the commit, the time and the person (`same` in `internal/store/packs.go`), so an import that changes nothing leaves the record, and the label in it, as it was.

### 2.6 Seeing the original today

`yardr pack check <pack>` prints the plan, which is already a compare of the yard against the original as it is now. A bare name is read again from the place the import recorded (`resolvePack` in `internal/cli/pack.go`): a folder as it stands, a URL fetched with the credential the record names, and for an embedded pack the copy in the running binary. The record's `Source` for an embedded import reads `embedded in yardr <build>`. Doctor adds one note for an import of an embedded pack whose copied roles, stage files or scripts differ from what the running binary would copy: `pack <pack> in <depot>: <n> files behind yardr <build>`. A directory, a URL and an earlier record are not compared (`stalePackNotes` in `internal/cli/doctor.go`).

`yardr upgrade` refreshes the scripts under `<home>/scripts`, which are the ones a yard set up by hand runs, from the checkout. On a checkout only, it also prints a `packscripts` line that names each stale copy of a script it refreshed under `<home>/packs/<pack>/scripts/`, with the command that re-applies its pack (`upgradeStalePackLine` in `internal/cli/upgrade_scripts.go`), and `merge-local` reads that line into its landing note. A release has no checkout, so nothing is named there. "The copies a pack carries under `<home>/packs/*` and `<home>/flows/*` are not refreshed" ([upgrade.md](../upgrade.md#the-scripts)).

### 2.7 Needs and installs today

A connector's declaration can carry `needs`: for each program, `program`, `env` (a variable that may name another path) and `install` (an argv run without a shell). `source.NeedPath` finds a program by that variable, then on PATH, and `yardr doctor` and the setup card name what is missing. `yardr pack install <pack>` installs it, in a terminal or on a run card that shows each install file whole with its sha256 and runs those bytes when the operator approves. An install that names an absolute program is terminal-only. A line has no `needs`. Its stage files and roles name tools in prose. The starter's reviewer role uses `gh` and `codegraph`. Its builder role uses headless Chrome and `agent-browser`. The setup notes of the `visuals` pack in the packs repository say its shots need headless Chrome, found as Google Chrome in `/Applications`, as `google-chrome` or `chromium` on the PATH, or at a path in `SHOT_CHROME`, and `python3`, and they give a shell command to find Chrome by hand.

### 2.8 Cards today

A run ask carries one yardr command of a fixed list (`runList` in `internal/store/runask.go`), shown whole with the yard's own sentence, and approving it has the server run exactly that command once. The general card carries any yardr command no row fits. The yardmaster files it, it shows the command whole and unchecked, it weighs `code`, it is good for an hour, and the server carries it out as `yardr --home <home> --actor <approver> <words>` (`internal/store/runask_general.go`, `internal/server/carry_general.go`). A command whose words parse as a row of the list is refused on the general card, which names the row as the only door (`askedGeneral`), and the words of a form no row parses file on it. The `general_cards` setting is on by default since yardr-mzy7 landed. So a card can carry any yardr command today. It cannot carry a shell line.

yardr-ar8t holds mckean's picks for one card for any command (any command may ride, an unregistered one weighty and the operator's alone). Its design draft went back to backlog on 24 Nov, and nothing here depends on the draft. yardr-rbh6, signing chosen packs, waits in backlog, and nothing here depends on it.

### 2.9 The names today

`pack` names every kind of folder: a connector, a line, and one frame alone. A connector is also called a source (the record `yardr source` keeps), an integration, "a pack of kind `source`" and "an integration pack". `line` is already a glossary word: "what a bead travels in a depot: its flow's stages, the groups the routes put at them, and the agents and people bound to those groups; a pack is a line written down, bound to no one, and importing it into a depot makes one". Footplate's Line window and the `Line` call read a depot's line, and `docs/line.md` follows a bead down it. `connector` is no word of the docs yet.

The pack API has number 8. A removal or a change of meaning of a stable seam raises the number, and then the build stops reading the numbers below it: no old form is kept readable beside the new one ([pack-api.md](../pack-api.md#taking-something-away), `docs/contracts/pack.md`). `kind: source` is stable. An addition, such as a second value for `kind`, raises the number and leaves the old value readable. `--take` and `--prune` are in no list of the pack API, so taking them away raises no number. `yardr pack ` is on 295 lines of 68 Markdown files outside `docs/design` and on 245 lines of 102 Go files (help text, cards, errors). `kind: source` is in 14 packs here and 14 in the packs repository. 69 of the 77 other entries of `docs/design` mention a pack, and a design document is not rewritten.

### 2.10 Where agents run

The daemon starts `herdr` agents and `exec` scripts on its own machine, which is the machine of the store. A `peer` group is worked by another yard on its own machine, and a `manual` group starts nothing. I found no runner that starts agents on a third machine.

### 2.11 What Footplate does with a pack today

Footplate is three apps in three repositories and three languages: `footplate`, the desktop app (Rust), `footplate-ios` (Swift) and `footplate-android` (Kotlin). A change to what a card says or what a window offers is a bead in each repository it shows in, so this design names its Footplate bead apart (F1, section 6) and counts its lines apart from the 21,466 of section 2.1.

- **How an app reaches the yard.** By the calls of a paired device on the yard's listener, `POST /user/call/<Method>`, and only by the methods of `Calls` and, for an operator's device, of `OperatorCalls` in `internal/rpc/api.go`. `Line`, which reads a depot's line whole, stands in `OperatorCalls`, and the listener refuses it in the clear (`lineInTheClear` in `internal/rpc/userclear.go`).
- **How a card is drawn.** All three draw a run ask with its command whole and the yard's own sentences, and approve it with `ApproveAsk`. For a depot pack's card the desktop also draws the plan in two sections, What changes and Already matches, by the `action` of each object: `create`, `change` and `dropped` are changes, `unchanged`, `kept` and `takes` already match, and an action the window has no word for stands in neither section (`plan_rows` in `footplate/src/window/ask.rs`). The line over the sections says `Nothing changes.` where the first holds no object. The two phone apps open the files of a summary card by name and show the action as a label. They draw the diff for `change` and the text for any other action (`packText` in `footplate-ios/Footplate/Model/DecisionModel.swift`, `PackFileSheet.kt` in `footplate-android`).
- **How the desktop's Line window changes a line.** It has three kinds of door. The first is a save at once, after a confirmation: `PutStageFile`, and `CopyRepoFlows`. The second is a mail to the yardmaster that asks it to file a card, which then stands in Decide, and the window says it "approves nothing". The third is one button that files a run ask itself, Remove on a teammate. The two phone apps' Line screens read and have no control that files a change to a line. Each has the conversation with the yardmaster and the queue of cards that wait.
- **What the desktop says about apply.** For a flow that a pack brought, the Line window says that applying the pack again keeps the yard's version, and shows a command to type in a terminal, `yardr pack apply <pack> --depot <depot> --take flow/<flow>`, as the way to set the pack's again (`line_pack_words` in `footplate/src/model.rs`, and `docs/window.md` there). Those words are copies of `flowPackWords` in `internal/store/runask_flow.go`, and the test beside them says that when the card's words move, these do. So Footplate names a terminal today for a step that, under requirement 6, must not need one.
- **No ceiling.** I found no table of file ceilings in the gate of any of the three repositories (`.yardr/test`, `.yardr/depot.md`, `AGENTS.md`), so F1 needs no card on that account.

## 3. The proposal

The smallest version keeps the machinery that already carries a pack, narrows what an apply does to one thing, and moves the rest of the work to cards and commands that exist. Five steps come first (section 3.11), and what waits is named in section 4.2.

### 3.1 The names

The smallest renaming is in words, with no code and no API number. `line` is the word, as the glossary has it: a way of working. Written to a folder it is still a line, and copied into a depot it is that depot's line, with its flow, groups and routes. The flow stays the stages and moves alone, as `flow copy-repo` already moves one. `connector` is the word for a service the yard calls or hears, today a pack of kind `source`, and for nothing else. The yard as an MCP server for a person's own client is the yard's MCP, since any MCP client may connect and not only Claude, and the glossary says so. The folder names groups and no model, person or secret. Those are the yard's and are given at apply. I propose no other noun for the folder. `pack` stays as the name of the commands and keys, `yardr pack` and `pack.yaml`, and the docs say line or connector wherever they mean one. `docs/packs.md` gets a first column in its table of packs saying which each is, a connector, a line or, for `pageframe`, a frame alone. The glossary gets `connector` with the rule above, a reworded `line` and a `pack` entry that says what the word now names, the folder of any of the three. `source` stays the name of the record a connector declares. That is B1.

The rungs beyond it are left out for now. Each is cheap to start and expensive to leave, so here is what each costs.

- The words yardr prints. The first line of `pack check`, `pack list` and `pack show` says `connector linear` and `line starter`, and the card sentences follow. It is strings in about twenty Go files and the golden tests that quote them, about 200 product lines and 600 test lines changed. The words after the start of a mistake are internal in the pack API, so no number rises.
- New spellings of the commands, `yardr line` and `yardr connector`, beside `yardr pack`. New files in `internal/cli`, and the rows of the run-ask list are keyed by the words `pack apply`, so a card for the new spelling needs the old words stored. Every prompt and mail that says `yardr pack` still works.
- `kind: connector` accepted beside `kind: source`. An addition, so the number rises and the old value stays readable, which means two spellings for good. The 27 connectors, the template and the packs outside move at their own pace.
- `kind: source` removed. A removal raises the number and the build stops reading API 8 and below, which means building the lower bound `docs/contracts/pack.md` calls planned. Every pack outside is refused until its author follows. I would not take this.

Renaming `pack` itself (the directory, the package, `pack.yaml`) is not on the ladder. It touches 21,466 lines of Go and every pack.

### 3.2 Apply writes only what the yard lacks

An apply creates what the yard lacks and leaves what the yard has. It never changes, keeps, drops or takes anything, and it compares nothing with a recorded original. Where the yard already has a flow, a role or a file of the same name, the plan says `unchanged` if it reads as the line says and `differs` if it reads otherwise, and in both cases it writes nothing. That is the answer to "replace on the operator's yes, or land beside" that mckean gave: neither. The compare and a card for each file do the rest (section 3.5).

What a person sees. A depot `web` was set up from the starter. Its operator has since edited the `new` stage file in Footplate and raised the builders' limit. The operator asks what applying the starter again would do:

```
$ yardr pack check starter --depot web
pack starter (embedded in yardr <build>), kind git, into depot web

flows
  unchanged        starter.default
  unchanged        starter.train

files
  differs          flows/default/new.md
                     the yard's copy reads otherwise; nothing is written
  unchanged        roles/builder.md
  unchanged        roles/reviewer.md
  unchanged        scripts/merge-local

groups
  differs          web-builders
                     limit 3 here, 2 in the line; nothing is written

pointers
  unchanged        depot web follows starter.default

2 differ and are left as the yard has them: yardr pack check starter --depot web --diff shows both texts of each
```

`yardr pack apply starter --depot web` says the same and writes nothing. Had the operator removed a group the line carries, the plan would list it as `create`, since an apply restores what the yard lacks, and the operator would see that before the yes.

The rules are the table of section 3.3. Four of them carry the idea.

- The unit is the object, not the flow. A flow, a stage file, a role file, a script, a group, a route, a crew member, a subscription and a hook line each stand alone. A flow whose stages equal the line's and whose stage file the yard edited is `unchanged` in one row and `differs` in another.
- A pointer is set only where the depot has none for that type, as on a first apply, and the question which of a pack's several flows the depot follows is asked only then. Where the depot follows a flow already, that flow stays. This ends today's habit of moving a depot back to the pack's flow.
- Groups, routes, crew members, hook lines and the answers to the plan's questions follow "create what is absent". A crew member's subscription for a depot's stages is an object of its own, so it is added where the crew member holds none of that source and those filters, because the filter names the depot and a crew member may serve many. It is left where the crew member holds one, whatever its scope.
- Nothing is removed. An object the line dropped since the yard copied it stays, and the plan does not mention it. `--prune` is for connectors alone (section 3.4).

The plan's actions on the wire fall from seven to six. `create`, `unchanged` and `takes` stay, and `differs` is new. `change` and `dropped` stay for a connector's declared source, which apply still replaces whole and prune still removes. `kept` and `not_in_pack` go, and so do the printed words `no longer in it`, `kept:` and `edited by hand`. `shadows` stays as the field it is. Of the flags, `--take`, `--yes` over edited files and, for lines, `--prune` go, and `--yes` keeps one meaning, to apply without the confirmation. One flag stays, in a narrower form, because some files have no other door. `--replace <file>` sets the original's version of a file under `<home>/packs/<line>/` that differs: a role's copy, a script or the frame. Its card shows both texts whole. A stage file is changed by `flow set-stage`, a role of one group by the prompt card and a flow by `flow set`, and all three cards exist, so `--take` of a flow, a stage file or a role has nothing left to do.

What the card and the apps see. The words above are what `pack check` prints. The card carries the same plan in `run.pack.plan`, and the three apps read it (section 2.11). A file that `--replace` sets is a `change` on the card, since the apply writes it, so the desktop draws it as a change and the phones draw its diff. A re-apply whose objects are all `unchanged` or `differs` writes nothing, so the line the desktop prints over it, `Nothing changes.`, is true. The `differs` rows it does not draw are a gap in what it shows and no false word, and F1 closes the gap. The `governs` entry already says which flow the depot's beads follow once the plan is applied, so for a depot that keeps its pointer it names the flow the depot follows now, and the desktop draws it as already following. `docs/api.md` describes `run.pack` in prose and lists no field of it in a table, so by that page's own rule an added word raises no API number.

### 3.3 What apply creates, and what stays the yard's

| thing | when the yard lacks it | when the yard has it |
|---|---|---|
| a flow `<line>.<flow>`: its stages, outcomes and limits on rounds | created | `unchanged` where equal (`Flow.Digest`), else `differs`. The yard's flow stays; `flow set` changes it |
| a stage file under `<home>/flows/<line>.<flow>/` | written, filled with the yard's names | `unchanged` where the filled text is equal, else `differs`. The yard's text stays; `flow set-stage` changes it |
| the depot's flow pointer, for the depot's flow and for each type | set to the line's flow | left, and the plan names the flow the depot follows |
| a role file under `<home>/packs/<line>/roles/`, filled | written | `unchanged` or `differs`. `--replace` sets the original's, and the prompt card gives one group a text of its own |
| a script under `<home>/packs/<line>/scripts/` | written, mode 0755 | `unchanged` or `differs`. `--replace` sets the original's |
| the page frame, and the choice of it for the scope | copied, and chosen for the scope only where the scope has none | `unchanged` or `differs`. `--replace` sets the original's |
| the yardmaster section file and the setup notes | not copied. Prime reads them from the place the label names (section 3.5) | read from there each time |
| the default frame yardr reads from its binary (`pageframe`) | not applied. A scope uses it where none has chosen a frame | stays the fallback whatever is applied or left (`FrameFor`) |
| a group `<depot>-<group>` | created with the line's role or script, limit and `env`, and with the agent, model, arguments and members the apply is answered with | left whole. `unchanged` where it reads as the line says, else `differs`, naming each setting that differs: prompt, command, limit, kind |
| a yard group that takes a line's group (`--group`) | | `takes`, and nothing of it is set |
| a route | written for a stage that no route serves, with the line's group | left |
| a crew member | created with the role and the event kinds the line names | left whole. Its role, event kinds and binding are the yard's |
| a crew member's subscriptions: one for each stage it serves and for done beads of the depot, and the line's `subscribe` entries | created, each an object of its own | one the crew member lacks, by source and filters, is added. One it holds is left, whatever its scope |
| a hook's stage map, `says` and `makes` lines | lines added where the hook lacks them, the map and `as` set where the hook has none | left. A line already in the table is not repeated |
| open threads, calls, exposure to peers | the operator is asked for what the line wants and the yard lacks | not asked again for what the record holds an answer to, a no included, as today |
| a declared source, a connector's | added | replaced whole with `env_file` kept, as today |
| what the line says it needs (section 3.7) | copied to the record and checked | the record's list is the yard's. The original's is checked and said again |
| the agent product, model and permission arguments of a group the yard has; a people group's members; `env_file`, secrets and identities; a person | not a line's to say. Asked for at apply where the yard lacks the group | never touched |
| the depot's repository, test command and rules; settings; beads, notes and mail | not a line's to say | never touched |
| what the line dropped since the yard copied it | | never touched, never removed, and not listed |
| what the yard made that the line does not carry | | never touched and not listed |

What differs from today. A second apply no longer brings the original's change to a copy the yard never touched. Today it does, silently. A group's limit that the line names is no longer set again by every apply. Today `plan_groups.go` sets it each time, so a limit the operator raised goes back. A crew member's role and event kinds are no longer set again, the pointer is no longer moved, a new frame no longer takes the scope from the one it has, a script the yard changed is no longer overwritten on `--yes`, and a script nobody changed is no longer refreshed. Nothing the line dropped is removed. The cost of the first and the last of these is section 3.5, and for the script it is section 3.6.

### 3.4 Connectors, and the pack that carries both

A connector is an outside fact. A service's API, the commands that verify its webhooks and the declaration of its calls change when the service or yardr changes, and a yard that does not follow them stops working. A line is opinion: how work moves, which stage means which status, what a reviewer is told. That is why the direction keeps connectors as they are. They stay applied and updated as today. A declaration is replaced whole, the yard's `env_file` is kept, `--prune` removes a source a pack no longer carries with its command files, and the record keeps the declaration's sha256 and the answers about region bases.

The line between the two runs inside a pack by key.

- The `sources.<name>.declaration` file, the command files it names and the `presets` are the connector. They are applied as a connector is, whatever the pack's kind. The file format already allows a line to carry a declaration, and no shipped pack does.
- Everything else under `sources` (`as`, `hears`, `says`, `makes`), and `calls`, `open_threads`, `expose_to_peers` and `subscribe` on groups and crew, are the line's words about a connector. They are copied once, by the table above, into a hook or a grant that belongs to the yard.
- A pack of kind `source` is all connector. A pack that carries a way of working and is of any other kind, or of none, is all line, except for a declaration it carries.
- A pack that carries a page frame and nothing else, as `pageframe` does, is a frame alone, neither a connector nor a line. Its frame is the one yardr reads from its binary where no scope has chosen another (`FrameFor` in `internal/pack/framefor.go` falls back to it even when no pack is applied). Nothing in this design takes that fallback away. Applying a frame alone copies its frame in as any pack's frame is copied, and the record keeps `Frame` and `FrameActive` as it does today.
- A line that cannot work without a connector says so in `needs` (section 3.7). The plan then says `missing connector github` with the command that adds it, in place of today's "optional".

### 3.5 The original again, the compare, how an improvement arrives, and the label

Getting the original again is `yardr pack apply <line> --depot <depot>` by the rule of section 3.2. The folder is copied in again, what the yard lacks is created, and what the yard has stays. For an embedded line the name is enough, and for a folder or a URL the name is enough too, since the label names the place. There is no one yes that replaces a whole changed flow with the original's, which mckean left out (section 4.2). What replaces is a card for each file, below.

The compare is `yardr pack check <line> --depot <depot>`, which exists. A bare name is read again from the place the record names (`resolvePack`): a folder as it stands, a URL fetched with the credential the record names, and for an embedded line the copy in the running binary. So the compare reads the original from the place the label names, as it is now, and no copy of it sits in the yard. What is new is that each object that differs carries both texts. `--diff` prints the unified diff of each, with `store.Unified`, the function the card already uses for a changed file, and `--json` carries the yard's text, the original's text and the diff.

What the compare must show for an agent to do this well:

1. For each flow, stage file, role, script or group setting that differs, the yard's version and the original's as it is now, both whole, and the unified diff of the two. A group's prompt is the text of the file its `prompt` names, so a role edited by a card shows as the group's own file beside the line's role.
2. Which yard object answers to which of the original's, by the names the plan already uses (`<line>.<flow>`, `<depot>-<group>`) or by the answers the record keeps (`Groups`, `Crew`, `Flow`), so that a custom `--group` is found again and not guessed from a name.
3. What the original changed since the label's commit, where the label names a commit of a git folder. That is `git diff <commit>` in the folder, a git command the yardmaster can run itself, so B5 adds nothing for it.

The third item is the original as applied, taken from git where git has it and kept by nobody. Is keeping it worth it? mckean says no: nothing of the original is kept. A file the yard has not edited equals the original as applied, so the two-way compare already is the improvement alone, and that is the common case. Where the yard did edit, the agent may take back a line the yard removed for a line the original added since, and the operator reads every card, which shows the text proposed against the yard's own. A wrong guess costs one declined card. Keeping the original would cost a copy of every file at every apply (the starter is 16 files and 151 KB) and a digest of each in the record to find it, which are the sums this design removes. The case where keeping would pay is an embedded line, whose older text no folder and no git history of the yard holds. If the yardmaster's proposals for embedded lines turn out poor, keeping their applied text by digest in the history directory is a later step, and nothing above changes.

How an improvement arrives. The cost of copying once, in the direction's own words, is that an improvement to the original, a better builder role in the starter with a new yardr, reaches no yard by itself. mckean's answer (mail 264465) is that it arrives the way a skill's does. The folder is copied in again, and where the yard has edited a file no code merges. The yardmaster reads what changed in the original and proposes that change on top of the yard's version, one card for each file. The cards exist. `flow set-stage` carries a stage file's whole text and shows the text that stands beside the text proposed. The prompt card (`group set <group> prompt=`) carries a role for one group, and `flow set` carries a flow's stages and outcomes. Each is weighty. The stage file's card keeps the text it replaces in the history directory, and the role's card writes a new file and leaves the old one. A file with no card of its own, a role's copy under `<home>/packs/<line>/roles/`, a script, the frame, is set to the original's by `yardr pack apply <line> --depot <depot> --replace <file>`, which is what is left of `--take` (section 3.2). The yardmaster writes the merged text for a stage file or a role the yard edited, and the card carries it. A script has no card that carries merged text, so for a script the yard edited and the original changed the operator takes the original's or keeps their own. How the yardmaster learns that the original moved is section 3.6 for scripts. For the other files it is the operator asking, or the count in doctor's note.

The label. A line in the yard carries where it came from: its source, the version or commit, when and by whom the record was last written (mckean, mail 264473). That is what `yardr pack list` shows today. It is for a person to read and for the compare to find the original. Nothing acts on it and it is no link back, with one reading in section 3.6: for an embedded line the build in it says which yardr's copy the line was last settled against. Is the record still needed for anything else? Field by field, after the five steps:

- The label stays: `Source`, `Commit` and `Dirty` (or the build, for an embedded line), `At`, `By`, and `Auth`, the name of the credential a URL was fetched with, since finding the original needs it. `At` and `By` say when and by whom the record was last written. An apply that wrote something does that, and so do `--replace` and `pack seen`. An apply that wrote nothing does not.
- The answers stay, because a second apply asks nothing only while the record holds them. `Groups` and `Crew` say which of the yard's groups and crew members take the line's (`groupName` in `plan_groups.go` and `names` in `plan.go` read them), and without them a second apply that is not given `--group` or `--crew` again plans a new `<depot>-<group>` and a crew member of the pack's own name beside the ones the yard chose. `Flow` says which of a pack's several flows the depot follows. `DepotRoles` and the answers `OpenThreads`, `Calls` and `Expose` stay, so that a recorded no is not asked again. The compare uses `Groups`, `Crew` and `Flow` to say which yard object answers to which of the original's.
- The page frame and its choice stay (`Frame` and `FrameActive`, read by `framefor.go` and `framedpage.go`), and so do the setup flag and the yardmaster section's name. One reader still goes back to the original by the label. Apply copies neither the yardmaster section nor the setup notes (`Pack.Files` holds the roles, the scripts and the frame), so prime reads both from the place the label names, and for those two files the label is a link back. Copying them at apply, as a role is copied, would end it: `Pack.Files` gains two names and prime reads the copy. I left it out of the five (section 4.2).
- The needs list joins the record (section 3.7), as one opaque field.
- The object list, `Objects`, stays for now. After B2 nothing in apply compares against it, and B2 stops writing the sha256 of a line's files and flows into it, so the record holds names alone for a line (section 3.6 says why that matters). Eight files outside the planner still read the list (section 4.2), so cutting it is a bead of its own and is left out for now. A connector's objects keep the declaration's sha256 and the `SourceBase` answers.

So after the five steps the record is still one record for each line and depot, and `yardr pack list` is its view. What changes is that it stops being a baseline that apply holds the yard against.

This design replaces yardr-68s3. The customised state it wanted is the only state: everything an apply copied is the yard's, so there is no mark to keep and no browser edit to protect from the next apply.

### 3.6 Scripts, and the yardr version a line holds

Scripts follow the one rule of section 3.2, as mckean decided. A script a line carries is copied once, like a stage file. It is the yard's from then on, it shows in the compare when it differs, and a card changes it. The record keeps no fingerprint of a script and no copy of what was applied.

Two differences from a stage file stay, and they are the reason for the rest of this section. The card that sets the original's script shows it whole and is weighed `code`, as the card of a changed script is today (`packYesWeight`). And an old script can fail a stage. The starter's `merge-local`, which four of the six embedded lines carry, is one of the scripts that ship with yardr. docs/pack-api.md says of those that they "read some JSON by its layout" and that they "are yardr's own, and change with it". Today `pack apply` refreshes the copy, and two places name the ones that went stale: doctor's note for an embedded import, and, for a yard that installs from a checkout, the `packscripts` line of `yardr upgrade`. A release has no checkout, so a yard on the release road is told by doctor alone, and only if it reads doctor. Under section 3.2 a re-apply refreshes nothing it has, so the command that `packscripts` line names stops being true, and B2 changes it to the `--replace` card.

The version a line holds. mckean's idea (mail 264710) is that the line holds the yardr version it was built for, so that after an upgrade the yard knows to look. Two candidates exist already. The pack API number in `pack.yaml` (`api: 8`) cannot do it, since it rises only when the stable list grows, and these scripts use what the page calls internal. The label can. For an embedded line the record's `Source` reads `embedded in yardr <build>`, and an apply that writes nothing leaves the record as it was. B2 adds one thing to make that hold across an upgrade. It stops writing the sha256 of a line's files and flows into the record. An upgrade changes the original's digests, so a record that carried them would differ at the next apply that wrote nothing and be written again, which would move the build and quiet the note that the build is there to give.

What the yard sees after an upgrade. Doctor already checks each embedded import (`stalePackNotes`), and B5 narrows what it says and adds the build to the test. For an embedded line whose recorded build is not the running one, doctor names each script that differs from the one this yardr carries, with the card that takes it, and counts the other files that differ with the command that shows them. Doctor prints it as one note, wrapped here for reading:

```
note  packs  line starter in web, copied from yardr <old build>: scripts/merge-local differs from the one yardr <build> carries.
             Take it: yardr pack apply starter --depot web --replace scripts/merge-local
             Keep yours until yardr changes it: yardr pack seen starter --depot web
             6 other files differ: yardr pack check starter --depot web --diff
```

A build that says nothing about itself (`unknown`, as a test or `go run` binary has) is not compared. A line that is not embedded gets no such note. Its scripts are its author's, who has the stable commands of the pack API to build on, and the commit in a folder's label lets the yardmaster diff the original itself.

Taking and declining. Taking is the `--replace` card. The apply writes, so the record is written, and its build becomes the running one. Declining needs a write too, and a declined card writes nothing, so a decline cannot move the build by itself. It gets one command. `yardr pack seen <line> --depot <depot>` records that the operator has read what this yardr carries and keeps the yard's copy. It reads the record and writes it back with the running build in `Source` (`RecordPack` with `changed` true), and touches no file. It has no row of the run list, so it files on the general card (section 3.8) and needs no code in `internal/store`. The yardmaster puts the two cards in Decide together, and the operator approves the one they want and puts the other aside.

After either card the build in the record is the running one, so doctor is quiet until the next upgrade. At the next upgrade it names a script again only if the yard's copy still differs from the one that yardr carries. For a script the operator took, the yard's copy is the old yardr's, so that means only an upgrade that changed the script, which is mckean's "until an upgrade changes that script again". For a script the operator kept, the yard's copy still differs, so it is named once at every upgrade, whether or not that upgrade changed it. That is option A of Q1. Being exact about the second case needs one digest of the original's scripts, kept when the operator takes or keeps. It is one fingerprint for the line and not one for each file, and mckean's decision rules out the per-file kind. Q1 puts it to mckean.

### 3.7 What a line says it needs

Two details a line carries about itself stand side by side: where it came from and what it needs (mckean, mail 264481). In the yard, where it came from is the label of section 3.5, and an apply writes it. In a line's own file the `from` key is written by an export, so that a line that travels says where its copy began, and it waits with the export (section 4.2). `needs` is built now.

```yaml
needs:
  - gh
  - name: chrome
    any: [google-chrome, google-chrome-stable, chromium, chromium-browser,
          "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"]
    env: SHOT_CHROME
  - name: python3
    check: [python3, -c, "import sys; print(sys.version.split()[0]); sys.exit(sys.version_info < (3, 9))"]
  - connector: github
```

`needs` is one list for tools and connectors alike (mckean, mail 264509). It is called needs and not dependencies, since a dependency in yardr is one bead waiting on another. An entry that is a bare word is a tool. A mapping with `name` is a tool with options. A mapping with `connector` is a connector, met when the yard has a declared source of that name. Tools are no new kind of thing, and neither is the list. It is the `needs` a connector's declaration has today, widened. The key raises the pack API number to 9.

How a tool is checked. A bare name is looked up on PATH, as `source.NeedPath` does for a connector. `env` names a variable that may hold a path to the program, checked first. `any` lists names or absolute paths to try in place of the name, and the visuals pack's five places for Chrome are the case it is for. `check` is an argv run without a shell that must exit 0, and it is how a version is asked. The example is Python 3.9 or later. The check's first line of output is shown beside `ok`, so the operator sees `3.11.4` and not only a pass. yardr parses no version, since a `--version` flag differs by tool (`go version`, `java -version` to stderr) and a pattern that works for five tools fails for the sixth. A bare name, `env` and `any` only look for a file and extend what a connector's `needs` does today, with the same code.

A `check` runs a program, so it runs only for a line that is applied. It runs after the operator's yes, as part of the apply, and in `pack needs` and doctor for a line the yard has applied. It never runs when a line is only looked at, in `pack check`, `pack show` or the reading a card makes at the ask. Those print `its check runs when the line is applied` beside such a need.

Where it is said, and offered. At apply, what is missing is said and offered as a card: install the tool, or apply the connector (mckean, mail 264509). The result of an apply prints each missing need beside the command that meets it. For a connector that is `yardr pack apply github`, whose card exists, and its setup ask where it has one. For a tool it is the install command of section 3.8. The yardmaster files the card. It reads the result of an apply it ran for the operator, and the notes of doctor. `yardr pack needs [<line>]` prints the same for every line the yard has applied, and doctor says each missing tool as a note, not a fault, since a line may serve most of its beads without it. Nothing stops a bead, as mckean accepted.

On which machine. The check runs on the machine of the daemon, which starts every `herdr` and `exec` group of the yard. A group a `peer` works is another yard's, and the plan says `not checked here` for the tools a stage of that group would use. If a runner ever starts agents on another host, the check is asked through that runner, so a host answers for itself. That is a seam and is not built.

The yard's own list. The line is the yard's after the copy, so its needs are too. The record keeps the list as it stood at apply, and mckean accepted that the yard keeps its own. Doctor and `yardr pack needs` read it, and `yardr pack needs <line> add <need>` and `rm` edit it, one word each, which the general card carries. The list is one opaque field of the record that package pack reads, so `internal/store` learns no part of a need. The field is about 8 lines with its comment, which is why B6 is among the steps that grow a package with a ceiling (section 6).

### 3.8 The install card

What the card is. A run ask carries one yardr command. The general card carries any yardr command whole. So the install card is the general card carrying one yardr command whose last words are the program to run, an argv form of the `pack install` that connectors have:

```
yardr pack install design --tool ffmpeg -- brew install ffmpeg
```

The command runs the words after `--` as a program, without a shell, as the operator would at a terminal, and then runs the tool's check again and says whether the tool is there. The row of `pack install` takes one word (`parsePackInstall`), so this longer form parses as no row and files on the general card. The card shows it as it shows any command, whole, beside the yardmaster's own words, and the operator's yes has the server run it:

```
ask 41 · open · to mckean · asked by yardmaster
  WEIGHTY · code: a yardr command no card fits, shown whole and unchecked: yardr pack install design --tool ffmpeg -- brew install ffmpeg
  Install ffmpeg for the design line?
  yardmaster says: the line needs ffmpeg and this machine has none; brew is installed here.
  when you approve, the yard runs this as mckean, and nothing else:
      yardr pack install design --tool ffmpeg -- brew install ffmpeg
```

That is all the card is. It is the pick of yardr-ar8t that any command may ride, used where it already stands, and it needs no new card kind and no code in `internal/store`, which has no room. It is the one card whose command starts another program, and it shows that program and its arguments as the last words, so the operator reads exactly what runs. There is no limit on what the command may be and no second approval, by mckean's correction.

Who writes the command. The yardmaster, at apply, as mckean accepted. The line says `needs: ffmpeg` and no more. The yardmaster reads the missing tools and the machine (which package manager is there) and files the card. So the line stays free of per-system knowledge and the command always fits the machine in front of it. The cost is that an agent writes the command at that moment and it is in no digest, so the card is the only place it is read, which the direction accepts. Connectors keep the line-written `install` they have.

How the yardmaster learns. Not by a paragraph in its role. The base role has a hard ceiling of 8,192 bytes and holds 8,191, and the ceiling's own comment says that what a case needs beyond it is read from the docs it points at. So B5 and B7 put what the yardmaster needs, the compare, the card for each file, the `seen` card and the install card, in one section of `docs/packs.md`, and add one pointer to it in the role, about 60 bytes. Each bead pays for its bytes by shortening another line of the role. The base is seeded into a yard's own yardmaster prompt once, and `YardmasterBase` says that later changes reach new yards only. A yard that already has its prompt takes the pointer by a prompt card for that file, once, as it takes any change of a role.

### 3.9 No step needs a terminal

Requirement 6 is that every operator step of applying a line, seeing how the yard's copy differs from the original, getting the original again and installing a tool is a card in Footplate, and that none needs a terminal. The five steps meet it for every step they add or change, and this section goes through them. They leave three things as they are today. A line from a folder whose card passes 40 lines needs a terminal. So does choosing a depot's own roles over the line's, since a card cannot carry `--depot-roles` or `--pack-roles`. And a refusal still ends by telling the asker to type the command in one. One more limit is a size and not a step, and I do not design a road past it: a line whose texts pass the 1 MiB that a card holds is outside what a card carries. One bead that is not among the five closes the three things (section 4.2), and Q2 asks whether to take it with them. This section describes the five as mckean chose them, which is option A of Q2.

A card reads at the ask only places the yard has reason to read before the operator's yes, because the server reads the pack when the ask is filed. Those are a pack yardr embeds, a directory inside a registered depot's main checkout, and the packs repository's URL pinned with `--sha256` (`packCardReading` in `internal/store/runask_packdir.go`, design yardr-6maq, section 3.3). The yardmaster has a road of its own beside the card. On the operator's mail it applies an embedded pack or one of the packs repository, with no card, and the card's bounds do not bind it (the delegated apply, below). Each path in the table works in an unchanged Footplate, through the yardmaster's conversation and the cards in Decide.

| step | the path | new in this design |
|---|---|---|
| apply an embedded line, a folder inside a depot's main checkout, or the packs repository's line pinned | a card. For the embedded and the repository's, also the yardmaster's road on the operator's mail | nothing |
| a line whose card passes 40 lines | an embedded or repository line rides a summary card, with the whole text behind it. A folder has none: `runask_pack.go` says a directory "is whole or refused too: it has no summary shape". A folder as large as the starter's is a terminal's today | nothing. This is the first of the three things the five leave (Q2) |
| a line whose texts and diffs pass the 1 MiB that a summary card holds behind it (`MaxRunPackCarried`) | an embedded or repository line has the yardmaster's road, which is bound only by what the run's record can hold (`blob_max_bytes`). A folder past it has no road | nothing. A line past that is outside what a card carries, and applying it needs a terminal, or the line cut in two as the design and contributors lines already stand beside the starter. The largest line is `contributors`, 342 KB, a third of the bound, so no line shipped or in the packs repository is near it |
| a folder outside every checkout, a URL that is not the packs repository's, a URL with `--auth`, a line by its recorded name | refused at the ask, since the server would read a place the operator has not named before the yes. The line comes in through a depot's repository: a bead brings it in, it is reviewed and lands, and a card reads the folder | nothing |
| a plan that still asks | the refusal names each open question, and the asker puts each to the operator as a question card and files the apply card again with the flag that answers it | nothing. The words of the refusal still end by telling the asker to type the command in a terminal. They are the second of the three things (Q2) |
| a flag no card carries | `--prune`, `--take` and `--yes` over edited files go in B2, and `--replace` rides the apply card. `--auth` is the row above. `--source-env`, `--notify` and `--source-base` set up a connector, which stays as it is | `--depot-roles` and `--pack-roles` stay a terminal's, the third of the three things (Q2) |
| see how the yard's copy differs from the original | the yardmaster runs `pack check <line> --depot <depot> --diff` in its own pane and answers with cards. For a line from a URL it is the same, since the label names the URL and its credential. Doctor's note is the yardmaster's and a terminal's | B5 |
| take the original's version of a stage file, of a role for one group, of a flow | the yardmaster's `flow set-stage`, prompt and `flow set` cards, which exist | nothing |
| take the original's version of a role's copy, a script, the frame | the apply card with `--replace <file>`, which shows both texts whole | B2. A card that overwrites is whole or refused, at most 128 KiB of texts and diffs together (`MaxRunPackFiles`), so a file past that has no card. `merge-local`, the largest script, is 66 KB, and its card fits unless the yard's copy differs from it on nearly every line |
| keep the yard's script and stop the naming | `yardr pack seen <line> --depot <depot>` on the general card | B5 |
| see what a line needs, install a missing tool | the result of an apply says each missing need beside the command that meets it, and the yardmaster, which reads `pack needs` and doctor, files the install card, which is the general card | B6, B7 |

The delegated apply runs on the operator's mail with no card. Today it refuses `--take` and removes nothing, because an overwrite needs the operator's yes. After B2 nothing it does overwrites or removes what the yard has, so there is nothing left for it to refuse on that account. It still refuses a plan that asks something after the mappings, and the question cards answer it. Whether a request in the conversation is the operator's yes for an apply at all is Q3.

### 3.10 What it removes, and what it adds

Under this direction the removal is larger than the 1,200 lines the bead counted for keeping, dropping and taking, because `change` goes too. A line's flow, file, group, crew member, route and hook line are no longer compared and updated. The figures are by function, from section 2.1's reading, for the five steps. B2 carries the whole of the removal.

| where | removed | what goes |
|---|---:|---|
| `internal/pack`, planner and apply | about 1,225 | `keep.go` (about 170). The take, edited and record-sum paths in `plan_files.go` (about 100), `plan_flows.go` (the keep branch and `strays`, about 80) and `apply.go` (about 60). `dropped`, `gone` and `strandedTypePointer` for lines in `plan_drop.go` (about 200), and `prune` and `remove` for lines (about 55). The existing-group and existing-crew branches with `keptPrompt`, `keptCmd`, `keptEnv`, `splitCmd` and most of `setScript` (about 240). The replace and drop logic of hook lines, `ownDrops`, `others` and `leftOf` (about 150). `delegatedBefore` (about 50). The rest in `result.go` and the route replace (about 120) |
| `internal/store` | about 350 | most of `runask_packtake.go` (about 110), part of `runask_packyes.go` (about 40), the take and edited paths and the diff of a changed file in `runask_pack.go` and `runask_packcard.go` (about 80), the `--take` and `--yes` flags in `packmappings.go` (about 20), the before-state of the delegated run (about 100) |
| `internal/cli` | about 140 | `--take`, `--yes` over edited files, `--prune` for lines, the `kept`, `no longer in it` and `not in the pack` rows and their printing |
| `internal/server`, `internal/web` | about 120 | the held take in `carry.go`, part of the diff reading in `packreader.go`, the diff pages for what a line no longer changes |
| total removed by B2 | about 1,835 | range 1,450 to 2,250 |

What stays of the take path is the part `--replace` needs: the card that shows a file whole, the hash it is held to and the `code` weight. The recorded answers (the maps of groups and crew members, the flow, the answers about opening threads, calls and exposure) are not in the table, because they stay (section 3.5). The record's object list stays too, so the table holds no removal for it.

B2 adds about 150 lines: the `differs` rows, the `--replace` plan and the record without sums, across four packages. So B2 is a net of about 1,700 product lines fewer. B5 adds about 290 and removes about 40, B6 adds about 440 and B7 about 150. The five together remove about 1,875 product lines and add about 1,030, a net of about 850 fewer, 4 percent of the 21,466. The tests that go or are rewritten are about 4,000 of the 31,116 test lines under the apply machinery. The functions that name a keep, take, prune or edited word hold 4,668 as an upper bound, and the tests of `change` rows add to that. The new rules and features need about 2,400 test lines of their own, so the five net about 1,600 test lines fewer. Docs: about 200 lines of `docs/packs.md` go and about 360 are written across the five.

That is the plain answer to the bead's measure. If the gain wanted is lines, 4 percent is what the first five give. All of it is B2, which nets about 1,700 fewer, and B5, B6 and B7 add back about 840. The gain is in what a person has to understand. A line's plan has five words, `create`, `unchanged`, `differs`, `takes` and `shadows`, where it had ten. A card carries one flag where it carried two. A line has no `--prune`, and no state in which a copy is "kept", "edited by hand" or "no longer in" its pack. A re-apply cannot overwrite anything.

F1 is in the Footplate repository, so it is in none of these figures. It comes to about 50 product lines added and 90 removed, 80 test lines added and 30 removed, and 20 doc lines added and 20 removed, by the sizes of section 6. Those sizes are by analogy and not a count. The nearest work is footplate-tvr3, which drew a pack's plan on the desktop's card in 1,208 lines added and 45 removed over 13 files, a shot and the docs included.

The method has a limit, and it is the one the card API design met. This is a reading of function sizes and branches, not a deletion done and counted. The range is wide on purpose. The part least sure is `internal/store`, where the cards share wording with other rows. B2's note must say what it removed, and a removal that falls under the range's lower end is a reason to stop and ask.

### 3.11 The order

The yardmaster's proposal was drift first, then needs and the install card, with renaming last. mckean has since chosen the first five steps and their order: the words (B1), apply again (B2), the compare (B5), needs (B6) and the install card (B7), with the Footplate work that B2's card needs (F1) beside them. I keep that order, and here is why it holds.

1. Words first. B1 is documents only, costs a day, and lets every later bead use `connector` and `line` in its own text. It also shows whether the words hold before code carries them.
2. The compare follows B2 at once. The cost of copying once is that improvements stop arriving, and the answer to it, a compare an agent can read, should be there when the first re-apply surprises someone. So B5 comes third and not last, and it needs B2's words (`differs`) and its flag (`--replace`).
3. Needs and the install card come after, because they add value without changing apply, and they add lines where B2 takes lines out. B6 touches the same files as B2, `internal/pack/pack.go` and `plan.go`, both at their file ceilings, so builders take turns in them, and B2 takes lines out of `plan.go` first. B7 waits for B6, since the install command names a need.
4. F1 follows B2 directly. From B2's landing to F1's, the desktop's Line window still tells the operator to type `--take`, and a person who types it gets an unknown-flag error.

The order is one list: **B1, B2, F1, B5, B6, B7**. What each waits for:

| bead | waits for |
|---|---|
| B1 | nothing |
| B2 | nothing, and B1 only for words |
| F1 | B2 |
| B5 | B2 |
| B6 | nothing. It takes turns with B2 in `plan.go` and `pack.go`, and its `Needs` field needs the room in `internal/store` that B2 makes |
| B7 | B6 |

B2 is the largest step, and it has two halves: files and flows first, then groups, routes, crew members and hook lines. A builder may land it as two commits, or the yardmaster may file it as two beads. Either half leaves a plan that prints, and nothing in the design needs them to be one bead. The yardmaster asks mckean for the card that the growth of a package needs before the bead that grows one starts, and section 6 names them.

## 4. Left out, and the case against

### 4.1 Against it

- Copying once gives up an update. Today `pack apply starter` after a yardr upgrade brings the new stage files, roles and scripts to every depot that never edited them. After B2 the same command writes nothing it has, so the improvements of an upgrade reach an unedited yard as a card for each file that differs, or as one `--replace` card for the files that have no card of their own. For a yard whose operator does not edit, that is worse: one command became several cards. The direction is right if a yard's way of working is evolved by its operator, and mckean's own words put it there. It is wrong if lines are expected to track their originals. The compare and the yardmaster's cards soften this and do not remove it.
- The yardmaster's proposals are only as good as its reading of the compare. The two-way compare is weakest where the yard removed something, and for an embedded line, whose older text nobody keeps.
- The scripts that ship with yardr are not a way of working. They are part of the build, they use seams that change with it, and a copy made once goes stale against the binary. This is the one place where the direction can break a yard and not only leave it behind. Section 3.6 narrows it, since the yard is told after an upgrade and given the card. A yard that never reads doctor still meets the failed landing, which names its script. And a script the operator kept is named again at every upgrade (Q1).
- Two depots that apply one line share one flow, one set of role files and one set of stage files, by name. Evolving the flow for one depot changes it for the other. The second depot's apply finds the first one's files and writes none of its own, so its groups read roles filled with the first depot's names where the two differ. Today the trap runs the other way: the second apply rewrites the files the first depot reads, which is why packs.md tells an operator to import with the same `--crew` in every depot. The plan says `differs` for a copy that the second depot's fill would change, so it is seen before the yes, and the advice still stands. Naming each copy for its depot would end the sharing and would rename flows everywhere, so I left it out (section 4.2).
- Idempotence was a good property: "twice is once". It holds now only while the yard has not evolved the copy. The direction trades it away on purpose.
- On its own measure the saving is small. The bead's first idea would have removed about 440 lines of 21,466, two percent. The direction that replaced it removes more, because `change` goes too, and the five steps net about 850 lines, 4 percent. Anyone who wants fewer lines gets them from B2 and gives about half of them back in B5, B6 and B7.
- The cheaper alternative is to change the words and add `needs`, and leave apply as it is. That is B1, B6 and B7, about 590 product lines added and none removed. It leaves `kept`, `--take`, `--prune`, the record and its sums, and the terminal-only prune, and it does not answer mckean's point that nothing should compare against an original. If the aim is a smaller yard to understand and fewer lines, the apply change is where both are. If the aim is the tools, it is not.
- `--replace` is `--take` in a smaller coat. It stays because a role's copy, a script and the frame have no card of their own, and because a script's card must show it whole. If mckean would rather have no flag of that kind, the same take can ride the general card as a command of its own, which needs no code in the store but shows the command and not the text, so the operator would read the diff in the yardmaster's words and not on the card.
- Footplate is three code bases outside this repository. The yard's side of every step holds in an unchanged app, through the yardmaster's conversation and the cards in Decide, but a drawing is a bead in each app it shows in. The design asks for one bead, on the desktop, and none on the phones. An app is also released apart from the yard, so a yard past B2 meets desktops that do not yet know `differs` and still say the old words about `--take` until F1 lands.

### 4.2 Left out for now

mckean chose five steps first and then to look. Each of these is named, with what it would be, so that it can be taken up without reading everything again. The sizes are product Go, tests and docs, in lines, and are readings and not counts.

- **Reference files (B8).** A line's folder may carry pages its stages point to, check lists and pattern cards, that are neither stage files nor roles nor scripts. A pack has no key for them today. The proposal is a root key `reference`, a map from a short name to a markdown file of at most 64 KB, under the path rules of a role and with the same placeholders filled, copied by the rule of section 3.3 to `<home>/packs/<line>/reference/`, and printed by `yardr pack ref <line> <name>`, so an agent reads one when it chooses and no brief carries it. `pack check` would name a dead pointer in a stage file by file and line. The alternative, a general `files` key, would also give a script a data file, but it makes the format a place to put anything and does not say how an agent reaches the file. About +250 product, +500 tests, +80 docs, a new API number, no store, and it takes turns with B6 in `pack.go`.
- **The whole export, and copying a line between depots (B9, B12, F3).** Lines should travel whole (mail 264525), and `pack export` leaves out today what keeps them from it. It writes no crew member, no hook line or stage map, no declared source and no page frame, no `env` of a script group, no agent, model or arguments and no members of a people group, the last four on purpose. A stage whose beads are worked by a conditional route is written with no group. It lists a crew member's name inside a role or stage file by file and line and leaves it as text. It refuses a group a peer works, two flows that would get one name, a script group whose command does not start with a script's full path, and a role or stage file that already holds a `{{placeholder}}`. Three beads would carry it. The first (B9) makes the export write the rest, `from` and `needs` among it, and `env` only under a `--with-env` for a copy inside one yard (about +350 product, +700 tests, +60 docs; `export.go` is at its file ceiling, so it splits first; it waits for B2 and B6). The second (B12) is `yardr pack copy <from-depot> <to-depot>`, which exports to a folder under `<home>/lines/` and asks the pack apply card for it into the second depot, so that a card reads that folder as it reads a depot's checkout (about +135 product, of which `internal/store` +15 and `internal/server` +40, +300 tests, +40 docs; it needs the folder card below and mckean's card for the store and server growth). The third (F3) is a Copy this line to another depot control in the desktop's Line window that mails the yardmaster the request and names the command (Rust, about +110 product, +100 tests, +30 docs). Until then the operator can tell the yardmaster in its conversation to copy a line, on any of the three apps. A call that exports and files the card itself, as `UninviteTeammate` files a removal, would be about 140 lines in `internal/store`, `internal/rpc` and `internal/server`, and it needs mckean's card.
- **The printed words and the rungs after them (B11, section 3.1).** About 200 product lines and 600 test lines of strings, with no key and no number changed.
- **The folder summary card, the two role flags and the refusal words (B10).** After the five, a line from a folder inside a depot's checkout whose card passes 40 lines still needs a terminal, as does `--depot-roles` and `--pack-roles`, and a refusal still ends by telling the asker to type a command. One bead would close them. It gives a folder the summary card an embedded line has, held to its digest, carries the two flags as words on the card, and words each refusal as the flag that answers its question or, for a place no card reads, as a bead that brings the line into a repository. About +120 product (`internal/store` +60, `internal/server` +30, `internal/cli` +30), +400 tests, +40 docs. It grows `internal/store` and `internal/server`, so it needs mckean's card, and B2 makes the room. Q2 asks whether to take it with the five.
- **Cutting the record to the label, the answers and the needs (the rest of B4).** After B2 nothing in apply compares against the object list, but eight files outside the planner read it: `internal/pack/export.go` (which pack a governing flow came from, and `binds_as`), `internal/pack/frame.go`, `internal/cli/depotsource.go` and `depotsources.go` (which flows and roles come from a pack), `internal/store/line.go` (the label on a flow), `internal/store/runask_flow.go`, `internal/store/grouprename.go` and `internal/store/delegation_pack_record.go`. Each would read what stays instead. About +150 product and -600, +250 tests and -900, with `internal/store` about 200 smaller. It is the step most likely to split. A connector's objects are not touched.
- **The yardmaster section and the setup notes.** Apply copies neither, and prime reads them from the place the label names (section 3.5). Copying them with the other files ends that link back. `Pack.Files` gains two names, prime reads the copy, and the record's `Section` names it. About +40 product and +80 tests. `internal/pack/pack.go` stands at its file ceiling, so room is made first, as B6 does.
- **The compare as a read call for a device, and the desktop window that draws it (the read of B5, and F2).** The five give the compare to the yardmaster alone, as `pack check --diff`. A device may call only what `Calls` and `OperatorCalls` list, so a window would need one new read that joins `OperatorCalls` as `Line` does and is refused in the clear (`internal/store`, `internal/rpc` and `internal/server`, about +60, +30 and +40, and mckean's card), and F2, about +400 product Rust, +350 tests and +60 docs by analogy with footplate-tvr3, that draws each file that differs with its two texts. The phones' Line screens read and have no control that files a change to a line, so a control there would be a new kind of door in Swift and in Kotlin. The conversation with the yardmaster and the cards in Decide cover all three apps.
- A merge of the original into an evolved copy by code. The compare shows the difference and the yardmaster proposes it, one card at a time.
- One yes that replaces a whole changed flow with the original's, with the replaced text kept in history. `stage-history` holds stage files only, so roles, scripts and flows would need it too. About +250 product, a per-object answer on the card and in the terminal, and it brings back a small part of what B2 removes.
- Keeping the original as applied, in the yard (section 3.5).
- A dispatch gate that holds a bead when a tool is missing. The plan and doctor say it, and a stage may not use the tool for every bead.
- A version syntax in `needs`. `check` covers it, and a syntax would need a parser per tool.
- A line that includes another line's files, or a package manager for lines. The starter's reviewer role is copied byte for byte into four packs today, and that is the cost of there being no inclusion. The bead rules out a package manager and a registry.
- Naming copies for the depot (`web.default`), as `flow copy-repo` names its copies. It would end the sharing of section 4.1, and it breaks the compare by name and renames flows in every yard.
- Writing the yard's edits back into the folder a line came from, and a per-stage overlay of the original. Under copy once there is nothing to write back to and nothing to overlay.
- A second approval, a limit on what the install command may be, or any restriction on what the operator runs on their own machine, by mckean's correction.
- Signing a line (yardr-rbh6). Nothing here depends on it.
- Refreshing a copy on `yardr upgrade`. The `packscripts` line stays as it is, with its command changed in B2, and doctor's note of section 3.6 covers every embedded line on any install.
- A compare for a connector. Connectors are replaced whole.
- A card that reads at the ask a URL other than the packs repository's, or a folder outside every checkout, and a secret card for a URL's credential. Each would have the server read a place before the operator's yes (yardr-6maq, section 3.3). A place the operator names once, on a card, would be the way that does not, at about 340 product lines of which about 150 are in `internal/store`.

## 5. Open questions

mckean answered the first draft's questions on 28 Nov. Each answer is followed in the proposal. This table says where, so that nothing reads as open that is settled.

| first draft's question | answer | where |
|---|---|---|
| Q1. What apply does over a flow, role or file that reads otherwise | writes nothing and says `differs`. No sibling. One yes that replaces a whole flow is left out | 3.2, 4.2 |
| Q2. Who writes the install command | the yardmaster, at apply | 3.8 |
| Q3. How far to rename | the words in the docs and the glossary now. The printed words and the rungs after are left out. No key changes | 3.1, 4.2 |
| Q4. The order | five first (B1, B2, B5, B6, B7), then mckean looks | 3.11 |
| Q5. The scripts that ship with yardr | one rule with the stage files, and the line holds the yardr version it was built for | 3.6 |
| Q6. A line from a place no card reads | through a repository, as today. The words and the folder card are left out | 3.9, 4.2 |
| Q7. The yard's own needs list | the yard keeps it | 3.7 |
| Q8. Reference files | a `reference` key. Left out for now | 4.2 |
| Q9. A missing tool | said and offered as a card, not held | 3.7 |
| Q10. The original as applied | not kept | 3.5 |
| Q11. How a copy starts in Footplate | left out for now with the copy | 4.2 |

Three questions are open. Q1 and Q2 come from this draft. Q3 is the critique's question about the yardmaster's road.

**Q1. A script the operator kept: named at every upgrade, or only when it changes?** After the operator takes or keeps a script, doctor is quiet until the next upgrade (section 3.6). The question is what it says then, for a script the operator kept.

- A. It names the script again at that upgrade if the yard's copy still differs from the one the new yardr carries, whether or not that upgrade changed it. Nothing is kept beyond the build. A customised script is named once for each upgrade, with the two cards, and the operator quiets it with `pack seen`. (I would take this: it needs nothing kept, it costs a sentence for each upgrade, and a customised landing script is the thing that can fail a stage, so being told is not noise.)
- B. It names the script only when the original's scripts have changed since the operator last took or kept. `--replace` and `pack seen` keep one digest of the line's scripts as this yardr carries them, in one more field of the record. That is exactly mckean's "until an upgrade changes that script again". It is one fingerprint for the line, and the decision rules out the per-file kind. It costs about 15 lines, of which about 8 are in `internal/store`, and so mckean's card.

**Q2. Take the step that closes the three gaps the five leave in requirement 6?** The five leave three things as they are today (section 3.9). A line from a folder inside a depot's checkout whose card passes 40 lines has no summary card, so it needs a terminal. A card cannot carry `--depot-roles` or `--pack-roles`, so choosing a depot's own roles over the line's needs a terminal, for any line. And a refusal still ends by telling the asker to type the command in one. Apart from these, an embedded line and the packs repository's line need no terminal.

- A. Leave them for later (section 4.2). The five stay as mckean chose. A line from a folder meets a terminal, as it does today, until a later bead. (This is the order mckean chose.)
- B. Take the bead with the five, as a sixth step after B2. About +120 product lines, of which `internal/store` +60 and `internal/server` +30, +400 tests, +40 docs, and mckean's card for the store and the server first. It makes requirement 6 hold for every line the five apply. (I would take this: requirement 6 is a requirement, the bead is small, and it is the one place where the first group knowingly leaves a gap in it.)

**Q3. Is a request in the yardmaster's conversation the operator's yes for an apply?** Today, on the operator's mail, the yardmaster applies an embedded line or one of the packs repository with no card (the delegated apply, section 3.9). Requirement 6 reads as a card for every step. The design does not change who may authorise an apply.

- A. Yes, as today. The mail is the yes. After B2 that apply can only add, since it overwrites and removes nothing. (I would take this: the operator's own words are the approval, and the harm a misheard mail can do falls to a few objects the yard lacks, shown in the result.)
- B. No. The yardmaster files the apply card even when asked in conversation, so that every apply stands in Decide and the operator taps once more. It costs a line in the yardmaster's docs section and no code, and the delegated apply stays for the commands it already carries.

## 6. The beads it would become

Sizes are product Go, tests, docs, in lines, as +added -removed. In the order they would land: B1, B2, F1, B5, B6, B7, with what each waits for in the table of section 3.11. They keep the numbers that mckean's mail and the bead's notes give them. B3 is folded into B2, and so is the drop and prune half of B4. The rest of B4, B8 to B12, F2 and F3 stand in section 4.2 under the same numbers. A step "grows a core package" when it adds product lines to a package that has a ceiling in `docs/contracts/core.md`. The bead's limit is that such a step needs mckean's card first, and the closing paragraph of this section names them. A step that adds a key to `pack.yaml` first moves at least as many lines out of `internal/pack/pack.go` (at its file ceiling) as it adds. B2 carries a `BREAKING CHANGE:` line, since `--take` and `--prune` leave and a second apply stops updating. F1 is in the desktop app's repository, `footplate`, and its sizes are product Rust, tests and docs, in the same form, counted as that repository's numstat would count them. The two phone apps have no bead here (section 4.2).

**B1. Name connectors and lines in the docs and the glossary**
- Goal: a reader sees `line` and `connector` named in the glossary, the packs table and the intro of pack-api.md, with no change to a command or a key, and the glossary says that the yard as an MCP server for a person's own client is the yard's MCP and never a connector.
- Where to look: `docs/glossary.md`, `docs/packs.md` (first column of the table of packs, the opening paragraphs), `docs/pack-api.md`, `README.md`, `docs/README.md`.
- Done when: `connector` is a glossary entry with that rule, `line` and `pack` are reworded as in section 3.1, the packs table says what each pack is (a connector, a line or, for `pageframe`, a frame alone), and the gate passes.
- Size: docs +80 -40. Waits for nothing.

**B2. Make apply write only what the yard lacks, and say `differs` for what reads otherwise**
- Goal: an apply of a line creates what the yard lacks (flows, stage files, role files, scripts, the frame, groups, routes, crew members, subscriptions, hook lines, and the flow pointer where the depot has none), says `unchanged` for what reads as the line says and `differs` for what reads otherwise, writes nothing of the latter and removes nothing. `--take`, `--yes` over edited files and, for lines, `--prune` go, with the rows `kept`, `no longer in it`, `not in the pack` and `edited by hand`. `--replace <file>` sets the original's version of a file under the line's directory that differs, on a card that shows both texts whole. A line's record holds names and no sums, and a connector's keeps its declaration's.
- Where to look: in `internal/pack`, `keep.go`, `plan_files.go`, `plan_flows.go`, `plan_groups.go`, `plan_crew.go`, `plan_maps.go`, `plan_drop.go`, `apply.go`, `apply_delegated.go` and `result.go`. In `internal/store`, `runask_packtake.go`, `runask_packyes.go`, `runask_pack.go`, `runask_packcard.go`, `packmappings.go`, `delegation_pack_record.go` and `runask_flow.go` (`flowPackWords`, which the desktop copies). `internal/cli/pack.go` and `upgrade_scripts.go` (the `packscripts` command), `internal/server/carry.go` and `packreader.go`, and `docs/packs.md`. The words that name `--take` or say what apply keeps are in other files too, and `grep -rn -- '--take' internal` finds the rest.
- Done when:
  - A second apply of an unchanged line is all `unchanged` and writes nothing, and the record is not written (the `pack_unchanged` event).
  - Over a yard whose operator edited a stage file and raised a group's limit, those two are `differs` and nothing is written. A third apply says the same. The same holds after an upgrade changed a file the yard has, so the record, and the build in it, are as they were.
  - A group's limit, a crew member's role and event kinds, a route and a hook line the yard has are not set again. A subscription is added only where the crew member holds none of that source and those filters. What the yard cut is created again, and the plan says so before the yes.
  - `--replace scripts/merge-local` on a card shows both texts whole, weighs `code`, and on approval writes the original's script and records the apply.
  - `--take`, `--prune` for lines and `--yes` over edited files are gone, and `--prune` of a connector works as before. No word of the card or the run list names `--take`, and the `packscripts` line of `yardr upgrade` names the `--replace` command.
  - The plan on the card carries `differs`, with what differs in `changes`, and what the apply writes stays a `create` or a `change` on the wire, as section 3.2 says, so that an unchanged desktop draws a card that writes as a change and not as `Nothing changes.`.
  - The packs.md passage "A pack is a starting point", its section "Pruning" and the `kept` rows are replaced, and the note gives the lines removed, with the range of section 3.10.
- Size: product +150 -1,835 (`internal/store` about +30 -350), tests +800 -4,000, docs +70 -160. It is the largest step and has two halves, as section 3.11 says. It takes about 320 lines out of the store on balance. Waits for nothing.

**F1. Say what apply does now in the desktop's Line window, and draw `differs` on a pack's card** (in `footplate`)
- Goal: where the desktop says what applying a pack again does to a flow, it says the rule of section 3.2 (the yard's flow stays, and what the yard lacks is created) and shows no command to type and no `--take`. The Run card of a pack apply draws a `differs` object in a section of its own, as left as the yard has it, beside What changes and Already matches. A card from a yard that is not yet past B2 is drawn as before.
- Where to look: `footplate/src/model.rs` (`line_pack_words`, `LinePackWords`, the stage-source line that says applying the pack again keeps the copy, and the tests beside them, one of which says that when the card's words move these do), `src/window/line.rs` (`line_stages`: the rows `pack whose`, `pack keeps` and `pack take`), `src/window/ask.rs` (`plan_rows`, `change_lines`, `keep_lines`), `src/api.rs` (`RunPlanObject`), `src/shot.rs` (the run-pack shot), `docs/window.md`, `docs/look.md`, `docs/building.md`.
- Done when: the Line window on a flow a pack brought shows no terminal command and no `--take`, and says the rule of section 3.2 in the words of the card it matches. A card whose plan holds `differs` objects draws them in their own section, in the shot of that card. A card from a yard before B2 is drawn as it was. The window's docs say the same.
- Size: product +50 -90, tests +80 -30, docs +20 -20. Waits for B2.

**B5. Show the yard's copy beside the original, and name a stale script after an upgrade**
- Goal: the compare is readable by the yardmaster and a person at a terminal. `pack check --diff` prints, for each object that differs, both texts and their unified diff, and `--json` carries them, read from the place the label names. Doctor, for an embedded line whose recorded build is not the running one, names each script that differs with the card that takes it, and counts the other files that differ. `yardr pack seen <line> --depot <depot>` records that the operator has read what this yardr carries and keeps the yard's copy. One section of `docs/packs.md` tells the yardmaster how to read the compare and which card takes what, and the role points at it.
- Where to look: `internal/pack` (`result.go`, `plan_files.go` and `plan_groups.go`, where a `differs` object gains its texts), `internal/cli/pack.go` (the `--diff` flag, a few lines, once B2 has taken lines out of the file at its ceiling), new files `internal/cli/packdiff.go`, `packseen.go` and `doctorpacks.go` (`stalePackNotes` moves into the last one first, since `doctor.go` stands at its file ceiling), `internal/store/packs.go` (read only: `RecordPack` and `same`), `internal/pack/yardmaster_base.md` and its ceiling test, `docs/packs.md`, `docs/doctor.md`, `docs/chat.md`.
- Done when:
  - `yardr pack check starter --depot web --diff` over a yard that edited one stage file prints that file's unified diff, and `--json` carries `yard_text`, `original_text` and `diff` for it and for no object that matches.
  - The compare reads the original where the label names it: an embedded line from the running binary, a folder as it stands, a URL with the credential the label names. A line whose place is gone is refused with the reason `pack apply <name>` gives today.
  - Over an embedded import recorded under another build whose script differs, doctor names the script with the take card and the keep card and counts the other differing files. After `--replace` or `pack seen` doctor is quiet. A script equal to the binary's is not named. A build that says `unknown` is not compared.
  - `pack seen` writes the record with the running build and no file, files on the general card, and is refused for a line that is not embedded.
  - The role stays within its ceiling with one pointer added, and the section of docs/packs.md holds the rest. A yard that already has its yardmaster prompt takes the pointer by a prompt card.
  - A take card and a keep card are filed, shown in Decide and approved, in an isolated yard, with the yardmaster played by hand.
- Size: product +290 -40 (`internal/pack` +120, `internal/cli` +170), tests +450, docs +50. It adds to no package with a ceiling: there is no read call, and the general card needs no code in the store. Waits for B2.

**B6. Let a line say what it needs, and say what is missing**
- Goal: `needs` is a root key of a line's file, one list of tools and connectors, checked on this machine, said in the plan, in `yardr pack needs` and in doctor, each missing need offered with the command that meets it. The record keeps the list as one opaque field, and `yardr pack needs <line> add` and `rm` edit it.
- Where to look: `internal/source/install.go` (`NeedPath`), `internal/pack/pack.go` (the root keys, where room is first made), new files `internal/pack/needs.go` and `plan_needs.go`, `internal/store/packs.go` (the field), `internal/cli/doctorsources.go`, a new `internal/cli/packneeds.go`, `docs/pack-api.md` (API 9), `docs/packs.md`, `docs/doctor.md`.
- Done when: the visuals pack's Chrome and Python are written as `needs` and the plan says `ok` or `missing` for each. A connector entry says `missing connector github` with `yardr pack apply github`. `pack needs` and doctor agree. A tool a `peer` group would use is `not checked here`. A `check` runs after the yes at apply and in `pack needs` and doctor for an applied line, and `pack check` and `pack show` print that it has not run. A yard whose record holds the list edits it with `add` and `rm`.
- Size: product +440 (`internal/store` +8), tests +900, docs +120. It grows `internal/store` by about 8 lines, so it starts on mckean's card, and it fits under the unchanged ceiling only after B2 has landed. It takes turns with B2 in `pack.go` and `plan.go`. Waits for nothing.

**B7. Install a missing tool by a general card**
- Goal: `yardr pack install <line> --tool <tool> -- <program> <args>` runs the program without a shell and checks the tool again, and the yardmaster files it on the general card. The section of docs/packs.md that B5 added tells the yardmaster to do so, and the role's one pointer reaches it.
- Where to look: `internal/cli/packinstall.go`, `internal/source/install.go` (`InstallNeed`), `internal/store/runask_general.go` (no change expected), `internal/store/runask_packinstall.go` (`parsePackInstall`, which keeps taking one word), `docs/packs.md`, `docs/chat.md`.
- Done when: a card for the command shows it whole, and approving it installs the tool on a machine that lacks it, checked in an isolated yard. The card is refused as the general card is when the setting is off. The longer form files on the general card and the one-word form still files on its row.
- Size: product +150, tests +250, docs +40. No store. Waits for B6.

The steps that grow a package with a ceiling, named as the bead asks, are two. The yardmaster asks mckean for the card before either starts, and this draft's approval is not that card. B2 adds about 30 lines to `internal/store`, the `differs` words on the card and the `--replace` flag, and removes about 350 in the same bead, so the package ends about 320 smaller. I name it because the bead's limit is worded as growth, and the yardmaster may judge that a step which ends smaller needs no card. B6 adds about 8 lines to `internal/store`, the record's field for the needs list and its comment, and needs the card whatever B2 did. `internal/store` stands at its ceiling today, 101,675 of 101,675, so B6 fits under the unchanged ceiling only after B2 has landed. A ceiling does not fall by itself, so no bead here lowers it. B5 and B7 add to no package with a ceiling. B1 is documents. F1 is in `footplate`, whose gate holds no file ceiling (section 2.11). Three file ceilings matter. B5 writes its doctor note in a new file because `internal/cli/doctor.go` and `internal/cli/pack.go` stand at their file ceilings, and B6 moves lines out of `internal/pack/pack.go` before it adds the `needs` key. B2 only removes lines from `internal/store/runask_pack.go`, which stands one line under its ceiling.
