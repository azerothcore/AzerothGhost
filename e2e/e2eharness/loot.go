package e2eharness

import (
	"sync"
	"testing"
	"time"

	"github.com/azerothcore/AzerothGhost/client"
)

// OpenLoot sends CMSG_LOOT and waits for OnLootOpened (or times out fatally).
func (b *ScenarioBot) OpenLoot(t *testing.T, lootGUID uint64, timeout time.Duration) []client.LootItem {
	t.Helper()
	items, ok := b.TryOpenLoot(t, lootGUID, timeout)
	if !ok {
		HarnessFailf(t, "OpenLoot timeout for guid=0x%X", lootGUID)
	}
	return items
}

// TryOpenLoot sends CMSG_LOOT and waits for loot open. Returns ok=false on timeout
// (does not fatal). Prefer this when outdoor corpses may not be lootable.
// Uses AddLootOpenedHook (race-safe multi-subscriber).
func (b *ScenarioBot) TryOpenLoot(t *testing.T, lootGUID uint64, timeout time.Duration) (items []client.LootItem, ok bool) {
	t.Helper()
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	type result struct {
		guid  uint64
		items []client.LootItem
	}
	ch := make(chan result, 2)
	cancel := b.World.AddLootOpenedHook(func(g uint64, items []client.LootItem) {
		if lootGUID == 0 || g == lootGUID {
			select {
			case ch <- result{g, items}:
			default:
			}
		}
	})
	defer cancel()

	if err := b.World.Loot(lootGUID); err != nil {
		HarnessFailf(t, "Loot: %v", err)
	}
	select {
	case r := <-ch:
		return r.items, true
	case <-time.After(timeout):
		return nil, false
	}
}

// ArmLootStartRoll installs a start-roll waiter before opening loot.
// Returns wait and cancel. Prefer: wait, cancel := ArmLootStartRoll(); defer cancel(); ...; wait(...)
// If wait is never called, cancel still removes the hook (use t.Cleanup(cancel)).
func (b *ScenarioBot) ArmLootStartRoll() (wait func(itemID uint32, timeout time.Duration) (client.LootStartRoll, bool), cancel func()) {
	ch := make(chan client.LootStartRoll, 8)
	cancel = b.World.AddLootStartRollHook(func(r client.LootStartRoll) {
		select {
		case ch <- r:
		default:
		}
	})
	wait = func(itemID uint32, timeout time.Duration) (client.LootStartRoll, bool) {
		if timeout <= 0 {
			timeout = 15 * time.Second
		}
		for _, r := range b.World.ActiveLootRolls() {
			if itemID == 0 || r.ItemID == itemID {
				cancel()
				return r, true
			}
		}
		deadline := time.After(timeout)
		for {
			select {
			case r := <-ch:
				if itemID == 0 || r.ItemID == itemID {
					cancel()
					return r, true
				}
			case <-deadline:
				cancel()
				return client.LootStartRoll{}, false
			}
		}
	}
	return wait, cancel
}

