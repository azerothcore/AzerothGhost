package e2eharness

import (
	"database/sql"
	"testing"
	"time"

	"github.com/walkline/AzerothGhost/client"
)

// --- money / inventory (protocol + CharDB) ---

// PlayerMoney returns live copper from PLAYER_FIELD_COINAGE (0 until first self update).
func (b *ScenarioBot) PlayerMoney() uint32 {
	return b.World.Money()
}

// MoneyAfterSave flushes character and returns characters.money for this bot.
// Polls CharDB briefly — `.save` is async and a single immediate SELECT often races to 0.
func (b *ScenarioBot) MoneyAfterSave(t *testing.T) uint32 {
	t.Helper()
	b.Save(t)
	deadline := time.Now().Add(3 * time.Second)
	var last uint32
	var lastErr error
	for time.Now().Before(deadline) {
		m, err := CharacterMoney(b.CharDB, b.GUID)
		if err != nil {
			lastErr = err
			time.Sleep(100 * time.Millisecond)
			continue
		}
		last = m
		// Any successful read after save is authoritative enough for callers that
		// only need "current DB money"; AssertMoneyAtLeast re-saves/polls if short.
		return m
	}
	if lastErr != nil {
		HarnessFailf(t, "CharacterMoney: %v", lastErr)
	}
	return last
}

