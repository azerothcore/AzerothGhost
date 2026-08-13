package client

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"time"
)

// Vehicle / control opcodes (3.3.5a / AzerothCore Opcodes.h).
const (
	CmsgSpellClick                      uint16 = 0x03F8
	CmsgRequestVehicleExit              uint16 = 0x0476
	CmsgDismissControlledVehicle        uint16 = 0x046D
	CmsgPlayerVehicleEnter              uint16 = 0x04A8
	SmsgClientControlUpdate             uint16 = 0x0159
	SmsgPlayerVehicleData               uint16 = 0x04A7
	SmsgOnCancelExpectedRideVehicleAura uint16 = 0x049D
)

// SpellClick sends CMSG_SPELLCLICK for guid (uint64, not packed).
// Primary entry path for creature vehicles (npc_spellclick_spells).
func (w *WorldClient) SpellClick(guid uint64) error {
	if guid == 0 {
		return fmt.Errorf("SpellClick: guid is 0")
	}
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, guid)
	return w.sendPacket(CmsgSpellClick, buf.Bytes())
}

// RequestVehicleExit sends empty CMSG_REQUEST_VEHICLE_EXIT (leave seat).
func (w *WorldClient) RequestVehicleExit() error {
	return w.sendPacket(CmsgRequestVehicleExit, nil)
}

// PlayerVehicleEnter sends CMSG_PLAYER_VEHICLE_ENTER (raid member vehicle kit).
func (w *WorldClient) PlayerVehicleEnter(playerGUID uint64) error {
	if playerGUID == 0 {
		return fmt.Errorf("PlayerVehicleEnter: guid is 0")
	}
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, playerGUID)
	return w.sendPacket(CmsgPlayerVehicleEnter, buf.Bytes())
}

// PlayerCharmGUID returns UNIT_FIELD_CHARM on the player object (controlled vehicle).
func (w *WorldClient) PlayerCharmGUID() uint64 {
	obj := w.GetObject(w.CharGUID())
	if obj == nil {
		return 0
	}
	return obj.GUIDField(UnitFieldCharm)
}

// ControlGUID returns the unit last granted by SMSG_CLIENT_CONTROL_UPDATE (0 if self/none).
func (w *WorldClient) ControlGUID() uint64 {
	w.vehicleMu.RLock()
	defer w.vehicleMu.RUnlock()
	return w.controlGUID
}

// VehicleGUID is the best-effort vehicle the player is on/controlling.
// Prefers UNIT_FIELD_CHARM, then non-self control GUID.
func (w *WorldClient) VehicleGUID() uint64 {
	if g := w.PlayerCharmGUID(); g != 0 {
		return g
	}
	if g := w.ControlGUID(); g != 0 && g != w.CharGUID() {
		return g
	}
	return 0
}

// IsOnVehicle reports charm or non-self control (driver/passenger control seat).
func (w *WorldClient) IsOnVehicle() bool {
	return w.VehicleGUID() != 0
}

// WaitOnVehicle waits until IsOnVehicle is true; returns the vehicle GUID.
func (w *WorldClient) WaitOnVehicle(timeout time.Duration) (uint64, error) {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if g := w.VehicleGUID(); g != 0 {
			return g, nil
		}
		time.Sleep(40 * time.Millisecond)
	}
	return 0, fmt.Errorf("WaitOnVehicle: not on vehicle within %s", timeout)
}

// WaitNotOnVehicle waits until IsOnVehicle is false.
func (w *WorldClient) WaitNotOnVehicle(timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !w.IsOnVehicle() {
			return nil
		}
		time.Sleep(40 * time.Millisecond)
	}
	return fmt.Errorf("WaitNotOnVehicle: still on vehicle 0x%X after %s", w.VehicleGUID(), timeout)
}

// handleClientControlUpdate parses SMSG_CLIENT_CONTROL_UPDATE: packed GUID + uint8 allowMove.
func (w *WorldClient) handleClientControlUpdate(data []byte) {
	r := bytes.NewReader(data)
	guid, err := readPackedGUID(r)
	if err != nil {
		return
	}
	var allow uint8
	_ = binary.Read(r, binary.LittleEndian, &allow)

	w.vehicleMu.Lock()
	if allow != 0 && guid != 0 && guid != w.charGUID {
		w.controlGUID = guid
	} else {
		// Control returned to self (or denied).
		if guid == 0 || guid == w.charGUID || allow == 0 {
			w.controlGUID = 0
		}
	}
	w.vehicleMu.Unlock()
	w.log("SMSG_CLIENT_CONTROL_UPDATE guid=0x%X allow=%d", guid, allow)
}