// SpawnKillLootable spawns entry, kills it, waits until the corpse is lootable.
//
// Important:
//   - Uses `.npc add` (persistent DB spawn) because `.npc add temp` is
//     TEMPSUMMON_CORPSE_DESPAWN — corpse is removed instantly and cannot be looted.
//   - Captures DB spawn id and registers t.Cleanup `.npc delete <id>` so residue
//     is removed even after the client drops the unit from cache.
//   - Despawns leftover same-entry units nearby before spawn (pad pollution).
//   - Does not re-teleport after kill; open loot while still next to the corpse.
//   - Leaves bot CombatReady (gm off + god) after kill for normal loot interaction.
func (b *ScenarioBot) SpawnKillLootable(t *testing.T, entry uint32, timeout time.Duration) uint64 {
	t.Helper()
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	deadline := time.Now().Add(timeout)

	// Persistent spawn + SQL-backed cleanup (SpawnPersistent).
	guid, dbSpawnID := b.SpawnPersistent(t, entry, time.Until(deadline))
	if dbSpawnID == 0 {
		t.Logf("SpawnKillLootable: warning entry=%d dbSpawn=0 — cleanup may fail (set E2E_WORLD_DSN)", entry)
	}
	t.Logf("SpawnKillLootable: entry=%d guid=0x%X dbSpawn=%d", entry, guid, dbSpawnID)

	// Damage under GM until dead (object-cache HP). Stay put — no tele after kill.
	MustGM(t, b.World, ".gm on")
	for time.Now().Before(deadline) {
		hp, max := b.UnitHP(guid)
		if max > 0 && hp == 0 {
			break
		}
		b.Damage(t, guid, 10_000_000)
		time.Sleep(50 * time.Millisecond)
	}
	b.WaitUnitDead(t, guid, 15*time.Second)
	// Normal-player loot interaction (gm off); stay next to corpse.
	b.CombatReady(t)
	b.WaitUnitLootable(t, guid, 15*time.Second)
	return guid
}

// LootRelease closes the loot window.
func (b *ScenarioBot) LootRelease(t *testing.T, lootGUID uint64) {
	t.Helper()
	if err := b.World.LootRelease(lootGUID); err != nil {
		HarnessFailf(t, "LootRelease: %v", err)
	}
}

// LootTakeItem takes slot via CMSG_AUTOSTORE_LOOT_ITEM.
func (b *ScenarioBot) LootTakeItem(t *testing.T, slot uint8) {
	t.Helper()
	if err := b.World.LootItem(slot); err != nil {
		HarnessFailf(t, "LootItem: %v", err)
	}
}

// WaitLootStartRoll waits for SMSG_LOOT_START_ROLL matching optional itemID (0 = any).
func (b *ScenarioBot) WaitLootStartRoll(t *testing.T, itemID uint32, timeout time.Duration) client.LootStartRoll {
	t.Helper()
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	for _, r := range b.World.ActiveLootRolls() {
		if itemID == 0 || r.ItemID == itemID {
			return r
		}
	}
	ch := make(chan client.LootStartRoll, 8)
	cancel := b.World.AddLootStartRollHook(func(r client.LootStartRoll) {
		if itemID == 0 || r.ItemID == itemID {
			select {
			case ch <- r:
			default:
			}
		}
	})
	defer cancel()
	for _, r := range b.World.ActiveLootRolls() {
		if itemID == 0 || r.ItemID == itemID {
			return r
		}
	}
	select {
	case r := <-ch:
		return r
	case <-time.After(timeout):
		HarnessFailf(t, "WaitLootStartRoll itemID=%d timeout (active=%d)", itemID, len(b.World.ActiveLootRolls()))
		return client.LootStartRoll{}
	}
}

// RollNeed / RollGreed / RollPass vote on a start-roll snapshot.
func (b *ScenarioBot) RollNeed(t *testing.T, roll client.LootStartRoll) {
	t.Helper()
	b.lootRoll(t, roll, client.RollNeed)
}
func (b *ScenarioBot) RollGreed(t *testing.T, roll client.LootStartRoll) {
	t.Helper()
	b.lootRoll(t, roll, client.RollGreed)
}
func (b *ScenarioBot) RollPass(t *testing.T, roll client.LootStartRoll) {
	t.Helper()
	b.lootRoll(t, roll, client.RollPass)
}

func (b *ScenarioBot) lootRoll(t *testing.T, roll client.LootStartRoll, rollType uint8) {
	t.Helper()
	if err := b.World.LootRoll(roll.ItemGUID, roll.ItemSlot, rollType); err != nil {
		HarnessFailf(t, "LootRoll type=%d: %v", rollType, err)
	}
}