// SetMoney sets absolute copper via GM `.modify money` relative to current DB/live value.
// Prefer ModMoney for additive funding; this drains then adds to approximate `copper`.
// Verifies CharDB (not only live coinage) — `.modify money` + `.save` can lag under thrash.
func (b *ScenarioBot) SetMoney(t *testing.T, copper uint32) {
	t.Helper()
	// modify money requires a selected player; keep GM mode so chat commands are reliable.
	MustGM(t, b.World, ".gm on")
	if guid := b.World.CharGUID(); guid != 0 {
		_ = b.World.SetTarget(guid)
	}
	DrainPlayerMoney(t, b.World)
	if copper == 0 {
		b.Save(t)
		return
	}
	// Apply + poll CharDB; re-apply if save races or the command was dropped.
	for attempt := 0; attempt < 5; attempt++ {
		if guid := b.World.CharGUID(); guid != 0 {
			_ = b.World.SetTarget(guid)
		}
		ModMoney(t, b.World, copper)
		b.Save(t)
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			m, err := CharacterMoney(b.CharDB, b.GUID)
			if err == nil && m >= copper {
				return
			}
			// Live field can lead CharDB — accept if live is already funded.
			if b.PlayerMoney() >= copper {
				// One more save so subsequent AssertMoneyAtLeast sees CharDB.
				b.Save(t)
				if m2, err2 := CharacterMoney(b.CharDB, b.GUID); err2 == nil && m2 >= copper {
					return
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Logf("SetMoney attempt %d: CharDB money short of %d (retry)", attempt+1, copper)
	}
	got, err := CharacterMoney(b.CharDB, b.GUID)
	if err != nil {
		Preconditionf(t, "SetMoney: CharacterMoney: %v", err)
	}
	Preconditionf(t, "SetMoney: characters.money=%d want>=%d after retries", got, copper)
}

// ModMoney is ScenarioBot wrapper for package ModMoney.
func (b *ScenarioBot) ModMoney(t *testing.T, copper uint32) {
	t.Helper()
	ModMoney(t, b.World, copper)
}

// WaitPlayerMoney waits until live PLAYER_FIELD_COINAGE is >= minCopper.
// Use after ModMoney/SetMoney when reading PlayerMoney() (not CharDB).
func (b *ScenarioBot) WaitPlayerMoney(t *testing.T, minCopper uint32, timeout time.Duration) uint32 {
	t.Helper()
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	deadline := time.Now().Add(timeout)
	var last uint32
	for time.Now().Before(deadline) {
		last = b.PlayerMoney()
		if last >= minCopper {
			return last
		}
		time.Sleep(40 * time.Millisecond)
	}
	HarnessFailf(t, "PlayerMoney=%d want>=%d within %s", last, minCopper, timeout)
	return last
}

// AssertMoneyAtLeast fatals if CharDB money after Save is below minCopper.
// Prefer this over live PlayerMoney() for durable economy asserts (trade/session).
// Polls after save — single-shot SELECT races the worldserver flush.
func (b *ScenarioBot) AssertMoneyAtLeast(t *testing.T, minCopper uint32) {
	t.Helper()
	b.Save(t)
	deadline := time.Now().Add(5 * time.Second)
	var last uint32
	var lastOK bool
	for time.Now().Before(deadline) {
		m, err := CharacterMoney(b.CharDB, b.GUID)
		if err != nil {
			time.Sleep(150 * time.Millisecond)
			continue
		}
		last, lastOK = m, true
		if m >= minCopper {
			return
		}
		// Re-save mid-poll in case the first .save was dropped under thrash.
		if time.Until(deadline) < 3*time.Second {
			b.Save(t)
		}
		time.Sleep(150 * time.Millisecond)
	}
	if !lastOK {
		Preconditionf(t, "characters.money: no readable row for guid=%d want>=%d", b.GUID, minCopper)
	}
	Preconditionf(t, "characters.money=%d want>=%d", last, minCopper)
}

// AssertMoneyEqual fatals if CharDB money after Save is not exactly copper.
func (b *ScenarioBot) AssertMoneyEqual(t *testing.T, copper uint32) {
	t.Helper()
	got := b.MoneyAfterSave(t)
	if got != copper {
		Preconditionf(t, "characters.money=%d want=%d", got, copper)
	}
}

// AssertInventoryAtLeast fatals if CharDB stack count for entry is below min after Save.
func (b *ScenarioBot) AssertInventoryAtLeast(t *testing.T, entry uint32, min int) {
	t.Helper()
	got := b.InventoryCount(t, entry)
	if got < min {
		Preconditionf(t, "inventory entry=%d count=%d want>=%d", entry, got, min)
	}
}

// InventoryCount returns sum of item stacks for entry after Save (CharDB).
func (b *ScenarioBot) InventoryCount(t *testing.T, entry uint32) int {
	t.Helper()
	b.Save(t)
	n, err := CharacterInventoryCount(b.CharDB, b.GUID, entry)
	if err != nil {
		HarnessFailf(t, "CharacterInventoryCount entry=%d: %v", entry, err)
	}
	return n
}

// InventoryCountNoSave queries CharDB without forcing Save (online may be stale).
func (b *ScenarioBot) InventoryCountNoSave(t *testing.T, entry uint32) int {
	t.Helper()
	n, err := CharacterInventoryCount(b.CharDB, b.GUID, entry)
	if err != nil {
		HarnessFailf(t, "CharacterInventoryCount entry=%d: %v", entry, err)
	}
	return n
}

// CharacterMoney reads characters.money for guid.
func CharacterMoney(db *sql.DB, guid uint64) (uint32, error) {
	var m uint32
	err := db.QueryRow(`SELECT money FROM characters WHERE guid=?`, guid).Scan(&m)
	return m, err
}

// CharacterInventoryCount sums item_instance.count for entry in character_inventory.
func CharacterInventoryCount(db *sql.DB, guid uint64, entry uint32) (int, error) {
	var n sql.NullInt64
	err := db.QueryRow(`
		SELECT COALESCE(SUM(ii.count), 0)
		FROM character_inventory ci
		INNER JOIN item_instance ii ON ii.guid = ci.item
		WHERE ci.guid=? AND ii.itemEntry=?`, guid, entry).Scan(&n)
	if err != nil {
		return 0, err
	}
	if !n.Valid {
		return 0, nil
	}
	return int(n.Int64), nil
}

// --- power / channel / casting-ish ---

// PlayerPower returns current and max power (mana/rage/energy from UnitFieldPower1).
func (b *ScenarioBot) PlayerPower() (current, max uint32) {
	return b.World.Power()
}

// IsChanneling reports UNIT_CHANNEL_SPELL != 0 on the player.
func (b *ScenarioBot) IsChanneling() bool {
	return b.World.IsChanneling()
}

// ChannelSpell returns UNIT_CHANNEL_SPELL on the player (0 if idle).
func (b *ScenarioBot) ChannelSpell() uint32 {
	return b.World.ChannelSpell()
}

// WaitChanneling waits until the player is channeling (optional spellID filter; 0 = any).
func (b *ScenarioBot) WaitChanneling(t *testing.T, spellID uint32, timeout time.Duration) {
	t.Helper()
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		cur := b.ChannelSpell()
		if cur != 0 && (spellID == 0 || cur == spellID) {
			return
		}
		time.Sleep(40 * time.Millisecond)
	}
	HarnessFailf(t, "not channeling spell=%d within %s (channel=%d)", spellID, timeout, b.ChannelSpell())
}

// WaitNotChanneling waits until UNIT_CHANNEL_SPELL is 0.
func (b *ScenarioBot) WaitNotChanneling(t *testing.T, timeout time.Duration) {
	t.Helper()
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !b.IsChanneling() {
			return
		}
		time.Sleep(40 * time.Millisecond)
	}
	HarnessFailf(t, "still channeling spell=%d after %s", b.ChannelSpell(), timeout)
}

