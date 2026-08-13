package client

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// Group loot roll opcodes (3.3.5a).
const (
	SmsgLootRollWon    uint16 = 0x029F
	CmsgLootRoll       uint16 = 0x02A0
	SmsgLootStartRoll  uint16 = 0x02A1
	SmsgLootRoll       uint16 = 0x02A2
	CmsgLootMasterGive uint16 = 0x02A3
	SmsgLootAllPassed  uint16 = 0x029E
)

// Roll types for CMSG_LOOT_ROLL (LootMgr.h).
const (
	RollPass       uint8 = 0
	RollNeed       uint8 = 1
	RollGreed      uint8 = 2
	RollDisenchant uint8 = 3
)

// LootStartRoll is SMSG_LOOT_START_ROLL.
type LootStartRoll struct {
	ItemGUID     uint64
	MapID        uint32
	ItemSlot     uint32
	ItemID       uint32
	RandomSuffix uint32
	RandomPropID uint32
	ItemCount    uint32
	CountdownMS  uint32
	RollVoteMask uint8
}

// LootRollEvent is SMSG_LOOT_ROLL (someone voted).
type LootRollEvent struct {
	ItemGUID     uint64
	ItemSlot     uint32
	TargetGUID   uint64
	ItemID       uint32
	RandomSuffix uint32
	RandomPropID uint32
	RollNumber   uint8
	RollType     uint8
	AutoPass     uint8
}

// LootRollWon is SMSG_LOOT_ROLL_WON.
type LootRollWon struct {
	ItemGUID     uint64
	ItemSlot     uint32
	ItemID       uint32
	RandomSuffix uint32
	RandomPropID uint32
	WinnerGUID   uint64
	RollNumber   uint8
	RollType     uint8
}

// LootAllPassed is SMSG_LOOT_ALL_PASSED.
type LootAllPassed struct {
	ItemGUID     uint64
	ItemSlot     uint32
	ItemID       uint32
	RandomPropID uint32
	RandomSuffix uint32
}

// ParseLootStartRoll decodes SMSG_LOOT_START_ROLL.
func ParseLootStartRoll(data []byte) (LootStartRoll, error) {
	var r LootStartRoll
	// 8+4+4+4+4+4+4+4+1 = 37
	if len(data) < 37 {
		return r, fmt.Errorf("SMSG_LOOT_START_ROLL too short: %d", len(data))
	}
	rd := bytes.NewReader(data)
	_ = binary.Read(rd, binary.LittleEndian, &r.ItemGUID)
	_ = binary.Read(rd, binary.LittleEndian, &r.MapID)
	_ = binary.Read(rd, binary.LittleEndian, &r.ItemSlot)
	_ = binary.Read(rd, binary.LittleEndian, &r.ItemID)
	_ = binary.Read(rd, binary.LittleEndian, &r.RandomSuffix)
	_ = binary.Read(rd, binary.LittleEndian, &r.RandomPropID)
	_ = binary.Read(rd, binary.LittleEndian, &r.ItemCount)
	_ = binary.Read(rd, binary.LittleEndian, &r.CountdownMS)
	_ = binary.Read(rd, binary.LittleEndian, &r.RollVoteMask)
	return r, nil
}

// ParseLootRoll decodes SMSG_LOOT_ROLL.
func ParseLootRoll(data []byte) (LootRollEvent, error) {
	var r LootRollEvent
	// 8+4+8+4+4+4+1+1+1 = 35
	if len(data) < 35 {
		return r, fmt.Errorf("SMSG_LOOT_ROLL too short: %d", len(data))
	}
	rd := bytes.NewReader(data)
	_ = binary.Read(rd, binary.LittleEndian, &r.ItemGUID)
	_ = binary.Read(rd, binary.LittleEndian, &r.ItemSlot)
	_ = binary.Read(rd, binary.LittleEndian, &r.TargetGUID)
	_ = binary.Read(rd, binary.LittleEndian, &r.ItemID)
	_ = binary.Read(rd, binary.LittleEndian, &r.RandomSuffix)
	_ = binary.Read(rd, binary.LittleEndian, &r.RandomPropID)
	_ = binary.Read(rd, binary.LittleEndian, &r.RollNumber)
	_ = binary.Read(rd, binary.LittleEndian, &r.RollType)
	_ = binary.Read(rd, binary.LittleEndian, &r.AutoPass)
	return r, nil
}

