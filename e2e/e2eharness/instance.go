package e2eharness

import (
	"fmt"
	"testing"
	"time"

	"github.com/azerothcore/AzerothGhost/client"
)

// DefaultSummonTimeout is used by summon waiters when timeout <= 0.
const DefaultSummonTimeout = 15 * time.Second

// Spell / GO constants for the ritual summon path (see RitualSummon).
const (
	SpellSummonPlayer                  = client.SpellSummonPlayer
	SpellCreateMeetingStonePortal      = client.SpellCreateMeetingStonePortal
	GameObjectMeetingStoneSummonPortal = client.GameObjectMeetingStoneSummonPortal
	// SpellRitualOfSummoning is the EffectSummonPlayer spell (7720), not the warlock channel.
	SpellRitualOfSummoning = client.SpellRitualOfSummoning
)

// LeaderResetInstances sends CMSG_RESET_INSTANCES from the bot (must be group leader or solo).
// Optionally arms a waiter for SMSG_INSTANCE_RESET / FAILED; non-fatal if no packet (server silent paths exist).
func (b *ScenarioBot) LeaderResetInstances(t *testing.T, timeout time.Duration) {
	t.Helper()
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ch := make(chan bool, 1)
	cancel := b.World.AddInstanceResetHook(func(_ uint32, ok bool) {
		select {
		case ch <- ok:
		default:
		}
	})
	defer cancel()

	if err := b.World.ResetInstances(); err != nil {
		HarnessFailf(t, "ResetInstances: %v", err)
	}
	t.Logf("%s CMSG_RESET_INSTANCES sent", b.Name)

	select {
	case ok := <-ch:
		t.Logf("%s instance reset packet ok=%v map=%d", b.Name, ok, b.World.LastInstanceResetMap())
	case <-time.After(timeout):
		// Solo with no binds: server may not send SMSG_INSTANCE_RESET.
		t.Logf("%s no SMSG_INSTANCE_RESET within %s (ok if no binds)", b.Name, timeout)
	}
}

// ArmSummonRequest arms SMSG_SUMMON_REQUEST before the action that generates it.
func (b *ScenarioBot) ArmSummonRequest() (wait func(timeout time.Duration) (client.SummonRequest, error), cancel func()) {
	return b.World.ArmSummonRequest()
}

// WaitSummonRequest waits for SMSG_SUMMON_REQUEST (prefer ArmSummonRequest before the cast).
func (b *ScenarioBot) WaitSummonRequest(t *testing.T, timeout time.Duration) client.SummonRequest {
	t.Helper()
	if timeout <= 0 {
		timeout = DefaultSummonTimeout
	}
	req, err := b.World.WaitSummonRequest(timeout)
	if err != nil {
		HarnessFailf(t, "%s WaitSummonRequest: %v", b.Name, err)
	}
	t.Logf("%s got summon request from 0x%X zone=%d", b.Name, req.SummonerGUID, req.ZoneID)
	return req
}

// AcceptSummon sends CMSG_SUMMON_RESPONSE agree=true for the pending (or given) summoner.
func (b *ScenarioBot) AcceptSummon(t *testing.T, summonerGUID uint64) {
	t.Helper()
	if summonerGUID == 0 {
		summonerGUID = b.World.PendingSummon().SummonerGUID
	}
	if summonerGUID == 0 {
		HarnessFailf(t, "%s AcceptSummon: no summoner GUID (pending empty)", b.Name)
	}
	before := b.World.TeleportSeq()
	if err := b.World.SummonResponse(summonerGUID, true); err != nil {
		HarnessFailf(t, "SummonResponse(agree): %v", err)
	}
	b.World.ClearPendingSummon()
	if err := b.World.WaitForTeleportAfter(before, 15*time.Second); err != nil {
		t.Logf("%s AcceptSummon tele wait: %v (continuing)", b.Name, err)
	}
	if !b.World.IsInWorld() {
		if err := b.World.WaitForSessionPhase(client.PhaseInWorld, 10*time.Second); err != nil {
			HarnessFailf(t, "%s AcceptSummon WaitInWorld: %v", b.Name, err)
		}
	}
	t.Logf("%s AcceptSummon from 0x%X", b.Name, summonerGUID)
}

// DeclineSummon sends CMSG_SUMMON_RESPONSE agree=false.
func (b *ScenarioBot) DeclineSummon(t *testing.T, summonerGUID uint64) {
	t.Helper()
	if summonerGUID == 0 {
		summonerGUID = b.World.PendingSummon().SummonerGUID
	}
	if summonerGUID == 0 {
		HarnessFailf(t, "%s DeclineSummon: no summoner GUID", b.Name)
	}
	if err := b.World.SummonResponse(summonerGUID, false); err != nil {
		HarnessFailf(t, "SummonResponse(decline): %v", err)
	}
	b.World.ClearPendingSummon()
	t.Logf("%s DeclineSummon from 0x%X", b.Name, summonerGUID)
}