// CancelAura sends CMSG_CANCEL_AURA for spellID.
func (b *ScenarioBot) CancelAura(t *testing.T, spellID uint32) {
	t.Helper()
	if err := b.World.CancelAura(spellID); err != nil {
		t.Fatalf("CancelAura(%d): %v", spellID, err)
	}
}

// CancelCast sends CMSG_CANCEL_CAST.
func (b *ScenarioBot) CancelCast(t *testing.T) {
	t.Helper()
	if err := b.World.CancelCast(); err != nil {
		t.Fatalf("CancelCast: %v", err)
	}
}

// AuraStacks returns stack count for spellID on the player (0 if absent).
func (b *ScenarioBot) AuraStacks(spellID uint32) int {
	return b.World.SelfAuraStacks(spellID)
}

// UnitAuraStacks returns stack count for spellID on a tracked unit.
func (b *ScenarioBot) UnitAuraStacks(guid uint64, spellID uint32) int {
	obj := b.World.GetObject(guid)
	if obj == nil {
		return 0
	}
	return obj.AuraStacks(spellID)
}

// --- target waiters ---

// UnitIsPvP reports UNIT_BYTE2_FLAG_PVP on a cached unit (false if unknown).
func UnitIsPvP(w *client.WorldClient, guid uint64) bool {
	if w == nil || guid == 0 {
		return false
	}
	obj := w.GetObject(guid)
	return obj != nil && obj.IsPvP()
}

// WaitUnitPvP waits until the unit is PvP-flagged in this client's object cache.
func WaitUnitPvP(t *testing.T, w *client.WorldClient, guid uint64, timeout time.Duration) {
	t.Helper()
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if UnitIsPvP(w, guid) {
			return
		}
		time.Sleep(40 * time.Millisecond)
	}
	Preconditionf(t, "unit 0x%X not PvP-flagged within %s", guid, timeout)
}

// WaitUnitPvP waits until guid is PvP-flagged in this bot's object cache.
func (b *ScenarioBot) WaitUnitPvP(t *testing.T, guid uint64, timeout time.Duration) {
	t.Helper()
	WaitUnitPvP(t, b.World, guid, timeout)
}

// WaitUnitTarget waits until unit guid's UNIT_FIELD_TARGET equals wantTarget
// (wantTarget==0 waits for clear target).
func (b *ScenarioBot) WaitUnitTarget(t *testing.T, guid, wantTarget uint64, timeout time.Duration) {
	t.Helper()
	WaitUnitTarget(t, b.World, guid, wantTarget, timeout)
}

