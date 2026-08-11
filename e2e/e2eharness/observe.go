package e2eharness

import (
	"fmt"
	"testing"
	"time"

	"github.com/walkline/AzerothGhost/client"
)

// UnitSnap is a point-in-time view of a tracked unit for observation helpers.
type UnitSnap struct {
	GUID      uint64
	Entry     uint32
	Health    uint32
	MaxHealth uint32
	InCombat  bool
	Target    uint64
}

// snapFromObject builds a UnitSnap from a WorldObject clone.
func snapFromObject(o *client.WorldObject) UnitSnap {
	if o == nil {
		return UnitSnap{}
	}
	entry := o.Entry
	if entry == 0 {
		entry = objectEntryFromGUID(o.GUID)
	}
	return UnitSnap{
		GUID:      o.GUID,
		Entry:     entry,
		Health:    o.Health(),
		MaxHealth: o.MaxHealth(),
		InCombat:  o.Value(client.UnitFieldFlags)&client.UnitFlagInCombat != 0,
		Target:    UnitTargetGUIDFromObj(o),
	}
}

// UnitTargetGUIDFromObj reads UNIT_FIELD_TARGET from an object.
func UnitTargetGUIDFromObj(o *client.WorldObject) uint64 {
	if o == nil {
		return 0
	}
	low := o.Value(client.UnitFieldTarget)
	high := o.Value(client.UnitFieldTarget + 1)
	return uint64(low) | (uint64(high) << 32)
}

// UnitsByEntry returns living units (hp>0) matching any of the given entries
// within maxDist (0 = no distance filter, same as GetNearbyUnits(0)).
func UnitsByEntry(w *client.WorldClient, maxDist float32, entries ...uint32) []UnitSnap {
	want := make(map[uint32]struct{}, len(entries))
	for _, e := range entries {
		want[e] = struct{}{}
	}
	var out []UnitSnap
	// GetNearbyUnits(0) still filters HasKnownPosition; use a large dist when 0.
	dist := maxDist
	if dist <= 0 {
		dist = 200
	}
	for _, u := range w.GetNearbyUnits(dist) {
		if u.Health() == 0 {
			continue
		}
		entry := u.Entry
		if entry == 0 {
			entry = objectEntryFromGUID(u.GUID)
		}
		if _, ok := want[entry]; !ok {
			continue
		}
		out = append(out, snapFromObject(u))
	}
	return out
}

// LivingByEntries returns GUIDs of living units matching entries.
func LivingByEntries(w *client.WorldClient, maxDist float32, entries ...uint32) []uint64 {
	snaps := UnitsByEntry(w, maxDist, entries...)
	out := make([]uint64, len(snaps))
	for i, s := range snaps {
		out[i] = s.GUID
	}
	return out
}

// CountLivingByEntry counts living units per entry.
func CountLivingByEntry(w *client.WorldClient, maxDist float32, entries ...uint32) map[uint32]int {
	out := make(map[uint32]int, len(entries))
	for _, e := range entries {
		out[e] = 0
	}
	for _, s := range UnitsByEntry(w, maxDist, entries...) {
		out[s.Entry]++
	}
	return out
}

