# Pack

A pack is a directory of files that a yard imports: `pack.yaml`, and the
flows, groups, roles, scripts, declarations and other files it names. This
contract is what a pack may say, run and hear, and the number that says which
version of that list a pack is written for. On one side is the core, which
owns the format and the number and keeps the records an import leaves
(`internal/store`). On the other is how-and-what: the packs themselves (the
directories under `packs/`, the packs repository, any other directory or URL)
and the planner that reads and applies them (`internal/pack`), which reaches
the yard through `store.API`. The user page for a pack's author is
[pack-api.md](../pack-api.md). It marks each seam stable or internal and lists
what each API number added, and this page does not copy it. This page holds
the rules that page's promises rest on, and the checks that hold them.

## Form

A pack is a file format and a directory layout, with the commands and scripts
a pack may call beside them.

- **The file.** A pack is a directory with `pack.yaml` at its root
  (`pack.File`, `internal/pack/pack.go:File`). `pack.Load` reads and checks
  it (`internal/pack/pack.go:Load`) and refuses it with every mistake named
  by file, line and key. `LoadDir` reads one from a directory
  (`internal/pack/pack.go:LoadDir`), `URLFS` serves one from a URL
  (`internal/pack/url.go:URLFS`), `LoadRepository` reads one from the packs
  repository (`internal/pack/repository.go:LoadRepository`), and the binary
  carries the packs of `packs/` (`packs/packs.go:FS`). Each reads the pack
  through `Load`.
- **The kinds.** A pack is for one kind of depot, `git` or `dir`
  (`pack.Kinds`, `internal/pack/pack.go:Kinds`), or is of kind `source` and
  is imported into the yard itself (`pack.KindSource`,
  `internal/pack/pack.go:KindSource`), or names none.
- **The number.** `api:` at the root of `pack.yaml` says which API the pack is
  written for. `pack.API` is the highest the build reads
  (`internal/pack/pack.go:API`).
- **The pin.** A pack's digest is a sha256 over each file the pack names, as
  its path, a NUL, its length, a NUL and its bytes, in path order
  (`internal/pack/digest.go:Pack.digest`). `pack apply --sha256` holds a pack
  to it (`CheckDigest`, `internal/pack/digest.go:Plan.CheckDigest`).
- **The scripts.** A program a group runs (`run:`) and a command a declaration
  names run as the yard's user. A group's script is given the variables of
  pack-api.md, What an exec group's script gets, and reaches the yard through
  the command line, run as `"$YARDR_BIN"`, with the commands, flags, exit
  codes and JSON fields of pack-api.md, The commands a script runs. A
  source's command reads and writes the JSON the source contract defines
  (design 3.2.2). A declaration a pack carries is that contract's.
- **The records.** Applying a pack writes records through `store.API`, and
  `store.PackImport` (`internal/store/packs.go:PackImport`) keeps what the
  import made. The delegated import of a pack from the packs repository
  (`pack.Repository`, `internal/pack/repository.go:Repository`) is
  how-and-what: it reads a pack through this contract and is no part of it.

## Owner

The core owns the API, which is the list of stable seams and the number. Pack
authors own what is in their packs.

## Version

`api:` at the root of `pack.yaml` is the pack API the pack is written for. It
is a whole number of 1 or more, and it is optional. A pack that names none is
API 1, so every pack written before the key reads as it did. A pack says the
lowest number whose list holds everything it uses. The build reads every
number from 1 to `pack.API` and no other. A pack that names a higher number
is refused before any other key is read, because an older build would call a
newer key unknown and blame the pack for what the build lacks.

`pack.API` rises by one in the release that changes the stable part of
pack-api.md, the part its What is stable section lists. That page lists what
each number added, under one heading for each. A declaration a pack carries
has no number of its own. A declaration added with `yardr source add <file>`
belongs to no pack and is read as the highest API.

