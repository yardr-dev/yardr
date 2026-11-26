package web

import (
	"fmt"
	"strconv"
	"strings"
)

// The shell is a window: a rail of four modes, the open mode's list, and a
// stage that holds the page. The rail is the first of them (docs/web.md,
// The window). Each tile opens its mode's root page, the open one stands
// marked, and the tiles of the modes that wait on the person carry a count.
// Under 900 px the rail is the bar at the foot. No page changes its address
// or its handler by it; the list beside the stage is list.go's.

// railTile is one tile of the rail: a mode with its glyph, and the badge it
// carries where the mode waits on the person reading.
type railTile struct {
	Name string // decide, lines, crew, settings
	// Label is the mode's name under its glyph.
	Label string
	// Path is the mode's root page, which the tile opens.
	Path string
	// Glyph is the tile's mark: semaphore, track, lamp, gear. The shell draws
	// each as inline SVG on footplate's 24-grid.
	Glyph string
	// Open is whether the page stands in this mode.
	Open bool
	// Badge is the count's numeral (2, 9+), "" where the tile carries none.
	Badge string
	// Title is the tile's title: the mode, with the count in words where a
	// badge stands. Aria is its aria-label, which says the same with a comma.
	Title, Aria string
}

// rail is the page's rail: the yard's chip and the tiles, in the rail's
// order, Settings last (it stands at the rail's foot).
type rail struct {
	Chip  string
	Tiles []railTile
}

// railModes are the rail's tiles in the rail's order.
var railModes = []railTile{
	{Name: "decide", Label: "Decide", Path: "/", Glyph: "semaphore"},
	{Name: "lines", Label: "Lines", Path: "/flows", Glyph: "track"},
	{Name: "crew", Label: "Crew", Path: "/crew", Glyph: "lamp"},
	{Name: "settings", Label: "Settings", Path: "/config", Glyph: "gear"},
}

// modeFor is the mode the page at path stands in, which is the tile the rail
// marks open and the list the shell shows: a page belongs to its mode by its
// address, not by where the link to it was. "" is no mode (an address no
// page answers, which an error page stands at).
func modeFor(path string) string {
	switch path {
	case "/", "/line":
		return "decide"
	case "/flows", "/depots", "/beads", "/sessions", "/previews", "/deps":
		return "lines"
	case "/crew", "/talk":
		return "crew"
	case "/config", "/groups", "/routes", "/peers", "/limits", "/telemetry", "/pairing":
		return "settings"
	}
	switch {
	case strings.HasPrefix(path, "/beads/"):
		return "decide"
	case strings.HasPrefix(path, "/depots/"):
		return "lines"
	case strings.HasPrefix(path, "/delegations/"):
		return "crew"
	case strings.HasPrefix(path, "/config/"):
		return "settings"
	}
	return ""
}

// pillLabel is the numeral a tile carries for n: 9+ above nine, as
// footplate's badge caps it.
func pillLabel(n int) string {
	if n > 9 {
		return "9+"
	}
	return strconv.Itoa(n)
}

// crewWords is Crew's count in words, for the tile's title and aria-label.
func crewWords(n int) string {
	if n == 1 {
		return "1 unread letter"
	}
	return fmt.Sprintf("%d unread letters", n)
}

// waitWords is Decide's count in words: what the list's head and the tile's
// title say.
func waitWords(n int) string {
	if n == 1 {
		return "1 waits for you"
	}
	return fmt.Sprintf("%d wait for you", n)
}

// railFor is the rail for the page: the tiles, the open mode marked. waits
// is what waits for the person (the list's head counts the same) and unread
// the acting user's aggregate unread mail: Crew's badge, which falls only when
// footplate, yardr chat or yardr inbox acknowledges the mail, since the web
// view acknowledges nothing. Lines carries none, for the web view keeps no
// per-person watermark to count its changed beads against. No badge stands
// at zero, and none on an error page, which passes no mode and no counts.
func railFor(yard, mode string, waits, unread int) rail {
	out := rail{Chip: chipLetters(yard)}
	for _, m := range railModes {
		t := m
		t.Open = m.Name == mode
		t.Title, t.Aria = m.Label, m.Label
		switch {
		case m.Name == "decide" && waits > 0:
			t.Badge = pillLabel(waits)
			t.Title = waitWords(waits)
			t.Aria = m.Label + ", " + waitWords(waits)
		case m.Name == "crew" && unread > 0:
			t.Badge = pillLabel(unread)
			t.Title = m.Label + " — " + crewWords(unread)
			t.Aria = m.Label + ", " + crewWords(unread)
		}
		out.Tiles = append(out.Tiles, t)
	}
	return out
}

// chipLetters are the yard chip's letters: the yard name's first two, as
// footplate's chip carries them.
func chipLetters(yard string) string {
	r := []rune(yard)
	if len(r) > 2 {
		r = r[:2]
	}
	return string(r)
}
