package pack

import "yardr.dev/yardr/internal/store"

// lineNeeds fills Plan.LineNeeds without running checks. A non-empty record
// takes precedence because a later apply keeps the yard's edited list.
func (b *planner) lineNeeds() error {
	needs := b.p.Needs
	if b.rec != nil {
		b.plan.recordedNeeds = b.rec.Needs
		if b.rec.Needs != "" {
			var err error
			needs, err = UnmarshalLineNeeds(b.rec.Needs)
			if err != nil {
				return err
			}
		}
	}
	b.plan.needs = needs
	if len(needs) == 0 {
		return nil
	}
	peerOnly := planGroupsPeerOnly(b.plan.Objects)
	statuses, err := CheckLineNeeds(b.ctx, b.st, needs, peerOnly, false)
	if err != nil {
		return err
	}
	b.plan.LineNeeds = statuses
	return nil
}

// planGroupsPeerOnly reports whether the plan has at least one group and
// every group in it is worked by the peer runner.
func planGroupsPeerOnly(objects []Object) bool {
	found := 0
	for _, o := range objects {
		if o.Kind != "group" || o.Group == nil {
			continue
		}
		found++
		if o.Group.Runner != store.PeerRunner {
			return false
		}
	}
	return found > 0
}