The other contracts name this one as their model. It has one number, stated by
the core and read as a range, and a refusal that says which number the build
reads.

## Change rule

The core changes the contract whole, in one release, and a pack follows. No
old form is kept readable beside the new one, and there is no date by which
an old form goes, because it goes in the release that changes it.

- An addition to the stable part (a key, a value a key takes, a variable, a
  command or a flag, an event kind or a field of one) raises `pack.API` by
  one in the release that adds it, adds its heading to pack-api.md under What
  each number added, and puts the seam in that list.
- A removal or a change of meaning of a stable seam also raises the number.
  The build then reads the numbers from the one that names the change, so a
  pack at a lower number is refused before it is planned, with its reason and
  the number the build needs. The packs in the build, the declarations in
  them and the fixtures move in the same commit.
- A pack from outside the build (the packs repository, a URL, a directory) is
  not in the commit. Its author follows the core, and until the author does
  the yard refuses the pack with a plain reason that names the number.

## Stored data

- The import record of each pack and depot (`store.PackImport`, bucket
  `packs`, `internal/store/packs.go:bPacks`). It keeps where the pack was
  read from, who imported it and when, the names it mapped, and each object
  it made with the sha256 of what was installed, so that a later import can
  tell what the pack dropped and what was changed by hand since.
- The records a pack becomes: flows, groups, routes, sources, crew and their
  subscriptions, and the files installed under the yard's home.
- The records stay when a pack is refused for its number. A refused pack
  cannot be applied until it follows the core, and nothing it earlier made is
  undone.

## Rules

| # | rule | check |
|---|---|---|
| 1 | `api:` is optional, and a whole number of 1 or more. A pack that names none is API 1. Any other value is refused at the key. | `TestAPIIsTheNumberThePackSaysItIsWrittenFor`, `TestStrictness` |
| 2 | The build reads every pack API from 1 to `pack.API`. | `TestAPIIsTheNumberThePackSaysItIsWrittenFor` |
| 3 | A pack that names a number above `pack.API` is refused at the line of `api`, as its one mistake and before any other key, file or placeholder is read. The refusal names the number the pack is written for and the highest the build reads, and says to upgrade. | `TestStrictness`, `TestPackCheckFileCallAPI8` |
| 4 | Every way of reading a pack (a directory, a URL, the packs repository, the binary's own) reads it through `pack.Load`, so none of them skips rule 3. | unchecked |
| 5 | pack-api.md has one heading under What each number added for each number from 1 to `pack.API`, so the number cannot rise without its entry. | `TestPackAPIPageListsEveryNumberYardrReads` |
| 6 | `pack.API` rises by one in the release that adds to, removes from or changes the meaning of the stable part of pack-api.md. | unchecked |
| 7 | The JSON fields of the stable commands a script runs are the ones pack-api.md's tables list, each with its type. | `TestPackAPIDoc` |
| 8 | What API 1 lists keeps working. The frozen fixture packs of API 1 load as API 1, plan and apply to their goldens and digests, and apply again as unchanged. Their commands, hears and makes lines, and their script run as they did. | `TestPackAPI1Plans`, `TestPackAPI1Stubs`, `TestPackAPI1Hears`, `TestPackAPI1MakesAndSays`, `TestPackAPI1Script` |
| 9 | A pack's digest is over every file the pack names: a byte more in any is another digest, and a file it does not name changes nothing. A pin that is another digest is refused with both digests, and nothing is changed. | `TestDigestIsOfEveryFileThePackNames` |

## Planned

- **planned**, design 3.2.5: the build reads a range of numbers that can
  begin above 1. Today `later` (`internal/pack/pack.go:reader.later`)
  refuses a number above `pack.API` and nothing else, and no number has been
  dropped, so no pack is refused for being too old. The first removal or
  change of meaning that drops a number adds the lower bound, its refusal
  that names the number the build needs, and a row for it in the Rules
  table.