// ArmLootRollOutcome arms LOOT_ROLL_WON and LOOT_ALL_PASSED before casting votes.
// Prefer Arm → Roll* → select/wait (same pattern as ArmLootStartRoll / OpenTrade).
// itemID 0 matches any item. Cancel removes both hooks (call via t.Cleanup).
func (b *ScenarioBot) ArmLootRollOutcome(itemID uint32) (won <-chan client.LootRollWon, allPassed <-chan client.LootAllPassed, cancel func()) {
	wonCh := make(chan client.LootRollWon, 8)
	allCh := make(chan client.LootAllPassed, 8)
	c1 := b.World.AddLootRollWonHook(func(r client.LootRollWon) {
		if itemID == 0 || r.ItemID == itemID {
			select {
			case wonCh <- r:
			default:
			}
		}
	})
	c2 := b.World.AddLootAllPassedHook(func(r client.LootAllPassed) {
		if itemID == 0 || r.ItemID == itemID {
			select {
			case allCh <- r:
			default:
			}
		}
	})
	var once sync.Once
	cancel = func() {
		once.Do(func() {
			c1()
			c2()
		})
	}
	return wonCh, allCh, cancel
}

// WaitLootRollWon waits for SMSG_LOOT_ROLL_WON matching optional itemID (0=any).
// Prefer ArmLootRollOutcome before Roll* — this helper arms only after the call,
// so it can miss a resolution that already fired.
func (b *ScenarioBot) WaitLootRollWon(t *testing.T, itemID uint32, timeout time.Duration) client.LootRollWon {
	t.Helper()
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	ch, _, cancel := b.ArmLootRollOutcome(itemID)
	defer cancel()
	select {
	case r := <-ch:
		return r
	case <-time.After(timeout):
		HarnessFailf(t, "WaitLootRollWon itemID=%d timeout", itemID)
		return client.LootRollWon{}
	}
}

// WaitLootAllPassed waits for SMSG_LOOT_ALL_PASSED.
// Prefer ArmLootRollOutcome before RollPass — arms only after the call.
func (b *ScenarioBot) WaitLootAllPassed(t *testing.T, itemID uint32, timeout time.Duration) client.LootAllPassed {
	t.Helper()
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	_, ch, cancel := b.ArmLootRollOutcome(itemID)
	defer cancel()
	select {
	case r := <-ch:
		return r
	case <-time.After(timeout):
		HarnessFailf(t, "WaitLootAllPassed itemID=%d timeout", itemID)
		return client.LootAllPassed{}
	}
}

// MasterLootGive assigns a loot slot to target (leader/master only).
func (b *ScenarioBot) MasterLootGive(t *testing.T, lootGUID uint64, slot uint8, target *ScenarioBot) {
	t.Helper()
	if target == nil {
		Preconditionf(t, "MasterLootGive: nil target")
	}
	if err := b.World.LootMasterGive(lootGUID, slot, target.GUID); err != nil {
		HarnessFailf(t, "LootMasterGive: %v", err)
	}
}

// WaitUnitLootable waits until unit has UNIT_DYNFLAG_LOOTABLE (or dead corpse dynflag).
// Preferred after Damage/DamageKill/WaitUnitDead before OpenLoot:
//
//	bot.DamageKill(t, []uint64{guid}, 0, 10*time.Second)
//	bot.WaitUnitLootable(t, guid, 15*time.Second)
//	items := bot.OpenLoot(t, guid, 10*time.Second)
//
// Soft-continues if dynflags never appear (some cores delay UPDATE_OBJECT).
func (b *ScenarioBot) WaitUnitLootable(t *testing.T, guid uint64, timeout time.Duration) {
	t.Helper()
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		obj := b.World.GetObject(guid)
		if obj != nil {
			dyn := obj.Value(client.UnitDynamicFlags)
			if dyn&(client.UnitDynflagLootable|client.UnitDynflagDead) != 0 {
				return
			}
		}
		time.Sleep(40 * time.Millisecond)
	}
	// Soft: still try loot
	t.Logf("WaitUnitLootable: dynflags not observed for 0x%X within %s (continuing)", guid, timeout)
}
