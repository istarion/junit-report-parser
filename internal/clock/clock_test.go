package clock

import (
	"testing"
	"time"
)

func TestFixedClockIsStable(t *testing.T) {
	ts := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	c := Fixed(ts)
	for i := 0; i < 3; i++ {
		if !c.Now().Equal(ts) {
			t.Fatalf("Fixed clock moved: %v", c.Now())
		}
	}
}

func TestRealClockAdvances(t *testing.T) {
	c := Real()
	first := c.Now()
	time.Sleep(2 * time.Millisecond)
	if !c.Now().After(first) {
		t.Fatal("Real clock did not advance")
	}
}
