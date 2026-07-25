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

// TestChargeServerRelocate_DoesNotRubberBand: after a Charge-like server relocate,
// updateMovement must not write the pre-charge path pose back over the new coords.
func TestChargeServerRelocate_DoesNotRubberBand(t *testing.T) {
	w := client.NewWorldClient("u", nil, func(string, ...interface{}) {})
	w.UpdatePosition(10, 20, 30, 1.0)

	b := NewHeadlessBot(w, Config{Mode: "lua", AITickMs: 200})
	b.movementMu.Lock()
	b.ensureMovementControllerLocked()
	if b.moveController != nil {
		// Simulate an active chase path from the charge cast position.
		b.moveController.InitPositionFromWorld(10, 20, 30, 1.0)
		b.isMoving = true
	}
	b.movementMu.Unlock()

	// Server relocates player to charge destination (as MONSTER_MOVE would).
	if b.world.OnServerRelocate == nil {
		t.Fatal("OnServerRelocate not wired")
	}
	b.world.OnServerRelocate(100, 200, 40, 1.5, "monster_move_charge")
	w.UpdatePosition(100, 200, 40, 1.5)

	if b.movementActive() {
		t.Fatal("expected path aborted after server relocate")
	}
	b.movementMu.Lock()
	cx, cy, cz, _ := b.moveController.CurrentPosition()
	b.movementMu.Unlock()
	if cx != 100 || cy != 200 || cz != 40 {
		t.Fatalf("controller pose=(%v,%v,%v) want post-charge", cx, cy, cz)
	}

	b.updateMovement()
	wx, wy, wz, _, _ := w.Position()
	if wx != 100 || wy != 200 || wz != 40 {
		t.Fatalf("rubber-band: world pose became (%v,%v,%v)", wx, wy, wz)
	}
}

// TestChargeRubberBand_WithActivePathSimulatesPreFix: while a local path is
// active, a server relocate must AbortAndSnap so updateMovement cannot write the
// pre-charge path pose over the charge landing (the live rubber-band bug).
func TestChargeRubberBand_WithActivePathSimulatesPreFix(t *testing.T) {
	w := client.NewWorldClient("u", nil, func(string, ...interface{}) {})
	w.UpdatePosition(0, 0, 10, 0)

	b := NewHeadlessBot(w, Config{Mode: "lua", AITickMs: 200})
	// Active chase path from cast origin toward a far waypoint.
	b.moveToPoint(200, 0, 10)
	if !b.movementActive() {
		t.Fatal("expected active path before charge")
	}

	// Charge landing (server + world pose).
	const cx, cy, cz float32 = 50, 80, 12
	b.world.OnServerRelocate(cx, cy, cz, 0.5, "monster_move_charge")
	w.UpdatePosition(cx, cy, cz, 0.5)

	if b.movementActive() {
		t.Fatal("path should be aborted after relocate")
	}

	// Pre-fix: isMoving stayed true → updateMovement rewrote world to ~path start.
	// Post-fix: path aborted → world pose stays at charge destination.
	for i := 0; i < 10; i++ {
		b.updateMovement()
		x, y, z, _, _ := w.Position()
		if abs32(x-cx) > 0.01 || abs32(y-cy) > 0.01 || abs32(z-cz) > 0.01 {
			t.Fatalf("tick %d rubber-band: pos=(%v,%v,%v) want (%v,%v,%v)", i, x, y, z, cx, cy, cz)
		}
	}
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// TestCastChargeDoesNotAbortPathOnAttempt: a failed/pending Charge must not
// cancel chase (that froze lvl-1 bots staring at mobs from 12 yards).
func TestCastChargeDoesNotAbortPathOnAttempt(t *testing.T) {
	w := client.NewWorldClient("u", nil, func(string, ...interface{}) {})
	w.UpdatePosition(0, 0, 10, 0)
	b := NewHeadlessBot(w, Config{Mode: "lua", AITickMs: 200})
	b.moveToPoint(100, 0, 10)
	if !b.movementActive() {
		t.Fatal("need active path")
	}
	_ = b.CastSpell(100, 999)
	if !b.movementActive() {
		t.Fatal("Charge cast attempt must not abort chase path")
	}
}
