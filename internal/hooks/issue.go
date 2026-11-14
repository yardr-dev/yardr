package hooks

import (
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"

	"yardr.dev/yardr/internal/source"
	"yardr.dev/yardr/internal/store"
)

// Linker is a source whose issues a bead can link to (yardr link). Nothing
// here calls the source: a link is made from the URL alone.
type Linker interface {
	// IssueHost is the primary host the source's issue URLs are on; a
	// declared source matches its named bases' hosts too.
	IssueHost() string
	// Issue turns a URL on one of those hosts into the issue's fixed
	// form, the one spelling a link stores and compares, and refuses
	// what is no issue of the source. u has an http(s) scheme and one
	// of the source's hosts.
	Issue(u *url.URL) (string, error)
}

// LinkSources lists the sources a bead can link to, sorted: the builtin
// ones, and the yard's declared sources among decls with a link key.
func LinkSources(decls ...*source.Declaration) []string {
	var out []string
	for name, ad := range Adapters {
		if _, ok := ad.(Linker); ok && !declaresName(name, decls) {
			out = append(out, name)
		}
	}
	for _, d := range decls {
		if d == nil {
			continue
		}
		if d.Link != nil {
			out = append(out, d.Name)
		}
	}
	sort.Strings(out)
	return out
}

// declaredLink is the yard's declared source of that name among decls, when
// it has a link key.

// NamesOrNone joins names as an error names what the yard's sources can
// do, with sep between them: "none of the yard's sources" when nothing
// can.
func NamesOrNone(names []string, sep string) string {
	if len(names) == 0 {
		return "none of the yard's sources"
	}
	return strings.Join(names, sep)
}

func declaredLink(name string, decls []*source.Declaration) *source.Declaration {
	for _, d := range decls {
		if d != nil && d.Name == name && d.Link != nil {
			return d
		}
	}
	return nil
}

func declaresName(name string, decls []*source.Declaration) bool {
	for _, d := range decls {
		if declaredName(name, d) {
			return true
		}
	}
	return false
}

// Puller is what an Adapter whose source has pull requests adds: a bead can
// track one (dispatch.Job.Pull), and a delivery of a CI run names its pull
// request by repository and number alone.
type Puller interface {
	// PullOf is the thread of the pull request raw names, in the form
	// IssueThread returns it; "" when raw is no pull request of the
	// source's.
	PullOf(raw string) string
	// PullURL is the URL of the pull request number of repo, as a link
	// stores it, on the primary host: it takes no URL, so a caller
	// naming a pull request of another host of the set by repository
	// and number does not resolve.
	PullURL(repo, number string) string
}

// PullerFor is the source's Puller, if it has pull requests: the one
// built from d, the yard's declared source of that name, when its link
// has a pull shape, else the builtin one of that name. No name is both,
// so at most one applies. d is nil when the yard declares no such
// source.
func PullerFor(source string, d *source.Declaration) (Puller, bool) {
	if dec, ok := declared(source, d, "", ""); ok {
		if dec.D.Link != nil && dec.D.Link.Pull != nil {
			return dec, true
		}
		return nil, false
	}
	p, ok := Adapters[source].(Puller)
	return p, ok
}

// ThreadAlias is the other URL the source uses for the same numbered
// thread: an issue and its pull request share their groups, and a bead
// may have been linked with either form. "" when raw names no thread of
// the source's, or the source has one shape alone, or its shapes number
// apart (link.pull.apart): an issue and a pull request of one number are
// then two threads, and a caller looks no further than the URL it has.
func ThreadAlias(source string, d *source.Declaration, raw string) string {
	if dec, ok := declared(source, d, "", ""); ok {
		if dec.D.Link == nil || dec.D.Link.Pull == nil {
			return ""
		}
		u, err := url.Parse(strings.TrimSpace(raw))
		if err != nil {
			return ""
		}
		matched, ok := matchLinkHost(dec, u.Hostname())
		if !ok {
			return ""
		}
		alias, ok := dec.D.Link.AliasOf(u.Path)
		if !ok {
			return ""
		}
		return reemitHost(alias, matched)
	}
	return ""
}

// PullParts reads the repository and the number from a pull request URL
// of the source's: "" when raw is no pull request URL of a source with
// pull requests.
func PullParts(source string, d *source.Declaration, raw string) (repo, number string) {
	if dec, ok := declared(source, d, "", ""); ok {
		if dec.D.Link == nil || dec.D.Link.Pull == nil {
			return "", ""
		}
		u, err := url.Parse(strings.TrimSpace(raw))
		if err != nil {
			return "", ""
		}
		if _, ok := matchLinkHost(dec, u.Hostname()); !ok {
			return "", ""
		}
		repo, number, _ := dec.D.Link.PullParts(u.Path)
		return repo, number
	}
	return "", ""
}

