package bot

import (
	"testing"

	"github.com/walkline/AzerothGhost/client"
)

// TestSummonNearTeleport_InterruptsAndFlagsLua mirrors the log sequence:
//   in_world -> near_teleport (MSG_MOVE_TELEPORT_ACK(smsg))
//   near_teleport -> in_world (MSG_MOVE_TELEPORT_ACK)
// and asserts movement/combat sticky state is cleared and Lua can consume the event.
func TestSummonNearTeleport_InterruptsAndFlagsLua(t *testing.T) {
	w := client.NewWorldClient("u", nil, func(string, ...interface{}) {})
	w.UpdatePosition(10, 20, 30, 1.0)

	b := NewHeadlessBot(w, Config{Mode: "lua", AITickMs: 200})
	b.movementMu.Lock()
	b.ensureMovementControllerLocked()
	if b.moveController != nil {
		b.moveController.InitPositionFromWorld(10, 20, 30, 1.0)
	}
	b.isMoving = true
	b.movementMu.Unlock()

	b.grindTargetGUID = 999
	b.lastPursuedTargetGUID = 999
	b.lastLootGUID = 888

	// Enter near_teleport (same as user log reason prefix).
	if b.world.OnSessionPhase == nil {
		t.Fatal("OnSessionPhase not wired")
	}
	b.world.OnSessionPhase(client.SessionPhaseChange{
		From:   client.PhaseInWorld,
		To:     client.PhaseNearTeleport,
		Reason: "MSG_MOVE_TELEPORT_ACK(smsg)",
	})
	if b.movementActive() {
		t.Fatal("expected movement aborted on near_teleport enter")
	}

	// Server relocates player before ACK completes.
	w.UpdatePosition(1500, 2500, 40, 2.5)

	b.world.OnSessionPhase(client.SessionPhaseChange{
		From:   client.PhaseNearTeleport,
		To:     client.PhaseInWorld,
		Reason: "MSG_MOVE_TELEPORT_ACK",
	})

	if !b.ConsumeTeleport() {
		t.Fatal("expected ConsumeTeleport true after resume")
	}
	if b.ConsumeTeleport() {
		t.Fatal("expected ConsumeTeleport false on second call")
	}

	if b.grindTargetGUID != 0 || b.lastPursuedTargetGUID != 0 || b.lastLootGUID != 0 {
		t.Fatalf("sticky state not cleared: grind=%d pursue=%d loot=%d",
			b.grindTargetGUID, b.lastPursuedTargetGUID, b.lastLootGUID)
	}
	if b.movementActive() {
		t.Fatal("still moving after teleport resume")
	}

	b.movementMu.Lock()
	if b.moveController == nil {
		b.movementMu.Unlock()
		t.Fatal("nil moveController")
	}
	cx, cy, cz, _ := b.moveController.CurrentPosition()
	b.movementMu.Unlock()
	if cx != 1500 || cy != 2500 || cz != 40 {
		t.Fatalf("controller pose=(%v,%v,%v) want post-teleport (1500,2500,40)", cx, cy, cz)
	}

	// updateMovement must not overwrite world pose with pre-teleport controller coords.
	b.updateMovement()
	wx, wy, wz, _, _ := w.Position()
	if wx != 1500 || wy != 2500 || wz != 40 {
		t.Fatalf("world pose corrupted by updateMovement: (%v,%v,%v)", wx, wy, wz)
	}
}
