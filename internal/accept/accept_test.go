package accept

import "testing"

// TestAcceptance runs all §19 fixtures (plan §13). Every fixture runs with
// piped stdout, so the harness also asserts no ANSI escapes anywhere
// (fixture 13).
func TestAcceptance(t *testing.T) {
	all := []string{
		"F01", "F02", "F03", "F04", "F05", "F06", "F07", "F08", "F09", "F10",
		"F11", "F12", "F13", "F14", "F15", "F16", "F17", "F18", "F19", "F20",
		"F21", "F22",
	}
	for _, id := range Selected(all) {
		id := id
		t.Run(id, func(t *testing.T) {
			RunFixture(t, id)
		})
	}
}