// NamesIssue reports whether raw, the URL of a delivery's thread, is an issue
// or pull request of the source's: one its link shape reads. A thread with no
// URL, and a source that declares no link shape, are taken to be: nothing
// says they are not. A thread that is not has no comments to answer in and no
// status to set; the branch a CI run on no pull request belongs to is one.
func NamesIssue(source string, d *source.Declaration, raw string) bool {
	dec, ok := declared(source, d, "", "")
	return !ok || dec.namesIssue(raw)
}

// namesIssue is NamesIssue for the declared source d.
func (d Declared) namesIssue(raw string) bool {
	if raw == "" || d.D.Link == nil {
		return true
	}
	_, err := d.IssueThread(raw)
	return err == nil
}

// Issue is the source and the fixed form of the issue raw names: the
// source is told by the URL's host, a builtin one before a declared one
// with the same host. Two URLs are of the same issue exactly when their
// fixed forms are equal. decls are the yard's declared sources.
func Issue(raw string, decls ...*source.Declaration) (source, fixed string, err error) {
	return IssueWithHooks(raw, decls, nil)
}

// IssueWithHooks is Issue with the yard's hooks taken into account: when a
// builtin source and a declared source share the URL's host, a declared
// source that has a hook in the yard is the link's source before a builtin
// one that has none. hooked reports whether the yard has a hook of the
// source, by name. Without hooks (nil), as Issue: a builtin before a
// declared one. No name is both: a declaration may not take a built-in
// name, so a builtin and a declared source meet only on a host.
//
// The order tried is: builtins with a hook, declared sources with a hook,
// builtins without one, declared sources without one; within each, builtins
// by name and declared sources as given. The first whose host matches and
// whose Issue takes the URL wins; a URL no Issue takes gets the error of
// the first whose host matches, and one on no known host the list of hosts.
func IssueWithHooks(raw string, decls []*source.Declaration, hooked map[string]bool) (source, fixed string, err error) {
	u, err := issueURL(raw)
	if err != nil {
		return "", "", err
	}
	type candidate struct {
		name string
		link Linker
	}
	var builtins, declared []candidate
	for _, d := range decls {
		if d == nil || d.Link == nil {
			continue
		}
		declared = append(declared, candidate{d.Name, Declared{D: d}})
	}
	for _, name := range LinkSources() {
		if declaresName(name, decls) {
			continue
		}
		builtins = append(builtins, candidate{name, Adapters[name].(Linker)})
	}
	ordered := []candidate{}
	for _, c := range builtins {
		if hooked[c.name] {
			ordered = append(ordered, c)
		}
	}
	for _, c := range declared {
		if hooked[c.name] {
			ordered = append(ordered, c)
		}
	}
	for _, c := range builtins {
		if !hooked[c.name] {
			ordered = append(ordered, c)
		}
	}
	for _, c := range declared {
		if !hooked[c.name] {
			ordered = append(ordered, c)
		}
	}
	var firstErr error
	var firstName string
	matched := false
	for _, c := range ordered {
		if _, ok := matchLinkHost(c.link, u.Hostname()); !ok {
			continue
		}
		matched = true
		fixed, err = c.link.Issue(u)
		if err == nil {
			return c.name, fixed, nil
		}
		if firstErr == nil {
			firstErr, firstName = err, c.name
		}
	}
	if matched {
		return firstName, "", firstErr
	}
	var hosts []string
	for _, c := range append(builtins, declared...) {
		for _, h := range linkHosts(c.link) {
			if !slices.Contains(hosts, h) {
				hosts = append(hosts, h)
			}
		}
	}
	return "", "", fmt.Errorf("%s is on %s, where yardr knows no issues (it does on %s)", raw, u.Hostname(), NamesOrNone(hosts, " and "))
}

// IssueIn is the fixed form of raw as an issue of source, for a URL whose
// source is already known (a delivery's thread). decls are the yard's
// declared sources.
func IssueIn(source, raw string, decls ...*source.Declaration) (string, error) {
	var l Linker
	if d := declaredLink(source, decls); d != nil {
		l = Declared{D: d}
	} else if declaresName(source, decls) {
		return "", fmt.Errorf("a bead cannot link to an issue in %s (it can in %s)", or(source, "an unknown source"), NamesOrNone(LinkSources(decls...), ", "))
	} else if b, ok := Adapters[source].(Linker); ok {
		l = b
	} else {
		return "", fmt.Errorf("a bead cannot link to an issue in %s (it can in %s)", or(source, "an unknown source"), NamesOrNone(LinkSources(decls...), ", "))
	}
	u, err := issueURL(raw)
	if err != nil {
		return "", err
	}
	if _, ok := matchLinkHost(l, u.Hostname()); !ok {
		return "", fmt.Errorf("%s is on %s, not on %s: no %s issue", raw, u.Hostname(), strings.Join(linkHosts(l), " and "), source)
	}
	return l.Issue(u)
}

