package web_test

import (
	"context"
	"encoding/json"
	"html"
	"maps"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/web"
)

// The Config hub shows an operator the yardmaster's delegation switch, and
// its form turns it off on the operator check alone and back on only under
// a fresh sign-in: a request to turn it on without one starts the sign-in
// and writes nothing. A member is refused both ways, and nothing but the
// switch changes.
func TestWebDelegationSwitch(t *testing.T) {
	t.Parallel()
	y := newApproveYard(t, true)
	ctx := context.Background()
	jo, sam := y.token("jo@example.net"), y.token("sam@example.net")
	setting := func() string {
		t.Helper()
		s, err := y.st.Settings(ctx)
		must(t, err)
		return s[store.DelegationSetting]
	}
	others := func() map[string]string {
		t.Helper()
		s, err := y.st.Settings(ctx)
		must(t, err)
		delete(s, store.DelegationSetting)
		return s
	}
	before := others()

	// Unset on a hosted yard, it reads on.
	if r := y.getNoFollow("/config", jo); r.status != http.StatusOK || !strings.Contains(r.body, "Yardmaster for the operator") || !strings.Contains(r.body, `value="off"`) {
		t.Fatalf("GET /config as jo: %d:\n%s", r.status, r.body)
	} else if body := squash(r.body); !strings.Contains(body, `<div class="wv-setting"><div class="wv-setting__text"><p class="wv-setting__name">Yardmaster for the operator</p>`) ||
		!strings.Contains(body, `<span class="wv-state wv-state--on">on</span><form method="post" action="/config/delegation"`) ||
		strings.Contains(body, "Turning it on takes a fresh sign-in.") {
		// On, the switch is a setting row with its state and the button for
		// the other, and says nothing of a sign-in, which only turning it on
		// takes.
		t.Errorf("GET /config as jo: the switch is no setting row that is on:\n%s", r.body)
	}
	if r := y.getNoFollow("/config", sam); strings.Contains(r.body, "Yardmaster for the operator") {
		t.Errorf("GET /config as sam shows the switch:\n%s", r.body)
	}
	for _, action := range []string{"off", "on"} {
		// A write the store refuses is a red line under the head.
		if r := y.post("/config/delegation", sam, url.Values{"action": {action}}); r.status != http.StatusOK ||
			!strings.Contains(squash(r.body), `<h1 class="wv-title">Config</h1><p class="wv-meta">The yard's configuration, one table a page</p><p class="wv-callout">the yardmaster&#39;s delegation is the operator&#39;s to switch</p></header>`) {
			t.Errorf("sam turns it %s: %d:\n%s", action, r.status, r.body)
		}
	}
	if got := setting(); got != "" {
		t.Fatalf("a member's posts set the switch to %q", got)
	}

	// Off by the operator, with no witness.
	if r := y.post("/config/delegation", jo, url.Values{"action": {"off"}}); r.status != http.StatusSeeOther || r.header.Get("Location") != "/config" {
		t.Fatalf("jo turns it off: %d %q: %s", r.status, r.header.Get("Location"), r.body)
	}
	if got := setting(); got != "false" {
		t.Fatalf("after off the switch is %q", got)
	}
	// Off, the setting row says so and that turning it on takes a fresh
	// sign-in.
	if body := squash(y.getNoFollow("/config", jo).body); !strings.Contains(body, `<span class="wv-state">off</span>`) ||
		!strings.Contains(body, `<p class="wv-setting__help">Turning it on takes a fresh sign-in.</p>`) ||
		!strings.Contains(body, `<button class="wv-btn" type="submit" name="action" value="on">Turn on</button>`) {
		t.Errorf("GET /config after off: the switch is no setting row that is off:\n%s", body)
	}

	// On without a fresh sign-in starts one and writes nothing.
	r := y.post("/config/delegation", jo, url.Values{"action": {"on"}})
	loc, err := url.Parse(r.header.Get("Location"))
	if r.status != http.StatusSeeOther || err != nil || loc.Path != "/edge/reauth" {
		t.Fatalf("jo turns it on: %d %q, want the gateway re-authentication", r.status, r.header.Get("Location"))
	}
	back := loc.Query().Get("return_to")
	if !strings.HasPrefix(back, "/config/delegation/fresh/") {
		t.Fatalf("the return is %q, want one-use state bound to the toggle", back)
	}
	if got := setting(); got != "false" {
		t.Fatalf("the challenge set the switch to %q", got)
	}
	// A return under a standing login is not fresh, and writes nothing.
	if r := y.getNoFollow(back, jo); r.status != http.StatusOK || !strings.Contains(r.body, "not fresh") || setting() != "false" {
		t.Fatalf("the return with no fresh sign-in: %d:\n%s", r.status, r.body)
	}
	// A callback challenged while hosted must not turn it on after the
	// operator has moved the yard off-hosted.
	must(t, y.st.SetSetting(ctx, store.HostedSetting, "false", "jo"))
	fresh := y.token("jo@example.net", authAt(time.Now().Add(5*time.Second)))
	if r := y.getNoFollow(back, fresh); !strings.Contains(r.body, "at a terminal") || setting() != "false" {
		t.Fatalf("the hosted callback after the yard moved off-hosted: %d:\n%s", r.status, r.body)
	}
	must(t, y.st.SetSetting(ctx, store.HostedSetting, "true", "jo"))
	r = y.post("/config/delegation", jo, url.Values{"action": {"on"}})
	loc, err = url.Parse(r.header.Get("Location"))
	if r.status != http.StatusSeeOther || err != nil || loc.Path != "/edge/reauth" {
		t.Fatalf("the second challenge: %d %q", r.status, r.header.Get("Location"))
	}
	back = loc.Query().Get("return_to")
	// With the witness it writes true, once.
	fresh = y.token("jo@example.net", authAt(time.Now().Add(5*time.Second)))
	if r := y.getNoFollow(back, fresh); r.status != http.StatusSeeOther || r.header.Get("Location") != "/config" {
		t.Fatalf("the fresh return: %d %q: %s", r.status, r.header.Get("Location"), r.body)
	}
	if got := setting(); got != "true" {
		t.Fatalf("after the fresh return the switch is %q", got)
	}
	if r := y.getNoFollow(back, fresh); !strings.Contains(r.body, "used already") {
		t.Errorf("replaying the return: %d:\n%s", r.status, r.body)
	}
	if after := others(); !maps.Equal(before, after) {
		t.Errorf("the toggle changed other settings: %v -> %v", before, after)
	}

	// Off a hosted yard, turning it on stays a terminal's.
	z := newApproveYard(t, false)
	must(t, z.st.SetSetting(ctx, store.DelegationSetting, "false", "jo"))
	if r := z.post("/config/delegation", z.token("jo@example.net"), url.Values{"action": {"on"}}); r.status != http.StatusOK || !strings.Contains(r.body, "at a terminal") {
		t.Errorf("turning it on off a hosted yard: %d:\n%s", r.status, r.body)
	}
}