// WaitUnitTarget is the package-level waiter used by multi-bot threat tests.
func WaitUnitTarget(t *testing.T, w *client.WorldClient, guid, wantTarget uint64, timeout time.Duration) {
	t.Helper()
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	deadline := time.Now().Add(timeout)
	var last uint64
	for time.Now().Before(deadline) {
		last = UnitTargetGUID(w, guid)
		if last == wantTarget {
			t.Logf("unit 0x%X target=0x%X (ok)", guid, last)
			return
		}
		time.Sleep(40 * time.Millisecond)
	}
	HarnessFailf(t, "unit 0x%X target=0x%X want=0x%X within %s", guid, last, wantTarget, timeout)
}

// AssertUnitTarget fatals if unit target is not wantTarget.
func (b *ScenarioBot) AssertUnitTarget(t *testing.T, guid, wantTarget uint64) {
	t.Helper()
	got := b.UnitTarget(guid)
	if got != wantTarget {
		Preconditionf(t, "unit 0x%X target=0x%X want=0x%X", guid, got, wantTarget)
	}
}

// --- death / reclaim ---

// CorpseReclaimRadius is AC CORPSE_RECLAIM_RADIUS (MiscHandler) — server silently
// rejects CMSG_RECLAIM_CORPSE if the ghost is farther than this.
const CorpseReclaimRadius float32 = 39

// ReclaimCorpse sends CMSG_RECLAIM_CORPSE once (resurrect at corpse when in range).
// Prefer ReclaimCorpseMust — a single send often races mid-teleport and is ignored.
func (b *ScenarioBot) ReclaimCorpse(t *testing.T) {
	t.Helper()
	if err := b.World.ReclaimCorpse(); err != nil {
		t.Fatalf("ReclaimCorpse: %v", err)
	}
}

// ReclaimCorpseMust follows AC reclaim protocol after ReleaseSpirit:
//
//  1. Wait SMSG_CORPSE_RECLAIM_DELAY (server legal time) — do not spam reclaim.
//  2. Teleport to corpse and wait PhaseInWorld + within CorpseReclaimRadius (39).
//  3. Send CMSG_RECLAIM_CORPSE a few times until Health()>0.
//
// Root cause of long “timeouts”: Unit::Kill(player,player) from .die sets PvP corpse
// (SetPvPDeath(true)), so delay is often 30s when Death.CorpseReclaimDelay.PvP=1.
// We honor the delay packet instead of guessing a fixed sleep.
func (b *ScenarioBot) ReclaimCorpseMust(t *testing.T, corpseX, corpseY, corpseZ float32, mapID uint32, timeout time.Duration) {
	t.Helper()
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	if b.World.Health() > 0 {
		return
	}
	deadline := time.Now().Add(timeout)

	// (1) Server-authoritative delay from BuildPlayerRepop / death path.
	remain := time.Until(deadline)
	if remain <= 0 {
		Preconditionf(t, "ReclaimCorpseMust: no time left before delay wait")
	}
	if err := b.World.WaitCorpseReclaimAllowed(remain); err != nil {
		Preconditionf(t, "ReclaimCorpseMust: reclaim delay: %v", err)
	}

	// (2) At corpse, fully settled — never reclaim mid near_teleport.
	b.Teleport(t, corpseX, corpseY, corpseZ, mapID)
	if err := b.World.WaitForSessionPhase(client.PhaseInWorld, 8*time.Second); err != nil {
		t.Logf("ReclaimCorpseMust: WaitInWorld after tele: %v", err)
	}
	b.WaitNear(t, corpseX, corpseY, corpseZ, CorpseReclaimRadius-5, 10*time.Second)
	if !b.World.IsInWorld() {
		if err := b.World.WaitForSessionPhase(client.PhaseInWorld, 5*time.Second); err != nil {
			Preconditionf(t, "ReclaimCorpseMust: not InWorld before reclaim: %v", err)
		}
	}

	// (3) Protocol send after preconditions; short retries for packet loss only.
	for attempt := 0; attempt < 5; attempt++ {
		if b.World.Health() > 0 {
			return
		}
		if time.Now().After(deadline) {
			break
		}
		px, py, pz, _ := b.Pos()
		if Distance3D(px, py, pz, corpseX, corpseY, corpseZ) > CorpseReclaimRadius-2 {
			b.Teleport(t, corpseX, corpseY, corpseZ, mapID)
			_ = b.World.WaitForSessionPhase(client.PhaseInWorld, 5*time.Second)
			b.WaitNear(t, corpseX, corpseY, corpseZ, CorpseReclaimRadius-5, 8*time.Second)
		}
		if err := b.World.ReclaimCorpse(); err != nil {
			t.Logf("ReclaimCorpse send: %v", err)
		}
		waitUntil := time.Now().Add(500 * time.Millisecond)
		for time.Now().Before(waitUntil) {
			if b.World.Health() > 0 {
				return
			}
			time.Sleep(40 * time.Millisecond)
		}
	}
	Preconditionf(t, "ReclaimCorpseMust: still dead hp=%d/%d delay_ms=%d near=(%.1f,%.1f,%.1f)",
		b.World.Health(), b.World.MaxHealth(), b.World.CorpseReclaimDelayMs(), corpseX, corpseY, corpseZ)
}