// The store brings every link's URL to its fixed form through this, so one
// issue is one link whoever names it; it cannot call Issue itself, since
// this package imports it.
func init() { store.IssueForm(storedIssue) }

// storedIssue is the fixed form of raw for the store (store.IssueForm):
// as an issue of source, or with no source of the one whose host it is on.
// A source that links no issues, or a host no source has issues on, has no
// fixed form, and raw is kept as it is.
func storedIssue(source, raw string) (string, error) {
	if source != "" {
		if _, ok := Adapters[source].(Linker); !ok {
			return raw, nil
		}
		return IssueIn(source, raw)
	}
	u, err := issueURL(raw)
	if err != nil {
		return "", err
	}
	for _, name := range LinkSources() {
		if l := Adapters[name].(Linker); strings.EqualFold(u.Hostname(), l.IssueHost()) {
			return l.Issue(u)
		}
	}
	return raw, nil
}

func issueURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, fmt.Errorf("%q is not the URL of an issue (want https://...)", raw)
	}
	return u, nil
}

func or(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// linkerBases is what a Linker built from a declaration carries beside
// IssueHost: the declaration, whose named bases' hosts the link matches
// too. A builtin Linker carries none and matches its host alone.
type linkerBases interface {
	linkDeclaration() *source.Declaration
}

func (d Declared) linkDeclaration() *source.Declaration { return d.D }

// linkHosts are the hosts a link of the source matches: IssueHost alone,
// plus the named bases' hosts for a declared source.
func linkHosts(l Linker) []string {
	if d, ok := l.(linkerBases); ok {
		if hosts := d.linkDeclaration().LinkHosts(); len(hosts) > 0 {
			return hosts
		}
	}
	return []string{l.IssueHost()}
}

// matchLinkHost is the host of the link's set host is on, lowercased for
// the fixed form. Matching is exact per host: a subdomain or a suffix of
// one matches nothing.
func matchLinkHost(l Linker, host string) (string, bool) {
	for _, h := range linkHosts(l) {
		if strings.EqualFold(h, host) {
			return strings.ToLower(h), true
		}
	}
	return "", false
}

// reemitHost is fixed on host: the template's scheme and path kept, the
// host replaced. A fixed form already on the host comes back as it is.
func reemitHost(fixed, host string) string {
	u, err := url.Parse(fixed)
	if err != nil || u.Host == host {
		return fixed
	}
	if port := u.Port(); port != "" {
		u.Host = host + ":" + port
	} else {
		u.Host = host
	}
	return u.String()
}

// Declared

func (d Declared) IssueHost() string { return d.D.Link.Host }

// Issue is the issue's one fixed form: link.fixed over the groups
// link.issue reads from the URL's path, or the pull request's, for a
// source with a pull shape, on the host the URL arrived on. It runs
// nothing: a link is made from the URL alone.
func (d Declared) Issue(u *url.URL) (string, error) {
	if fixed, ok := d.D.Link.FixedForm(u.Path); ok {
		return reemitIssueHost(d, u, fixed), nil
	}
	if d.D.Link.Pull != nil {
		if fixed, ok := d.D.Link.PullFixedForm(u.Path); ok {
			return reemitIssueHost(d, u, fixed), nil
		}
		return "", fmt.Errorf("%s is no %s issue or pull request", u, d.D.Name)
	}
	return "", fmt.Errorf("%s is no %s issue", u, d.D.Name)
}

// reemitIssueHost is fixed on the host u arrived on, lowercased: US and
// EU issues are different issues, so their fixed forms differ by host. A
// URL on no host of the set keeps the form the template gives, as the
// callers that checked the host before this never pass one.
func reemitIssueHost(d Declared, u *url.URL, fixed string) string {
	if matched, ok := matchLinkHost(d, u.Hostname()); ok {
		return reemitHost(fixed, matched)
	}
	return fixed
}

// PullOf is the thread of the pull request raw names, "" when it names
// none: the URL's host is one of the link's, and its path matches the
// link's pull shape.
func (d Declared) PullOf(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	if _, ok := matchLinkHost(d, u.Hostname()); !ok {
		return ""
	}
	thread, _ := d.D.Link.PullThreadOf(u.Path)
	return thread
}

// PullURL is where the pull request number of repo is read: the pull
// fixed template over the repository's path and the number, on the
// primary host. "" when they are no pull request of the source's.
func (d Declared) PullURL(repo, number string) string {
	url, _ := d.D.Link.PullURL(repo, number)
	return url
}