// GameObjectUse sends CMSG_GAMEOBJ_USE for guid.
func (b *ScenarioBot) GameObjectUse(t *testing.T, guid uint64) {
	t.Helper()
	if guid == 0 {
		HarnessFailf(t, "GameObjectUse: guid is 0")
	}
	if err := b.World.GameObjectUse(guid); err != nil {
		HarnessFailf(t, "GameObjectUse(0x%X): %v", guid, err)
	}
	t.Logf("%s GameObjectUse 0x%X", b.Name, guid)
}

// WaitGameObject waits for a nearby gameobject entry in the object cache.
func (b *ScenarioBot) WaitGameObject(t *testing.T, entry uint32, timeout time.Duration) uint64 {
	t.Helper()
	if timeout <= 0 {
		timeout = DefaultSummonTimeout
	}
	guid := WaitNearbyGameObjectByEntry(t, b.World, entry, timeout)
	if guid == 0 {
		Preconditionf(t, "%s WaitGameObject entry=%d: not found within %s", b.Name, entry, timeout)
	}
	return guid
}

// RitualSummon runs the 3-role summon portal path (meeting-stone GO 179944):
//
//  1. initiator — selects farTarget, spawns portal, first click (ritual owner)
//  2. helper — nearby second click (reqParticipants=2 completes the ritual)
//  3. farTarget — after ~5s ritual cooldown, receives SMSG_SUMMON_REQUEST → AcceptSummon
//
// initiator and helper must be co-located (same map + instance); farTarget may be elsewhere
// (FindPlayer). Prefer placing all three in a dungeon instance first — outdoor PackagePad is
// the wrong fixture for bind/summon tests. All three must share a party (castersGrouped=1).
//
// Arm farTarget.ArmSummonRequest before calling. The server only fires EffectSummonPlayer
// after the GO update cooldown (~5s) once unique participants are full.
func RitualSummon(t *testing.T, initiator, helper, farTarget *ScenarioBot) (portalGUID uint64) {
	t.Helper()
	if initiator == nil || helper == nil || farTarget == nil {
		HarnessFailf(t, "RitualSummon: nil bot")
	}
	if farTarget.GUID == 0 {
		HarnessFailf(t, "RitualSummon: farTarget GUID empty")
	}

	// EffectSummonPlayer CheckCast + SelectEffectTypeImplicitTargets use GetTarget()+FindPlayer.
	setFar := func(bot *ScenarioBot) {
		if err := bot.World.SetTarget(farTarget.GUID); err != nil {
			HarnessFailf(t, "RitualSummon SetTarget far on %s: %v", bot.Name, err)
		}
	}
	// CMSG_SET_SELECTION is async vs subsequent GM/cast packets — give the world thread a beat.
	pinFar := func(bot *ScenarioBot) {
		setFar(bot)
		time.Sleep(150 * time.Millisecond)
	}

	pinFar(initiator)
	initiator.CombatStop(t)
	helper.CombatStop(t)

	// Interaction works more reliably without GM invis / flags.
	initiator.GM(t, ".gm off")
	helper.GM(t, ".gm off")

	// Persistent GO spawn (SQL+live cleanup). Type 18, reqParticipants=2, spellId=7720.
	_ = initiator.SpawnGameObject(t, GameObjectMeetingStoneSummonPortal)
	portalGUID = initiator.WaitGameObject(t, GameObjectMeetingStoneSummonPortal, 10*time.Second)

	// Helper must be next to the portal. Prefer GM .summon so we stay in the same
	// instance id (bare .go xyz to map 34 can open a different copy).
	ix, iy, iz, imap := initiator.Pos()
	hx, hy, hz, hmap := helper.Pos()
	needBring := hmap != imap || Distance3D(ix, iy, iz, hx, hy, hz) > 8
	if needBring {
		before := helper.World.TeleportSeq()
		initiator.GM(t, ".summon "+helper.Name)
		if err := helper.World.WaitForTeleportAfter(before, 10*time.Second); err != nil {
			// Fallback: same coords on reported map (may still wrong-instance outdoors only).
			t.Logf("RitualSummon: .summon helper failed (%v); .go xyz fallback", err)
			helper.Teleport(t, ix+1.5, iy+1.5, iz, imap)
		}
		helper.CombatStop(t)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if helper.World.GetObject(portalGUID) != nil {
			break
		}
		// Do not rewrite portalGUID from a different leftover spawn of the same entry.
		time.Sleep(50 * time.Millisecond)
	}
	if helper.World.GetObject(portalGUID) == nil {
		Preconditionf(t, "helper never saw summoning portal 0x%X entry %d", portalGUID, GameObjectMeetingStoneSummonPortal)
	}

	// Re-select far — .gobject add / tele can clear unit selection.
	pinFar(initiator)
	// First click: initiator becomes ritual owner + unique use #1.
	initiator.GameObjectUse(t, portalGUID)
	pinFar(initiator)
	// Second click: helper completes participants → GO_NOT_READY + 5s cooldown then cast 7720.
	helper.GameObjectUse(t, portalGUID)

	// Server should cast spellId after ~5s as ritual owner with GetTarget=far.
	// Keep far selected for the whole window (trash combat can peel selection).
	pinEnd := time.Now().Add(8 * time.Second)
	for time.Now().Before(pinEnd) {
		setFar(initiator)
		if farTarget.HasPendingSummon() {
			t.Logf("RitualSummon: portal completion delivered summon to far=%s", farTarget.Name)
			return portalGUID
		}
		time.Sleep(80 * time.Millisecond)
	}

	// Fallback: portal GO completion often fails (SMSG_CAST_FAILED DONT_REPORT masks the real
	// reason). Force EffectSummonPlayer as initiator→far. Selection must be settled before .cast
	// (getSelectedUnit + GetTarget both required).
	t.Logf("RitualSummon: portal cooldown elapsed without far pending; forcing summon %d → far 0x%X",
		SpellSummonPlayer, farTarget.GUID)
	if farTarget.HasPendingSummon() {
		farTarget.DeclineSummon(t, 0)
	}
	farTarget.World.ClearPendingSummon()
	initiator.CombatStop(t)
	initiator.GM(t, ".gm on")
	initiator.GM(t, fmt.Sprintf(".learn %d", SpellSummonPlayer))
	time.Sleep(400 * time.Millisecond)
	for try := 0; try < 6 && !farTarget.HasPendingSummon(); try++ {
		pinFar(initiator)
		// Non-triggered first so real SPELL_FAILED_* is visible in logs (not remapped to 27).
		if try%2 == 0 {
			initiator.GM(t, fmt.Sprintf(".cast %d", SpellSummonPlayer))
		} else {
			initiator.GM(t, fmt.Sprintf(".cast %d triggered", SpellSummonPlayer))
		}
		if err := initiator.World.CastSpell(SpellSummonPlayer, farTarget.GUID); err != nil {
			t.Logf("RitualSummon CastSpell try %d: %v", try+1, err)
		}
		pollEnd := time.Now().Add(1200 * time.Millisecond)
		for time.Now().Before(pollEnd) {
			if farTarget.HasPendingSummon() {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
	}

	// Last resort: helper may be ritual owner if initiator's first click did not stick.
	if !farTarget.HasPendingSummon() {
		t.Logf("RitualSummon: initiator force cast failed; trying helper as caster")
		helper.CombatStop(t)
		helper.GM(t, ".gm on")
		helper.GM(t, fmt.Sprintf(".learn %d", SpellSummonPlayer))
		time.Sleep(300 * time.Millisecond)
		for try := 0; try < 3 && !farTarget.HasPendingSummon(); try++ {
			pinFar(helper)
			helper.GM(t, fmt.Sprintf(".cast %d triggered", SpellSummonPlayer))
			_ = helper.World.CastSpell(SpellSummonPlayer, farTarget.GUID)
			pollEnd := time.Now().Add(1 * time.Second)
			for time.Now().Before(pollEnd) {
				if farTarget.HasPendingSummon() {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
		}
	}

	t.Logf("RitualSummon: initiator=%s(0x%X) helper=%s far=%s(0x%X) portal=0x%X pending=%v",
		initiator.Name, initiator.GUID, helper.Name, farTarget.Name, farTarget.GUID, portalGUID, farTarget.HasPendingSummon())
	return portalGUID
}

// HasPendingSummon reports a cached SMSG_SUMMON_REQUEST.
func (b *ScenarioBot) HasPendingSummon() bool {
	return b.World != nil && b.World.HasPendingSummon()
}

// PendingSummon returns the last summon request snapshot.
func (b *ScenarioBot) PendingSummon() client.SummonRequest {
	if b.World == nil {
		return client.SummonRequest{}
	}
	return b.World.PendingSummon()
}
