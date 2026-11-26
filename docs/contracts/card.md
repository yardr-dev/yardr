# Card and ask

An ask is a question stored as data on a bead. A run ask proposes one yardr
command, and its card is what the person approving it reads: the command
whole, and the yard's sentences for what it would change. This contract is
the ask as the core's data, what the card holds, and what one command
supplies to be proposed. On one side is the core, which keeps the ask and does
the approval, the check that a card is still true, and the carrying out
(`internal/store`, and `internal/server` for the carrying out). On the other
are the readers of an ask (the web, chat, mcp, footplate, and every other
client of [the view](view.md)), who render a card without knowing its command,
and the commands that can be proposed, each of which knows its own words. Today
the commands' rows are written in `internal/store` too, in the same functions
as the approval, which is the mixed case core.md's first case names.

## Form

Two forms cross, and today both are Go in `internal/store`.

**The ask** is the core's data. A question is a note of kind `ask` whose JSON
is `Ask` (`internal/store/asks.go:Ask`), and what became of it is a note of
kind `answer` whose JSON is `Answer` (`internal/store/asks.go:Answer`): a pick,
words, put aside, declined, taken back by its asker and, for a run ask, the
record of what the yard ran. `ParseAsk` and `ParseAnswer` read them
(`internal/store/asks.go:ParseAsk`, `internal/store/asks.go:ParseAnswer`) and
`ResolveAsks` says where each ask stands (`internal/store/asks.go:ResolveAsks`).
A client reads asks with `UserAsks` and `Bead`, answers with `AnswerAsk`,
`DeferAsk`, `DeclineAsk` and their guarded forms, and approves with `ApproveAsk`
([api.md](../api.md)). An `Ask` may take one of several shapes besides a plain
question: a secret (`Secret`), a run (`Run`), a setup (`Setup`), a depot's push
(`DepotPush`), a mapping of an unknown sender (`Identity`) or a pairing's check
(`Pair`). The rules below are those of the ask and of the run ask.

**The card** is the run ask's data for the person approving it. There is no
Card type: it is `AskRun` (`internal/store/runask.go:AskRun`). `Argv` is the
command as the yard parsed it, in one order however it was typed. `Does` is
the yard's sentences for what it changes, `Back` the command that takes the
change back and `BackBy` who may run it. `Seen` holds what the yard held of
each thing the command names, whole, and an approval is held to it. `Plan`
is the change field by field, and `Carried`, `Pack` and the other fields hold
the bytes an approval installs, for the rows that carry any. `General` holds
the words of a general card, the command no row fits, which the yard shows
whole and unchecked (`generalRow`,
`internal/store/runask_general.go:generalRow`). The yard writes all of it from
its own records, and none of it from the asker's words. A client is sent
`RunAsked` (`internal/store/runask_proposed.go:RunAsked`) and the card's weight
(`AskWeight`, `internal/store/weight.go:AskWeight`).

**The row** is what one command supplies to be proposed: `runCommand`
(`internal/store/runask.go:runCommand`), a struct of functions. The rows are the
list `runList` (`internal/store/runask.go:runList`) and what the `init()` of
files beside it append, and `parseRun` finds a row by the words of the stored
command (`internal/store/runask_parse.go:parseRun`). A row's functions are of
three kinds.

- Those that read only the typed words take no store: `parse`, and `whose`,
  `sole` and `link`, which read the parsed command.
- Two read before the write and take the `*Store`: `ready`, for the
  repository's flows, and `fetch`, for the pack a URL names, held to its pin.
  They exist because git and the packs repository must not be read inside a
  write.
- The rest are bound to the write and take its private `*wtx`: `check`,
  `card`, `weigh`, `stale`, `plan` and `run`, and the forms that take the ask
  whole (`checkRun`, `cardRun`, `runRun`).

**The write a row runs in.** The card is drawn again, compared with the stored
one and run inside the one write that approval opens, so the yard cannot change
between the check and the run, and an error anywhere rolls the write back, so
that nothing ran. This is the rule that any move of the rows must keep. Today
it holds as follows. `ApproveAsk` (`internal/store/runask.go:Store.ApproveAsk`)
reads before the write what the card needs of git and of the packs repository
(`approvalRepos`, `internal/store/runask_prewrite.go:Store.approvalRepos`),
opens the write with `writeVia`, and hands those readings into it. In that write
`approveAsk` (`internal/store/runask.go:wtx.approveAsk`) finds the row by the
stored words, checks who is approving, draws the card again with the row's
`card`, compares its `Seen`, `Does`, `Back`, `BackBy` and `Plan` with the stored
card's and calls the row's `stale` where they differ, weighs it again, holds the
approval to the weight it showed, runs the row, records the carrying out of what
runs after the write, and records the answer. Running a row through `store.API`
after the approving write instead would let the yard change between the check
and the run, and that recheck is what makes a card safe to approve.