// CountLivingWithRetry re-polls living count for entries until non-zero or timeout.
// Use after mass DESTROY when the object cache may lag.
func CountLivingWithRetry(w *client.WorldClient, maxDist float32, entries []uint32, timeout time.Duration) (n int, guids []uint64) {
	deadline := time.Now().Add(timeout)
	for {
		guids = LivingByEntries(w, maxDist, entries...)
		n = len(guids)
		if n > 0 || !time.Now().Before(deadline) {
			return n, guids
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// WaitNewUnits waits until at least one living unit with an allowed entry appears
// whose GUID is not in known. New GUIDs are added to known before return.
// Returns the new units (may be more than one if a pack spawns together).
func WaitNewUnits(t *testing.T, w *client.WorldClient, known map[uint64]struct{}, entries []uint32, maxDist float32, timeout time.Duration) []UnitSnap {
	t.Helper()
	if known == nil {
		HarnessFailf(t, "WaitNewUnits: known map is nil")
	}
	if maxDist <= 0 {
		maxDist = 120
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var fresh []UnitSnap
		for _, s := range UnitsByEntry(w, maxDist, entries...) {
			if _, ok := known[s.GUID]; ok {
				continue
			}
			fresh = append(fresh, s)
		}
		if len(fresh) > 0 {
			for _, s := range fresh {
				known[s.GUID] = struct{}{}
			}
			t.Logf("WaitNewUnits: +%d new (entries=%v)", len(fresh), entries)
			return fresh
		}
		time.Sleep(100 * time.Millisecond)
	}
	HarnessFailf(t, "no new units for entries %v within %s", entries, timeout)
	return nil
}

// SpawnSet is one batch of ally/add GUIDs detected by SpawnSetTracker.
type SpawnSet struct {
	// Kind is the kind key from KindOf (or the entry as string when KindOf is nil).
	Kind   string
	Entry  uint32 // first/dominant entry seen in the batch
	Guids  []uint64
	SpawnT time.Time
}

// SpawnSetTracker groups newly seen living units of given entries into sets.
// Units of the same kind (via KindOf, default: entry id) arriving within
// SameSetGrace merge into the open set; a different kind or grace expiry opens
// a new set. Use KindOf to merge multi-entry packs (e.g. Freya Trio).
type SpawnSetTracker struct {
	Entries      []uint32
	MaxDist      float32
	SameSetGrace time.Duration
	// KindOf maps template entry → kind key. Nil means each entry is its own kind.
	KindOf   func(entry uint32) string
	seen     map[uint64]struct{}
	sets     []SpawnSet
	lastKind string
	lastT    time.Time
}

// NewSpawnSetTracker builds a tracker. sameSetGrace defaults to 3s when <=0.
func NewSpawnSetTracker(entries []uint32, sameSetGrace time.Duration) *SpawnSetTracker {
	if sameSetGrace <= 0 {
		sameSetGrace = 3 * time.Second
	}
	return &SpawnSetTracker{
		Entries:      append([]uint32(nil), entries...),
		MaxDist:      120,
		SameSetGrace: sameSetGrace,
		seen:         make(map[uint64]struct{}),
	}
}

// Sets returns a copy of recorded spawn sets.
func (tr *SpawnSetTracker) Sets() []SpawnSet {
	out := make([]SpawnSet, len(tr.sets))
	copy(out, tr.sets)
	return out
}

// Known returns the set of all GUIDs absorbed so far.
func (tr *SpawnSetTracker) Known() map[uint64]struct{} {
	out := make(map[uint64]struct{}, len(tr.seen))
	for g := range tr.seen {
		out[g] = struct{}{}
	}
	return out
}

func (tr *SpawnSetTracker) kindOf(entry uint32) string {
	if tr.KindOf != nil {
		return tr.KindOf(entry)
	}
	return fmt.Sprintf("entry:%d", entry)
}

// Poll absorbs currently living matching units into sets. Call frequently.
func (tr *SpawnSetTracker) Poll(w *client.WorldClient, now time.Time) {
	if tr.MaxDist <= 0 {
		tr.MaxDist = 120
	}
	// Group fresh by kind.
	type batch struct {
		entry uint32
		gs    []uint64
	}
	byKind := map[string]*batch{}
	for _, s := range UnitsByEntry(w, tr.MaxDist, tr.Entries...) {
		if _, ok := tr.seen[s.GUID]; ok {
			continue
		}
		tr.seen[s.GUID] = struct{}{}
		k := tr.kindOf(s.Entry)
		b := byKind[k]
		if b == nil {
			b = &batch{entry: s.Entry}
			byKind[k] = b
		}
		b.gs = append(b.gs, s.GUID)
	}
	for k, b := range byKind {
		if len(b.gs) == 0 {
			continue
		}
		if len(tr.sets) > 0 && tr.lastKind == k && now.Sub(tr.lastT) < tr.SameSetGrace {
			last := &tr.sets[len(tr.sets)-1]
			last.Guids = append(last.Guids, b.gs...)
			continue
		}
		tr.sets = append(tr.sets, SpawnSet{
			Kind:   k,
			Entry:  b.entry,
			Guids:  append([]uint64(nil), b.gs...),
			SpawnT: now,
		})
		tr.lastKind = k
		tr.lastT = now
	}
}

// WaitSets polls until n sets are recorded or timeout (harness fail).
func (tr *SpawnSetTracker) WaitSets(t *testing.T, w *client.WorldClient, n int, timeout time.Duration) []SpawnSet {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		tr.Poll(w, time.Now())
		if len(tr.sets) >= n {
			for i, s := range tr.sets {
				t.Logf("SpawnSet %d kind=%s entry=%d units=%d at %s",
					i+1, s.Kind, s.Entry, len(s.Guids), s.SpawnT.Format("15:04:05.000"))
			}
			return tr.Sets()
		}
		time.Sleep(150 * time.Millisecond)
	}
	HarnessFailf(t, "SpawnSetTracker: need %d sets within %s (got %d)", n, timeout, len(tr.sets))
	return nil
}

// WaitNextNew waits for units not in knownAtStart (caller snapshot), returns them.
func (tr *SpawnSetTracker) WaitNextNew(t *testing.T, w *client.WorldClient, knownAtStart map[uint64]struct{}, timeout time.Duration) []UnitSnap {
	t.Helper()
	// Merge tracker seen into known for WaitNewUnits semantics.
	known := make(map[uint64]struct{}, len(knownAtStart)+len(tr.seen))
	for g := range knownAtStart {
		known[g] = struct{}{}
	}
	for g := range tr.seen {
		known[g] = struct{}{}
	}
	fresh := WaitNewUnits(t, w, known, tr.Entries, tr.MaxDist, timeout)
	// Keep tracker seen in sync.
	for _, s := range fresh {
		tr.seen[s.GUID] = struct{}{}
	}
	return fresh
}

// TargetHoldSample is one attacker's observed targeting behaviour.
type TargetHoldSample struct {
	Attacker  uint64
	Entry     uint32
	Target    uint64
	HoldTicks int
	HurtTicks int
	EverHurt  bool
}

// ObserveUnitTargets samples attackers matching attackerEntries. For each living
// attacker whose UNIT_FIELD_TARGET points at a unit where targetOK(entry) is true,
// it tracks consecutive hold ticks and whether that target's HP dropped.
func ObserveUnitTargets(
	t *testing.T,
	w *client.WorldClient,
	attackerEntries []uint32,
	targetOK func(entry uint32) bool,
	every, for_ time.Duration,
) []TargetHoldSample {
	t.Helper()
	if every <= 0 {
		every = 200 * time.Millisecond
	}
	if for_ <= 0 {
		for_ = 10 * time.Second
	}
	type state struct {
		sample    TargetHoldSample
		lastTgt   uint64
		lastTgtHP uint32
	}
	byAtk := map[uint64]*state{}
	deadline := time.Now().Add(for_)
	for time.Now().Before(deadline) {
		for _, atk := range UnitsByEntry(w, 120, attackerEntries...) {
			st, ok := byAtk[atk.GUID]
			if !ok {
				st = &state{sample: TargetHoldSample{Attacker: atk.GUID, Entry: atk.Entry}}
				byAtk[atk.GUID] = st
			}
			tgt := atk.Target
			if tgt == 0 {
				st.lastTgt = 0
				continue
			}
			tobj := w.GetObject(tgt)
			if tobj == nil {
				continue
			}
			tEntry := tobj.Entry
			if tEntry == 0 {
				tEntry = objectEntryFromGUID(tgt)
			}
			if targetOK != nil && !targetOK(tEntry) {
				st.lastTgt = 0
				continue
			}
			hp := tobj.Health()
			if tgt == st.lastTgt {
				st.sample.HoldTicks++
				st.sample.Target = tgt
				if st.lastTgtHP > 0 && hp < st.lastTgtHP {
					st.sample.HurtTicks++
					st.sample.EverHurt = true
				}
			} else {
				st.lastTgt = tgt
				st.sample.Target = tgt
				st.sample.HoldTicks = 1
			}
			st.lastTgtHP = hp
		}
		time.Sleep(every)
	}
	out := make([]TargetHoldSample, 0, len(byAtk))
	for _, st := range byAtk {
		out = append(out, st.sample)
	}
	return out
}

// AssertNoIdleTargeters fails with CONFIRMED BUG when any attacker held a valid
// target for at least idleHold without ever dealing HP damage to it.
func AssertNoIdleTargeters(t *testing.T, issue int, samples []TargetHoldSample, idleHold time.Duration, sampleEvery time.Duration) {
	t.Helper()
	if sampleEvery <= 0 {
		sampleEvery = 200 * time.Millisecond
	}
	needTicks := int(idleHold / sampleEvery)
	if needTicks < 1 {
		needTicks = 1
	}
	for _, s := range samples {
		if s.HoldTicks >= needTicks && !s.EverHurt {
			ConfirmedBugf(t, issue,
				"attacker 0x%X entry=%d held target 0x%X for %d ticks without dealing damage",
				s.Attacker, s.Entry, s.Target, s.HoldTicks)
		}
	}
	t.Logf("no idle targeters among %d samples (needHoldTicks=%d)", len(samples), needTicks)
}

// SampleUntil calls fn every `every` until it returns true or timeout (harness fail).
func SampleUntil(t *testing.T, every, timeout time.Duration, fn func() bool) {
	t.Helper()
	if every <= 0 {
		every = 50 * time.Millisecond
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(every)
	}
	HarnessFailf(t, "SampleUntil: condition not met within %s", timeout)
}
