package e2eharness

import (
	"fmt"
	"testing"
	"time"

	"github.com/azerothcore/AzerothGhost/client"
)

// Ergonomic methods on ScenarioBot so authors rarely choose bot.World vs bot.Session.

// DefaultCastTimeout is the default cast wait used by CastOrGM / CastRetries.
const DefaultCastTimeout = 10 * time.Second

// Teleport moves the bot with `.go xyz` and waits for teleport completion.
func (b *ScenarioBot) Teleport(t *testing.T, x, y, z float32, mapID uint32) {
	t.Helper()
	TeleportXYZ(t, b.World, x, y, z, mapID)
}

// TeleNamed runs `.tele <name>` and waits for transfer.
func (b *ScenarioBot) TeleNamed(t *testing.T, name string) {
	t.Helper()
	TeleNamed(t, b.World, name)
}

// GoCreatureID teleports to `.go creature id <entry>`.
func (b *ScenarioBot) GoCreatureID(t *testing.T, entry uint32) {
	t.Helper()
	GoCreatureID(t, b.World, entry)
}

// GoCreatureGUID teleports to `.go creature <spawnGUID>`.
func (b *ScenarioBot) GoCreatureGUID(t *testing.T, spawnGUID uint32) {
	t.Helper()
	GoCreatureGUID(t, b.World, spawnGUID)
}

// TeleportPad teleports to a Position3 pad.
func (b *ScenarioBot) TeleportPad(t *testing.T, pad Position3) {
	t.Helper()
	TeleportPad(t, b.World, pad)
}

// CombatReady turns GM mode off and enables god (and optional power).
// Prefer this over manual .gm off + CheatGod before pulls.
func (b *ScenarioBot) CombatReady(t *testing.T) {
	t.Helper()
	CombatReady(t, b.World, CombatReadyOpts{God: true})
}

// CombatReadyFull is CombatReady with god + power.
func (b *ScenarioBot) CombatReadyFull(t *testing.T) {
	t.Helper()
	CombatReady(t, b.World, CombatReadyOpts{God: true, Power: true})
}

// Engage faces/attacks until the target is in combat.
func (b *ScenarioBot) Engage(t *testing.T, targetGUID uint64, timeout time.Duration) {
	t.Helper()
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	EngageUntilCombat(t, b.World, targetGUID, timeout)
}

// Damage applies `.damage` without enabling GM mode.
func (b *ScenarioBot) Damage(t *testing.T, targetGUID uint64, amount uint32) {
	t.Helper()
	DamageGM(t, b.World, targetGUID, amount)
}

// DamageKill damages guids until dead without enabling GM mode.
func (b *ScenarioBot) DamageKill(t *testing.T, guids []uint64, amount uint32, timeout time.Duration) {
	t.Helper()
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	DamageKillGM(t, b.World, guids, amount, timeout)
}

// DieAndRepop dies, waits for death, and releases spirit.
// After repop the client is teleported to a graveyard; waits on TeleportSeq / PhaseInWorld.
func (b *ScenarioBot) DieAndRepop(t *testing.T) {
	t.Helper()
	// DieMust (not bare Die) under pad thrash.
	b.DieMust(t, 20*time.Second)
	before := b.World.TeleportSeq()
	b.ReleaseSpirit(t)
	// Ghost yard transfer is packet-driven (near/far tele). Soft-continue if no tele opcode.
	if err := b.World.WaitForTeleportAfter(before, 10*time.Second); err != nil {
		t.Logf("repop teleport wait: %v (continuing)", err)
	}
	if b.World.IsInWorld() {
		return
	}
	// Ghost remains gameplay-capable once InWorld again.
	if err := b.World.WaitForSessionPhase(client.PhaseInWorld, 5*time.Second); err != nil {
		t.Logf("repop WaitInWorld: %v (continuing)", err)
	}
}

// UnitInCombat reports combat flag on a unit.
func (b *ScenarioBot) UnitInCombat(guid uint64) bool {
	return UnitInCombat(b.World, guid)
}

// UnitTarget returns UNIT_FIELD_TARGET for a unit.
func (b *ScenarioBot) UnitTarget(guid uint64) uint64 {
	return UnitTargetGUID(b.World, guid)
}

