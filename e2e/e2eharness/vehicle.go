package e2eharness

import (
	"fmt"
	"testing"
	"time"

	"github.com/walkline/AzerothGhost/client"
)

// CreatureStormwindSteed is a friendly outdoor vehicle (VehicleId=349, SPELLCLICK, no zone script).
// Prefer for pad-local EnterVehicle smoke; always despawn via Spawn cleanup.
// Note: Mechano-hog (29929) is UNINTERACTIBLE and will not accept SpellClick.
const CreatureStormwindSteed uint32 = 33217

// CreatureMechanoHog is VehicleId=318 but unit_flags UNINTERACTIBLE — not a good click fixture.
// Kept as name alias only for docs; prefer CreatureStormwindSteed.
const CreatureMechanoHog uint32 = 29929

// SpellRideVehicleHardcoded is VEHICLE_SPELL_RIDE_HARDCODED (46598) — control-vehicle aura.
const SpellRideVehicleHardcoded uint32 = 46598

// DefaultVehicleTimeout is used by Enter/Exit vehicle waiters when timeout <= 0.
const DefaultVehicleTimeout = 10 * time.Second

// SpellClick sends CMSG_SPELLCLICK for guid (creature vehicle board path).
func (b *ScenarioBot) SpellClick(t *testing.T, guid uint64) {
	t.Helper()
	if guid == 0 {
		HarnessFailf(t, "SpellClick: guid is 0")
	}
	if err := b.World.SpellClick(guid); err != nil {
		HarnessFailf(t, "SpellClick(0x%X): %v", guid, err)
	}
	t.Logf("%s SpellClick 0x%X", b.Name, guid)
}

// EnterVehicle boards vehicleGUID via SpellClick, then GM-cast ride fallback if needed.
// Returns the controlled vehicle GUID (may match vehicleGUID).
// Turns GM mode off before board (many vehicles ignore GM clickers).
func (b *ScenarioBot) EnterVehicle(t *testing.T, vehicleGUID uint64, timeout time.Duration) uint64 {
	t.Helper()
	if timeout <= 0 {
		timeout = DefaultVehicleTimeout
	}
	if vehicleGUID == 0 {
		HarnessFailf(t, "EnterVehicle: vehicleGUID is 0")
	}
	// GM invis / flags can block seat entry.
	b.GM(t, ".gm off")
	_ = b.World.SetTarget(vehicleGUID)
	b.SpellClick(t, vehicleGUID)
	guid, err := b.World.WaitOnVehicle(timeout / 2)
	if err != nil {
		// Fallback: cast ride-hardcoded on the selected vehicle (core EnterVehicle path).
		t.Logf("%s SpellClick did not board; trying .cast %d on target", b.Name, SpellRideVehicleHardcoded)
		_ = b.World.SetTarget(vehicleGUID)
		// Need brief GM for .cast reliability on restricted spells, then off again.
		b.GM(t, ".gm on")
		b.GM(t, fmt.Sprintf(".cast %d triggered", SpellRideVehicleHardcoded))
		b.GM(t, ".gm off")
		guid, err = b.World.WaitOnVehicle(timeout)
	}
	if err != nil {
		Preconditionf(t, "%s EnterVehicle 0x%X: %v (charm=0x%X control=0x%X)",
			b.Name, vehicleGUID, err, b.World.PlayerCharmGUID(), b.World.ControlGUID())
	}
	t.Logf("%s EnterVehicle on 0x%X (reported 0x%X)", b.Name, vehicleGUID, guid)
	return guid
}

// ExitVehicle sends CMSG_REQUEST_VEHICLE_EXIT and waits until not on vehicle.
func (b *ScenarioBot) ExitVehicle(t *testing.T, timeout time.Duration) {
	t.Helper()
	if timeout <= 0 {
		timeout = DefaultVehicleTimeout
	}
	if !b.IsOnVehicle() {
		t.Logf("%s ExitVehicle: already off vehicle", b.Name)
		return
	}
	if err := b.World.RequestVehicleExit(); err != nil {
		HarnessFailf(t, "RequestVehicleExit: %v", err)
	}
	if err := b.World.WaitNotOnVehicle(timeout); err != nil {
		// Clear stale control if charm already gone.
		if b.World.PlayerCharmGUID() == 0 {
			t.Logf("%s ExitVehicle: charm clear, control residual: %v", b.Name, err)
			return
		}
		Assertf(t, "%s ExitVehicle: %v", b.Name, err)
	}
	t.Logf("%s ExitVehicle OK", b.Name)
}

// IsOnVehicle reports whether the bot controls/charms a vehicle.
func (b *ScenarioBot) IsOnVehicle() bool {
	return b.World != nil && b.World.IsOnVehicle()
}

// VehicleGUID returns the current vehicle GUID or 0.
func (b *ScenarioBot) VehicleGUID() uint64 {
	if b.World == nil {
		return 0
	}
	return b.World.VehicleGUID()
}

// WaitOnVehicle waits until IsOnVehicle; returns vehicle GUID.
func (b *ScenarioBot) WaitOnVehicle(t *testing.T, timeout time.Duration) uint64 {
	t.Helper()
	if timeout <= 0 {
		timeout = DefaultVehicleTimeout
	}
	guid, err := b.World.WaitOnVehicle(timeout)
	if err != nil {
		HarnessFailf(t, "%s WaitOnVehicle: %v", b.Name, err)
	}
	return guid
}

// WaitNotOnVehicle waits until the bot is off the vehicle.
func (b *ScenarioBot) WaitNotOnVehicle(t *testing.T, timeout time.Duration) {
	t.Helper()
	if timeout <= 0 {
		timeout = DefaultVehicleTimeout
	}
	if err := b.World.WaitNotOnVehicle(timeout); err != nil {
		HarnessFailf(t, "%s WaitNotOnVehicle: %v", b.Name, err)
	}
}

// EnterPlayerVehicle boards another player's vehicle kit (raid/party path).
func (b *ScenarioBot) EnterPlayerVehicle(t *testing.T, player *ScenarioBot, timeout time.Duration) {
	t.Helper()
	if player == nil || player.GUID == 0 {
		HarnessFailf(t, "EnterPlayerVehicle: player GUID empty")
	}
	if timeout <= 0 {
		timeout = DefaultVehicleTimeout
	}
	if err := b.World.PlayerVehicleEnter(player.GUID); err != nil {
		HarnessFailf(t, "PlayerVehicleEnter: %v", err)
	}
	if _, err := b.World.WaitOnVehicle(timeout); err != nil {
		Preconditionf(t, "%s EnterPlayerVehicle: %v", b.Name, err)
	}
}

// Compile-time surface for docs / examples.
var _ = client.CmsgSpellClick