Some approvals take another path.

- Four kinds of command have an effect outside the store: registering a hook
  with its service (`approveHookRegister`,
  `internal/store/runask_hook.go:Store.approveHookRegister`), a test message to
  a user (`approveSourceCheck`,
  `internal/store/runask_check.go:Store.approveSourceCheck`), a call of a source
  (`approveSourceCall`, `internal/store/runask_call.go:Store.approveSourceCall`)
  and a pack install (`approvePackInstall`,
  `internal/store/runask_packinstall.go:Store.approvePackInstall`). Each is
  approved in phases that no one write holds. A first write holds the approval
  to the card, the effect runs with no write open, and a last write records the
  answer (`finishPhasedRun`,
  `internal/store/runask_finish.go:wtx.finishPhasedRun`). A failed effect leaves
  the ask open, and approving again retries it, but for a pack install that
  stopped after some of its installs, which asks for a new card for the rest.
- A device pairing is approved inside the usual write, under the pairing lock
  (`approveDevicePair`,
  `internal/store/runask_devicepair.go:Store.approveDevicePair`).
- What runs after the write (a pack apply, an up, a source add with command
  files, a depot add, a teammate invite or removal, a general card) leaves a
  carrying record in the approving write (`needsCarry`,
  `internal/store/carry.go:needsCarry`; `recordCarry`,
  `internal/store/carry.go:wtx.recordCarry`). The record is `Carry`
  (`internal/store/carry.go:Carry`) in the `carry` bucket, and the server
  carries it out (`internal/server/carry*.go`).

## Owner

The core owns the ask, the approval, staleness and the carrying out, and the
shape of a row. Each command's owner owns its row: its words, its card
sentences and its run.

## Version

None of its own. The ask's JSON is on the wire for footplate and every other
client: `UserAsks` and `AnswerAsk` are in `rpc.Calls` and `ApproveAsk` is in
`rpc.OperatorCalls`. So the ask follows the view contract's rule (view.md,
Version).

## Change rule

- A change to the shape of the ask or the card, which clients read, follows
  the view contract's change rule. It is made whole, in one release, and the
  client follows.
- A new command on the card list is today a `runCommand` in `internal/store`,
  added to `runList` by its own file's `init()` or in the literal, with a test
  that drives its card and its approval. It keeps the rule of the write above.
- A change to the sentences of a row's card, or to what its `Seen` holds,
  makes every open card of that command stale after the upgrade, because an
  approval compares them. Each such ask must be asked again.

## Stored data

- Ask and answer notes. A run ask's note holds `AskRun`: the stored arguments
  with the sentences and the fingerprints approval compares.
- The carrying records in the `carry` bucket, and what the answer of an
  approval holds while a command is carried out.
- A note written before a field existed reads back with it empty, and an older
  build ignores a field it does not know.
- When a row's card sentences change, every open card of that command goes
  stale on upgrade, and nothing is lost. The ask is asked again. A stored card
  whose arguments the build cannot read is refused on approval with its
  reason, and never carried.

## Rules

