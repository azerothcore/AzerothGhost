package e2eharness

import (
	"testing"
	"time"

	"github.com/azerothcore/AzerothGhost/client"
)

// AssertAuraRemains fails with CONFIRMED BUG if spellID is missing after waiting `after`.
// Use after an action that must NOT consume the aura (e.g. mount while Blending In).
func AssertAuraRemains(t *testing.T, w *client.WorldClient, spellID uint32, after time.Duration, issue int) {
	t.Helper()
	if after > 0 {
		time.Sleep(after)
	}
	if w.SelfHasAura(spellID) {
		t.Logf("aura %d still present (ok)", spellID)
		return
	}
	// Brief re-poll for delayed aura updates.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if w.SelfHasAura(spellID) {
			t.Logf("aura %d reappeared after delay (ok)", spellID)
			return
		}
		time.Sleep(40 * time.Millisecond)
	}
	if issue > 0 {
		ConfirmedBugf(t, issue, "aura %d missing after action (auras=%v)", spellID, w.SelfAuras())
	}
	Preconditionf(t, "aura %d missing after action (auras=%v)", spellID, w.SelfAuras())
}

// AssertAuraConsumed fails with CONFIRMED BUG if spellID is still present after timeout.
func AssertAuraConsumed(t *testing.T, w *client.WorldClient, spellID uint32, timeout time.Duration, issue int) {
	t.Helper()
	if WaitAuraGone(t, w, spellID, timeout) {
		t.Logf("aura %d consumed (gone)", spellID)
		return
	}
	if issue > 0 {
		ConfirmedBugf(t, issue, "aura %d still present after %s (auras=%v)", spellID, timeout, w.SelfAuras())
	}
	Preconditionf(t, "aura %d still present after %s", spellID, timeout)
}

// AssertUnitAuraStable samples a unit aura every 50ms for `window`.
// Fails with CONFIRMED BUG if the aura is missing on two consecutive samples
// (debounce transient flicker gaps).
func AssertUnitAuraStable(t *testing.T, w *client.WorldClient, guid uint64, spellID uint32, window time.Duration, issue int) {
	t.Helper()
	if window <= 0 {
		window = 5 * time.Second
	}
	deadline := time.Now().Add(window)
	missStreak := 0
	for time.Now().Before(deadline) {
		if UnitHasAura(w, guid, spellID) {
			missStreak = 0
		} else {
			missStreak++
			if missStreak >= 2 {
				if issue > 0 {
					ConfirmedBugf(t, issue, "unit 0x%X aura %d dropped during %s window", guid, spellID, window)
				}
				Preconditionf(t, "unit 0x%X aura %d dropped during %s window", guid, spellID, window)
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Logf("unit 0x%X aura %d stable for %s", guid, spellID, window)
}

// WaitAuraGoneFatal is WaitAuraGone that fatals on timeout (aligned with WaitAura).
func WaitAuraGoneFatal(t *testing.T, w *client.WorldClient, spellID uint32, timeout time.Duration) {
	t.Helper()
	if !WaitAuraGone(t, w, spellID, timeout) {
		HarnessFailf(t, "aura %d still present within %s (auras=%v)", spellID, timeout, w.SelfAuras())
	}
}