// WaitUnitCombat waits until the unit is in combat.
func (b *ScenarioBot) WaitUnitCombat(t *testing.T, guid uint64, timeout time.Duration) {
	t.Helper()
	WaitUnitCombat(t, b.World, guid, timeout)
}

// WaitUnitDead waits until the unit is dead or gone.
func (b *ScenarioBot) WaitUnitDead(t *testing.T, guid uint64, timeout time.Duration) {
	t.Helper()
	WaitUnitDead(t, b.World, guid, timeout)
}

// WaitUnitAny waits for any of the given entries; returns the live GUID.
func (b *ScenarioBot) WaitUnitAny(t *testing.T, timeout time.Duration, entries ...uint32) uint64 {
	t.Helper()
	g, _ := WaitNearbyUnitAnyEntry(t, b.World, entries, timeout)
	return g
}

// WaitAuraGone waits until the player loses spellID; fatals on timeout.
func (b *ScenarioBot) WaitAuraGone(t *testing.T, spellID uint32, timeout time.Duration) {
	t.Helper()
	WaitAuraGoneFatal(t, b.World, spellID, timeout)
}

// TryWaitAuraGone is soft WaitAuraGone (returns false on timeout).
func (b *ScenarioBot) TryWaitAuraGone(t *testing.T, spellID uint32, timeout time.Duration) bool {
	t.Helper()
	return WaitAuraGone(t, b.World, spellID, timeout)
}

// AssertHasAura fatals if the player lacks spellID.
func (b *ScenarioBot) AssertHasAura(t *testing.T, spellID uint32) {
	t.Helper()
	if !b.HasAura(spellID) {
		Preconditionf(t, "missing aura %d (auras=%v)", spellID, b.World.SelfAuras())
	}
}

// AssertNoAura fatals if the player still has spellID.
func (b *ScenarioBot) AssertNoAura(t *testing.T, spellID uint32) {
	t.Helper()
	if b.HasAura(spellID) {
		Preconditionf(t, "unexpected aura %d still present (auras=%v)", spellID, b.World.SelfAuras())
	}
}

// AssertAuraRemains is AssertAuraRemains on the player.
func (b *ScenarioBot) AssertAuraRemains(t *testing.T, spellID uint32, after time.Duration, issue int) {
	t.Helper()
	AssertAuraRemains(t, b.World, spellID, after, issue)
}

// AssertAuraConsumed is AssertAuraConsumed on the player.
func (b *ScenarioBot) AssertAuraConsumed(t *testing.T, spellID uint32, timeout time.Duration, issue int) {
	t.Helper()
	AssertAuraConsumed(t, b.World, spellID, timeout, issue)
}

// NearbyUnits returns tracked units within maxDist.
func (b *ScenarioBot) NearbyUnits(maxDist float32) []*client.WorldObject {
	return b.World.GetNearbyUnits(maxDist)
}

// UnitsByEntry returns living unit snaps for entries.
func (b *ScenarioBot) UnitsByEntry(maxDist float32, entries ...uint32) []UnitSnap {
	return UnitsByEntry(b.World, maxDist, entries...)
}

// WaitNewUnits waits for new living units of the given entries.
func (b *ScenarioBot) WaitNewUnits(t *testing.T, known map[uint64]struct{}, entries []uint32, timeout time.Duration) []UnitSnap {
	t.Helper()
	return WaitNewUnits(t, b.World, known, entries, 120, timeout)
}

// CastOrGM casts via the client path; on failure or timeout falls back to `.cast self` / targeted GM cast.
func (b *ScenarioBot) CastOrGM(t *testing.T, spellID uint32, targetGUID uint64, timeout time.Duration) SpellCastResult {
	t.Helper()
	if timeout <= 0 {
		timeout = DefaultCastTimeout
	}
	// Reduce cast interrupts from leftover pad aggro.
	MustGM(t, b.World, ".combatstop")
	res, err := b.TryCast(t, spellID, targetGUID, timeout)
	if err == nil && res.Success {
		return res
	}
	t.Logf("CastOrGM: client cast spell=%d success=%v err=%v — GM fallback", spellID, res.Success, err)
	if targetGUID != 0 {
		_ = b.World.SetTarget(targetGUID)
		MustGM(t, b.World, fmt.Sprintf(".cast %d", spellID))
	} else {
		b.CastSelfGM(t, spellID)
		// Summon spells: also try player-as-caster `.cast N` with self selected
		// (some cores reject `.cast self` for SUMMON effects).
		if g := b.World.CharGUID(); g != 0 {
			_ = b.World.SetTarget(g)
		}
		MustGM(t, b.World, fmt.Sprintf(".cast %d", spellID))
	}
	time.Sleep(500 * time.Millisecond)
	return SpellCastResult{SpellID: spellID, Success: true}
}

