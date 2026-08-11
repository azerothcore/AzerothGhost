package e2eharness

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/walkline/AzerothGhost/client"
)

// objectEntryFromGUID extracts the 24-bit entry field from a map-specific
// 3.3.5a ObjectGuid (Unit / GameObject / Vehicle / Pet).
func objectEntryFromGUID(guid uint64) uint32 {
	return uint32((guid >> 24) & 0xFFFFFF)
}

// findTabardDesignerGUID polls the client object cache for Aldwin Laughlin
// (entry 4974). Cache is filled by SMSG_UPDATE_OBJECT after teleport — no spawn-id
// fallback; if the unit is missing the object stream is wrong.
func findTabardDesignerGUID(t *testing.T, w *client.WorldClient, timeout time.Duration) uint64 {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if g := w.FindUnitByEntry(StormwindTabardDesignerEntry, 40); g != 0 {
			return g
		}
		if g := w.FindUnitByEntry(StormwindTabardDesignerEntry, 0); g != 0 {
			return g
		}
		// Also match by entry embedded in ObjectGuid high bits (CREATE without template entry).
		for _, u := range w.GetNearbyUnits(50) {
			if u.Entry == StormwindTabardDesignerEntry || objectEntryFromGUID(u.GUID) == StormwindTabardDesignerEntry {
				return u.GUID
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return 0
}

// EnableGM turns on GM mode via chat (requires account GM level).
func EnableGM(t *testing.T, w *client.WorldClient) {
	t.Helper()
	MustGM(t, w, ".gm on")
}

// ModMoney adds copper via GM `.modify money` (live world path).
// AC's modify money targets the selected player only — select self first.
// Single command only (do not also send `.mod money`) to avoid double credit.
func ModMoney(t *testing.T, w *client.WorldClient, copper uint32) {
	t.Helper()
	if guid := w.CharGUID(); guid != 0 {
		if err := w.SetTarget(guid); err != nil {
			t.Logf("SetTarget(self) before mod money: %v", err)
		}
	}
	// No fixed settle: bank/money asserts wait on SMSG_GUILD_BANK_LIST / money opcodes.
	MustGM(t, w, fmt.Sprintf(".modify money %d", copper))
}

// DrainPlayerMoney best-effort zeros player copper via repeated negative
// .modify money so deposit-insufficient tests are not polluted by fixture funding.
// Uses modest deltas (large negatives have been observed to misbehave on some cores).
func DrainPlayerMoney(t *testing.T, w *client.WorldClient) {
	t.Helper()
	if guid := w.CharGUID(); guid != 0 {
		_ = w.SetTarget(guid)
	}
	for i := 0; i < 30; i++ {
		MustGM(t, w, ".modify money -1000000") // -100 gold each
	}
}

// AddItem grants item(s) via GM .additem (live world inventory; defaults to self).
// Callers that need inventory placement must wait on SMSG_ITEM_PUSH_RESULT.
func AddItem(t *testing.T, w *client.WorldClient, entry, count uint32) {
	t.Helper()
	MustGM(t, w, fmt.Sprintf(".additem %d %d", entry, count))
}

// TeleportGo teleports the player with AC `.go xyz x y z [map]` (live world).
// Note: bare `.go x y z` is not a valid AC command (needs the `xyz` subcommand).
// Waits for self teleport completion (SMSG_MOVE_TELEPORT / SMSG_NEW_WORLD).
// Near-teleport clears the client's object cache; callers that need nearby units
// must re-wait for SMSG_UPDATE_OBJECT after this returns.
func TeleportGo(t *testing.T, w *client.WorldClient, x, y, z float32, mapID uint32) {
	t.Helper()
	MustGMTeleport(t, w, fmt.Sprintf(".go xyz %.2f %.2f %.2f %d", x, y, z, mapID))
}

// TeleportToStormwindTabardDesigner places the player at Aldwin Laughlin
// (acore_world creature guid=79681) via `.go creature`. Visibility creates should
// arrive immediately after near-tele ACK. No NPC spawn/entry/xyz fallbacks —
// missing cache means UPDATE_OBJECT stream desync (see skipMovementUpdate).
func TeleportToStormwindTabardDesigner(t *testing.T, w *client.WorldClient) {
	t.Helper()
	MustGMTeleport(t, w, fmt.Sprintf(".go creature %d", StormwindTabardDesignerGUIDLow))
	if findTabardDesignerGUID(t, w, 3*time.Second) != 0 {
		t.Logf("tabard designer visible after .go creature %d", StormwindTabardDesignerGUIDLow)
		return
	}
	t.Logf("tabard designer not yet in object cache after .go creature (will wait for live UPDATE_OBJECT)")
}

// TeleportToStormwindGuildVault places the player at guild vault guid=41911
// coordinates (acore_world gameobject) via `.go xyz`.
func TeleportToStormwindGuildVault(t *testing.T, w *client.WorldClient) {
	t.Helper()
	TeleportGo(t, w,
		StormwindGuildVaultX, StormwindGuildVaultY, StormwindGuildVaultZ,
		StormwindGuildVaultMap,
	)
}

// WaitNearbyUnitByEntry polls the WorldClient object cache for a unit with the
// given template entry (from live SMSG_UPDATE_OBJECT after teleport).
// Modern AC assigns runtime ObjectGuid counters (not DB spawn ids), so petition
// buy / vendor interact must use the live GUID.
func WaitNearbyUnitByEntry(t *testing.T, w *client.WorldClient, entry uint32, timeout time.Duration) uint64 {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		// Prefer distance-limited matches, then any tracked unit of that entry
		// (CREATE may lack a position block briefly after tele).
		if g := w.FindUnitByEntry(entry, 40); g != 0 {
			t.Logf("live unit entry=%d guid=0x%X", entry, g)
			return g
		}
		if g := w.FindUnitByEntry(entry, 0); g != 0 {
			t.Logf("live unit entry=%d guid=0x%X (no dist filter)", entry, g)
			return g
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Dump nearby for diagnostics.
	units := w.GetNearbyUnits(60)
	for _, u := range units {
		t.Logf("  nearby unit guid=0x%X entry=%d guidEntry=%d",
			u.GUID, u.Entry, objectEntryFromGUID(u.GUID))
	}
	t.Fatalf("no live unit with entry=%d within %s (%d nearby units)", entry, timeout, len(units))
	return 0
}

// WaitNearbyGameObjectByEntry polls for a gameobject create with the given entry.
// AC GameObject ObjectGuids use runtime counters (not creature/gameobject.guid spawn ids).
func WaitNearbyGameObjectByEntry(t *testing.T, w *client.WorldClient, entry uint32, timeout time.Duration) uint64 {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if g := findTrackedGameObject(w, entry); g != 0 {
			t.Logf("live gameobject entry=%d guid=0x%X", entry, g)
			return g
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no live gameobject with entry=%d within %s", entry, timeout)
	return 0
}

// TryNearbyGameObjectByEntry is like WaitNearbyGameObjectByEntry but returns 0 on timeout.
func TryNearbyGameObjectByEntry(t *testing.T, w *client.WorldClient, entry uint32, timeout time.Duration) uint64 {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if g := findTrackedGameObject(w, entry); g != 0 {
			t.Logf("live gameobject entry=%d guid=0x%X", entry, g)
			return g
		}
		time.Sleep(20 * time.Millisecond)
	}
	return 0
}

func findTrackedGameObject(w *client.WorldClient, entry uint32) uint64 {
	return w.FindGameObjectByEntry(entry, 40)
}

// CharterBuyResult is the protocol outcome of CMSG_PETITION_BUY.
type CharterBuyResult struct {
	PetitionItemGUID uint64
	PetitionID       uint32 // DB petition_id when available; SMSG may use item low
	ItemLow          uint32
	ItemPush         *client.ItemPushResult
	ShowEmpty        *client.PetitionShowSignatures
}

// BuyGuildCharter teleports to the SW tabard designer and sends CMSG_PETITION_BUY.
// Primary success is protocol:
//  1. SMSG_ITEM_PUSH_RESULT with entry=Guild Charter (5863) — required (hard fail if missing)
//  2. SMSG_PETITION_SHOW_SIGNATURES empty after CMSG_PETITION_SHOW_SIGNATURES
//
// The petition item ObjectGuid is not present in ITEM_PUSH; charDB is used only *after*
// a successful push to resolve petitionguid/item low for subsequent petition opcodes
// (identity). DB alone is never treated as buy success. No SeedGuildCharter.
func BuyGuildCharter(t *testing.T, sess *Session, charDB *sql.DB, guildName string) CharterBuyResult {
	t.Helper()
	w := sess.World
	ownerGUID := sess.GUID

	sess.ArmAllWaiters()
	sess.DrainItemPushes()

	// GM first (before tele) so the player is not stuck in combat at starter zone.
	EnableGM(t, w)
	MustGM(t, w, ".combatstop")
	ModMoney(t, w, GuildCharterCostCopper+GuildBankFirstTabCost+5_000_000)

	// One tele to Aldwin; petition buy needs a live ObjectGuid from UPDATE_OBJECT.
	TeleportToStormwindTabardDesigner(t, w)
	ModMoney(t, w, GuildCharterCostCopper)
	// Hard wait — no spawn-id GUID fallback (runtime counters; fake GUIDs always fail).
	npcGUID := WaitNearbyUnitByEntry(t, w, StormwindTabardDesignerEntry, 12*time.Second)
	t.Logf("live tabard NPC guid=0x%X", npcGUID)

	_ = w.SetTarget(npcGUID)

	// Retry buy once — gRPC interact can flake under LB reconnect.
	// Success requires SMSG_ITEM_PUSH_RESULT; petition DB row alone is not enough.
	//
	// Keep waiters armed across attempts (do not re-Arm between Send retries):
	// gateway may take >12s when CanPlayerInteractWithNPC gRPC thrashs, and a late
	// push from attempt 1 would be dropped if channels were recreated/drained.
	sess.ArmAllWaiters()
	sess.DrainItemPushes()
	var push *client.ItemPushResult
	var itemLow, petitionID uint32
	var lastPushErr error
	for attempt := 1; attempt <= 2; attempt++ {
		if err := w.SendPetitionBuy(npcGUID, guildName); err != nil {
			t.Fatalf("SendPetitionBuy: %v", err)
		}
		t.Logf("CMSG_PETITION_BUY sent name=%s npc=0x%X attempt=%d", guildName, npcGUID, attempt)

		// Protocol ownership: SMSG_ITEM_PUSH_RESULT from world StoreNewItem path.
		// Long window: gateway may queue buy while world gRPC interact recovers.
		var err error
		push, err = sess.WaitItemPushEntry(ItemGuildCharter, 25*time.Second)
		if err == nil {
			t.Logf("item push OK: entry=%d bag=%d slot=%d count=%d invCount=%d",
				push.Entry, push.BagSlot, push.ItemSlot, push.Count, push.InventoryCount)
			// Identity only after push — ITEM_PUSH has no item ObjectGuid.
			itemLow, petitionID = resolvePetitionIdentity(t, charDB, ownerGUID, guildName, 10*time.Second)
			break
		}
		lastPushErr = err
		t.Logf("item push not seen on attempt %d: %v", attempt, err)
		if attempt < 2 {
			// Re-ensure money; leave waiter channel intact for a late push from attempt 1.
			ModMoney(t, w, GuildCharterCostCopper+1_000_000)
		}
	}
	if push == nil {
		// Do not accept DB petition row as buy success without item push.
		t.Fatalf("charter buy failed: no SMSG_ITEM_PUSH_RESULT entry=%d (owner=%d name=%s npc=0x%X lastErr=%v)",
			ItemGuildCharter, ownerGUID, guildName, npcGUID, lastPushErr)
	}
	if itemLow == 0 {
		t.Fatalf("charter buy: item push OK but petition identity missing (owner=%d name=%s)", ownerGUID, guildName)
	}
	petitionGUID := ItemGUID(itemLow)
	t.Logf("petition identity itemLow=%d petition_id=%d guid=0x%X", itemLow, petitionID, petitionGUID)

	// Protocol confirm: show signatures with empty list (fresh charter).
	sess.ArmAllWaiters()
	if err := w.SendPetitionShowSignatures(petitionGUID); err != nil {
		t.Fatalf("show signatures after buy: %v", err)
	}
	show, err := sess.WaitShowSignatures(15 * time.Second)
	if err != nil {
		t.Fatalf("wait show signatures after buy: %v", err)
	}
	if show.PetitionID != itemLow && show.PetitionID != petitionID && petitionID != 0 {
		t.Logf("WARNING: show petitionID=%d (itemLow=%d petition_id=%d) — accepting if signatures parse",
			show.PetitionID, itemLow, petitionID)
	}
	if len(show.SignatoryGUIDs) != 0 {
		t.Fatalf("fresh charter should have 0 signatures, got %v", show.SignatoryGUIDs)
	}
	if show.OwnerGUID != 0 && uint32(show.OwnerGUID) != uint32(ownerGUID) {
		t.Fatalf("show owner=0x%X want ~%d", show.OwnerGUID, ownerGUID)
	}
	t.Logf("show signatures empty OK (owner=0x%X petitionID=%d)", show.OwnerGUID, show.PetitionID)

	return CharterBuyResult{
		PetitionItemGUID: petitionGUID,
		PetitionID:       petitionID,
		ItemLow:          itemLow,
		ItemPush:         push,
		ShowEmpty:        show,
	}
}

// resolvePetitionIdentity polls charDB for petitionguid/petition_id after a live buy.
// This is identity resolution only (ITEM_PUSH has no item ObjectGuid).
func resolvePetitionIdentity(t *testing.T, charDB *sql.DB, ownerGUID uint64, guildName string, timeout time.Duration) (itemLow, petitionID uint32) {
	t.Helper()
	itemLow, petitionID = tryResolvePetitionIdentity(charDB, ownerGUID, guildName, timeout)
	if itemLow == 0 {
		t.Fatalf("petition identity not found after buy (owner=%d name=%s)", ownerGUID, guildName)
	}
	return itemLow, petitionID
}

// tryResolvePetitionIdentity is a non-fatal poll for petition identity.
func tryResolvePetitionIdentity(charDB *sql.DB, ownerGUID uint64, guildName string, timeout time.Duration) (itemLow, petitionID uint32) {
	if charDB == nil {
		return 0, 0
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		err := charDB.QueryRow(
			`SELECT petitionguid, IFNULL(petition_id,0) FROM petition WHERE ownerguid=? AND type=9 AND name=? LIMIT 1`,
			ownerGUID, guildName,
		).Scan(&itemLow, &petitionID)
		if err == nil && itemLow != 0 {
			return itemLow, petitionID
		}
		time.Sleep(50 * time.Millisecond)
	}
	return 0, 0
}

// BuyGuildCharterWorld is a thin wrapper for callers that only have WorldClient.
// Prefer BuyGuildCharter with a Session so item-push waiters work.
func BuyGuildCharterWorld(t *testing.T, w *client.WorldClient, charDB *sql.DB, ownerGUID uint64, guildName string) (petitionItemGUID uint64, petitionID uint32) {
	t.Helper()
	// Synthetic session around the world client for waiters.
	sess := &Session{World: w, GUID: ownerGUID}
	res := BuyGuildCharter(t, sess, charDB, guildName)
	return res.PetitionItemGUID, res.PetitionID
}

// SignPetition sends CMSG_PETITION_SIGN and asserts SMSG_PETITION_SIGN_RESULTS OK on signer.
func SignPetition(t *testing.T, signer *Session, petitionGUID uint64) *client.PetitionSignResults {
	t.Helper()
	signer.ArmAllWaiters()
	if err := signer.World.SendPetitionSign(petitionGUID); err != nil {
		t.Fatalf("SendPetitionSign: %v", err)
	}
	res, err := signer.WaitSignResults(15 * time.Second)
	if err != nil {
		t.Fatalf("wait sign results: %v", err)
	}
	if res.Result != client.PetitionSignOK {
		t.Fatalf("sign result=%d want OK(0)", res.Result)
	}
	return res
}

// ShowPetitionSignatures requests and returns SMSG_PETITION_SHOW_SIGNATURES.
func ShowPetitionSignatures(t *testing.T, sess *Session, petitionGUID uint64) *client.PetitionShowSignatures {
	t.Helper()
	sess.ArmAllWaiters()
	if err := sess.World.SendPetitionShowSignatures(petitionGUID); err != nil {
		t.Fatalf("SendPetitionShowSignatures: %v", err)
	}
	show, err := sess.WaitShowSignatures(15 * time.Second)
	if err != nil {
		t.Fatalf("wait show signatures: %v", err)
	}
	return show
}

// TurnInPetition sends CMSG_TURN_IN_PETITION and returns SMSG_TURN_IN_PETITION_RESULTS.
func TurnInPetition(t *testing.T, owner *Session, petitionGUID uint64, timeout time.Duration) uint32 {
	t.Helper()
	owner.ArmAllWaiters()
	if err := owner.World.SendTurnInPetition(petitionGUID); err != nil {
		t.Fatalf("SendTurnInPetition: %v", err)
	}
	res, err := owner.WaitTurnIn(timeout)
	if err != nil {
		t.Fatalf("wait turn-in: %v", err)
	}
	return res
}

// GuildSetup is a leader (+ optional signers) after successful guild creation.
type GuildSetup struct {
	Leader       *Session
	Signers      []*Session
	All          []*Session
	GuildName    string
	PetitionGUID uint64
	ItemLow      uint32
}

// CreateGuildViaCharter buys a charter, collects minSigns signatures, shows, and turns in.
// minSigns should match charserver MIN_PETITION_SIGNS (default 9).
// Requires len(signers) >= minSigns. Asserts SMSG_TURN_IN_PETITION_RESULTS == OK.
func CreateGuildViaCharter(t *testing.T, leader *Session, signers []*Session, charDB *sql.DB, guildName string, minSigns int) GuildSetup {
	t.Helper()
	if minSigns <= 0 {
		minSigns = MinPetitionSigns
	}
	if len(signers) < minSigns {
		t.Fatalf("need >=%d signers, have %d", minSigns, len(signers))
	}

	for _, s := range append([]*Session{leader}, signers...) {
		s.ArmAllWaiters()
	}

	buy := BuyGuildCharter(t, leader, charDB, guildName)
	petitionGUID := buy.PetitionItemGUID
	itemLow := buy.ItemLow

	for i := 0; i < minSigns; i++ {
		signer := signers[i]
		res := SignPetition(t, signer, petitionGUID)
		t.Logf("signatory %d (%s) signed OK player=0x%X", i, signer.Name, res.PlayerGUID)
	}

	show := ShowPetitionSignatures(t, leader, petitionGUID)
	if show.PetitionID != itemLow && show.PetitionID != buy.PetitionID && buy.PetitionID != 0 {
		t.Logf("WARNING: show petitionID=%d itemLow=%d petition_id=%d", show.PetitionID, itemLow, buy.PetitionID)
	}
	if len(show.SignatoryGUIDs) < minSigns {
		t.Fatalf("show signatures count=%d want >=%d (%v)", len(show.SignatoryGUIDs), minSigns, show.SignatoryGUIDs)
	}
	t.Logf("show signatures OK: count=%d petitionID=%d", len(show.SignatoryGUIDs), show.PetitionID)

	turnRes := TurnInPetition(t, leader, petitionGUID, 60*time.Second)
	if turnRes != client.PetitionTurnOK {
		t.Fatalf("turn-in result=%d want OK(0)", turnRes)
	}
	t.Logf("turn-in OK guild=%s (SMSG_TURN_IN_PETITION_RESULTS)", guildName)

	all := make([]*Session, 0, 1+len(signers))
	all = append(all, leader)
	all = append(all, signers...)
	return GuildSetup{
		Leader:       leader,
		Signers:      signers,
		All:          all,
		GuildName:    guildName,
		PetitionGUID: petitionGUID,
		ItemLow:      itemLow,
	}
}

// BankListItemCount returns how many non-empty slots match entry (0 = any non-empty).
func BankListItemCount(list *client.GuildBankList, entry uint32) int {
	if list == nil {
		return 0
	}
	n := 0
	for _, it := range list.Items {
		if it.Entry == 0 {
			continue
		}
		if entry == 0 || it.Entry == entry {
			n++
		}
	}
	return n
}

// FirstBankListItemSlot returns the first slot with the given entry, or false.
func FirstBankListItemSlot(list *client.GuildBankList, entry uint32) (uint8, bool) {
	if list == nil {
		return 0, false
	}
	for _, it := range list.Items {
		if it.Entry == entry {
			return it.Slot, true
		}
	}
	return 0, false
}

// ActivateGuildBank teleports to the SW vault, resolves banker GUID, activates, and
// returns the first SMSG_GUILD_BANK_LIST. Asserts non-nil list; prefers FullUpdate
// (logs WARNING if only a partial list arrives).
func ActivateGuildBank(t *testing.T, leader *Session) (bankerGUID uint64, list *client.GuildBankList) {
	t.Helper()
	leader.ArmAllWaiters()
	EnableGM(t, leader.World)
	bankerGUID = ResolveStormwindGuildVaultGUID(t, leader.World)
	t.Logf("bankerGUID=0x%X", bankerGUID)

	leader.DrainBankLists()
	if err := leader.World.SendGuildBankerActivate(bankerGUID, true); err != nil {
		t.Fatalf("bank activate: %v", err)
	}
	list, err := leader.WaitBankList(15 * time.Second)
	if err != nil {
		t.Fatalf("SMSG_GUILD_BANK_LIST after activate: %v (banker=0x%X)", err, bankerGUID)
	}
	if list == nil {
		t.Fatalf("nil SMSG_GUILD_BANK_LIST after activate (banker=0x%X)", bankerGUID)
	}
	if !list.FullUpdate {
		// Partial updates omit TabInfos; re-activate once for a full list when possible.
		t.Logf("activate list was partial (full=%v tabs=%d) — re-activate for FullUpdate", list.FullUpdate, len(list.TabInfos))
		leader.DrainBankLists()
		if err := leader.World.SendGuildBankerActivate(bankerGUID, true); err != nil {
			t.Fatalf("re-activate for FullUpdate: %v", err)
		}
		if full, err := leader.WaitBankList(15 * time.Second); err == nil && full != nil {
			list = full
		}
	}
	if !list.FullUpdate {
		t.Logf("WARNING: SMSG_GUILD_BANK_LIST after activate still partial (tabs only on FullUpdate&&Tab0)")
	}
	t.Logf("bank list: money=%d tabs=%d items=%d full=%v", list.Money, len(list.TabInfos), len(list.Items), list.FullUpdate)
	return bankerGUID, list
}

// BuyGuildBankTab purchases tab index and waits for an updated SMSG_GUILD_BANK_LIST.
func BuyGuildBankTab(t *testing.T, leader *Session, bankerGUID uint64, tab uint8) *client.GuildBankList {
	t.Helper()
	leader.ArmAllWaiters()
	leader.DrainBankLists()
	if err := leader.World.SendGuildBankBuyTab(bankerGUID, tab); err != nil {
		t.Fatalf("buy tab: %v", err)
	}
	list, err := leader.WaitBankList(15 * time.Second)
	if err != nil {
		// Some cores only refresh permissions; re-query tab 0.
		t.Logf("no list after buy tab, querying tab 0: %v", err)
		leader.DrainBankLists()
		if err := leader.World.SendGuildBankQueryTab(bankerGUID, 0, true); err != nil {
			t.Fatalf("query tab after buy: %v", err)
		}
		list, err = leader.WaitBankList(15 * time.Second)
		if err != nil {
			t.Fatalf("SMSG_GUILD_BANK_LIST after buy tab: %v", err)
		}
	}
	return list
}

// EnsureGuildBankTabReady hard-fails unless the leader has at least one purchased
// bank tab visible in SMSG_GUILD_BANK_LIST TabInfos (after optional buy + re-list).
func EnsureGuildBankTabReady(t *testing.T, leader *Session, bankerGUID uint64, list *client.GuildBankList) *client.GuildBankList {
	t.Helper()
	if list != nil && len(list.TabInfos) >= 1 {
		return list
	}
	if list == nil || len(list.TabInfos) == 0 {
		list = BuyGuildBankTab(t, leader, bankerGUID, 0)
	}
	if list != nil && len(list.TabInfos) >= 1 {
		return list
	}
	// Partial updates may omit tab metadata — re-activate for a full list.
	leader.DrainBankLists()
	if err := leader.World.SendGuildBankerActivate(bankerGUID, true); err != nil {
		t.Fatalf("re-activate after buy tab: %v", err)
	}
	full, err := leader.WaitBankList(15 * time.Second)
	if err != nil {
		t.Fatalf("list after re-activate (need tab proof): %v", err)
	}
	if len(full.TabInfos) < 1 {
		t.Fatalf("guild bank has no purchased tab after buy/activate (full=%v items=%d money=%d) — cannot run deposit/withdraw",
			full.FullUpdate, len(full.Items), full.Money)
	}
	t.Logf("bought bank tab 0 (tabs=%d)", len(full.TabInfos))
	return full
}

// QueryGuildBankTab sends CMSG_GUILD_BANK_QUERY_TAB and waits for SMSG_GUILD_BANK_LIST.
func QueryGuildBankTab(t *testing.T, leader *Session, bankerGUID uint64, tab uint8, fullUpdate bool) *client.GuildBankList {
	t.Helper()
	leader.ArmAllWaiters()
	leader.DrainBankLists()
	if err := leader.World.SendGuildBankQueryTab(bankerGUID, tab, fullUpdate); err != nil {
		t.Fatalf("query tab %d: %v", tab, err)
	}
	list, err := leader.WaitBankList(15 * time.Second)
	if err != nil {
		t.Fatalf("SMSG_GUILD_BANK_LIST after query tab %d: %v", tab, err)
	}
	return list
}

// WaitBankMoneyAtLeast polls until bank Money >= want.
func WaitBankMoneyAtLeast(t *testing.T, leader *Session, bankerGUID uint64, want uint64, timeout time.Duration) uint64 {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last uint64
	for time.Now().Before(deadline) {
		list := QueryGuildBankTab(t, leader, bankerGUID, 0, true)
		last = list.Money
		if last >= want {
			return last
		}
	}
	t.Fatalf("bank money still %d, want >= %d within %s", last, want, timeout)
	return last
}

// WaitBankMoneyAtMost polls until bank Money <= want.
func WaitBankMoneyAtMost(t *testing.T, leader *Session, bankerGUID uint64, want uint64, timeout time.Duration) uint64 {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last uint64
	for time.Now().Before(deadline) {
		list := QueryGuildBankTab(t, leader, bankerGUID, 0, true)
		last = list.Money
		if last <= want {
			return last
		}
	}
	t.Fatalf("bank money still %d, want <= %d within %s", last, want, timeout)
	return last
}

// WaitBankListItemCount polls until the tab list has exactly wantCount of entry.
func WaitBankListItemCount(t *testing.T, leader *Session, bankerGUID uint64, tab uint8, entry uint32, wantCount int, timeout time.Duration) *client.GuildBankList {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last *client.GuildBankList
	for time.Now().Before(deadline) {
		list := QueryGuildBankTab(t, leader, bankerGUID, tab, true)
		last = list
		if BankListItemCount(list, entry) == wantCount {
			return list
		}
	}
	t.Fatalf("bank item entry=%d count still %d, want %d (items=%+v)",
		entry, BankListItemCount(last, entry), wantCount, last.Items)
	return last
}

// DepositItemToBank deposits bag/slot into bank and waits until entry count rises.
func DepositItemToBank(t *testing.T, leader *Session, bankerGUID uint64, tab, slot, bag, bagSlot uint8, entry uint32) *client.GuildBankList {
	t.Helper()
	before := QueryGuildBankTab(t, leader, bankerGUID, tab, true)
	beforeN := BankListItemCount(before, entry)

	leader.ArmAllWaiters()
	leader.DrainBankLists()
	if err := leader.World.SendGuildBankDepositItem(bankerGUID, tab, slot, bag, bagSlot, entry, 0); err != nil {
		t.Fatalf("deposit item entry=%d: %v", entry, err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		l, err := leader.WaitBankList(2 * time.Second)
		if err != nil {
			l = QueryGuildBankTab(t, leader, bankerGUID, tab, true)
		}
		if BankListItemCount(l, entry) > beforeN {
			return l
		}
	}
	t.Fatalf("deposit item entry=%d: bank list never showed new stack (before=%d)", entry, beforeN)
	return nil
}

// WithdrawItemFromBank withdraws bank tab/slot and waits until entry count drops.
func WithdrawItemFromBank(t *testing.T, leader *Session, bankerGUID uint64, tab, slot uint8, entry uint32) *client.GuildBankList {
	t.Helper()
	before := QueryGuildBankTab(t, leader, bankerGUID, tab, true)
	beforeN := BankListItemCount(before, entry)
	if beforeN < 1 {
		t.Fatalf("withdraw: no entry=%d in bank before send", entry)
	}

	leader.ArmAllWaiters()
	leader.DrainBankLists()
	if err := leader.World.SendGuildBankWithdrawItem(bankerGUID, tab, slot, entry, 0); err != nil {
		t.Fatalf("withdraw item entry=%d slot=%d: %v", entry, slot, err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		l, err := leader.WaitBankList(2 * time.Second)
		if err != nil {
			l = QueryGuildBankTab(t, leader, bankerGUID, tab, true)
		}
		if BankListItemCount(l, entry) < beforeN {
			return l
		}
		leader.DrainBankLists()
		_ = leader.World.SendGuildBankQueryTab(bankerGUID, tab, true)
	}
	t.Fatalf("withdraw item entry=%d: still %d stacks in bank", entry, beforeN)
	return nil
}

// FillBackpackWithJunk best-effort floods bags via .additem (for full-bag withdraw tests).
func FillBackpackWithJunk(t *testing.T, sess *Session) {
	t.Helper()
	junk := []uint32{
		2589, 2592, 4306, 4338, 14047,
		117, 159, 4540, 4536, 4604, 4605, 2070,
		25861, 2901, 7005, 5956, 6256, 6365,
		6948, 1710, 858, 118,
	}
	for _, entry := range junk {
		MustGM(t, sess.World, fmt.Sprintf(".additem %d 20", entry))
	}
	t.Logf("flooded backpack with %d junk entries via .additem", len(junk))
}

// AddItemForBankDeposit .additem's entry and returns bag/slot from SMSG_ITEM_PUSH_RESULT.
// Strips residual stacks first so shared-fixture withdraws do not cause 0xFFFFFFFF stack merges.
func AddItemForBankDeposit(t *testing.T, sess *Session, entry, count uint32) (bag, slot uint8) {
	t.Helper()
	w := sess.World
	MustGM(t, w, fmt.Sprintf(".additem %d -1000", entry))
	for _, junk := range []uint32{6948, 49778, 4604, 117, 159, 2070, 4540, 4536, 4605, 2589, 2592, 4306} {
		if junk == entry {
			continue
		}
		MustGM(t, w, fmt.Sprintf(".additem %d -1000", junk))
	}

	sess.ArmAllWaiters()
	sess.DrainItemPushes()
	AddItem(t, w, entry, count)
	push, err := sess.WaitItemPushEntry(entry, 12*time.Second)
	if err != nil {
		t.Fatalf("SMSG_ITEM_PUSH_RESULT entry=%d after .additem: %v (cannot guess bag/slot)", entry, err)
	}
	bag = push.BagSlot
	// 0xFFFFFFFF = stacked onto existing (no new bag slot for deposit opcodes).
	if push.ItemSlot == 0xFFFFFFFF {
		t.Fatalf("item push for entry=%d stacked onto existing (slot=0xFFFFFFFF); need a free backpack slot for deposit", entry)
	}
	if push.ItemSlot > 0xFF {
		t.Fatalf("item push entry=%d bag=%d slot=%d out of uint8 range", entry, push.BagSlot, push.ItemSlot)
	}
	slot = uint8(push.ItemSlot)
	// Deposit wants bag=255 (INVENTORY_SLOT_BAG_0); some pushes encode backpack as 0.
	if bag == 0 {
		bag = InventoryBagBackpack
	}
	t.Logf("item push for deposit: entry=%d bag=%d slot=%d count=%d invCount=%d",
		push.Entry, bag, slot, push.Count, push.InventoryCount)
	return bag, slot
}

// logObjectTrackStats dumps WorldClient object cache stats (debug for GO visibility).
func logObjectTrackStats(t *testing.T, w *client.WorldClient, where string) {
	t.Helper()
	byType, goHigh, total := w.ObjectTrackStats()
	t.Logf("object cache @%s: total=%d goHigh=%d byType=%v sampleGOEntries=%v",
		where, total, goHigh, byType, w.ListGameObjectEntries(12))
}

// ResolveStormwindGuildVaultGUID teleports to the vault and returns a banker GUID.
// Prefer live UPDATE_OBJECT GUID when the vault is visible to the client; otherwise
// pack DB spawn-id (server CanPlayerInteractWithGO resolves spawn-id store).
func ResolveStormwindGuildVaultGUID(t *testing.T, w *client.WorldClient) uint64 {
	t.Helper()
	EnableGM(t, w)

	// AC: tele to spawn forces the grid (and vault) to load server-side.
	MustGMTeleport(t, w, fmt.Sprintf(".go gameobject %d", StormwindGuildVaultGUIDLow))
	// Creates should arrive right after tele ACK; short wait only.
	if g := TryNearbyGameObjectByEntry(t, w, StormwindGuildVaultEntry, 1500*time.Millisecond); g != 0 {
		logObjectTrackStats(t, w, "after go gameobject (live)")
		return g
	}
	if g := w.FindGameObjectByEntry(StormwindGuildVaultEntry, 0); g != 0 {
		t.Logf("live gameobject entry=%d guid=0x%X", StormwindGuildVaultEntry, g)
		return g
	}
	logObjectTrackStats(t, w, "after go gameobject")

	// Type-34 vaults often omit client creates; world interact accepts spawn-id store.
	spawnPacked := client.GameObjectGUID(StormwindGuildVaultEntry, StormwindGuildVaultGUIDLow)
	t.Logf("using spawn-id packed vault guid=0x%X (entry=%d spawn=%d) — live GO not in client cache",
		spawnPacked, StormwindGuildVaultEntry, StormwindGuildVaultGUIDLow)
	return spawnPacked
}
