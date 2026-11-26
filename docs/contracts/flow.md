# Flow

A flow is the stages a bead passes through and the moves allowed between them, and a stage is one step of it. What crosses the core's edge here is the files that describe flows: a flow file, the stage files beside it, the layout of `.yardr/flows/` in a depot's repository, and the `flows:` of a pack. The core's flow engine reads them, in `internal/store`. Depots and packs write them: the repository of a depot, and the packs under `packs/` read by `internal/pack`. [flows.md](../flows.md) teaches a user to write a flow, and this page links to it for the line format and does not copy it. The design is section 3.2.4 of [the design](../design/2026-11-24-yardr-h5wn-core-contracts.md).

## Form

**The flow file** is lines, one rule each, or one JSON object. `store.ParseFlow` (`internal/store/flows.go:ParseFlow`) reads both into `store.Flow` (`internal/store/flows.go:Flow`: the stages in the order written, the transitions between them, the terminal stages and the outcomes of each stage). `Flow.String` writes the line format back. The names are held to patterns: a flow's by `validFlowName`, a stage's by `validStage` and an outcome's by `validOutcome` (`internal/store/flows.go:validFlowName`, `internal/store/flows.go:validStage` and `internal/store/flows.go:validOutcome`). The vocabulary of outcomes is open, and the code names two of them, `done` (`store.DefaultOutcome`, `internal/store/flows.go:DefaultOutcome`) and `question` (`internal/store/flows.go:questionOutcome`).

**In a repository**, a depot's flows are `.yardr/flows/<name>.flow` and `.yardr/flows/type/<type>.flow`, and the stage files of a flow are Markdown beside it, `.yardr/flows/<name>/<stage>.md` and `.yardr/flows/type/<type>/<stage>.md` (`store.RepoFlowDir`, `internal/store/repoflows.go:RepoFlowDir`). They are read from the tip of the depot's base branch, never from a bead's branch or worktree, so that a bead cannot change its own lifecycle: a change takes effect once it has been reviewed and landed. A depot with no base is read from its directory as it is. [flows.md](../flows.md#repo-flows) says more.

**In the store**, a flow is saved under a name (`Store.PutFlow`, `internal/store/flows.go:Store.PutFlow`), a depot or a bead type points at it (`Store.SetFlowPointer`, `internal/store/flows.go:Store.SetFlowPointer`), and a bead may pin one by name in its `flow` metadata. The stage files of a stored flow are in the yard's home, `<home>/flows/<name>/<stage>.md`.

**In a pack**, `pack.yaml` has a `flows:` key (`internal/pack/pack.go:reader.pack`). Each flow names its `type`, if it is for one bead type, and its `stages`; each stage has `group`, `file`, `outcomes`, `manual` and `end`, and an outcome may be written as a mapping that limits its rounds (`to`, `max` and `then`). `reader.flow` (`internal/pack/pack.go:reader.flow`) reads it and `pack.Flow.StoreFlow` (`internal/pack/pack.go:Flow.StoreFlow`) turns it into a `store.Flow`.

**Which flow governs a bead** is decided by `resolveFlow` (`internal/store/flows.go:resolveFlow`): the bead's pin, else the store's pointer for its type, else the store's pointer for its depot, else the repository's `type/<type>.flow`, else its `default.flow`, else none, and a bead that no flow governs may move to any stage.

## Owner

The core owns the formats and the flow engine that reads them. Depots own the flows in their repositories and packs own the flows they carry, with their stage files.

## Version

None. A flow file has no version key. A flow in a depot's `.yardr/flows/`, and one saved with `yardr flow set`, is read at the running build's format, which is the line format of `store.ParseFlow` or its JSON form. A flow in a pack is read with the pack, which says the API number it is written for with `api:`; a pack above `pack.API` (`internal/pack/pack.go:API`) is refused before any other key is read. A flow the build cannot read is refused with the line and its reason, as `ParseFlow` refuses a line today (`flow line 3: missing stage before ->`). The beads that a repository flow the build cannot read would govern are neither claimed nor moved until the base is fixed: the depot follows the core.

## Change rule