// CastRetries attempts Cast up to n times; returns last result and whether any succeeded.
func (b *ScenarioBot) CastRetries(t *testing.T, spellID uint32, targetGUID uint64, n int, each time.Duration) (SpellCastResult, bool) {
	t.Helper()
	if n <= 0 {
		n = 3
	}
	if each <= 0 {
		each = DefaultCastTimeout
	}
	var last SpellCastResult
	for i := 0; i < n; i++ {
		if targetGUID != 0 {
			b.Face(t, targetGUID)
		}
		last = b.Cast(t, spellID, targetGUID, each)
		t.Logf("CastRetries %d/%d spell=%d success=%v reason=%d (%s)",
			i+1, n, spellID, last.Success, last.FailReason, SpellFailReasonName(last.FailReason))
		if last.Success {
			return last, true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return last, false
}

// EquipEntry adds items and auto-equips from backpack slots (best-effort).
func (b *ScenarioBot) EquipEntry(t *testing.T, entry, count uint32) {
	t.Helper()
	if count == 0 {
		count = 1
	}
	// Prefer item-push path when waiters are armed; otherwise fire-and-forget add.
	b.Session.DrainItemPushes()
	MustGM(t, b.World, fmt.Sprintf(".additem %d %d", entry, count))
	// Auto-equip from main backpack slots.
	for slot := uint8(23); slot < 39; slot++ {
		_ = b.World.AutoEquipItem(255, slot)
	}
	time.Sleep(200 * time.Millisecond)
}

// AssertWorldAlive pings the world via this bot (for crash tests without a probe).
func (b *ScenarioBot) AssertWorldAlive(t *testing.T) {
	t.Helper()
	ProbeWorldAlive(t, b, 0)
}

// QuestStatusAfterSave saves then returns quest status.
func (b *ScenarioBot) QuestStatusAfterSave(t *testing.T, questID uint32) (status uint8, ok bool) {
	t.Helper()
	b.Save(t)
	return b.QuestStatus(t, questID)
}

// Die kills the bot via GM `.die`.
func (b *ScenarioBot) Die(t *testing.T) {
	t.Helper()
	Die(t, b.World)
}

// DieMust ensures player Health()==0 then WaitDead.
// Under concurrent pad load a single `.die` often no-ops (selection not applied yet,
// god cheat, or GM thrash). Prefer DieMust over bare Die.
//
// Strategy (re-select self before every attempt):
//  1. god off + combatstop (account GM perms still allow .die/.damage)
//  2. retry `.die` / `.damage 100 pct` / absolute damage
//
// Never use `.modify hp 1` — that sets max HP to 1 and can leave the bot stuck at 1/1 alive.
func (b *ScenarioBot) DieMust(t *testing.T, timeout time.Duration) {
	t.Helper()
	if timeout <= 0 {
		timeout = 25 * time.Second
	}
	if b.World.Health() == 0 {
		return
	}
	// God makes DealDamage a no-op. Prefer gm off so self is selectable and killable.
	MustGM(t, b.World, ".cheat god off")
	MustGM(t, b.World, ".combatstop")
	MustGM(t, b.World, ".gm off")

	deadline := time.Now().Add(timeout)
	reselect := func() {
		_ = b.World.SetTarget(b.GUID)
		// Selection must be applied server-side before chat commands that require a target.
		time.Sleep(200 * time.Millisecond)
	}
	waitHP0 := func(window time.Duration) bool {
		until := time.Now().Add(window)
		for time.Now().Before(until) && time.Now().Before(deadline) {
			if b.World.Health() == 0 {
				return true
			}
			time.Sleep(40 * time.Millisecond)
		}
		return b.World.Health() == 0
	}

	// Longer settle between attempts under pad thrash.
	cmds := []string{
		".die", ".die", ".damage 100 pct", ".damage 99999999",
		".die", ".damage 100 pct", ".die",
	}
	for i, cmd := range cmds {
		if !time.Now().Before(deadline) {
			break
		}
		reselect()
		// Account still has GM command access without GM *mode*.
		MustGM(t, b.World, cmd)
		if waitHP0(2*time.Second + time.Duration(i)*100*time.Millisecond) {
			b.WaitDead(t, time.Until(deadline)+time.Second)
			return
		}
	}
	// Last attempt with gm on (some cores accept .die better that way).
	MustGM(t, b.World, ".gm on")
	reselect()
	MustGM(t, b.World, ".die")
	if waitHP0(3 * time.Second) {
		b.WaitDead(t, time.Until(deadline)+time.Second)
		return
	}
	reselect()
	MustGM(t, b.World, ".damage 100 pct")
	b.WaitDead(t, time.Until(deadline))
}

// WaitNear waits until the bot is within maxDist of (x,y,z).
func (b *ScenarioBot) WaitNear(t *testing.T, x, y, z, maxDist float32, timeout time.Duration) {
	t.Helper()
	if maxDist <= 0 {
		maxDist = DefaultNearPadDist
	}
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	deadline := time.Now().Add(timeout)
	var last float32
	for time.Now().Before(deadline) {
		px, py, pz, _ := b.Pos()
		last = Distance3D(px, py, pz, x, y, z)
		if last <= maxDist {
			return
		}
		time.Sleep(40 * time.Millisecond)
	}
	HarnessFailf(t, "%s WaitNear dist=%.2f max=%.2f timeout", b.Name, last, maxDist)
}

// WaitUnitHPKnown waits until unit has max HP > 0 in object cache.
func (b *ScenarioBot) WaitUnitHPKnown(t *testing.T, guid uint64, timeout time.Duration) (hp, max uint32) {
	t.Helper()
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		hp, max = b.UnitHP(guid)
		if max > 0 {
			return hp, max
		}
		time.Sleep(40 * time.Millisecond)
	}
	Preconditionf(t, "unit 0x%X max HP unknown after %s", guid, timeout)
	return 0, 0
}

// DamageToFraction damages guid until hp/max <= frac (e.g. 0.49 for below half).
// When frac > 0, leaves at least 1 HP (never finishes the unit as "below half").
// Caps each hit so a large .damage cannot overshoot to 0 from full.
func (b *ScenarioBot) DamageToFraction(t *testing.T, guid uint64, frac float64, timeout time.Duration) {
	t.Helper()
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		hp, max := b.WaitUnitHPKnown(t, guid, 5*time.Second)
		if max == 0 {
			continue
		}
		if float64(hp)/float64(max) <= frac {
			if frac > 0 && hp == 0 {
				Preconditionf(t, "DamageToFraction: unit 0x%X died while aiming for frac<=%.2f (hp=0/%d)", guid, frac, max)
			}
			return
		}
		// Target remaining HP floor (at least 1 when frac > 0).
		wantRemain := uint32(float64(max) * frac)
		if frac > 0 && wantRemain < 1 {
			wantRemain = 1
		}
		if hp <= wantRemain {
			return
		}
		// Hit size: up to 1/5 max, but never more than (hp - wantRemain).
		chunk := max / 5
		if chunk < 1 {
			chunk = 1
		}
		maxHit := hp - wantRemain
		if chunk > maxHit {
			chunk = maxHit
		}
		if chunk < 1 {
			return
		}
		b.Damage(t, guid, chunk)
		time.Sleep(50 * time.Millisecond)
	}
	hp, max := b.UnitHP(guid)
	Preconditionf(t, "DamageToFraction: still hp=%d/%d want frac<=%.2f", hp, max, frac)
}

