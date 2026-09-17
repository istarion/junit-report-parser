// Package clock provides an injectable time source so tests and acceptance
// goldens are deterministic (plan D16).
package clock

import "time"

// Clock wraps the current-time function used by the tool.
type Clock struct {
	Now func() time.Time
}

// Real returns a Clock wired to time.Now (wired by main).
func Real() Clock {
	return Clock{Now: time.Now}
}

// Fixed returns a Clock pinned to t; for tests and the acceptance harness.
func Fixed(t time.Time) Clock {
	return Clock{Now: func() time.Time { return t }}
}
