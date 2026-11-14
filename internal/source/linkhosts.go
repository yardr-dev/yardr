package source

import (
	"net/url"
	"slices"
	"strings"
)

// LinkHosts are the hosts the source's issues are linked on: link.host
// first, then the hosts of the declaration's named bases, each once, in
// sorted base-name order. base: is the API endpoint, never an
// issue host, and stays out: matching it would fix phantom issue URLs,
// two fixed forms for one issue. Nil for a source without a link.
func (d *Declaration) LinkHosts() []string {
	if d == nil || d.Link == nil || d.Link.Host == "" {
		return nil
	}
	hosts := []string{d.Link.Host}
	for _, name := range d.BaseNames() {
		u, err := url.Parse(d.Bases[name])
		if err != nil || u.Hostname() == "" {
			continue
		}
		if !slices.ContainsFunc(hosts, func(h string) bool { return strings.EqualFold(h, u.Hostname()) }) {
			hosts = append(hosts, u.Hostname())
		}
	}
	return hosts
}

// SharedLinkHost is the first host of to that any host of from matches,
// in to's spelling; ok is false when the sets share none. The move of a
// yard's links from one source to another keeps the URLs, so it needs
// one.
func SharedLinkHost(from, to []string) (shared string, ok bool) {
	for _, h := range to {
		if slices.ContainsFunc(from, func(f string) bool { return strings.EqualFold(f, h) }) {
			return h, true
		}
	}
	return "", false
}
