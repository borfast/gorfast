package web

import "sync"

// HoldHashSlots takes n hash slots, as n sign-ups and sign-ins in progress
// would, and returns a function that releases them. It panics if none is free.
func HoldHashSlots(n int) (release func()) {
	for range n {
		select {
		case hashSlots <- struct{}{}:
		default:
			panic("web: HoldHashSlots: every hash slot is taken")
		}
	}
	return sync.OnceFunc(func() {
		for range n {
			<-hashSlots
		}
	})
}