// WaitAlive waits until player health > 0.
func (b *ScenarioBot) WaitAlive(t *testing.T, timeout time.Duration) {
	t.Helper()
	WaitAlive(t, b.World, timeout)
}

// WaitAlive is the package-level helper.
func WaitAlive(t *testing.T, w *client.WorldClient, timeout time.Duration) {
	t.Helper()
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if w.Health() > 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("player still dead (hp=%d/%d) after %s", w.Health(), w.MaxHealth(), timeout)
}

// --- pets ---

// PlayerPetGUID returns UNIT_FIELD_SUMMON on the player (0 if none).
func (b *ScenarioBot) PlayerPetGUID() uint64 {
	return b.World.PlayerPetGUID()
}

// WaitPlayerPet waits until UNIT_FIELD_SUMMON is non-zero (or returns existing).
func (b *ScenarioBot) WaitPlayerPet(t *testing.T, timeout time.Duration) uint64 {
	t.Helper()
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if g := b.PlayerPetGUID(); g != 0 {
			// Prefer live object; still return field even if cache lags.
			t.Logf("player pet guid=0x%X", g)
			return g
		}
		// Fallback: unit with SUMMONEDBY == player (guardians / delayed field).
		if g := findUnitSummonedBy(b.World, b.GUID, 40); g != 0 {
			t.Logf("player pet via summonedby guid=0x%X", g)
			return g
		}
		time.Sleep(50 * time.Millisecond)
	}
	HarnessFailf(t, "no player pet within %s", timeout)
	return 0
}

// AssertNoPlayerPet fatals if a pet GUID is still present.
func (b *ScenarioBot) AssertNoPlayerPet(t *testing.T) {
	t.Helper()
	if g := b.PlayerPetGUID(); g != 0 {
		Preconditionf(t, "unexpected player pet 0x%X still present", g)
	}
}

// WaitNoPlayerPet waits until UNIT_FIELD_SUMMON is cleared.
func (b *ScenarioBot) WaitNoPlayerPet(t *testing.T, timeout time.Duration) {
	t.Helper()
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if b.PlayerPetGUID() == 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	HarnessFailf(t, "player pet still 0x%X after %s", b.PlayerPetGUID(), timeout)
}

// DismissPet sends CMSG_PET_ACTION COMMAND_ABANDON for the current (or given) pet.
// Soft when the socket is closed (cleanup path) — does not fail the test.
func (b *ScenarioBot) DismissPet(t *testing.T, petGUID uint64) {
	t.Helper()
	if b == nil || b.World == nil || b.World.IsStopped() {
		return
	}
	if petGUID == 0 {
		petGUID = b.PlayerPetGUID()
	}
	if petGUID == 0 {
		return
	}
	if err := b.World.DismissPet(petGUID); err != nil {
		// Soft: closed connection during t.Cleanup is expected.
		t.Logf("DismissPet 0x%X: %v", petGUID, err)
	}
}

