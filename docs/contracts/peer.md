# Peer

A peer is another yard that this yard is linked with. This contract is the
protocol between two yards that run different builds of yardr: the messages
they post and ask for, how each is signed and sealed, what each says of its
own build, and what the receiver does with a message it cannot read. Both
ends are yardr. On this yard's side the core keeps the peer records, the
outbox and settlement, which are its delivery to another yard
(`internal/store`). The receiver and the caller are `internal/yards`, the
daemon that posts and asks is `internal/dispatch`, and the signatures and the
seal are `internal/yardkey`, a library. On the other side is the other yard, a
different build of the same code. The user page is [peers.md](../peers.md),
whose Two yards on different builds states the rule this page holds to
checks. The link a yard keeps open to its edge has the same shape, and The
edge link, below, is its section.

## Form

An HTTP protocol between two yards, each on its own build.

- **The requests.** A yard takes its peers' requests on its listener under
  `/peers/`. `POST /peers/messages` posts one message, and
  `GET /peers/messages?after=<seq>` asks for what waits in the other yard's
  outbox for the asker (`yards.Path`, `internal/yards/receiver.go:47`). A
  question may add `refused`, the messages it got and did not take with the
  reason, and `wait`, to have the answer held until there is something to hand
  over. A pairing is three more requests, `POST /peers/pair/hello`, `reveal`
  and `confirm` (`internal/pairing/pairing.go:10`), which a yard that is no
  peer yet makes to one that opened a pairing. Their bytes are
  [peers.md, The requests](../peers.md#the-requests).
- **The signature and the seal.** Each request and each answer carries an
  HTTP Message Signature (RFC 9421) of the sender's ed25519 key, and each body
  crosses sealed with HPKE (RFC 9180) to the receiver's X25519 key, in the
  envelope `{"v","enc","ct"}` and nothing else (`internal/store/peersealed.go:34`).
  The kind of a message and the name of its sender are inside the seal.
- **The message.** `PeerMessage` is `{id, kind, re, body, seq}`
  (`internal/store/peers.go:216`): the id the sender mints, by which the
  receiver takes it once, its kind, the id of the message it answers, a JSON
  body, and the place in the sender's outbox, set where a question hands it
  over. A link carries the kinds of `PeerKinds` (`peers.go:54`): `ask`, `bead`,
  `file`, `mail`, `ping` and `thread`, each direction allowed on its own.
  `bead-return` goes with the kind `bead`, and `bead-event`, `bead-read` and
  `bead-note` with `thread`.
- **What a yard says of itself.** With every signed request and every signed
  answer a yard says what its build understands: `PeerSays`
  (`internal/store/peersaid.go:19`) holds the kinds it knows (`understands`),
  its build (`yardr`) and whom it exposes to the other (`roster`). The other
  yard keeps it by the key, as the peer's word, and refuses nothing by it.
- **A refusal.** The receiver answers a message it does not take with a reason
  (`RefusedUnknownKind`, `internal/store/peermail.go:107`, `RefusedVersion`,
  `internal/store/peerbeads.go:62`, and the others beside them), logs it as
  `peer_message_refused`, and tells the sender in the 403 of a post or with the
  next question. The sender words it in its own words (`PeerRefusal`,
  `peermail.go:125`) and gives the message up, never dropping it silently.

## Owner

The core. Peering as delivery stays in the core, and its wire is this
contract.

## Version

Per body. A body whose shape may change carries a `v`, and the receiver reads
only the `v` it writes. A field a yard does not know is ignored, so adding an
optional field changes no `v`. A kind a build does not know is refused by name,
so a new kind needs no day on which both yards change.

| message | body | `v` |
|---|---|---|
| the seal | `{"v","enc","ct"}` | 2 (`internal/store/peersealed.go:97`) |
| `ping` | none | none |
| `mail` | `to`, `from`, `text`, `files` | none |
| `file` | the blob's reference and its bytes | none |
| `ask`: an ask, its answer, its settle | `peerAsk`, `peerAnswer` | 1 (`PeerAskVersion`, `internal/store/peerasks.go:27`) |
| `bead`, `bead-return`, `bead-event`, `bead-read`, `bead-note` | `PeerBeadMessage` and the bodies beside it | 1 (`PeerBeadVersion`, `internal/store/peerbeads.go:41`) |
| a pairing's requests | `hello`, `reveal`, `confirm` | none |

The table holds a row for each kind. A kind or a body added to the protocol
adds its row, with the `v` it writes.

## Change rule

- A change to the shape of a body raises its `v` in the release that makes it,
  and a build reads only the `v` it writes. No old `v` is kept readable beside
  the new one.
- A message at a `v` the receiver does not read is refused as `version`,
  logged and told to the sender, as a kind the receiver does not know is
  refused as `kind_unknown`. It is never dropped silently. The linked yard
  that is behind follows by upgrading, as a pack's author follows the core
  ([pack.md](pack.md), Change rule), and its message waits as given up, with
  its reason, until it does.
- A new kind is a name in `PeerKinds`, with its row in the table above. A
  build that does not know it refuses it by name, and the two yards still
  exchange every kind both know.

## Stored data

Peer records with the key each is known by, the outbox of each peer, peer
beads and the asks that cross in both directions, reads of a bead at another
yard, and the roster: the peer files of `internal/store`. A message refused
for its `v` stays in the outbox as given up, with its reason.

## Rules

| # | rule | check |
|---|---|---|
| 1 | A request that is no linked yard's is answered 401 in the same words, unsigned, and nothing of it is kept. That holds for a key no link has, a signature by another key, a changed body, a timestamp too old or ahead, a request signed for another yard, a changed query, no signature, and a yard that was removed. | `TestReceiverRefusesWhatIsNoPeers` |
| 2 | Only the sealed envelope crosses: neither the kind of a message nor its body is in the clear. An envelope sealed to another yard, or changed after it was signed, is refused and nothing is taken. | `TestWireCarriesOnlyCiphertext`, `TestSealedToAnotherAndTampered` |
| 3 | A posted message is taken once: the request played again, and the same id in a new request, are answered 200 and dropped. Every answer to a peer is signed and sealed to the asker, and nothing but the post and the question is served. | `TestReceiverPost`, `TestCallerPull` |
| 4 | A kind the link does not carry in is answered 403 as `kind_not_allowed` and logged. | `TestReceiverPost` |
| 5 | Every signed request and every signed answer says what the sender's build understands, and the other yard keeps what was said, once the store has checked that the answer is the peer's. | `TestYardsSayWhatTheyUnderstand`, `TestPostAnswerSaysWhatThePeerUnderstands` |
| 6 | A yard that says nothing of what it understands is still heard. The ping and the mail of an older yardr are taken, and it is kept as a yard that has not said. | `TestOlderYardrIsStillHeard` |
| 7 | A message of a kind the receiver's build does not know is refused as `kind_unknown`, which no `peer allow` mends. It is logged, not remembered as taken, and the messages that came with it are taken. The sender is told, and gives the message up with the reason `refused by the peer: <peer>'s yardr does not know <kind>`. | `TestPeerUnknownKind`, `TestPeerUnknownKindToldToTheSender`, `TestUnknownKindIsRefusedAsThat` |
| 8 | A body whose `v` the receiver does not read is refused as `version`, whichever body it is: a bead, its return, an event of it, a read of it, a note on it, an ask or an answer. | `TestPeerBeadRefusals`, `TestPeerBeadReturnsThatAreNone`, `TestPeerBeadEventTaken`, `TestPeerBeadReadOfNoSuchBead`, `TestPeerBeadNoteOfNoSuchBead`, `TestPeerAskReceive`, `TestPeerAnswerUnsolicited` |
| 9 | A bead refused for its `v` creates nothing, and is logged once with the `v` it carried and the one the receiver knows. | `TestPeerBeadRefusals` |
| 10 | What the sender is told of a refusal is said in its own words, one for each reason it knows. A reason it does not know is not repeated. | `TestPeerRefusalOfBeads`, `TestPeerUnknownKindToldToTheSender` |
| 11 | A link made before the sealed wire is refused before any dial, in both directions, with the line that says to link it again. | `TestOldWireYardIsRefusedBeforeAnyDial` |
| 12 | A change to the shape of a body raises its `v` in the release that makes it, and a build reads only the `v` it writes. | unchecked |
| 13 | On the edge link, the yard answers the edge's challenge only when it is signed by the key the yard pinned when it connected, over this socket's nonces. Otherwise it sends no hello, and the link is told down as a refused key. | `TestLinkRefusesAChallengeNotTheEdges` |
| 14 | On the edge link, a frame is served only when it is the edge's as it came, over this link's session and in its place in the sequence. A frame signed by another key, changed after the edge signed it, sent again, or recorded from an earlier link never reaches the handler, and the link stays up. | `TestLinkRefusesAFrameNotTheEdgesAsItCame`, `TestLinkRefusesAFrameSentAgain`, `TestLinkRefusesAFrameFromAnotherLink` |
| 15 | On the edge link, a socket that falls silent is counted dead after `DeadAfter`, told down as unanswered pings and dialled again, and the new link serves. | `TestLinkDialsAgainAfterASilentSocket` |
| 16 | On the edge link, an answer that does not fit one frame goes as a stream, the head with an empty body and then the body in data frames, and the link serves the next call as before. | `TestLinkStreamsAnAnswerLargerThanOneFrame` |
| 17 | On the edge link, the frames that cross are those listed in the comment at the head of `internal/edgelink/link.go`, which the edge defines. | unchecked |

## The edge link

The yard keeps one WebSocket open to its edge, so that what reaches the edge
for the yard comes down it and needs no port of the yard's own. It has the
same shape as a peer: two ends that change on their own days, and no version
on any frame. Its far end is the edge service, which yardr also runs (the
`yardr-hosted` router, in its own repository). The rules of this section are
rows 13 to 17 of the table above.

### Form

- **The handshake.** The yard dials `GET /edge/link` (`LinkPath`,
  `internal/edgelink/link.go`), signed as an enrolling call is, with a nonce of
  its own. The edge sends a `challenge`: its own nonce and its signature over
  both. The yard checks it against the key it pinned when it connected, answers
  with a `hello` it signs, and the edge answers `welcome`.
- **The frames.** After the welcome the edge sends `request` and `socket`
  frames, each numbered and signed over this link's session, and the yard
  answers with `response`, `data`, `end`, `socket-head`, `socket-data`,
  `socket-close` and `ack`, and sends `push-intent` and `push-remove` up, which
  the edge answers with `push-ack`. Either end sends `ping` and `pong`. The
  list, with every field of each, is the comment at the head of
  `internal/edgelink/link.go:37`.
- **The pin.** The key of the edge is fetched once and pinned in the `edge`
  setting (`store.Edge`, `internal/store/settings_edge.go:28`). It is
  never taken from the edge again, and a different key at the same address is a
  different edge.
- **The calls beside it.** The yard also calls the edge's own paths, signed
  with its key and answered signed by the edge's (`internal/edgelink/edgelink.go:18`).

### Owner

The core. The edge service is the far end of the contract, and it follows the
core.

### Version

None today. No frame carries one.

### Change rule

The core changes the frames whole, in one release, and the edge follows, as a
linked yard follows the core.

### Stored data

The pinned edge (`store.Edge`) with the yard's handle and name at the edge. The
link keeps no record of its own: its state (`State`,
`internal/edgelink/link.go:186`) is what `yardr status` and `yardr doctor`
show, and a link that goes down is told as an `edge_link_down` event.

## Planned

- **planned**, design 3.2.8: `PeerSays` gains `versions`, the `v` the build
  reads for each kind that has one. It is a field an older peer ignores and an
  older sender does not send, so adding it raises no number. A sender whose
  peer said its versions holds a message at a `v` the peer does not read in the
  outbox as refused before it sends, with the same words as the refusal after
  sending. A peer that said no versions (an older build) is sent to, and the
  refusal after sending says the same. Today a sender learns that the peer does
  not read a `v` only from the refusal.
- **planned**, design 3.2.8: a refusal for a `v` names the version and the
  build it needs, as `refused by the peer: <peer>'s yardr reads bead v2; it
  needs an upgrade`. Today the sender says `it does not know this version of
  the message`, and the `v` and the one the receiver knows are in the
  receiver's event alone.
- **planned**, design 3.2.8: one `v` on the edge link's `hello`, rather than on
  each frame, and either end refuses a `v` it does not read with the version it
  needs. The edge service is the far end, which yardr also runs, so the two
  change in one release.