// HardDisconnectAndProbe closes victim without logout and probes world via probe bot.
func HardDisconnectAndProbe(t *testing.T, victim, probe *ScenarioBot, issue int) {
	t.Helper()
	if victim != nil {
		victim.HardDisconnect(t)
	}
	if probe == nil {
		HarnessFailf(t, "HardDisconnectAndProbe: nil probe")
	}
	ProbeWorldAlive(t, probe, issue)
}

// WaitLootMethod waits until GroupState.LootMethod matches method while InGroup.
func (b *ScenarioBot) WaitLootMethod(t *testing.T, method uint8, timeout time.Duration) client.GroupState {
	t.Helper()
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	deadline := time.Now().Add(timeout)
	var st client.GroupState
	for time.Now().Before(deadline) {
		st = b.GroupState()
		if st.InGroup && st.LootMethod == method {
			return st
		}
		time.Sleep(40 * time.Millisecond)
	}
	Preconditionf(t, "WaitLootMethod want=%d got inGroup=%v method=%d", method, st.InGroup, st.LootMethod)
	return st
}

// TryWaitChanneling returns true if channeling (optional spellID match) within timeout.
func (b *ScenarioBot) TryWaitChanneling(t *testing.T, spellID uint32, timeout time.Duration) bool {
	t.Helper()
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if b.IsChanneling() {
			if spellID == 0 || b.ChannelSpell() == spellID {
				return true
			}
		}
		time.Sleep(40 * time.Millisecond)
	}
	return false
}

