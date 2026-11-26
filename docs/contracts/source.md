# Source

A source is an outside service that the yard calls or hears from, and a declaration is the file that describes it. What crosses the core's edge here is that file, the commands it names, and the envelope that a delivery from the service becomes. The core owns the format of the declaration, the protocol its commands run under and the shape of the envelope, in `internal/source` (reading a declaration, calling and running), `internal/hooks` (receiving a delivery) and `internal/store` (the records). What one service's API or webhook looks like is how-and-what: it lives in the commands and declarations of the packs under `packs/` and in the operator's own. The hooks package compiles in two adapters only, `routine` and `token` (`internal/hooks/hooks.go:Adapters`); every other source is a declared one. The design is section 3.2.2 of [the design](../design/2026-11-24-yardr-h5wn-core-contracts.md).

## Form

Three forms cross.

**The declaration** is a YAML file read by `source.Parse` (`internal/source/declaration_parse.go:Parse`) into `source.Declaration` (`internal/source/declaration.go:Declaration`). It is at most `source.MaxDeclaration` bytes (256 KiB, `internal/source/declaration.go:MaxDeclaration`). Its root keys are `name`, `about`, `base`, `bases`, `base_template`, `path_template`, `credential`, `check`, `idempotency`, `calls`, `notify`, `hear`, `limits`, `thread`, `link`, `hook`, `identity`, `events`, `git` and `needs` (`internal/source/declaration_parse.go:reader.declaration`), and the reader refuses any other key at every level. A declaration may not take a name of `source.BuiltIn` (`internal/source/declaration.go:BuiltIn`). A pack brings a declaration with fewer keys than a yard's own: `pack.DeclarationKeys` for a pack for a depot and `pack.SourceDeclarationKeys` for a pack of kind source (`internal/pack/pack.go:DeclarationKeys` and `internal/pack/pack.go:SourceDeclarationKeys`), and `limits` in none. [integrations.md](../integrations.md#a-declared-source) says what each key means, and this page does not repeat it.

