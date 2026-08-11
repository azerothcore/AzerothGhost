package e2eharness

import (
	"fmt"
	"testing"
	"time"

	"github.com/walkline/AzerothGhost/client"
)

// Position3 is a map position used by pads and StartPad opts.
type Position3 struct {
	X, Y, Z float32
	Map     uint32
}

// Common teleport pads (shared across AC tests).
var (
	// PadStormwindOutskirts is a flat combat pad outside SW trade district.
	PadStormwindOutskirts = Position3{
		X: -8913.23, Y: 554.91, Z: 93.79, Map: MapEasternKingdoms,
	}
)

// TeleNamed runs `.tele <name>` and waits for far transfer / login when needed.
func TeleNamed(t *testing.T, w *client.WorldClient, name string) {
	t.Helper()
	beforeMap := uint32(0)
	_, _, _, beforeMap = Position(w)
	MustGM(t, w, fmt.Sprintf(".tele %s", name))
	// Far transfers use SMSG_NEW_WORLD; WaitForLogin covers map change.
	if err := w.WaitForLogin(60 * time.Second); err != nil {
		t.Logf("WaitForLogin after .tele %s: %v (continuing)", name, err)
	}
	// Soft settle for instance enter visibility (object creates).
	time.Sleep(500 * time.Millisecond)
	x, y, z, afterMap := Position(w)
	t.Logf("TeleNamed %q -> %.1f,%.1f,%.1f map=%d (was map=%d)", name, x, y, z, afterMap, beforeMap)
}

// GoCreatureGUID teleports to a creature by DB spawn guid (`.go creature N`).
func GoCreatureGUID(t *testing.T, w *client.WorldClient, spawnGUID uint32) {
	t.Helper()
	MustGMTeleport(t, w, fmt.Sprintf(".go creature %d", spawnGUID))
}

// GoCreatureID teleports to a creature by template entry (`.go creature id N`).
// Prefer this after `.tele` when the pad is short of melee range.
func GoCreatureID(t *testing.T, w *client.WorldClient, entry uint32) {
	t.Helper()
	MustGMTeleport(t, w, fmt.Sprintf(".go creature id %d", entry))
}

// TeleportPad teleports to a named pad via .go xyz.
func TeleportPad(t *testing.T, w *client.WorldClient, pad Position3) {
	t.Helper()
	TeleportGo(t, w, pad.X, pad.Y, pad.Z, pad.Map)
}
