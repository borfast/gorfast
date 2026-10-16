package bunstore

import "time"

// SetChallengeClock replaces the store's clock, for tests.
func SetChallengeClock(s *ChallengeStore, now func() time.Time) { s.now = now }
