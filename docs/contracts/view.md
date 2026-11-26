# View

A view is anything that shows the yard to a person, or that a person reaches
the yard through, without being the yard: the web pages, `yardr chat`, the mcp
server a person's own client talks to, footplate (the desktop app, kept in
its own repository), and any program that calls the socket. This contract is
the form they read: RPC calls and the kinds of event. On one side is the core:
`store.API`, the socket and listener that serve it (`internal/rpc`), and the
reads the store assembles in the shape of one window or page. On the other
are the views, which are how-and-what, since each knows the layout of one
page, window or terminal view: `internal/web`, `internal/chat`,
`internal/mcp`, footplate, and the programs of other people. The user page is
[api.md](../api.md), whose generated sections are written by `TestAPIDoc`.

## Form

The form that crosses is RPC over HTTP, and a stream of events.

- **The socket.** `POST /call/<Method>` on the yard's unix socket
  (`rpc.Handler`, `internal/rpc/server.go:Handler`). The body is a JSON object
  of the arguments by name for the calls api.md names, and the reply is
  `{"result": ...}`, `{}` or `{"error": {"code", "message"}}`. The array form
  of the arguments is yardr's own. `rpc.Client`
  (`internal/rpc/client.go:Client`) is generated from `store.API`
  (`internal/store/api.go:API`), and every yardr process but the one that owns
  the store calls the yard through it.
- **The listener.** A device calls `POST /user/call/<Method>` on the yard's
  listener with its token (`internal/rpc/user.go:UserPath`,
  `internal/rpc/usere2e.go:UserServer.routes`). Every device is served the calls
  of `Calls`, and a device of the operator is served the calls of
  `OperatorCalls` besides, but for those in `socketOnly`, which no device is
  served. Device enrolment, which keeps what a device is paired with, is a
  library outside the core.
- **The lists.** `rpc.Calls` (`internal/rpc/api.go:Calls`) and
  `rpc.OperatorCalls` (`internal/rpc/api.go:OperatorCalls`) are the calls a
  program that is not yardr may rely on, each with the names of its arguments.
  `listenerOnly` (`internal/rpc/api.go:listenerOnly`) and `socketOnly`
  (`internal/rpc/api.go:socketOnly`) say which door serves which. Every other
  method of `store.API` is yardr's own and is no contract. Every method the
  socket serves has a class that says which callers may make it (`rpc.Access`,
  `internal/rpc/access.go:Access`).
- **The events.** The stream carries event kinds, documented in api.md under
  The stream. For each kind the store decides which of its data a person who
  is not the operator is told (`store.EventDataTold`).