// Known temporary summon entries left by e2e casts (not DB spawns — live only).
const (
	CreatureRisenGhoul         uint32 = 26125 // DK Raise Dead
	CreatureRisenAlly          uint32 = 30230 // Raise Ally
	CreatureArmyOfTheDeadGhoul uint32 = 24207 // Army of the Dead
)

// CleanupOwnedSummons dismisses the controlled pet and despawns nearby units this
// player summoned/created (Risen Ghoul, guardians, totems, etc.).
// Soft — safe from t.Cleanup after socket close. Prefer calling while still InWorld.
func (b *ScenarioBot) CleanupOwnedSummons(t *testing.T) {
	t.Helper()
	if b == nil || b.World == nil || b.World.IsStopped() {
		return
	}
	owner := b.GUID
	if owner == 0 {
		owner = b.World.CharGUID()
	}
	// 1) Primary pet slot (UNIT_FIELD_SUMMON).
	if pet := b.PlayerPetGUID(); pet != 0 {
		b.DismissPet(t, pet)
	}
	// 2) Any nearby unit with SUMMONEDBY / CREATEDBY == us (ghouls that lag the pet field,
	//    extra guardians, grounding totems, engineering dummies spell-summoned, etc.).
	const maxDist float32 = 80
	for _, u := range b.World.GetNearbyUnits(maxDist) {
		if u == nil || u.IsPlayer || u.GUID == 0 {
			continue
		}
		sumBy := u.GUIDField(client.UnitFieldSummonedBy)
		creBy := u.GUIDField(client.UnitFieldCreatedBy)
		if sumBy != owner && creBy != owner {
			continue
		}
		// Soft live despawn (select + .npc delete). Temps have no creature row.
		b.DespawnNPC(t, u.GUID)
	}
	// 3) Best-effort dismiss again if the field still points at something.
	if pet := b.PlayerPetGUID(); pet != 0 {
		b.DismissPet(t, pet)
	}
}

// PetAttack commands the pet to attack targetGUID.
// Resolves pet via UNIT_FIELD_SUMMON, then SUMMONEDBY/CREATEDBY fallback (same as WaitPlayerPet).
func (b *ScenarioBot) PetAttack(t *testing.T, targetGUID uint64) {
	t.Helper()
	pet := b.PlayerPetGUID()
	if pet == 0 {
		pet = findUnitSummonedBy(b.World, b.GUID, 40)
	}
	if pet == 0 {
		Preconditionf(t, "PetAttack: no pet")
	}
	if err := b.World.PetAttackCommand(pet, targetGUID); err != nil {
		t.Fatalf("PetAttack: %v", err)
	}
}

// WaitUnitGUID waits until guid appears in this bot's object cache (multi-bot spawn sync).
func (b *ScenarioBot) WaitUnitGUID(t *testing.T, guid uint64, timeout time.Duration) {
	t.Helper()
	if guid == 0 {
		Preconditionf(t, "WaitUnitGUID: guid is 0")
	}
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if b.World.GetObject(guid) != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	Preconditionf(t, "unit 0x%X never appeared in object cache within %s", guid, timeout)
}

func findUnitSummonedBy(w *client.WorldClient, ownerGUID uint64, maxDist float32) uint64 {
	if maxDist <= 0 {
		maxDist = 40
	}
	for _, u := range w.GetNearbyUnits(maxDist) {
		if u == nil || u.IsPlayer || !u.IsAlive() {
			continue
		}
		if u.GUIDField(client.UnitFieldSummonedBy) == ownerGUID {
			return u.GUID
		}
		if u.GUIDField(client.UnitFieldCreatedBy) == ownerGUID {
			return u.GUID
		}
	}
	return 0
}