A change to the line format or to `store.Flow` updates every flow the repository carries (the packs under `packs/` and this depot's `.yardr/flows/`) in the same commit, and migrates the stored flows. A new outcome name needs nothing in the core, since the vocabulary is open. A new outcome name that the code itself names, as `question` is, is a core change, and it goes on this page next to `done` and `question` under Form.

## Stored data

Flows, flow pointers, the flow a bead pins, each bead's stage, and its history in events. A stage that a flow loses strands the open beads in it: `Stranded` (`internal/store/flows.go:Store.Stranded`) reads that case, saving a flow or setting a pointer that would strand open beads is refused, and a bead in a stage its flow does not know may move only to the flow's first stage.

## Rules

| # | rule | check |
|---|---|---|
| 1 | A flow file is lines of `stage -> next` with optional outcomes and `stage terminal`, with `#` comments and blank lines, or one JSON object; any other line is refused, and what `Flow.String` writes reads back as the same flow. | `TestParseFlow` |
| 2 | An outcome is written `on <name>` after the stage it leads to and may carry a limit, `max <n> then <stage>`, in that order, with `n` a whole number of 1 or more; any other form is refused. | `TestParseFlowOutcomes` |
| 3 | A flow is refused when it is saved if a stage has two outcomes of one name, an outcome leaves a terminal stage, an outcome named `done` has a limit, a limit has a `max` without a `then` or a `then` without a `max`, or a `then` is the stage the outcome already leads to. | `TestPutFlowValidatesOutcomes` |
| 4 | A flow's stages are in the order written; a JSON flow that lists none and has two or more stages that no rule leads to is refused as ambiguous, and never sorted. | `TestJSONFlowStageOrder` |
| 5 | A flow with no name, a name or a stage that breaks its pattern, no stage, or a way out of a terminal stage is refused when it is saved, and a pointer needs a depot and a flow that exist. | `TestPutFlowValidates` |
| 6 | A bead that no flow governs may start in any stage and move to any stage. | `TestNoFlowPassesThrough` |
| 7 | A bead created with no stage starts in its flow's first stage, or in `new` where no flow governs it; one created in a stage the flow lacks, or in a terminal stage, is refused. | `TestFlowCreateStage` |
| 8 | A move that the flow does not allow is refused with the stages it allows, and changes nothing. | `TestFlowAllowsAndRefusesTransitions` |
| 9 | Entering a terminal stage closes the bead in the same write as the move, with the stage as the reason, and ends its session. | `TestFlowTerminalStageCloses` |
| 10 | A bead in a stage its flow does not know, or one reopened in a terminal stage, may move only to the flow's first stage. | `TestFlowStageUnknownToFlowCanRejoin`, `TestFlowTerminalStageCloses` |
| 11 | The flow that governs a bead is its pin, else the store's pointer for its type, else the store's pointer for its depot, else the repository's flow for its type, else its `default.flow`, else none; a pin that names no flow is refused. | `TestFlowResolutionOrder` |
| 12 | A depot whose repository has no flows has none; a checkout that git cannot read is an error for its beads, which are refused and not left ungoverned. | `TestFlowResolutionOrder` |
| 13 | A depot's flows and stage files are read from the tip of its base branch, not from a bead's branch or the working tree, and follow the base when it moves. | `TestRepoFlowsReadFromBase`, `TestStageFilesReadFromBase` |
| 14 | A repository flow that does not parse is listed with its error, and creating or moving a bead it would govern is refused with the file named; a JSON flow whose stage order is ambiguous is broken in the same way. | `TestRepoFlowsReadFromBase` |
| 15 | A stage file belongs to the flow whose directory holds it, so the same stage name in another flow is another file, and a file named after no stage of its flow, or in a directory with no flow file beside it, is stray. | `TestStageFilesReadFromBase`, `TestStrayStageFiles` |
| 16 | Saving a flow, or pointing a depot or a type at one, is refused with the beads named while an open bead it would govern sits in a stage that flow lacks; a pinned bead, a closed one and one that follows another flow do not stand in the way. | `TestPutFlowRefusesStrays`, `TestSetFlowPointerRefusesStrays` |
| 17 | A base commit that drops a stage of a repository flow strands the open beads in it: `Stranded` lists them with the flow and where it was read, and a flow that does not parse is a bad flow, not a stranding. | `TestStrandedByRepoFlowChange` |
| 18 | A pack's flows keep the order written, and a stage key the reader does not know, an outcome or a `manual` move to a stage the flow lacks, a flow with no `end: true` stage, and an end stage with a way out are each refused with the file, the line and the key. | `TestStrictness`, `TestStageOrderIsFileOrder` |
| 19 | The flows of every pack the binary embeds, and this depot's default flow, are read by this build. | `TestEmbeddedPacksAreValid`, `TestParseFlowOutcomes` |
| 20 | An outcome name that the code itself names is listed under Form. | unchecked |

## Planned

- **planned** (design 3.2.4): the pack flow versioned by the pack API. A flow that arrives in a pack is versioned by the pack API, and this page says so: a change to the keys of `flows:` in `pack.yaml` raises `pack.API` in the release that makes it, as a change to any other stable key of a pack does. Today the flows keys are part of the pack API's first number and no number has added to them.
