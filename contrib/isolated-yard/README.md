# contrib/isolated-yard

A throwaway yard with its own herdr server, for testing anything that starts
agents without touching your own herdr session or yard. Run it from the
worktree under test:

```
contrib/isolated-yard/isolated-yard up        # builds it, proves it, prints its directory D
D/e yardr init --harness claude --start       # every command of the test goes through D/e
D/e yardr bead list
contrib/isolated-yard/isolated-yard down D    # stops what runs in it, removes D
```

`up` builds yardr from the current directory into `D/bin`, with a stand-in
`claude` (`fake/`) and a copy of herdr, and starts a herdr server of its
own. It then checks, in a pane of that herdr, that `command -v claude` is
`D/bin/claude` and that the agent list is empty, and prints both; if either
fails it tears the yard down and exits 1. `D/e` is `env -i` with `HOME`,
`XDG_CONFIG_HOME`, `XDG_STATE_HOME` and `YARDR_HOME` under D, and
`__CF_USER_TEXT_ENCODING` from the caller's environment (without it a fresh
binary's first run under the codex sandbox stalls in dyld, before main),
so nothing else of the calling session (no `HERDR_SOCKET_PATH`, no `YARDR_*`
but the three below) reaches a command run through it. The first two are
`YARDR_TEST_OWNER` and `YARDR_TEST_HOME_CHECK`, with which a Go test binary
names itself to the servers it starts (`storetest.NameTestBinary`): `D/e`
carries each that the environment making the yard has, so the yard's server
and daemon stop within a few seconds of that binary's end, however it ended (a
kill or a timeout runs no cleanup). The third is `YARDR_TEST_START_WAIT`, the
long wait the same binary gives the `yardr` a test runs for a server it starts
(a command that finds none starts one, and the real wait is ten seconds, which
a loaded machine does not always leave); only that `yardr` reads it, and the
server does not get it. A yard made by hand has none of the three, and its
`D/e` carries none. The herdr server of an `up` yard and the stand-in
agents in its panes do not look for the owner; the yard's watchdog does (see
"When the test binary is killed", below).
`D/h/.profile` sets `PATH` to the same directories, so the
yard's login shell, which yardr asks for a program its PATH lacks (`yardr
doctor`, and `yardr up` for herdr), finds nothing of the machine's own: a
trial of that lookup writes the file itself, with directories that hold no
real herdr or agent. Use agent kind `claude` only: `D/agents.log` shows what each
stand-in was started with. Started as `claude -p <prompt>` (what `yardr
doctor --try` and the onboarding's "Check that claude answers?" run) the
stand-in prints `ok` and exits; with a file
`D/print-fails` it prints that file's text on stderr and exits 1 instead.
Started as `claude auth login` (what `yardr harness login claude --json`
plays) it prints what Claude Code's login command prints on pipes, with an
address of no host, and reads one line: `good#code` is the login, which
removes `D/login-needed` and `D/print-fails`, and any other line ends it
with `Login failed: Request failed with status code 400` and status 1.
With a file `D/login-needed` a stand-in started in a pane sits at a login
screen (Claude Code's menu of login methods: `Select login method:` with the
selected option carrying `❯`, then `Paste code here if prompted>`; no prompt
box) and takes what it is sent without showing it, holding its first prompt
until it is ready; removing the file is the login, and the prompt box and
held prompt come. `D/agents.log` says `held a prompt` once it holds one, so a
trial that sends a prompt and then logs it in waits for that line: a prompt
not yet read when the file goes is typed after the login, not held. With a file
`D/answer` a stand-in says that file's text (as `⏺ <line>`) to every prompt
it takes and waits for input again, as a harness does that stops on an error
of its own (`API Error: Repeated 529 Overloaded errors`). With a file
`D/busy` a stand-in that takes a prompt stays in that turn until the file is
removed: it shows the line Claude Code draws while it works and no prompt
box, so herdr says `working` of it. The box
leaves the cursor after its `❯`, so text sent to the pane and not yet
submitted shows on that line, where yardr reads an agent's input. A stand-in
whose first prompt asks it to greet a user by mail, as the yardmaster's does,
sends that mail. The script's header says why each of these holds.