// CancelCastWhenChanneling waits for channel then CancelCast. Returns false if never channeling.
func (b *ScenarioBot) CancelCastWhenChanneling(t *testing.T, spellID uint32, timeout time.Duration) bool {
	t.Helper()
	if !b.TryWaitChanneling(t, spellID, timeout) {
		return false
	}
	b.CancelCast(t)
	return true
}

// WaitDead blocks until health is 0.
func (b *ScenarioBot) WaitDead(t *testing.T, timeout time.Duration) {
	t.Helper()
	WaitDead(t, b.World, timeout)
}

// ReleaseSpirit sends CMSG_REPOP_REQUEST.
func (b *ScenarioBot) ReleaseSpirit(t *testing.T) {
	t.Helper()
	ReleaseSpirit(t, b.World)
}

// ApplyAura applies spellID via GM and waits until SelfHasAura.
func (b *ScenarioBot) ApplyAura(t *testing.T, spellID uint32) {
	t.Helper()
	ApplyAura(t, b.World, spellID)
}

// HasAura reports whether the bot currently has spellID.
func (b *ScenarioBot) HasAura(spellID uint32) bool {
	return AuraPresent(b.World, spellID)
}

// Cast casts spellID at targetGUID and waits for cast result.
func (b *ScenarioBot) Cast(t *testing.T, spellID uint32, targetGUID uint64, timeout time.Duration) SpellCastResult {
	t.Helper()
	return CastAndWait(t, b.Session, spellID, targetGUID, timeout)
}

// TryCast is like Cast but returns an error on timeout instead of failing the test.
// Use for hang-detection scenarios (e.g. Charge on oversized targets).
func (b *ScenarioBot) TryCast(t *testing.T, spellID uint32, targetGUID uint64, timeout time.Duration) (SpellCastResult, error) {
	t.Helper()
	b.ArmSpellWaiter()
	if targetGUID != 0 {
		_ = b.World.SetTarget(targetGUID)
	}
	if err := b.World.CastSpell(spellID, targetGUID); err != nil {
		return SpellCastResult{}, err
	}
	return b.WaitSpellID(spellID, timeout)
}

// CastMust fails unless the cast succeeds (SMSG_SPELL_GO).
func (b *ScenarioBot) CastMust(t *testing.T, spellID uint32, targetGUID uint64, timeout time.Duration) {
	t.Helper()
	MustCastSuccess(t, b.Session, spellID, targetGUID, timeout)
}

// CastAtPosition casts a ground-targeted spell and waits for cast result.
func (b *ScenarioBot) CastAtPosition(t *testing.T, spellID uint32, x, y, z float32, timeout time.Duration) SpellCastResult {
	t.Helper()
	return CastAtPositionAndWait(t, b.Session, spellID, x, y, z, timeout)
}

// Learn learns a spell and waits until KnowsSpell reports it.
func (b *ScenarioBot) Learn(t *testing.T, spellID uint32) {
	t.Helper()
	LearnSpell(t, b.World, spellID)
}

// LearnAll runs `.learn all my class`.
func (b *ScenarioBot) LearnAll(t *testing.T) {
	t.Helper()
	LearnAllMyClass(t, b.World)
}