// ParseLootRollWon decodes SMSG_LOOT_ROLL_WON.
func ParseLootRollWon(data []byte) (LootRollWon, error) {
	var r LootRollWon
	// 8+4+4+4+4+8+1+1 = 34
	if len(data) < 34 {
		return r, fmt.Errorf("SMSG_LOOT_ROLL_WON too short: %d", len(data))
	}
	rd := bytes.NewReader(data)
	_ = binary.Read(rd, binary.LittleEndian, &r.ItemGUID)
	_ = binary.Read(rd, binary.LittleEndian, &r.ItemSlot)
	_ = binary.Read(rd, binary.LittleEndian, &r.ItemID)
	_ = binary.Read(rd, binary.LittleEndian, &r.RandomSuffix)
	_ = binary.Read(rd, binary.LittleEndian, &r.RandomPropID)
	_ = binary.Read(rd, binary.LittleEndian, &r.WinnerGUID)
	_ = binary.Read(rd, binary.LittleEndian, &r.RollNumber)
	_ = binary.Read(rd, binary.LittleEndian, &r.RollType)
	return r, nil
}

// ParseLootAllPassed decodes SMSG_LOOT_ALL_PASSED.
func ParseLootAllPassed(data []byte) (LootAllPassed, error) {
	var r LootAllPassed
	if len(data) < 24 {
		return r, fmt.Errorf("SMSG_LOOT_ALL_PASSED too short: %d", len(data))
	}
	rd := bytes.NewReader(data)
	_ = binary.Read(rd, binary.LittleEndian, &r.ItemGUID)
	_ = binary.Read(rd, binary.LittleEndian, &r.ItemSlot)
	_ = binary.Read(rd, binary.LittleEndian, &r.ItemID)
	_ = binary.Read(rd, binary.LittleEndian, &r.RandomPropID)
	_ = binary.Read(rd, binary.LittleEndian, &r.RandomSuffix)
	return r, nil
}

// LootRoll sends CMSG_LOOT_ROLL (itemGUID, slot, rollType).
func (w *WorldClient) LootRoll(itemGUID uint64, itemSlot uint32, rollType uint8) error {
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, itemGUID)
	_ = binary.Write(buf, binary.LittleEndian, itemSlot)
	_ = binary.Write(buf, binary.LittleEndian, rollType)
	return w.sendPacket(CmsgLootRoll, buf.Bytes())
}

// LootMasterGive sends CMSG_LOOT_MASTER_GIVE.
func (w *WorldClient) LootMasterGive(lootGUID uint64, slot uint8, targetGUID uint64) error {
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, lootGUID)
	_ = binary.Write(buf, binary.LittleEndian, slot)
	_ = binary.Write(buf, binary.LittleEndian, targetGUID)
	return w.sendPacket(CmsgLootMasterGive, buf.Bytes())
}

func (w *WorldClient) handleLootStartRoll(data []byte) {
	r, err := ParseLootStartRoll(data)
	if err != nil {
		w.log("SMSG_LOOT_START_ROLL: %v", err)
		return
	}
	w.lootRollMu.Lock()
	w.activeRolls = append(w.activeRolls, r)
	// Keep a bounded history.
	if len(w.activeRolls) > 32 {
		w.activeRolls = w.activeRolls[len(w.activeRolls)-32:]
	}
	w.lootRollMu.Unlock()
	w.invokeLootStartRollHooks(r)
}

func (w *WorldClient) handleLootRoll(data []byte) {
	r, err := ParseLootRoll(data)
	if err != nil {
		w.log("SMSG_LOOT_ROLL: %v", err)
		return
	}
	w.invokeLootRollHooks(r)
}

func (w *WorldClient) handleLootRollWon(data []byte) {
	r, err := ParseLootRollWon(data)
	if err != nil {
		w.log("SMSG_LOOT_ROLL_WON: %v", err)
		return
	}
	w.lootRollMu.Lock()
	// Drop matching active roll.
	out := w.activeRolls[:0]
	for _, a := range w.activeRolls {
		if a.ItemGUID != r.ItemGUID || a.ItemSlot != r.ItemSlot {
			out = append(out, a)
		}
	}
	w.activeRolls = out
	w.lootRollMu.Unlock()
	w.invokeLootRollWonHooks(r)
}

func (w *WorldClient) handleLootAllPassed(data []byte) {
	r, err := ParseLootAllPassed(data)
	if err != nil {
		w.log("SMSG_LOOT_ALL_PASSED: %v", err)
		return
	}
	w.lootRollMu.Lock()
	out := w.activeRolls[:0]
	for _, a := range w.activeRolls {
		if a.ItemGUID != r.ItemGUID || a.ItemSlot != r.ItemSlot {
			out = append(out, a)
		}
	}
	w.activeRolls = out
	w.lootRollMu.Unlock()
	w.invokeLootAllPassedHooks(r)
}

// ActiveLootRolls returns a copy of pending start-roll snapshots.
func (w *WorldClient) ActiveLootRolls() []LootStartRoll {
	w.lootRollMu.RLock()
	defer w.lootRollMu.RUnlock()
	out := make([]LootStartRoll, len(w.activeRolls))
	copy(out, w.activeRolls)
	return out
}

// ClearActiveLootRolls drops cached start-roll state (e.g. between tests).
func (w *WorldClient) ClearActiveLootRolls() {
	w.lootRollMu.Lock()
	w.activeRolls = nil
	w.lootRollMu.Unlock()
}
