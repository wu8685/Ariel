package desktopipc

import "sync"

// Keep the currently available evidence while a native write is awaiting its
// exact postcondition. Historical null projections never count as evidence.
func (f *Follower) pinHistoryForMutation() func() {
	f.stateMu.Lock()
	r := f.o.reducer
	if r.pinned == nil {
		r.pinned = map[string]int{}
	}
	ids := []string{}
	turns, _ := orderedNativeTurns(r.state)
	for _, turn := range turns {
		id, _ := turn["turnId"].(string)
		if id != "" && !containsRetired(turn) {
			r.pinned[id]++
			ids = append(ids, id)
		}
	}
	f.stateMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			f.stateMu.Lock()
			defer f.stateMu.Unlock()
			for _, id := range ids {
				r.pinned[id]--
				if r.pinned[id] <= 0 {
					delete(r.pinned, id)
				}
			}
			if r.state != nil && r.recent {
				if err := retireHistoricalBodies(r.state, r.pinned); err != nil {
					f.err = err
					_ = f.c.Close()
					return
				}
				r.stateBytes = encodedValueSize(r.state)
				f.o.summary.StateBytes = r.stateBytes
			}
		})
	}
}