// A delegated record's page is an operator's alone; a member, and a
// record that is not there, are answered not found.
func TestWebDelegationRecord(t *testing.T) {
	t.Parallel()
	y := newApproveYard(t, true)
	ctx := context.Background()
	must(t, y.st.PutGroup(ctx, store.Group{Name: "builders", Runner: "herdr", Config: map[string]any{"kind": "claude"}}, "jo"))
	must(t, y.st.Mail(ctx, "yardmaster", "put the builders on codex", "jo"))
	mail, err := y.st.Head(ctx)
	must(t, err)
	dctx := store.WithDelegation(ctx, store.DelegatedCall{For: "jo", Mail: mail, Command: "yardr group set builders kind=codex"})
	_, err = y.st.SetGroup(dctx, "builders", store.GroupEdit{Set: map[string]any{"kind": "codex"}}, "yardmaster")
	must(t, err)

	r := y.getNoFollow("/delegations/1", y.token("jo@example.net"))
	for _, want := range []string{"Record 1: yardmaster for jo", "yardr group set builders kind=codex", "<td><code>claude</code></td><td><code>codex</code></td>", "yardr group set builders &#39;kind=claude&#39;"} {
		if r.status != http.StatusOK || !strings.Contains(squash(r.body), want) {
			t.Errorf("jo's record page lacks %q: %d:\n%s", want, r.status, r.body)
		}
	}
	// The record is Crew's page, the sixth stage kind in its other form: the
	// conversation's head with the yardmaster's name and state, under it the
	// day's separator and exactly one letter from the yardmaster, whose body is
	// the record, and no thread and no composer. Its time is the letter's: the
	// minute in the gutter and the seconds in the time's title. The
	// yardmaster's row stands open in Crew's list, which is the way back.
	page := squash(r.body)
	for _, want := range []string{
		`<article class="wv-page wv-talk" data-kind="talk"><header class="wv-head"><h1 class="wv-title">yardmaster</h1><p class="wv-meta">not running<span class="wv-sep">·</span>herdr · claude</p></header>`,
		`<div class="wv-letters" id="letters"><p class="wv-day">Today</p><article class="wv-letter"><time class="wv-letter__at" datetime="`,
		`<span class="wv-letter__who">yardmaster</span><section class="wv-record" aria-labelledby="record"><h2 class="wv-sh" id="record"><span>Record 1: yardmaster for jo</span></h2>`,
		`<h3 class="wv-record__label">before and after</h3>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("jo's record page lacks %q:\n%s", want, r.body)
		}
	}
	if m := regexp.MustCompile(`<time class="wv-letter__at" datetime="[^"]*" title="(\d\d:\d\d:\d\d)">(\d\d:\d\d)</time>`).FindStringSubmatch(page); m == nil || !strings.HasPrefix(m[1], m[2]) {
		t.Errorf("the letter's time is no minute with its seconds in the title: %v", m)
	}
	if n := strings.Count(page, `<article class="wv-letter`); n != 1 {
		t.Errorf("the record page has %d letters, want the one", n)
	}
	for _, gone := range []string{"<form", "<textarea", `class="wv-compose"`, "‹ Config", `class="page-head"`, `class="wv-live"`} {
		if strings.Contains(page, gone) {
			t.Errorf("the record page has %s", gone)
		}
	}
	if got := marked(listOf(t, r.body), "aria-current", "page"); !slices.Equal(got, []string{"/talk?with=yardmaster"}) {
		t.Errorf("Crew's list opens %v, want the yardmaster's row", got)
	}
	for _, target := range []string{"/delegations/1", "/delegations/2", "/delegations/x"} {
		token := y.token("sam@example.net")
		if target != "/delegations/1" {
			token = y.token("jo@example.net")
		}
		if r := y.getNoFollow(target, token); r.status != http.StatusNotFound || strings.Contains(r.body, "codex") {
			t.Errorf("GET %s: %d:\n%s", target, r.status, r.body)
		}
	}

	// A delegated pack apply's record says on its page what yardr delegation
	// show prints of it, in its words: how far the run got (a started one is
	// unfinished), the pack and its digest, where it came from and the digest
	// that stood before it, what each object does with its diff and
	// before-state, what was written and why it failed. The objects stand in
	// the record's blob whatever their size, and the page reads them from it:
	// a record of small objects lists them with their diffs as one whose
	// before-state passes the inline bound does.
	const origin = "https://repo.example.com/yardr-packs/main/starter/"
	stood := strings.Repeat("what stood ", 500)
	plan := &store.AskPlan{Objects: []store.AskPlanObject{
		{Kind: "group", Name: "builders", Action: "change"},
		{Kind: "file", Name: "roles/builder.md", Action: "change"},
		{Kind: "file", Name: "roles/reviewer.md", Action: "create"},
	}}
	reading := func(digest string) store.PackReader {
		return func(_ context.Context, name, depot string, _ store.Mappings, _ store.API) (*store.PackReading, error) {
			return &store.PackReading{Name: name, Kind: "git", Depot: depot, Digest: digest, Plan: plan}, nil
		}
	}
	y.own.SetPackReader(reading("abc123"))
	y.own.SetPackFetcher(func(_ context.Context, _, digest string) (store.PackReader, error) { return reading(digest), nil })
	objects := []store.DelegatedPackObject{
		{Kind: "group", Name: "builders", Action: "change", Diff: "model: sonnet -> opus", Before: `{"kind":"claude","model":"sonnet"}`, Had: true},
		{Kind: "file", Name: "roles/builder.md", Action: "change", Diff: "--- roles/builder.md\n+++ roles/builder.md\n@@ -1 +1 @@\n-" + stood + "\n+what stands now", Before: stood, Had: true},
		{Kind: "file", Name: "roles/reviewer.md", Action: "create"},
	}
	bare := []store.DelegatedPackObject{{Kind: "group", Name: "builders", Action: "change"}, {Kind: "file", Name: "roles/builder.md", Action: "change"}, {Kind: "file", Name: "roles/reviewer.md", Action: "create"}}
	small := []store.DelegatedPackObject{
		{Kind: "group", Name: "builders", Action: "change", Diff: "model: sonnet -> opus", Before: `{"kind":"claude","model":"sonnet"}`, Had: true},
		{Kind: "file", Name: "roles/builder.md", Action: "change", Diff: "-the role that stood\n+what stands now", Before: "the role that stood", Had: true},
		{Kind: "file", Name: "roles/reviewer.md", Action: "create", Diff: "+the reviewer's role"},
	}
	written := []string{"group builders", "file roles/builder.md", "file roles/reviewer.md"}
	packCtx := store.WithDelegation(ctx, store.DelegatedCall{For: "jo", Mail: mail, Command: "yardr pack apply starter --depot home"})
	begin := func(from, digest string, objs []store.DelegatedPackObject) uint64 {
		t.Helper()
		id, err := y.st.StartDelegatedPack(packCtx, store.DelegatedPackStart{Pack: "starter", Depot: "home", Digest: digest, From: from, Objects: objs}, "yardmaster")
		must(t, err)
		return id
	}
	finish := func(id uint64, done bool, wrote []string, why string) {
		t.Helper()
		must(t, y.st.FinishDelegatedPack(packCtx, id, done, wrote, why, "yardmaster"))
	}
	embedded := begin("", "abc123", objects)
	finish(embedded, true, written, "")
	// Two runs from the packs repository: the second names the digest the
	// first left, and fails.
	first := begin(origin, "aaa111", objects)
	finish(first, true, written, "")
	failed := begin(origin, "bbb222", objects)
	finish(failed, false, written[:1], "route web-builders: the disk is full")
	little := begin("", "abc123", small)
	finish(little, true, written, "")
	started := begin("", "abc123", bare)
	for _, c := range []struct {
		name string
		id   uint64
		want []string
		not  []string
	}{
		{"done, from the binary", embedded, []string{
			"Record " + itoa(embedded) + ": yardmaster for jo",
			"<dt>state</dt><dd>done</dd>",
			"<dt>pack</dt><dd>starter, digest sha256:abc123</dd>",
			"<dt>written</dt><dd>group builders, file roles/builder.md, file roles/reviewer.md</dd>",
			"<code>group builders</code>: change", "<dt>diff</dt>", "model: sonnet -> opus",
			"<dt>before</dt>", `{"kind":"claude","model":"sonnet"}`, stood,
			"<code>file roles/reviewer.md</code>: create",
			"no one command takes a pack apply back",
		}, []string{"<dt>from</dt>", "<dt>prior digest</dt>", "<dt>failed</dt>", "unfinished"}},
		{"done, objects small enough to stand inline", little, []string{
			"<dt>state</dt><dd>done</dd>",
			"<code>group builders</code>: change", "<dt>diff</dt>", "model: sonnet -> opus",
			"<dt>before</dt>", `{"kind":"claude","model":"sonnet"}`,
			"<code>file roles/builder.md</code>: change", "-the role that stood", "+what stands now", "the role that stood",
			"<code>file roles/reviewer.md</code>: create", "+the reviewer's role",
		}, []string{"<dt>failed</dt>", "unfinished"}},
		{"done, the first from the repository", first, []string{
			"<dt>state</dt><dd>done</dd>",
			"<dt>pack</dt><dd>starter, digest sha256:aaa111</dd>",
			"<dt>from</dt><dd>" + origin + "</dd>",
			"<dt>prior digest</dt><dd>none known</dd>",
		}, []string{"<dt>failed</dt>", "unfinished"}},
		{"failed, with a prior digest", failed, []string{
			"<dt>state</dt><dd>failed</dd>",
			"<dt>pack</dt><dd>starter, digest sha256:bbb222</dd>",
			"<dt>from</dt><dd>" + origin + "</dd>",
			"<dt>prior digest</dt><dd>sha256:aaa111</dd>",
			"<dt>written</dt><dd>group builders</dd>",
			`<dt>failed</dt><dd class="wv-fault">route web-builders: the disk is full</dd>`,
			"<code>file roles/builder.md</code>: change",
		}, []string{"unfinished", "<dt>state</dt><dd>done</dd>"}},
		{"started, left unfinished", started, []string{
			"<dt>state</dt><dd>started (unfinished)</dd>",
			"<dt>pack</dt><dd>starter, digest sha256:abc123</dd>",
		}, []string{"<dt>state</dt><dd>done</dd>", "<dt>written</dt>", "<dt>failed</dt>"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			target := "/delegations/" + itoa(c.id)
			r := y.getNoFollow(target, y.token("jo@example.net"))
			// The page as read: html/template escapes the quote of a command
			// and the plus of a diff.
			body := html.UnescapeString(r.body)
			if r.status != http.StatusOK {
				t.Fatalf("GET %s as jo: %d:\n%s", target, r.status, r.body)
			}
			for _, want := range c.want {
				if !strings.Contains(body, want) {
					t.Errorf("jo's page for record %d lacks %q:\n%s", c.id, want, r.body)
				}
			}
			for _, not := range c.not {
				if strings.Contains(body, not) {
					t.Errorf("jo's page for record %d holds %q:\n%s", c.id, not, r.body)
				}
			}
			pageObjects(t, y, c.id, body)
			// A member is told it is not there, and nothing of it, in either
			// form.
			for _, accept := range []string{"text/html", "application/json"} {
				if r := y.getNoFollow(target, y.token("sam@example.net"), "Accept", accept); r.status != http.StatusNotFound || strings.Contains(r.body, "abc123") || strings.Contains(r.body, "starter") || strings.Contains(r.body, "what stood") {
					t.Errorf("GET %s as sam, accepting %s: %d:\n%s", target, accept, r.status, r.body)
				}
			}
		})
	}
	t.Run("daemon lost", func(t *testing.T) {
		must(t, y.st.CrewStarted(ctx, "yardmaster", map[string]any{"agent": "yard-yardmaster"}))
		must(t, y.st.CrewStatus(ctx, "yardmaster", "working"))
		stopped := time.Now().Add(-time.Hour)
		if _, _, err := y.own.NoteDaemonLost(ctx, stopped); err != nil {
			t.Fatal(err)
		}
		r := y.getNoFollow("/delegations/1", y.token("jo@example.net"))
		if r.status != http.StatusOK || !strings.Contains(squash(r.body), `<h1 class="wv-title">yardmaster</h1><p class="wv-meta">not running<span class="wv-sep">·</span>herdr · claude</p>`) {
			t.Errorf("the record's head keeps the member's stale state after the daemon is lost: %d:\n%s", r.status, r.body)
		}
		var got struct {
			Member *web.CrewRow `json:"member"`
		}
		if r := y.getNoFollow("/delegations/1", y.token("jo@example.net"), "Accept", "application/json"); json.Unmarshal([]byte(r.body), &got) != nil ||
			got.Member == nil || got.Member.State != store.UnknownSince(stopped) || got.Member.Status != "" || got.Member.Running || got.Member.Since != nil {
			t.Errorf("the lost member in the record = %+v, want its current state and no live session", got.Member)
		}
	})
	t.Run("member left", func(t *testing.T) {
		jo := y.token("jo@example.net")
		if r := y.getNoFollow("/delegations/1", jo); len(marked(listOf(t, r.body), "aria-current", "page")) != 1 {
			t.Fatalf("with the member in the crew Crew's list opens %v", marked(listOf(t, r.body), "aria-current", "page"))
		}
		must(t, y.st.DeleteCrew(ctx, "yardmaster", "", "jo"))
		r := y.getNoFollow("/delegations/1", jo)
		if r.status != http.StatusOK || !strings.Contains(squash(r.body), `<header class="wv-head"><h1 class="wv-title">yardmaster</h1></header><div class="wv-letters" id="letters">`) ||
			!strings.Contains(r.body, "Record 1: yardmaster for jo") {
			t.Errorf("the record of a member who left: %d:\n%s", r.status, r.body)
		}
		if got := marked(listOf(t, r.body), "aria-current", "page"); len(got) != 0 {
			t.Errorf("Crew's list opens %v for a member who left, want no row", got)
		}
		// The head has no state line, in the JSON no member either.
		if strings.Contains(r.body, `<p class="wv-meta">`) {
			t.Errorf("the head of a member who left has a state line:\n%s", r.body)
		}
		var got struct {
			Member *web.CrewRow `json:"member"`
		}
		if r := y.getNoFollow("/delegations/1", jo, "Accept", "application/json"); json.Unmarshal([]byte(r.body), &got) != nil || got.Member != nil {
			t.Errorf("the JSON of a record of a member who left names the member: %s", r.body)
		}
	})
}

