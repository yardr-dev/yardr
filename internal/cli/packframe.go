package cli

import (
	"fmt"

	"yardr.dev/yardr/internal/pack"
)

// appliedLine is what an import that wrote no file says of itself: nothing
// changed, unless the import moved its scope's frame choice to this pack
// (an unchanged re-apply of a displaced frame pack), which the record
// wrote and the plan named in its FrameUse line, or left objects as the yard
// has them, which are counted: an import whose plan had objects that differ
// is not one that found everything as the pack has it.
func appliedLine(plan *pack.Plan, res *pack.Result) string {
	head, what := "is already applied to", "nothing changed"
	moved := res.FrameMoved && plan.FrameUse != ""
	if moved {
		what = "no file changed; " + plan.FrameUse
	}
	if res.Differs > 0 {
		head, what = "is applied to", fmt.Sprintf("nothing written; the yard's own version of %s stands", plural(res.Differs, "object"))
		if moved {
			what += "; " + plan.FrameUse
		}
	}
	return fmt.Sprintf("pack %s %s %s: %s", res.Pack, head, inDepot(res.Depot), what)
}
