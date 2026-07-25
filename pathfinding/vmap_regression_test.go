package pathfinding

import (
	"math"
	"testing"
	"time"
)

func TestVMapGetHeight_StormwindFenceM2DoesNotAllocateFromChunkName(t *testing.T) {
	dataDir := requireACData(t)
	vm := NewVMapManager(dataDir + "/vmaps")
	x, y, z := float32(-8980.235), float32(-93.89123), float32(85.5113)

	done := make(chan struct{}, 1)
	go func() {
		h, ok := vm.GetHeight(0, x, y, z+Z_OFFSET_FIND_HEIGHT, DEFAULT_HEIGHT_SEARCH)
		if ok && (math.IsNaN(float64(h)) || math.IsInf(float64(h), 0)) {
			t.Errorf("GetHeight returned invalid height %.3f", h)
		}
		done <- struct{}{}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("GetHeight timed out; likely stuck parsing/loading vmap model")
	}
}
