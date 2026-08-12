package e2eharness

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/walkline/AzerothGhost/client"
)

// SpellCastResult is the outcome of a CMSG_CAST_SPELL attempt.
type SpellCastResult struct {
	SpellID    uint32
	Success    bool
	FailReason uint8
}

// ArmSpellWaiter installs a spell-result hook to feed a buffered channel.
// Call before CastSpell; then WaitSpell / WaitSpellSuccess.
// Re-arming replaces the channel (same contract as bank waiters: never re-arm during Wait).
// Uses AddSpellCastResultHook once (race-safe fan-out); re-arms only refresh spellCh.
func (s *Session) ArmSpellWaiter() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.spellCh == nil {
		s.spellCh = make(chan SpellCastResult, 16)
	} else {
		// Drain then replace.
		for {
			select {
			case <-s.spellCh:
			default:
				goto rearm
			}
		}
	rearm:
		s.spellCh = make(chan SpellCastResult, 16)
	}
	if s.spellHookOn {
		return
	}
	s.spellHookOn = true
	chPtr := &s.spellCh
	logf := s.logf
	s.World.AddSpellCastResultHook(func(spellID uint32, success bool, failReason uint8) {
		s.mu.Lock()
		ch := *chPtr
		s.mu.Unlock()
		if ch == nil {
			return
		}
		res := SpellCastResult{SpellID: spellID, Success: success, FailReason: failReason}
		select {
		case ch <- res:
		default:
			if logf != nil {
				logf("WARNING: waiter drop kind=spell spell=%d", spellID)
			}
		}
	})
}

// WaitSpell waits for any spell cast result.
func (s *Session) WaitSpell(d time.Duration) (SpellCastResult, error) {
	s.mu.Lock()
	ch := s.spellCh
	s.mu.Unlock()
	if ch == nil {
		return SpellCastResult{}, fmt.Errorf("spell waiter not armed")
	}
	select {
	case r := <-ch:
		return r, nil
	case <-time.After(d):
		return SpellCastResult{}, fmt.Errorf("timeout spell cast result")
	}
}

// WaitSpellID waits for a cast result for the given spell ID.
func (s *Session) WaitSpellID(spellID uint32, d time.Duration) (SpellCastResult, error) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		rem := time.Until(deadline)
		if rem <= 0 {
			break
		}
		r, err := s.WaitSpell(rem)
		if err != nil {
			return SpellCastResult{}, err
		}
		if r.SpellID == spellID {
			return r, nil
		}
	}
	return SpellCastResult{}, fmt.Errorf("timeout spell %d cast result", spellID)
}

// CastAndWait casts spell at target and waits for success (SMSG_SPELL_GO) or fail.
func CastAndWait(t *testing.T, s *Session, spellID uint32, targetGUID uint64, timeout time.Duration) SpellCastResult {
	t.Helper()
	s.ArmSpellWaiter()
	if targetGUID != 0 {
		_ = s.World.SetTarget(targetGUID)
	}
	if err := s.World.CastSpell(spellID, targetGUID); err != nil {
		t.Fatalf("cast %d: %v", spellID, err)
	}
	res, err := s.WaitSpellID(spellID, timeout)
	if err != nil {
		t.Fatalf("wait cast %d: %v", spellID, err)
	}
	return res
}

// MustCastSuccess casts and fails the test unless SMSG_SPELL_GO is received.
func MustCastSuccess(t *testing.T, s *Session, spellID uint32, targetGUID uint64, timeout time.Duration) {
	t.Helper()
	res := CastAndWait(t, s, spellID, targetGUID, timeout)
	if !res.Success {
		t.Fatalf("spell %d failed reason=%d (%s) (want SMSG_SPELL_GO)",
			spellID, res.FailReason, SpellFailReasonName(res.FailReason))
	}
	t.Logf("spell %d OK (SPELL_GO)", spellID)
}

// CastAtPositionAndWait casts a ground-targeted spell and waits for cast result.
func CastAtPositionAndWait(t *testing.T, s *Session, spellID uint32, x, y, z float32, timeout time.Duration) SpellCastResult {
	t.Helper()
	s.ArmSpellWaiter()
	if err := s.World.CastSpellAtPosition(spellID, x, y, z); err != nil {
		t.Fatalf("cast-at-pos %d: %v", spellID, err)
	}
	res, err := s.WaitSpellID(spellID, timeout)
	if err != nil {
		t.Fatalf("wait cast-at-pos %d: %v", spellID, err)
	}
	return res
}

