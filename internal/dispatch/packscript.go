package dispatch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"yardr.dev/yardr/internal/config"
	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/packs"
)

// packScriptReason appends a line to reason when an exec script group's run
// ends in died or failed and its cmd names a script under
// <home>/packs/<line>/scripts/<script>. For an embedded line, it compares
// the on-disk copy against the running binary's script: equal bytes add
// nothing, differing bytes say so with the --replace command. For a line
// not embedded in the binary, the line names the script alone. Scripts
// elsewhere (including <home>/scripts/) are left alone.
func (d *Dispatcher) packScriptReason(ctx context.Context, s store.Session, state, reason string) string {
	if (state != "died" && state != "failed") || d.Home == "" || s.Group == "" {
		return reason
	}
	g, err := d.Store.GetGroup(ctx, s.Group)
	if err != nil || g == nil || g.Runner != "exec" {
		return reason
	}
	cmd, _ := g.Config["cmd"].(string)
	script, _ := config.ScriptOf(cmd)
	if script == "" {
		return reason
	}
	line, scriptRel, ok := packScriptPath(d.Home, script)
	if !ok {
		return reason
	}
	relInPack := path.Join("scripts", scriptRel)
	if !packs.Carries(line) {
		return appendReasonLine(reason, fmt.Sprintf("script: packs/%s/%s", line, relInPack))
	}
	orig, err := fs.ReadFile(packs.FS, path.Join(line, relInPack))
	if err != nil {
		return appendReasonLine(reason, fmt.Sprintf("script: packs/%s/%s", line, relInPack))
	}
	info, err := os.Stat(script)
	if errors.Is(err, fs.ErrNotExist) {
		depot := packScriptDepot(ctx, d.Store, s.Bead)
		return appendReasonLine(reason, fmt.Sprintf("%s is missing from the yard's copy; take it: yardr pack apply %s%s",
			relInPack, line, depot))
	}
	if err != nil || !info.Mode().IsRegular() {
		return reason
	}
	differs := info.Mode().Perm()&0o111 == 0
	if !differs {
		have, err := os.ReadFile(script)
		if err != nil || bytes.Equal(have, orig) {
			return reason
		}
	}
	depot := packScriptDepot(ctx, d.Store, s.Bead)
	return appendReasonLine(reason, fmt.Sprintf("%s differs from yardr's; take it: yardr pack apply %s%s --replace %s",
		relInPack, line, depot, relInPack))
}

func packScriptDepot(ctx context.Context, st store.API, bead string) string {
	depot := ""
	if bead != "" {
		if b, err := st.GetBead(ctx, bead); err == nil && b != nil && b.Depot != nil && *b.Depot != "" {
			depot = " --depot " + *b.Depot
		}
	}
	return depot
}

// packScriptPath parses script when it lies under <home>/packs/<line>/scripts/<rel>.
func packScriptPath(home, script string) (line, scriptRel string, ok bool) {
	rel, err := filepath.Rel(config.At(home).Packs(), script)
	if err != nil || !filepath.IsLocal(rel) {
		return "", "", false
	}
	slash := filepath.ToSlash(rel)
	line, rest, ok := strings.Cut(slash, "/")
	if !ok || line == "" {
		return "", "", false
	}
	scriptRel, ok = strings.CutPrefix(rest, "scripts/")
	if !ok || scriptRel == "" || !fs.ValidPath(scriptRel) {
		return "", "", false
	}
	return line, scriptRel, true
}

func appendReasonLine(reason, line string) string {
	if reason == "" {
		return line
	}
	return reason + "\n" + line
}