- **The headers.** `X-Yardr-Client` (`rpc.ClientHeader`,
  `internal/rpc/api.go:ClientHeader`) is where a program that is not yardr says
  what it is. A yardr caller sends `X-Yardr-Revision` instead (`version.Header`,
  `internal/version/version.go:Header`) and the server answers with its own
  build in the same header ([api.md](../api.md#builds)).
- **The readers inside the module.** Every reader uses `store.API`, which is
  `*store.Store` in the process that owns the store and `rpc.Client`
  elsewhere. Chat reads through the interface `chat.Store`
  (`internal/chat/chat.go:Store`), and mcp through `mcp.Yard`
  (`internal/mcp/mcp.go:Yard`), each the calls it uses and no more. The web
  holds the whole `store.API` (`internal/web/server.go:Server`).
- **The view-shaped reads.** Where a read must be consistent in one
  transaction, the store assembles records in the shape of one window or page,
  and the call is made for that view (core.md, Where the rule cannot decide,
  case 3). These are `Line` (`internal/store/line.go:Store.Line`), for
  footplate's Line window, `Flows`, which serves the board of
  `internal/store/flowsboard.go` to footplate's timetable and the web's flows
  page, and `UserAsks`, `UserOpenAsks` and `Bead`
  (`internal/store/decisions.go`), for the decisions a person is asked. They
  stand in `Calls` or `OperatorCalls` like any other call.

## Owner

The core owns the calls, the names of their arguments, the fields of their
replies and the kinds of event.

## Version

The contract has no number of its own today. What holds is the promise in
`internal/rpc/api.go:Calls`: a name in `Calls` or `OperatorCalls` is on the wire
for good. A later build may add an argument, which a client of today leaves
out and which means what the call did before when it is the zero value, and
may add fields to a reply and kinds of event, which a client ignores when it
does not know them.

`X-Yardr-Client` decides nothing. yardr's own callers (the command line, the
daemon, chat, mcp, the web and every `rpc.Client`) do not send it. They send
the build's revision, and the server holds them to one build. When a call
names a revision the server does not run, and the binary the server started
from has since been replaced by exactly that build, the server refuses the call
as replacing, finishes the calls it is running and starts the new build in its
place (`internal/server/server.go:runner.handler`). A caller built from an older
binary is served as it is. A program that is not yardr is served whatever build
the server is.

## Change rule

- A call joins `Calls` or `OperatorCalls` on purpose, with its section in
  api.md and its row in `apiCalls` (`internal/rpc/api_test.go`). The tests
  hold the lists, the page and the server together, and the counts of the calls
  in `internal/rpc/callcounts_test.go` change with them.
- An argument is added at the end of a call, and means what the call did before
  when it is the zero value. Where an older yard would take a new argument for
  the call it knows and do something else, the new behaviour is a new call. The
  guarded answers are new names, not a new argument of the old three, so that
  a yard from before them answers one as a method it does not have.
- While the contract has no number, a name is not removed and its meaning is
  not changed. The core's rule for every contract is to change it whole, in one
  release, with the far end following and refused with a reason that names what
  it needs, as for a pack ([pack.md](pack.md), Change rule). For the view that
  rule needs the number (see Planned).
- A view-shaped read is listed under Form with the view it is made for, so
  that it can be found and later moved.

## Stored data

None of its own. A view reads the records of the other contracts. What the
listener holds of a device (its token and channel key) is device enrolment's,
a library outside the core, and the footplate push queue
(`internal/store/footplate.go`) is the outbound queue of mail and
notifications.

## Rules

| # | rule | check |
|---|---|---|
| 1 | The calls a program that is not yardr may rely on are those of `rpc.Calls` and `rpc.OperatorCalls` and no other. Each is served on the socket with as many arguments as it has names, and each has its section in api.md under those names. No call is in both lists. | `TestCallsAreTheDocs`, `TestAPIContract` |
| 2 | A call made as api.md shows it answers every field the page promises, with its type, on the socket and for a device under `/user/` on the listener. Every event the page documents is in the stream with the fields it promises. | `TestAPIContract` |
| 3 | The generated sections of api.md (the calls, the operator's calls, the events and the errors) are what the yard answers. | `TestAPIDoc` |
| 4 | A call by name takes an argument left out as the zero value of its type, and refuses an argument the call does not have, so a misspelt name is not another call. The array form of the arguments is served as before. | `TestCallByName` |
| 5 | The listener serves a device exactly the calls of `rpc.Calls`, by name, and every other method as no method at all. A device of the operator is served the calls of `OperatorCalls` besides, but for the socket's own. | `TestUserServesTheCallsAndNoOther` |
| 6 | Every method the socket serves has a class that says which callers may make it, so a method added to `store.API` cannot ship without one. A class lets through only the callers it names. | `TestAccessClassesEveryServedMethod`, `TestClassAllows` |
| 7 | Every kind of event the code writes has a row that says which of its data a person who is not the operator is told, and the data api.md promises of an event is in the row of its kind. | `TestEveryKindIsDecided`, `TestTheTableTellsWhatTheStreamPromises` |
| 8 | A program that is not yardr is served whatever build the server is, and never makes the server replace itself. The header it names itself in decides nothing. | `TestAClientThatIsNotYardrIsServed` |
| 9 | A yardr caller sends its build's revision. A caller that is the build installed over the server's makes the server replace itself, and its call is served by the new build. A caller of an older build is served as it is and changes nothing. A call refused as replacing is made again. | `TestNewerClientReplacesTheServer`, `TestOlderClientDoesNotReplace`, `TestCallSendsRevisionAndRetriesWhileReplacing` |
| 10 | A yardr caller told that the server has no method of the name, by a server of another build, is told that the builds differ. One of the same build is told the plain error. | `TestSkewMessage`, `TestSameBuildKeepsPlainError` |
| 11 | A guarded answer never lands unguarded. A yard from before the guarded answers answers each as a method it does not have, and the calls they guard refuse a turn. | `TestGuardedAnswerFailsClosedOnAnOlderYard` |
| 12 | A name in `rpc.Calls` or `rpc.OperatorCalls` is on the wire for good: no build removes one or changes its meaning, and an argument a build adds means what the call did before when it is the zero value. | unchecked |
| 13 | The view-shaped reads are listed under Form, each with the view it is made for, and a new one joins the list. | unchecked |

## Planned

- **planned**, design 3.2.7: one number for the view contract, stated by the
  yard. A client says the number it speaks in `X-Yardr-Client`, so that
  "footplate/0.1" becomes, for example, "footplate/0.4 view/3". A call from a
  client that names another number is answered with a plain refusal that names
  the number the yard needs and tells the person to upgrade the client. The
  number binds the callers that name themselves in that header, and only
  them: yardr's own callers send the revision and need no view number, and
  on the unix socket a call that names neither header is served as it is
  today, since tests and a person's own `curl` call that way. On the listener,
  whose callers are devices and never yardr's own build, a call of `Calls` or
  `OperatorCalls` with no client header is refused with the number it needs.
  The edge's enrolment of a device arrives on the same path and is no view
  call: it belongs to the edge link, and the edge's signature holds it, not
  this number. Adding a call or an argument whose zero value means the old
  behaviour keeps the number. Removing or changing one raises it, in one
  release, and "on the wire for good" in `internal/rpc/api.go` gives way to
  the number. Row 12 then gives way to a row for the number, and a row for each
  refusal joins the table with its check.
- **planned**, design 3.2.7: every view reads through a named interface of the
  calls it uses, as chat and mcp do, and no view imports `dispatch` or
  `server`. The web holds the whole `store.API` today, and it imports
  `dispatch` and `server` among others.