The stand-in runs no command a prompt asks for, and its pane has no shell to
type one into. To run a command from an agent's own pane, as the session
yardr started there (the store tells such a caller by the pane's shell),
write it to `D/run` before the session starts. A stand-in runs each line of
that file (`sh -c`, with its own environment and directory, no input) when
it takes its first prompt, and appends the line, its output and its exit
status to `D/run.out`, each marked `pid=<the stand-in's> session=<its
YARDR_SESSION>`; then it draws its box and goes on as before. Every stand-in
that takes a first prompt while the file is there runs it, the yardmaster's
too: write it once those you do not mean have started, and remove it after.
No file, nothing runs.

`isolated-yard bare` makes the same yard without a herdr server and prints D
alone: for what starts no agent (a server, a daemon, the CLI), and where
herdr is not installed. Nothing in it can open a pane: `yardr up` there
finds no herdr, on PATH or with the login shell, and says so. It ends with
`down` too.

`sources/` is a stand-in for the Linear and GitHub APIs, for testing packs
and hooks in the yard. With `-github-app-key D/app.pem` it is a GitHub App
too: it writes a throwaway private key there and gives installation tokens
for the repositories of `-github-app-repos`, so a crew member whose env_file
has `GITHUB_APP_ID=12345` and `GITHUB_APP_PRIVATE_KEY_FILE=D/app.pem`
answers as `yardr[bot]`. It also takes messages for a person at
`/1/messages.json`, as Pushover does, for a declared source that tells a
user outside the yard (`yardr user set <name> notify=<source>`), as a JSON
body or as a multipart form with a picture for its `attachment` part (at
most 5,242,880 bytes, with a file name and a content type, as the service
requires): `GET /messages` lists what it took, a picture by its name, type,
size and SHA-256, and `POST /messages/answer` with `status=500` makes it
fail.
With `-bot-dir D/bot` it is the Bot API of Telegram too, for a declared
source whose file has `base: http://A` and takes its credential in the
path (`packs/telegram`, with that one line changed). It takes `sendMessage`
as JSON and `sendPhoto` and `sendDocument` as a multipart form, under the
token of `-bot-token`, and answers `getWebhookInfo`, `setWebhook` and
`deleteWebhook` without keeping a webhook. It keeps every request, taken or
refused, and the bytes of every file in `D/bot`, a refused picture's too.
`curl http://A/requests` lists the requests as JSON (method, chat, text or
caption; for a file its name, type, size and SHA-256; for a picture its
width and height; and the status each was answered with and, for a
refusal, the service's words), and `curl http://A/requests/<n>/file`
returns the bytes of the nth as they came, to compare with the file that
was sent. A photo is held to the rule that
[sendPhoto](https://core.telegram.org/bots/api#sendphoto) states (read 22
October 2026): at most 10 MB, width and height at most 10000 in total, a
ratio of at most 20. A photo over one of them is a 400 with the service's
words, `file is too big` or `PHOTO_INVALID_DIMENSIONS`, and a file that is
no picture is `IMAGE_PROCESS_FAILED`. Nothing else is checked: not the 4096
characters of a text, the 1024 of a caption or the 50 MB of a document, and
`getFile` is not served, so an inbound file is not rehearsed.
And it is a customer system under `/crm/`, for a declared source whose calls
a session makes with `yardr call`: `sources/crm.yaml` is its declaration
(replace `BASE` by the stand-in's address), and `-crm-token` the bearer
token it takes (`crm-standin`). It takes a note on a customer too, an
update call (`POST /crm/customers/<id>/notes`), with the token of
`-crm-update-token` (`crm-update-standin`) and once for each
`Idempotency-Key`. `curl http://A/notes` lists the notes it wrote and how
many requests it got for one; `curl -X POST http://A/notes/hold` has it
write the next note and hold the answer until the caller gives up, which is
how a trial gets a timeout after a write.

## Rehearsing the Telegram pack

A builder whose change reaches Telegram, in a tell, in `yardr reply --file`
or in the pack's scripts, cannot make a live receipt. A bead's session may
not run `yardr reply`, the installed yardr is the last landing's, and the
operator's own tells may not go through Telegram at all. `rehearse-telegram`
is the rehearsal in its place: the real binary, daemon and pack scripts in a
bare yard, against the stand-in above. From any directory of the checkout:

```
contrib/isolated-yard/rehearse-telegram
```

It builds yardr from the checkout, so the `yardr version` it prints names
the commit rehearsed (`-modified` when the tree has changes not committed).
It starts the stand-in and the yard's hook listener on free loopback ports,
applies `packs/telegram` with its `base:` line pointed at the stand-in (the
one line that differs, which the script checks), and sets the operator `ada`
up to be told through it. Then it runs these, with the five pictures that
`pictures/` makes (PNGs, each its own, one of them 1280x9000):

- A delivery from the operator's chat reaches the yardmaster's inbox.
- C1, `yardr reply <seq> "..." --file reply.png`, goes by `sendPhoto` with
  the words as its caption.
- C2, a reply of 1025 characters with two pictures and a document, goes as
  the text first, whole, by `sendMessage`, then each file without a caption,
  in order, the document under its own name (`shed;log, v2.txt`) and type.
- C3, a reply file whose name holds a line feed, is refused before anything
  is stored or sent.
- T1, `yardr mail ada "..." --file mail.png`, is told by the daemon with the
  picture.
- T2, a bead closed by a builder with a screenshot on the second of its
  three notes, is told with that screenshot.
- T3, a mail with a 1280x9000 picture, which the stand-in refuses as the
  service does, is told as the same words alone, and the daemon logs `user
  told without the picture`.

It prints each request the stand-in took and, for a file, the file of the run
it equals, with the same SHA-256 and the same byte count. At the end it
fetches each upload back from the stand-in and compares its bytes with the
file using `cmp`. A check that fails prints `FAIL` and the request, the run
goes on, and the script exits 1 at the end. A command that fails stops the
run, with the daemon's last lines and what the stand-in held by then. A run
takes about 90 seconds, since each of the three tells waits out its 20
second grace. `--keep` leaves the yard and the stand-in up and says how to
end them.

It needs `go`, `curl`, `jq`, `file`, `perl` (for a free port) and `shasum` or
`sha256sum`. It is no live receipt. The stand-in is not Telegram, and the
real-API smoke test stays the operator's
([the design](../../docs/design/2026-10-13-yardr-c55e-telegram-hears.md)).

### Traps

Each of these cost a run when the rehearsal was first made by hand.

- Under `D/e` there is no `USER`, so a command acts as the login name, and
  the operator's own acts are refused (`only the operator (ada) may change a
  setting; this is mckean`). Give every operator act `--actor <operator>`.
- `yardr reply` calls the source with the credential in its own environment
  and is refused with none (`TELEGRAM_BOT_TOKEN is not set in this
  environment`). A crew member's pane has it from its env file. The
  rehearsal runs `D/e env TELEGRAM_BOT_TOKEN=<token> yardr --actor ada reply
  <seq> "..." --crew yardmaster`.
- The hook listener needs an address, and the server reads `hook_addr` only
  when it starts. The script takes a free port first (perl asks the kernel
  for one), sets it, stops the server, lets `daemon start` start it again,
  and waits for a delivery with no secret to be answered 401. Never 8790 or
  8791, which are the real yard's.
- A user's own acts are never told to them. The mail comes from the
  yardmaster, and T2's bead is closed by `yd-builder`, because a bead closed
  by `ada` would tell her nothing.
- A tell comes about 20 seconds after what caused it (`store.PushGrace`, a
  constant). The script polls the stand-in's list and the daemon's log for
  the effect, each poll with a fuse against a hang, and does not sleep for a
  set time and hope.
- A bare yard has no herdr, so the daemon logs that the yardmaster failed to
  start, every 30 seconds. That is not a failure of the rehearsal.
- curl sends a double quote in a file name as `%22`, and the stand-in lists a
  name as it arrived. The document's name holds `;` and `,`, which curl's
  `-F` read as syntax until the pack quoted them, and no quote.

## Serving a view that acts as a user

A trial whose headless browser must hold the view's cookie serves a view
that acts as a user: start the view through `D/e`, take its URL with `D/e
yardr web open --print`, and give that URL to the trial's headless browser.
Never run plain `yardr web open` there: it opens the machine's own browser.
The printed URL holds the view's token, as the view's record does, and is
for the trial alone.

## Looking at a page of the view

A builder whose change shows in the web view looks at the page in a browser and
shoots it at the commit they hand on. A bare yard holds what the page draws and
`agent-browser` drives it, so no herdr starts and no agent runs. This is the
recipe for one ask card. Another page takes the same steps with its own data;
for Crew, a conversation, a delegated run's record and Settings, `stock-view`
makes the data ("Stocking a yard for Crew, talk and Settings", below).
Everything of the yard goes through `D/e`:

```
contrib/isolated-yard/isolated-yard bare      # builds yardr from this checkout, prints D
D/e yardr version                             # names the commit the page is shot at
D/e yardr init --harness claude --operator ada    # no --start: no agent starts
mkdir D/depot
D/e yardr --actor ada depot add notes D/depot --kind dir
D/e yardr --actor ada bead create "a title" --depot notes    # prints the bead's id
D/e yardr --actor yardmaster bead ask <id> --recommend 1 - <<'ASK'
Q: the question
C: a line of context
O: a label: its consequence
O: another label: its consequence
W: why the recommended one
ASK
D/e yardr web serve --as ada --addr 127.0.0.1:0 --pages-addr 127.0.0.1:0 > D/web.log 2>&1 &
D/e yardr web open --print                    # the URL with the view's token, for the browser alone
```

Before opening the URL in the browser, wait for `D/web.log` to show `serving
the yard`; that line names the two ports it took. Build the yard after your
last commit. `yardr version` prints 12 characters of the commit it was built
from, and `-modified` after them when the tree has changes not committed. A
version that begins as `git rev-parse --short HEAD` does, with no `-modified`,
is the commit the shot shows.

Then the browser. Choose a session name of your own and use that same value on
every `agent-browser` command: a builder's shell starts afresh for each one.
Put the picture in a directory of its own outside the worktree; `mktemp -d`
makes one.

```
AGENT_BROWSER_SESSION=<a name of its own> agent-browser set viewport 1280 800
AGENT_BROWSER_SESSION=<a name of its own> agent-browser open <the URL>                  # the token becomes the browser's cookie
AGENT_BROWSER_SESSION=<a name of its own> agent-browser fill "input[name=words]" "..."  # to drive the card: type in it,
AGENT_BROWSER_SESSION=<a name of its own> agent-browser press Enter                     # and press a key
AGENT_BROWSER_SESSION=<a name of its own> agent-browser wait --load networkidle
AGENT_BROWSER_SESSION=<a name of its own> agent-browser open <the URL>                  # / again, for the shot
AGENT_BROWSER_SESSION=<a name of its own> agent-browser wait --load networkidle
AGENT_BROWSER_SESSION=<a name of its own> agent-browser screenshot <that directory>/shot.png
file <that directory>/shot.png                # PNG image data, 1280 x 800
AGENT_BROWSER_SESSION=<a name of its own> agent-browser close                           # this session's browser, never --all
contrib/isolated-yard/isolated-yard down D
```

Leave out `fill`, `press` and the second `open` where the page needs no
driving. Put the picture on the bead's note with `--file`, and remove its
directory. `down` stops the view as well, since it stops everything whose home
is D.

### Traps

Each of these cost calls the first time the view was shot by hand.

- `/` draws one card, the newest asked wait's, however many wait. The rail
  lists the rest. Create the bead whose card is to be shot last.
- A run ask is refused where its asker may run the command themselves, with
  `needs no approval: run it yourself`, so ask it `--actor yardmaster` and not
  as the operator. `--run` is refused beside `-`. Write a run ask in the
  labelled stdin form with an `R:` line, such as `R: settings set
  max_sessions 4`, and no `O:` or `W:` lines, since it takes no options.
- Under `D/e` there is no `USER`, so a command acts as the login name, and an
  operator's act such as `depot add` is refused. Give every operator act
  `--actor <operator>`, `bead create` too, so the bead is the operator's. The
  traps of "Rehearsing the Telegram pack", above, say the same.
- `web serve` has a second address, `--pages-addr`, beside its own. Give both
  `127.0.0.1:0`, never a port the real yard uses. 8791 is its view's.
- The page's main column, `#stage`, scrolls inside its own container, so
  `AGENT_BROWSER_SESSION=<a name of its own> agent-browser scroll down` does
  nothing there. Use `eval` to scroll that container, for example:
  `AGENT_BROWSER_SESSION=<a name of its own> agent-browser eval "document.querySelector('#stage').scrollTo(0, document.querySelector('#stage').scrollHeight)"`.
- A form post from `/` lands on the bead's page, whether it comes from the
  Send button or from Enter in the words field. Open `/` again for the shot.
- `AGENT_BROWSER_SESSION=<a name of its own> agent-browser network requests`
  shows whether a key press sent anything. Empty the log with
  `AGENT_BROWSER_SESSION=<a name of its own> agent-browser network requests --clear`
  before the press, and list it with the first command after. `No requests
  captured` means the press sent nothing. A reload is the control: it lists
  its GETs, so an empty list is no dead listing.

## Stocking a yard for Crew, talk and Settings

The recipe above makes one ask card in a bare yard. A page of Crew, a
conversation with a crew member (`/talk`), a delegated run's record
(`/delegations/<id>`) or a page of Settings draws more: crew members that run
and one that does not, letters both ways with a picture among them, a record of
what the yardmaster ran for the operator, and a yard that has a name.
`stock-view` makes that yard in one run, in about 40 seconds after `up`. From
the worktree under test, after your last commit:

```
contrib/isolated-yard/isolated-yard up        # not bare: builds yardr from this checkout, prints D
contrib/isolated-yard/stock-view D            # stocks the yard, prints what it made
D/e yardr web serve --as ada --addr 127.0.0.1:0 --pages-addr 127.0.0.1:0 > D/web.log 2>&1 &
D/e yardr web open --print                    # the URL with the view's token, for the browser alone
```

From there the steps are those of "Looking at a page of the view", above: wait
for `serving the yard` in `D/web.log`, give every `agent-browser` command the
same `AGENT_BROWSER_SESSION` of your own, put the picture in a directory of its
own outside the worktree, and `down` the yard at the end. `stock-view` serves
and shoots nothing, since the page and the session's name are yours. It takes a
yard that `up` made and nothing has used, and refuses any other, saying why. It
runs every command through `D/e`, as the operator `ada` (`--actor ada`), prints
a line for each thing it made, and ends with the two commands above, with `D`
spelled out. Each of its waits has a fuse of 60 seconds against a hang, which
`STOCK_VIEW_WAIT=<seconds>` raises for a loaded machine.

`TestStockView` runs it and reads the yard back through yardr. It needs herdr,
so it runs in the quiet run alone, and by hand:

```
YARDR_QUIET_RUN=1 go test -run TestStockView ./contrib/isolated-yard
```

What it makes:

- The yard, as `yardr init --harness claude --operator ada --start` makes it,
  named `kestrel` (the letters of the rail's chip, `ke`), with the yardmaster's
  delegation on, a user `jo`, a directory depot `notes`, a manual group `writers`
  of ada and jo, and a route that sends the stage `review` to it. It has no
  beads and no peers.
- Crew: `yardmaster` and `scribe` running, `night` stopped.
- A conversation with the yardmaster: its greeting, which the stand-in sends
  when it starts, then letters both ways, one from ada with a picture and one
  from the yardmaster with another. Ada's letters are read; the yardmaster's to
  her are not.
- A delegated run: ada's mail to the yardmaster asking for room for ten notes,
  and delegation 1, in which the yardmaster ran `group set writers --limit 10`
  for ada on that mail.

What the pages then show, as the view acts as ada. The view acknowledges
nothing, so looking changes none of it:

- `/crew`: the list holds `night` as `stopped · disabled`, `scribe` and
  `yardmaster` idle, and the person `jo`. The yardmaster is marked `unread mail`,
  since its letters to ada are unread, and Crew's tile in the rail carries a
  badge of ada's unread mail. Beside the list stands the conversation with the
  yardmaster.
- `/talk`: that conversation as a page of its own, `Today` and six letters,
  oldest first, `read` under ada's and a picture on two. It opens the
  yardmaster's; `?with=<name>` names another party, and no other has mail.
- `/delegations/1`: `Record 1: yardmaster for ada`, the mail it ran on, what
  changed, the command that ran and the one that takes it back, and a table of
  the setting before and after (`--limit`, 50 and 10).
- Settings, at `/config`: the pages of the list and the yardmaster's switch,
  `on`. `/groups` lists `writers` (manual, limit 10, ada and jo), `/routes` the
  route to it and `/depots` the depot `notes`.

A page that needs what this yard lacks, a bead or a peer, needs more than
`stock-view` makes.

### Traps

Each of these cost time when the yard was first stocked by hand.

- It needs `up`, not `bare`: a crew member runs only under herdr, and a bare
  yard has none. `stock-view` refuses a bare yard, and one `yardr init` has been
  run in, since init is the first thing it does.
- Run `agent-browser set viewport 1280 800` and `agent-browser set media dark`
  (or `light`) before each shot, each with the same `AGENT_BROWSER_SESSION` as
  the rest. A session keeps both from one `open` to the next and a new one
  starts at the browser's own size and colour scheme, so a shot that sets
  neither is of whatever the session held. The scheme changes on the page that
  is open, so one page is shot in both without opening it again.
- The conversation's pane, `#letters`, runs `column-reverse`: its `scrollTop` is
  0 at the newest letter and negative above it, so `scroll down` does nothing
  there, and `scrollTo(0, scrollHeight)` stays at the newest. Scroll to the
  oldest letter with `-(scrollHeight - clientHeight)`:
  `AGENT_BROWSER_SESSION=<a name of its own> agent-browser eval "(l => l.scrollTo(0, -(l.scrollHeight - l.clientHeight)))(document.querySelector('#letters'))"`.
  The traps of "Looking at a page of the view" hold here too, `#stage`'s among
  them.
- Mail takes its time from the clock, and nothing else sets a letter's day: every
  letter of this yard stands under `Today`, so a `Yesterday` separator cannot be
  shot in it.
- Every stand-in that takes a first prompt runs `D/run` while it is there, the
  yardmaster's too. `stock-view` writes it for one restart of the yardmaster and
  removes it before it ends. A command of your own goes in the same way: write it
  once the members you do not mean to run it have started, and remove it after.
- A member's state follows the command that changes it by seconds: `crew start`
  takes about five seconds to show `running`, and a member that `crew stop`
  stopped reads `running` until the daemon's next pass. Poll `crew list --json`
  for the state before a shot, as `stock-view` does.

## The yard-shape matrix

`matrix_test.go` walks every way a yard is set up through the same list of
steps, so that proving the default setup does not stand in for the others. A
shape is a row of its table. The first three are a yard with a git depot, one
with a directory depot, and one set up as a server by
[contrib/server-yard](../server-yard/README.md), whose daemon and web view
are login services kept by the script's stand-in service managers. Each shape
is a yard of its own, made by `up`, and every step runs through its `D/e`
(for the server yard, `D/e env ...` with the variables the managers read).

The steps, in order: init; doctor; depot add; depot init, where the pack of
the other kind of depot is refused; a bead created, listed and shown; the bead
worked to its end by the stand-in agents, a builder and a reviewer that do
what `D/run` tells them; land; status, and doctor with no setup step left;
the daemon stopped, started (a second start is refused) and stopped again,
then stopped once more with none running.

A shape that cannot do a step does not skip it. Its table entry holds the
refusal, and the matrix runs the attempt and pins the exit status and the
start of the error. The exit status says what kind of refusal it is: 1 for a
command refused, 2 for one that had to ask and had no terminal to ask in, 3
for something not there (`exitCode` in `internal/cli/root.go`). A dir depot
cannot land. Its flow ends at review, with no stage to hand a bead to, so
that is its entry for `land`. A new shape is a new row, and each step it
cannot do is one more entry.

A shape takes 35 to 50 s and the three run beside one another. The matrix
skips unless `YARDR_QUIET_RUN` is set, which the daemon's quiet run sets for
the whole suite (docs/testing.md R6) and nothing else does. By hand, for all
shapes or for one:

```
YARDR_QUIET_RUN=1 go test -run TestYardShapes ./contrib/isolated-yard
YARDR_QUIET_RUN=1 go test -run TestYardShapes/dir ./contrib/isolated-yard
```

It needs herdr, as `up` does, and with the variable set it is red without it.

## What `down` stops

`down D` stops a yard by its home, not by its binary. It stops every process
whose `YARDR_HOME` is D or inside it, whichever yardr it runs and whoever
started it, together with every process running a file of `D/bin` (the
herdr server, the stand-in agents) and every exec script its sessions run:

1. the exec scripts, each by process group, while the server answers with
   their handles and the daemon proves them (see below);
2. the herdr server, by its own `server stop`;
3. the daemon, then the server, by the pid each left in its home
   (`daemon.lock`, `server.pid`), so the daemon ends its pass with its
   server still there. A pid from a file is signalled only when it is also
   one of the yard's: a file left by a process that died may name someone
   else's;
4. everything still found, with TERM, then KILL after 5 seconds;
5. it removes D, looks once more for a process whose home is in it, and
   exits 1 naming those it finds. If something survived KILL, D is kept and
   the exit status is 1 as well.

Two cases an earlier `down`, which went by the files in `D/bin` alone, left
running while it printed "stopped and removed":

- **`D/bin/yardr` rebuilt during the test** (`go build -o D/bin/yardr .`, or
  a new file moved over it). The running server and daemon then run a file
  `D/bin` no longer names.
- **a server started by another yardr** with `YARDR_HOME` in D, such as the
  installed one. It runs no file of `D/bin` at all.

Rebuilding `D/bin/yardr` is fine, then: the server and the daemon replace
themselves with the new build within about 10 seconds, as they do after an
install, and `down` finds them either way.

`down` refuses a directory `up` (or `bare`) did not make. D must be
`/tmp/iy.*` and carry the mark `up` left in it, `D/.isolated-yard` with D's
own path; otherwise nothing is stopped or removed and the exit status is 2.
A real yard has neither the name nor the mark, and no process of it has a
home under `/tmp/iy.*`.

The processes are found by the environment they were started with:
`ps axeww` on both Linux and macOS. Neither shows another user's processes,
which an isolated yard does not have. macOS's `ps` also shows no environment
for the system's own binaries (`/bin/sleep`, `/bin/sh`): one of those is
found by its home only on Linux. An exec group's running script is one of
those (`sh -c`, and what it runs): on macOS it is found by its session
handle instead (`yardr session list --running`), each pid kept only while
it is alive and its parent chain reaches the daemon, and stopped by process
group, since `sh -c` may have forked.

The home is compared as a string, the same way on both: `YARDR_HOME` equal
to D or a path inside it. It is never made into a pattern, and `down` stops
nothing when it cannot tell: for a home that is empty, `/` or no isolated
yard, or when the processes cannot be listed, it says so, exits 1 and
leaves D. An earlier `down` escaped the home into a regular expression with
a `sed` expression GNU sed refuses; on Linux the pattern came out empty and
`down` signalled every process that had any `YARDR_HOME`.

`isolated-yard pids D` prints the pids `down D` would stop and stops none.

A server or daemon that `down` does not get to (the yard removed by hand, a
temp home of your own) stops by itself within a minute of its home going
away: see "yardr's own state" in
[docs/configuration.md](../../docs/configuration.md#yardrs-own-state).

`down_test.go` tests the cases above, an exec group's running script among
them, and the refusals with bare yards, that processes of other yards
outlive a `down`, and the comparison itself
(`isolated-yard match <home>`, which reads `<pid> <environment>` lines) with
homes that hold `.`, `[`, `*`, `+` and spaces. The sleepers it starts as
processes of other yards end with the test binary that named them too.

## When the test binary is killed

A Go test binary that is killed, or timed out, runs none of its cleanups, so
nothing calls `down`. The servers and daemons of its yards stop themselves
(they look for the binary that named them). The herdr server and the stand-in
agents do not, so `up` and `bare` start a watchdog for each yard
(`isolated-yard watchdog D`, its output in `D/watchdog.log`), through `D/e`,
which hands it the name. Once a second it asks `D/bin/alive` (`alive/main.go`),
which goes by the pid and the start time, as the servers do. When the binary
is gone it runs `down D`: everything in the yard stops and D is removed, a few
seconds after the end. Only status 1 of `alive` (gone) and its status 2
(`YARDR_TEST_OWNER` missing or malformed: a handoff that broke takes the yard
down, where it would otherwise leak) do that. Any other status (a signal, a
failed start) says nothing about the binary and is looked at again a second
later. A yard made by hand has no owner: its watchdog says so in its log and
ends at once.

Before the watchdog starts, `up` and `bare` run `D/bin/alive` and
`D/bin/yardr version` once, through `D/e`. The first run of a file written a
moment ago, a script such as `D/e` included, waits behind the machine's other
first runs: macOS checks a new executable one at a time, and the test binaries
of a busy machine's builds queue for it by the hundred, so the wait can be tens
of seconds. Taken there it is the wait of whoever makes the yard, which a test
gives no fuse; left to the watchdog it came out of the time a test gives its
first look and its `down`.

The watchdog is a process of the yard like the stand-ins: a normal `down`
stops it with the rest, and it ends by itself if D is removed by other means.
macOS's `ps` shows no environment for the system's own binaries, a `/bin/sh`
among them, so `down` cannot find the shell by the yard's home as it finds the
stand-ins. The watchdog is started with `D/bin/alive` as its stdin instead,
and `lsof` finds what has a file of `D/bin` open (the herdr server and the
stand-in agents are found so too). The `down` it runs gets `/dev/null` as its
stdin: with the watchdog's, `down` and every program it starts would hold that
file open, and `pids` would list them as the yard's own.

A `down` can meet another remover of the same yard: the watchdog's, and the
cleanup of a test that gave up waiting for it. The one that finds D gone, or
going, when it comes to remove it goes on to its last check, which still
looks for what the yard left running. One that finds D gone between two of
its process listings goes on the same way: it read the mark once, at its
start, and lists by the home from then on, whether or not D is still there.
It exits 1 when a process of the home still runs at either check, saying
`D already removed` at the first where it would say `D kept` with D there.
One that starts after D is gone says `no yard at D` and exits 2, which the
cleanups of the tests let pass.

`watchdog_test.go` tests the watchdog with owners that have ended, and with an
`alive` that answers as told: nothing in it kills a test binary. It waits for a
`down` for as long as the `down` goes on, not for a set time: a `down` is some
hundred process starts and a listing of the process table for each of its
stages, and what they cost is what the machine makes them. The wait gives up
when no `down` of the yard has started, or the shells of the one that has have
not changed, for a minute as the machine scales it (`internal/testload`).
