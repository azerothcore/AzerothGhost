package client

import (
	"encoding/binary"
	"fmt"
)

// SMSG_ITEM_PUSH_RESULT (3.3.5a).
const SmsgItemPushResult uint16 = 0x0166

// ItemPushResult is a parsed SMSG_ITEM_PUSH_RESULT payload.
// Note: the packet does not carry the item ObjectGuid — only entry + bag/slot.
type ItemPushResult struct {
	PlayerGUID      uint64
	Received        uint32 // 0=looted, 1=from npc
	Created         uint32 // 0=received, 1=created
	ShowChatMessage uint32
	BagSlot         uint8
	// ItemSlot is the backpack/equip slot, or 0xFFFFFFFF when stacked onto existing.
	ItemSlot         uint32
	Entry            uint32
	SuffixFactor     uint32
	RandomPropertyID int32
	Count            uint32
	// InventoryCount is the player's total count of this entry after the push.
	InventoryCount uint32
}

// ParseItemPushResult decodes SMSG_ITEM_PUSH_RESULT.
// Layout (AC Player::SendNewItem): guid(8) + received(4) + created(4) + chat(4)
// + bag(1) + slot(4) + entry(4) + suffix(4) + randomProp(4) + count(4) + invCount(4).
func ParseItemPushResult(data []byte) (*ItemPushResult, error) {
	const need = 8 + 4 + 4 + 4 + 1 + 4 + 4 + 4 + 4 + 4 + 4
	if len(data) < need {
		return nil, fmt.Errorf("SMSG_ITEM_PUSH_RESULT too short: %d", len(data))
	}
	return &ItemPushResult{
		PlayerGUID:       binary.LittleEndian.Uint64(data[0:8]),
		Received:         binary.LittleEndian.Uint32(data[8:12]),
		Created:          binary.LittleEndian.Uint32(data[12:16]),
		ShowChatMessage:  binary.LittleEndian.Uint32(data[16:20]),
		BagSlot:          data[20],
		ItemSlot:         binary.LittleEndian.Uint32(data[21:25]),
		Entry:            binary.LittleEndian.Uint32(data[25:29]),
		SuffixFactor:     binary.LittleEndian.Uint32(data[29:33]),
		RandomPropertyID: int32(binary.LittleEndian.Uint32(data[33:37])),
		Count:            binary.LittleEndian.Uint32(data[37:41]),
		InventoryCount:   binary.LittleEndian.Uint32(data[41:45]),
	}, nil
}