**The command protocol** is how the core runs a program a declaration names (`hook.normalize`, `thread.*`, `events.pr` and `events.ci`, `notify.image`, `call.<name>.build` and `call.<name>.run`, `credential.mint_command`). `source.Run` (`internal/source/command.go:Run`) starts the program from an argument list, with no shell, in a directory and under an environment of the core's making (`Declaration.CommandEnv`). It writes one JSON object to the program's stdin, or closes stdin when it has none to give. The program answers with exit 0 and exactly one JSON object of at most `source.MaxCommandOutput` bytes on stdout (64 KiB, `internal/source/command.go:MaxCommandOutput`), within `source.Timeout` (30 s, `internal/source/call.go:Timeout`), after which it is killed with everything it started. [pack-api.md](../pack-api.md#what-a-sources-command-gets) gives the same protocol to a pack's author.

**The hook envelope** is what a delivery becomes. `hooks.Envelope` (`internal/hooks/hooks.go:Envelope`) has the fields `source`, `type`, `action`, `actor`, `thread`, `mentions`, `assignee`, `delegate`, `text`, `url`, `delivery`, `message`, `parent`, `entity`, `fields` and `files`. For a declared hook, `Declared.Normalize` (`internal/hooks/declared.go:Declared.Normalize`) hands the hook's `normalize` command `{"headers", "body"}` (the body as text when it is not JSON) and reads an envelope from its answer. `wtx.delivered` (`internal/store/hooks.go:wtx.delivered`) records the envelope as one `hook` event. The `fields` are what the source says about where a delivery belongs (team, project, repo, label); matching a subscription's `--where` against them is per source, and is how-and-what.

## Owner

The core owns the format of the declaration, the command protocol and the envelope. Pack authors own the declarations their packs carry, and the operator owns a declaration added by hand with `yardr source add`.

## Version

None. A declaration has no version key (`version` is an unknown key to the reader), and the command protocol and the envelope carry none. A declaration that a pack brings is read when the pack is, and the pack says the API number it is written for with `api:`: `pack.API` (`internal/pack/pack.go:API`) is the highest this build reads, and a pack above it is refused before any other key is read (`internal/pack/pack.go:reader.later`). The declaration inside has no number of its own. A declaration added by hand is read at the running build's format. A declaration the build cannot read is refused with the file, the line and the key it cannot read.

## Change rule

A change to the declaration format, the command protocol or the envelope is made whole, in one release: every declaration the repository carries (the packs under `packs/` and the source carried in `internal/source/known/`) moves with it, and a change that makes a stored declaration unreadable comes with a migration of the stored ones (`migrate_NNN_*.go` in `internal/store`). Whoever stands at the other end of a declaration follows the core. The provider-specific Go that remains in the core (`HostedGoogleMint`, `internal/source/mint.go:HostedGoogleMint`; `internal/store/googlecred.go`; `CallHostedGoogle`, `internal/store/sourcecalls.go:CallHostedGoogle`) is how-and-what by the sorting rule of [core.md](core.md#what-is-core), although it stands in the core's packages.

## Stored data

Source records with their stored declarations, grants, keys and credential files, the call log, hook deliveries, links and threads. Of all the contracts this one touches the most stored data. A delivery is kept as a `hook` event with the envelope's fields.

## Rules

| # | rule | check |
|---|---|---|
| 1 | A declaration with a key the format does not know, at the root or inside another key, is refused whole, with the file, the line and the key. | `TestParseRefuses` |
| 2 | A declaration is read to its end: one run names every mistake, in file order. | `TestParseNamesEveryMistake` |
| 3 | A declared source does not take a name of `source.BuiltIn`, which is the adapters the build compiles in (less those a pack carries), `yardr` and `peer`. | `TestParseRefuses`, `TestDeclaredSourceNamesKeepClearOfAdapters` |
| 4 | A declaration in a pack has only the keys of `pack.DeclarationKeys`, or of `pack.SourceDeclarationKeys` in a pack of kind source, and `limits` in none; a key outside them is refused at its line, once. | `TestDeclaredSourceRefusals` |
| 5 | Every pack the binary embeds and every known source is read by this build's reader. | `TestEmbeddedPacksAreValid`, `TestKnownSources` |
| 6 | A command is handed the JSON object it is given on its stdin, or has its stdin closed when it is given none, and what it prints is read by the caller. | `TestRunAnObjectInAnObjectOut` |
| 7 | A command that does not exit 0 with exactly one JSON object of at most 64 KiB on stdout has failed: its error ends with the command's stderr and holds none of its stdout. | `TestRunFailures` |
| 8 | A command that does not answer in time is given up on, and killed with everything it started. | `TestRunTimeout`, `TestCommandTimeout` |
| 9 | A command sees `PATH`, `HOME`, `USER`, `LOGNAME`, the proxy and CA variables of the process that runs it, the credential under the name the declaration gives and `YARDR_SOURCE_BASE`, and nothing else of the yard's environment. | `TestRunEnv` |
| 10 | The envelope a `normalize` command prints is the core's to complete: its `source` is the declaration's name whatever the command printed, its `text` is cut to `hooks.MaxText`, its `mentions` is never nil, and one with no `type` is refused. | `TestDeclaredNormalize` |
| 11 | A hook secret that the source sends in a header or in the body (the `secret` verify scheme) is taken out before the `normalize` command is handed them. | `TestDeclaredNormalizeNotHandedSecret` |
| 12 | A delivery with a good signature is recorded as one `hook` event that carries the envelope, and the same body sent again is dropped. | `TestDeclaredDeliveryRecorded` |
| 13 | A delivery with a bad signature, a timestamp outside the window or a failing `normalize` command is refused and reaches no subscription. | `TestDeclaredDeliveryRefused` |

## Planned

- **planned** (design 3.2.2): a version for the declaration. A declaration carried inside a pack is versioned by the pack's `api:` number, and this page says so. A standalone declaration, added with `yardr source add`, is read at the running build's format. A declaration the build cannot read is refused with its reason: the key or line it cannot read, and the pack API the build reads.
