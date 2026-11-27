package web_test

import (
	"context"
	"strings"
	"testing"

	"yardr.dev/yardr/internal/store"
)

// The routes page says every label a route asks for and the bead
// priorities it takes, and a route that asks for neither says neither.
func TestRoutesPageSaysLabelsAndBeadPriorities(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	must(t, f.st.PutGroup(ctx, store.Group{Name: "g", Runner: "manual"}, "me"))
	_, err := f.st.AddRoute(ctx, store.Route{Stage: "plain", Group: "g"}, "me")
	must(t, err)
	// A route with no label and no bead priority has a dash in their columns.
	if page := squash(f.get("/routes").body); !strings.Contains(page, `<tr><td class="wv-name">plain</td>`+strings.Repeat(`<td><span class="wv-nil">–</span></td>`, 4)+`<td>g</td>`) {
		t.Fatalf("a route with no label and no bead priority says one:\n%s", page)
	}
	hard, urgent := "difficulty:3", 1
	_, err = f.st.AddRoute(ctx, store.Route{Stage: "new", Group: "g", Label: &hard, AndLabels: []string{"size:large"}, BeadPriority: &urgent}, "me")
	must(t, err)
	page := squash(f.get("/routes").body)
	if want := `<tr><td class="wv-name">new</td>` + strings.Repeat(`<td><span class="wv-nil">–</span></td>`, 2) + `<td>difficulty:3, size:large</td><td>P0-P1</td><td>g</td>`; !strings.Contains(page, want) {
		t.Errorf("the routes page lacks %q:\n%s", want, page)
	}
}