| # | rule | check |
|---|---|---|
| 1 | An ask is a note of kind `ask` that `ParseAsk` reads, with the yard's own option last. It is refused, and nothing is stored, without a question, without context, with an option that lacks its consequence, or when the writer names the yard's own option. | `TestAddAsk` |
| 2 | A person answers an ask with a note of kind `answer` that names it: a pick, words (where the ask has no options), putting it aside for 24 hours or until its asker's next note, or declining it in words, which ends it. A newer pick takes the place of the older for display, and every pick stays a note. | `TestAnswerPick`, `TestAnswerWords`, `TestAnswerLater`, `TestAnswerDecline` |
| 3 | A run ask is stored as the yard writes it: the command in one form however it was typed, and the sentences, the command that takes the change back and what the yard saw, all the yard's, whatever the asker sent. | `TestRunAskIsStoredInTheYardsForm` |
| 4 | A run ask is refused, and nothing is stored, for a command not on the list, a character no word of the list has, a command that changes nothing or needs no approval, an option, a secret or an about-bead beside it, an addressee who cannot approve it, and a writer that is no crew member or user. | `TestRunAskRefusals` |
| 5 | The card is the stored ask's own. The notes of the bead, as prime and review print them, are the card, and the queue's row and the bead's ask show the command and the weight the yard wrote, the queue's row with the yard's sentences and the command that takes the change back. | `TestRunAskCard` |
| 6 | The weight of a card is the yard's. What a writer sends for it is thrown away. | `TestWeightIsTheYards` |
| 7 | Nothing but `ApproveAsk` approves a run ask. A pick, words and an answer that says what ran are refused. | `TestRunAskTakesNoOtherAnswer` |
| 8 | Approving a command is the operator's. Another user, the asker and nobody are refused, and so is an ask that proposes no command or was declined or taken back. A refused approval records nothing and changes nothing. | `TestApproveAskRefusals` |
| 9 | A command runs once for one ask, and an approved ask takes no other answer. The approval records one answer that holds the command and what it printed, the change's own event names the bead and the ask, and the asker's inbox is told. | `TestApproveAsk` |
| 10 | An approval is held to the yard the card was written from. Where the card the yard draws again differs from the stored one, in what it says or in what the command names, nothing runs and the ask stays open. A change that neither covers leaves the approval good. | `TestApproveAskRefusesWhenTheLimitChanged`, `TestApproveAskRefusesAnotherLinkOfTheSameName`, `TestApproveAskHoldsWhenSomethingElseChanged` |
| 11 | A card is weighed again at approval. One whose weight is not the one it showed is refused, and a weighty card is approved only by a call that says its person saw the weight. | `TestWeightIsHeldAtApproval` |
| 12 | An approval runs the command as the yard stored it. A stored form the yard did not write runs nothing. | `TestGeneralApprovalCarriesOnce` |
| 13 | A stored card whose arguments the build cannot read is refused on approval, with the reason, and is never carried. | unchecked |
| 14 | The card is drawn again, compared and run inside the one write that approval opens, and an error anywhere in it rolls the write back. | unchecked |
| 15 | An approval whose command runs after the write leaves a carrying record in that write. One server boot claims it, only that boot completes it, and a claim of a dead boot is taken up at the next start. | `TestCarryRecordClaimAndComplete`, `TestCarryResumeAndFail` |
| 16 | A client reads the ask and its card as [api.md](../api.md) promises: each field of the calls' tables is in the answer, with its type. | `TestAPIContract` |

## Planned

What the design decided and has not built (design 3.2.6, decision 3). The rows
move after the deletions of card types are done, so that nothing is moved that
is about to be deleted.

- **planned**, design 3.2.6: a card row interface in Go, replacing the
  function fields of `runCommand`. Its methods are the same three kinds. Those
  without a store are `Words`, `Use`, `Description` and `Parse`, and the
  optional `Whose`, `Sole`, `Link` and `Commas`. Before the write, and
  optional, are `Ready` and `Fetch`, on a `store.Before`. Inside the write,
  on a `store.Tx`, are `Check`, `Card`, `Weigh`, `Stale` and `Run`, and the
  optional `Plan` and the forms that take the ask whole.
- **planned**, design 3.2.6: `store.Tx`, an exported interface for the write.
  A row receives it as an argument and never makes one: it has no method that
  opens, commits or keeps a write, and the store hands it out only inside its
  own write. Its methods are the in-write forms of the store functions that
  rows call today, and the reads a card makes of records. It gives a row no
  `API`, since a row that called `store.API` from inside `Run` would open a
  second write inside the first, which bbolt's one writer does not allow. This
  page then lists its methods, and a new one is a line there.
- **planned**, design 3.2.6: `store.Before`, a narrow read handle for `Ready`
  and `Fetch` that opens no write. What they return the store hands into the
  write as it does today.
- **planned**, design 3.2.6: `ApprovedOn`, the exported form of `approvedOn`
  (`internal/store/runask_proposed.go:approvedOn`).
- **planned**, design 3.2.6: the rows are handed to the store when the server
  starts, as an argument of `server.Run`, which gives them to the store it
  opened before the socket is served. They are no longer added by the `init()`
  of the store's own files, and they live beside the commands they card. A row
  in `internal/cli` is in the list's own package, and a row in a package a
  command calls is imported as that command already imports it. A store test
  of the machinery registers a test row of its own, and a row's tests live
  beside the row.
- **planned**, design 3.2.6: a table of the rows on this page. A new command on
  the card list is then a row in its own package and a line in that table, and
  the core does not change.