// AddQuest adds a quest via GM and waits until character_queststatus is INCOMPLETE.
// Bare `.quest add` is async; under pad thrash a following Save can miss the row.
func (b *ScenarioBot) AddQuest(t *testing.T, questID uint32) {
	t.Helper()
	MustGM(t, b.World, ".gm on")
	AddQuest(t, b.World, questID)
	// Flush then poll — worldserver applies chat commands on the next tick.
	SaveCharacter(t, b.World)
	WaitQuestStatus(t, b.CharDB, b.GUID, questID, QuestStatusIncomplete, 10*time.Second)
}

// AssertQuestStatus saves and asserts character_queststatus.
func (b *ScenarioBot) AssertQuestStatus(t *testing.T, questID uint32, want uint8) {
	t.Helper()
	AssertQuestStatusEqual(t, b.CharDB, b.Session, questID, want)
}

// QuestStatus returns current DB quest status (call Save first for online chars).
func (b *ScenarioBot) QuestStatus(t *testing.T, questID uint32) (status uint8, ok bool) {
	t.Helper()
	return MustQuestStatus(t, b.CharDB, b.GUID, questID)
}

// Save forces a character DB flush.
func (b *ScenarioBot) Save(t *testing.T) {
	t.Helper()
	SaveCharacter(t, b.World)
}

// CheatGod enables god mode.
func (b *ScenarioBot) CheatGod(t *testing.T) {
	t.Helper()
	CheatGod(t, b.World)
}

// CheatPower enables infinite power.
func (b *ScenarioBot) CheatPower(t *testing.T) {
	t.Helper()
	CheatPower(t, b.World)
}

// Spawn places a combat/pull fixture (training dummy, etc.) via persistent `.npc add`
// and registers SQL+live cleanup. Returns the live client GUID.
//
// Not `.npc add temp`: temps are invisible to the creature table, live ~120s, and
// select-delete cleanup races Session.Close — that is why Heroic Training Dummies piled up.
func (b *ScenarioBot) Spawn(t *testing.T, entry uint32, timeout time.Duration) uint64 {
	t.Helper()
	guid, _ := b.SpawnPersistent(t, entry, timeout)
	return guid
}

// Face turns toward a unit GUID.
func (b *ScenarioBot) Face(t *testing.T, targetGUID uint64) {
	t.Helper()
	FaceUnit(t, b.World, targetGUID)
}

// GM sends a GM command.
func (b *ScenarioBot) GM(t *testing.T, cmd string) {
	t.Helper()
	MustGM(t, b.World, cmd)
}

// Pos returns current position.
func (b *ScenarioBot) Pos() (x, y, z float32, mapID uint32) {
	return Position(b.World)
}

// Alive reports whether the world session still looks usable.
func (b *ScenarioBot) Alive() bool {
	return SessionAlive(b.Session)
}

// AddItem grants items via GM .additem (no push wait).
func (b *ScenarioBot) AddItem(t *testing.T, entry, count uint32) {
	t.Helper()
	AddItem(t, b.World, entry, count)
}

// AddItemWait grants items and waits for SMSG_ITEM_PUSH_RESULT for that entry.
func (b *ScenarioBot) AddItemWait(t *testing.T, entry, count uint32) (bag, slot uint8) {
	t.Helper()
	return AddItemForBankDeposit(t, b.Session, entry, count)
}

// WaitUnit waits for a nearby unit with the given template entry.
func (b *ScenarioBot) WaitUnit(t *testing.T, entry uint32, timeout time.Duration) uint64 {
	t.Helper()
	return WaitNearbyUnitByEntry(t, b.World, entry, timeout)
}

// FindUnit returns a nearby unit GUID by entry, or 0.
func (b *ScenarioBot) FindUnit(entry uint32, maxDist float32) uint64 {
	return b.World.FindUnitByEntry(entry, maxDist)
}

// WaitUnitAura waits until a unit has the given aura.
func (b *ScenarioBot) WaitUnitAura(t *testing.T, guid uint64, spellID uint32, timeout time.Duration) {
	t.Helper()
	WaitUnitAura(t, b.World, guid, spellID, timeout)
}