// pageObjects reads the delegated pack record id's page in its JSON form and
// holds what it says of the objects to the body the record keeps in its blob
// and to the page as the HTML form shows it (shown): the names stand under
// objects, and the objects whole under plan, each with its action, diff and
// before-state, which the HTML form shows too.
func pageObjects(t *testing.T, y *approveYard, id uint64, shown string) {
	t.Helper()
	target := "/delegations/" + itoa(id)
	r := y.getNoFollow(target, y.token("jo@example.net"), "Accept", "application/json")
	var page struct {
		Record struct {
			Objects []string                    `json:"objects"`
			Plan    []store.DelegatedPackObject `json:"plan"`
		} `json:"record"`
	}
	if err := json.Unmarshal([]byte(r.body), &page); err != nil || r.status != http.StatusOK {
		t.Fatalf("GET %s as jo, accepting JSON: %d, %v:\n%s", target, r.status, err, r.body)
	}
	body, err := y.st.DelegationBlob(context.Background(), id, "jo")
	if err != nil || len(body.Objects) == 0 {
		t.Fatalf("record %d's body = %+v, %v; want its objects", id, body, err)
	}
	if !reflect.DeepEqual(page.Record.Plan, body.Objects) {
		// Each field is cut short: a before-state runs to the blob cap.
		t.Errorf("record %d: the page's JSON plan = %+.60v, want the body's objects %+.60v", id, page.Record.Plan, body.Objects)
	}
	var names []string
	for _, o := range body.Objects {
		names = append(names, o.Kind+" "+o.Name)
	}
	if !reflect.DeepEqual(page.Record.Objects, names) {
		t.Errorf("record %d: the page's JSON objects = %v, want the body's names %v", id, page.Record.Objects, names)
	}
	for _, o := range page.Record.Plan {
		if want := "<code>" + o.Kind + " " + o.Name + "</code>: " + o.Action; !strings.Contains(shown, want) {
			t.Errorf("record %d: the page lacks %q, which its JSON plan carries", id, want)
		}
		if o.Diff != "" && !strings.Contains(shown, o.Diff) {
			t.Errorf("record %d: the page lacks the diff of %s %s, which its JSON plan carries: %q", id, o.Kind, o.Name, o.Diff)
		}
		if o.Had && !strings.Contains(shown, o.Before) {
			t.Errorf("record %d: the page lacks the before-state of %s %s, which its JSON plan carries: %.60q", id, o.Kind, o.Name, o.Before)
		}
	}
}