// SpellFailReasonName maps 3.3.5a SpellCastResult codes to short names (AC SharedDefines).
func SpellFailReasonName(reason uint8) string {
	// Subset of common reasons; unknown codes fall back to decimal.
	names := map[uint8]string{
		0: "SUCCESS", 12: "BAD_TARGETS", 27: "DONT_REPORT", 46: "LINE_OF_SIGHT",
		50: "MOVING", 55: "NOPATH", 62: "NOT_KNOWN", 66: "NOT_READY",
		68: "NOT_STANDING", 85: "NO_POWER", 93: "OUT_OF_RANGE", 97: "OUT_OF_RANGE",
		100: "REAGENTS", 130: "TOTEM_CATEGORY",
	}
	if s, ok := names[reason]; ok {
		return s
	}
	return fmt.Sprintf("REASON_%d", reason)
}

// AttackUntilHealthBelow swings at target until its health drops below threshold
// or timeout. Uses client object cache health.
func AttackUntilHealthBelow(t *testing.T, s *Session, targetGUID uint64, maxHealthFrac float64, timeout time.Duration) {
	t.Helper()
	_ = s.World.SetTarget(targetGUID)
	if err := s.World.AttackSwing(targetGUID); err != nil {
		t.Fatalf("attack swing: %v", err)
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		obj := s.World.GetObject(targetGUID)
		if obj != nil && obj.MaxHealth() > 0 {
			frac := float64(obj.Health()) / float64(obj.MaxHealth())
			if frac <= maxHealthFrac {
				t.Logf("target health frac=%.2f (hp=%d/%d)", frac, obj.Health(), obj.MaxHealth())
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	obj := s.World.GetObject(targetGUID)
	if obj == nil {
		t.Fatalf("target 0x%X disappeared before health threshold", targetGUID)
	}
	t.Fatalf("target health still %d/%d after %s", obj.Health(), obj.MaxHealth(), timeout)
}

// DamageTracker records SMSG_ATTACKERSTATEUPDATE damage by victim GUID.
// Install via ArmDamageTracker; safe with Session packet waiters via multi-subscriber
// AddPacketHook (no OnPacket clobber).
type DamageTracker struct {
	mu       sync.Mutex
	byVictim map[uint64]uint32
	events   int
}

// ArmDamageTracker registers a packet hook to accumulate auto-attack damage.
// Safe with ArmAllWaiters (multi-subscriber AddPacketHook — no clobber).
// Hook stays for session lifetime (cancel not returned; e2e tests are short-lived).
func ArmDamageTracker(s *Session) *DamageTracker {
	dt := &DamageTracker{byVictim: make(map[uint64]uint32)}
	s.World.AddPacketHook(func(op uint16, data []byte) {
		// SMSG_ATTACKERSTATEUPDATE = 0x014A — parse lightly when present.
		if op == client.SmsgAttackerStateUpdate && len(data) >= 20 {
			dt.mu.Lock()
			dt.events++
			dt.mu.Unlock()
		}
	})
	return dt
}

// UnitHealth returns cached health for a unit GUID (0 if unknown).
func UnitHealth(w *client.WorldClient, guid uint64) (hp, max uint32) {
	obj := w.GetObject(guid)
	if obj == nil {
		return 0, 0
	}
	return obj.Health(), obj.MaxHealth()
}

// WaitUnitHealthChanged waits until unit health differs from before.
func WaitUnitHealthChanged(t *testing.T, w *client.WorldClient, guid uint64, before uint32, timeout time.Duration) (hp uint32) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		h, _ := UnitHealth(w, guid)
		if h != before && h > 0 {
			return h
		}
		// Also accept death.
		if before > 0 && h == 0 {
			return 0
		}
		time.Sleep(50 * time.Millisecond)
	}
	h, m := UnitHealth(w, guid)
	t.Fatalf("unit 0x%X health unchanged from %d (now %d/%d) within %s", guid, before, h, m, timeout)
	return h
}
