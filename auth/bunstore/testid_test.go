package bunstore_test

import (
	"sync"
	"uuid"
)

var (
	testIDsMu sync.Mutex
	testIDs   = map[string]uuid.UUID{}
)

// testID returns the same version 7 UUID for a name on every call, so tests
// can keep readable names such as "alice" for their IDs.
func testID(name string) uuid.UUID {
	testIDsMu.Lock()
	defer testIDsMu.Unlock()
	id, ok := testIDs[name]
	if !ok {
		id = uuid.NewV7()
		testIDs[name] = id
	}
	return id
}