// UnitHasAura reports whether a tracked unit has spellID.
func (b *ScenarioBot) UnitHasAura(guid uint64, spellID uint32) bool {
	return UnitHasAura(b.World, guid, spellID)
}

// UnitHP returns cached health/max for a unit.
func (b *ScenarioBot) UnitHP(guid uint64) (hp, max uint32) {
	return UnitHealth(b.World, guid)
}

// Attack swings at targetGUID (sets target first).
func (b *ScenarioBot) Attack(t *testing.T, targetGUID uint64) {
	t.Helper()
	if err := b.World.SetTarget(targetGUID); err != nil {
		t.Logf("SetTarget: %v", err)
	}
	if err := b.World.AttackSwing(targetGUID); err != nil {
		t.Fatalf("AttackSwing: %v", err)
	}
}

// AttackUntil swings until target health fraction is at or below maxHealthFrac.
func (b *ScenarioBot) AttackUntil(t *testing.T, targetGUID uint64, maxHealthFrac float64, timeout time.Duration) {
	t.Helper()
	AttackUntilHealthBelow(t, b.Session, targetGUID, maxHealthFrac, timeout)
}

// SetSkill sets a skill via GM `.setskill <id> <level> <max>`.
func (b *ScenarioBot) SetSkill(t *testing.T, skillID, level, max uint32) {
	t.Helper()
	MustGM(t, b.World, fmt.Sprintf(".setskill %d %d %d", skillID, level, max))
}

// CastSelfGM forces a self-cast via `.cast self <spell>` (bypasses client cast path).
// Selects the player first — AC requires a selected unit for `.cast self`.
func (b *ScenarioBot) CastSelfGM(t *testing.T, spellID uint32) {
	t.Helper()
	if g := b.World.CharGUID(); g != 0 {
		_ = b.World.SetTarget(g)
	}
	MustGM(t, b.World, fmt.Sprintf(".cast self %d", spellID))
}

// CombatStop clears combat via GM `.combatstop` (self).
func (b *ScenarioBot) CombatStop(t *testing.T) {
	t.Helper()
	MustGM(t, b.World, ".combatstop")
}

// GiveTotems adds shaman totem tools.
func (b *ScenarioBot) GiveTotems(t *testing.T) {
	t.Helper()
	GiveShamanTotems(t, b.World)
}

// EnablePvP disables GM mode and flags the player for PvP via CMSG_TOGGLE_PVP
// (AC has no `.pvp on` command). FlushWorld acks `.gm off`; WaitSelfPvP waits
// for UNIT_BYTE2_FLAG_PVP on this session's own object.
func (b *ScenarioBot) EnablePvP(t *testing.T) {
	t.Helper()
	MustGM(t, b.World, ".gm off")
	FlushWorld(t, b.World)
	if err := b.World.SetPvP(true); err != nil {
		Preconditionf(t, "CMSG_TOGGLE_PVP: %v", err)
	}
	WaitSelfPvP(t, b.World, 5*time.Second)
}

// --- multi-bot conveniences ---

// TeleportAll teleports every bot to the same coordinates.
func TeleportAll(t *testing.T, bots []*ScenarioBot, x, y, z float32, mapID uint32) {
	t.Helper()
	for _, b := range bots {
		if b == nil {
			continue
		}
		b.Teleport(t, x, y, z, mapID)
	}
}

// TeleportAllPad teleports every bot to pad via .go xyz (same as TeleportPad per bot).
// Prefer this over TeleportAll(..., pad.X, pad.Y, pad.Z, pad.Map).
func TeleportAllPad(t *testing.T, bots []*ScenarioBot, pad Position3) {
	t.Helper()
	TeleportAll(t, bots, pad.X, pad.Y, pad.Z, pad.Map)
}

// EnableHostilePvP enables PvP on both bots (for cross-faction combat).
func EnableHostilePvP(t *testing.T, a, b *ScenarioBot) {
	t.Helper()
	a.EnablePvP(t)
	b.EnablePvP(t)
}

// FormPartyWith is FormParty(leader, members...) for chained multi-bot setup.
// Prefer package FormParty when the leader is known.
func (b *ScenarioBot) FormPartyWith(t *testing.T, members ...*ScenarioBot) {
	t.Helper()
	FormParty(t, b, members...)
}
