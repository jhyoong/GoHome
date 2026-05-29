package guard

import (
	"sync"
	"testing"
)

func TestYolo_BasicToggle(t *testing.T) {
	g := NewGuard(emptyWhitelist(t), &fakeFrontend{})

	if g.Yolo() {
		t.Error("expected yolo=false initially")
	}

	g.SetYolo(true)
	if !g.Yolo() {
		t.Error("expected yolo=true after SetYolo(true)")
	}

	g.SetYolo(false)
	if g.Yolo() {
		t.Error("expected yolo=false after SetYolo(false)")
	}
}

func TestYolo_ConcurrentAccess(t *testing.T) {
	g := NewGuard(emptyWhitelist(t), &fakeFrontend{})

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			// Alternate set/get in parallel; just exercising the race detector.
			g.SetYolo(n%2 == 0)
			_ = g.Yolo()
		}(i)
	}

	wg.Wait()
	// No assertions needed beyond race-detector cleanliness.
}
