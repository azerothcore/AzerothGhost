package e2eharness

import (
	"fmt"
	"testing"
	"time"

	"github.com/walkline/AzerothGhost/client"
)

// CombatReadyOpts configures CombatReady.
type CombatReadyOpts struct {
	// God enables `.cheat god on`.
	God bool
	// Power enables `.cheat power on`.
	Power bool
}

// CombatReady prepares a bot for real combat:
//   - `.gm off` so NPCs can aggro (GM mode blocks threat / can evade mid-fight)
//   - optional god / power cheats
//
// Account GM security still allows `.damage` / `.go` without GM *mode*.
// Do NOT call `.gm on` mid-fight — bosses may evade/reset.
func CombatReady(t *testing.T, w *client.WorldClient, opts CombatReadyOpts) {
	t.Helper()
	MustGM(t, w, ".gm off")
	if opts.God {
		CheatGod(t, w)
	}
	if opts.Power {
		CheatPower(t, w)
	}
}

// CombatReadyDefaults is CombatReady with god on (power off).
func CombatReadyDefaults(t *testing.T, w *client.WorldClient) {
	t.Helper()
	CombatReady(t, w, CombatReadyOpts{God: true})
}

// EngageUntilCombat faces and attacks target until UNIT_FLAG_IN_COMBAT or timeout.
// Falls back to `.damage 1` (without enabling GM mode) if swings alone do not pull.
func EngageUntilCombat(t *testing.T, w *client.WorldClient, targetGUID uint64, timeout time.Duration) {
	t.Helper()
	if targetGUID == 0 {
		Preconditionf(t, "EngageUntilCombat: target guid is 0")
	}
	FaceUnit(t, w, targetGUID)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_ = w.SetTarget(targetGUID)
		_ = w.AttackSwing(targetGUID)
		time.Sleep(400 * time.Millisecond)
		if UnitInCombat(w, targetGUID) {
			t.Logf("engaged 0x%X combat=true", targetGUID)
			return
		}
		// Nudge threat without toggling GM mode.
		_ = w.SetTarget(targetGUID)
		MustGM(t, w, ".damage 1")
		time.Sleep(300 * time.Millisecond)
		if UnitInCombat(w, targetGUID) {
			t.Logf("engaged 0x%X combat=true (via .damage 1)", targetGUID)
			return
		}
	}
	Preconditionf(t, "unit 0x%X never entered combat within %s", targetGUID, timeout)
}

// DamageGM applies `.damage <amount>` to targetGUID. Does NOT toggle GM mode.
func DamageGM(t *testing.T, w *client.WorldClient, targetGUID uint64, amount uint32) {
	t.Helper()
	if targetGUID == 0 {
		Preconditionf(t, "DamageGM: target guid is 0")
	}
	if err := w.SetTarget(targetGUID); err != nil {
		t.Logf("SetTarget before .damage: %v", err)
	}
	MustGM(t, w, fmt.Sprintf(".damage %d", amount))
}

// DamageKillGM damages each GUID until hp==0 or timeout. Does NOT toggle GM mode.
func DamageKillGM(t *testing.T, w *client.WorldClient, guids []uint64, amount uint32, timeout time.Duration) {
	t.Helper()
	if amount == 0 {
		amount = 10_000_000
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		alive := 0
		for _, g := range guids {
			hp, _ := UnitHealth(w, g)
			if hp == 0 {
				continue
			}
			alive++
			DamageGM(t, w, g, amount)
			time.Sleep(50 * time.Millisecond)
		}
		if alive == 0 {
			t.Logf("DamageKillGM: all %d targets dead", len(guids))
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	// Final count
	left := 0
	for _, g := range guids {
		if hp, _ := UnitHealth(w, g); hp > 0 {
			left++
		}
	}
	if left > 0 {
		Preconditionf(t, "DamageKillGM: %d/%d targets still alive after %s", left, len(guids), timeout)
	}
}

// WaitUnitCombat waits until UnitInCombat is true.
func WaitUnitCombat(t *testing.T, w *client.WorldClient, guid uint64, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if UnitInCombat(w, guid) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	Preconditionf(t, "unit 0x%X not in combat within %s", guid, timeout)
}

// WaitUnitDead waits until unit health is 0 or object gone.
func WaitUnitDead(t *testing.T, w *client.WorldClient, guid uint64, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		obj := w.GetObject(guid)
		if obj == nil {
			return
		}
		if obj.Health() == 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	hp, max := UnitHealth(w, guid)
	Preconditionf(t, "unit 0x%X still alive hp=%d/%d after %s", guid, hp, max, timeout)
}

// WaitNearbyUnitAnyEntry waits for any of the given template entries.
// Returns guid and the matched entry.
func WaitNearbyUnitAnyEntry(t *testing.T, w *client.WorldClient, entries []uint32, timeout time.Duration) (guid uint64, entry uint32) {
	t.Helper()
	if len(entries) == 0 {
		HarnessFailf(t, "WaitNearbyUnitAnyEntry: empty entries")
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, e := range entries {
			if g := w.FindUnitByEntry(e, 40); g != 0 {
				t.Logf("live unit entry=%d guid=0x%X", e, g)
				return g, e
			}
			if g := w.FindUnitByEntry(e, 0); g != 0 {
				t.Logf("live unit entry=%d guid=0x%X (no dist filter)", e, g)
				return g, e
			}
		}
		time.Sleep(40 * time.Millisecond)
	}
	units := w.GetNearbyUnits(60)
	for _, u := range units {
		t.Logf("  nearby unit guid=0x%X entry=%d", u.GUID, u.Entry)
	}
	HarnessFailf(t, "no live unit with entries=%v within %s (%d nearby)", entries, timeout, len(units))
	return 0, 0
}

// ProbeWorldAlive checks that a probe bot's world session still responds.
// Use after risky casts that may crash the worldserver.
func ProbeWorldAlive(t *testing.T, probe *ScenarioBot, issue int) {
	t.Helper()
	if probe == nil || !SessionAlive(probe.Session) {
		if issue > 0 {
			ConfirmedBugf(t, issue, "world session dead (probe not alive) — possible worldserver crash")
		}
		HarnessFailf(t, "probe session not alive")
	}
	// Soft GM ping — failure may mean crash mid-command.
	if err := probe.World.SendGMCommand(".gm on"); err != nil {
		if issue > 0 {
			ConfirmedBugf(t, issue, "probe GM command failed (session dead?): %v", err)
		}
		HarnessFailf(t, "probe GM command failed: %v", err)
	}
	t.Logf("probe world alive (session ok)")
}
