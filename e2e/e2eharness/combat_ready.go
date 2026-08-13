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

// EngageUntilCombat faces and attacks target until the pull is observed, or timeout.
// Pull is accepted when either:
//   - UNIT_FLAG_IN_COMBAT is set on the target, or
//   - target HP drops while still alive (training dummies often take .damage / swings
//     without ever setting IN_COMBAT — Heroic Training Dummy is the common case), or
//   - the unit is targeting this player (thrash pads may clear IN_COMBAT briefly).
//
// Falls back to `.damage 1` (without enabling GM mode) if swings alone do not pull.
//
// If the unit dies before a pull is observed (common with L1 target dummies
// vs L80 autoattack), fails immediately with a oneshot precondition instead of
// burning the full timeout swinging a corpse.
func EngageUntilCombat(t *testing.T, w *client.WorldClient, targetGUID uint64, timeout time.Duration) {
	t.Helper()
	if targetGUID == 0 {
		Preconditionf(t, "EngageUntilCombat: target guid is 0")
	}
	// Multi-bot: unit may not be in this client's cache yet after another bot's Spawn.
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if w.GetObject(targetGUID) != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if w.GetObject(targetGUID) == nil {
		Preconditionf(t, "EngageUntilCombat: unit 0x%X never appeared in object cache within %s", targetGUID, timeout)
	}

	// Quiet the bot so pad thrash does not cancel AttackSwing mid-pull.
	MustGM(t, w, ".combatstop")

	startHP, startMax := UnitHealth(w, targetGUID)
	if startMax == 0 {
		// Fields not populated yet — wait briefly for UNIT_FIELD_HEALTH.
		fieldDeadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(fieldDeadline) {
			startHP, startMax = UnitHealth(w, targetGUID)
			if startMax > 0 {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	self := w.CharGUID()
	pulled := func() bool {
		if UnitInCombat(w, targetGUID) {
			return true
		}
		// Dummy targeting the puller is a reliable thrash-tolerant oracle.
		if self != 0 && UnitTargetGUID(w, targetGUID) == self {
			return true
		}
		// Damage-taken oracle: HP reduced, still alive. Training dummies rarely set IN_COMBAT.
		hp, max := UnitHealth(w, targetGUID)
		if max > 0 && hp > 0 && startHP > 0 && hp < startHP {
			return true
		}
		return false
	}

	FaceUnit(t, w, targetGUID)
	for time.Now().Before(deadline) {
		if unitDeadOrGone(w, targetGUID) {
			Preconditionf(t, "unit 0x%X died before combat flag (oneshot? use tankier target e.g. HeroicTrainingDummy)", targetGUID)
		}
		if w.GetObject(targetGUID) == nil {
			// Pad thrash / despawn — fail with a clear message rather than spinning.
			Preconditionf(t, "unit 0x%X left object cache mid-engage (pad thrash / despawn?)", targetGUID)
		}
		_ = w.SetTarget(targetGUID)
		_ = w.AttackSwing(targetGUID)
		time.Sleep(350 * time.Millisecond)
		if pulled() {
			hp, _ := UnitHealth(w, targetGUID)
			t.Logf("engaged 0x%X combat=%v targetSelf=%v hp=%d→%d",
				targetGUID, UnitInCombat(w, targetGUID), UnitTargetGUID(w, targetGUID) == self, startHP, hp)
			return
		}
		if unitDeadOrGone(w, targetGUID) {
			Preconditionf(t, "unit 0x%X died before combat flag (oneshot? use tankier target e.g. HeroicTrainingDummy)", targetGUID)
		}
		// Nudge threat without toggling GM mode. Re-select first — thrash can steal selection.
		_ = w.SetTarget(targetGUID)
		MustGM(t, w, ".damage 1")
		time.Sleep(250 * time.Millisecond)
		if pulled() {
			hp, _ := UnitHealth(w, targetGUID)
			t.Logf("engaged 0x%X combat=%v targetSelf=%v hp=%d→%d (via .damage 1)",
				targetGUID, UnitInCombat(w, targetGUID), UnitTargetGUID(w, targetGUID) == self, startHP, hp)
			return
		}
	}
	hp, max := UnitHealth(w, targetGUID)
	if max == 0 {
		max = startMax
	}
	Preconditionf(t, "unit 0x%X never pulled within %s (combat=%v hp=%d/%d start=%d obj=%v)",
		targetGUID, timeout, UnitInCombat(w, targetGUID), hp, max, startHP, w.GetObject(targetGUID) != nil)
}

// unitDeadOrGone reports zero health for a unit present in this client's cache.
// Missing cache entries are NOT treated as dead — multi-bot spawns often lag
// on non-spawning clients (GetObject nil until UPDATE_OBJECT arrives).
func unitDeadOrGone(w *client.WorldClient, guid uint64) bool {
	obj := w.GetObject(guid)
	if obj == nil {
		return false
	}
	// MaxHealth==0 often means fields not yet populated; do not treat as dead.
	if obj.MaxHealth() > 0 && obj.Health() == 0 {
		return true
	}
	return false
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
