package client

import (
	"encoding/binary"
	"fmt"
)

// Guild bank opcodes (3.3.5a).
const (
	CmsgGuildBankerActivate    uint16 = 0x03E6
	CmsgGuildBankQueryTab      uint16 = 0x03E7
	SmsgGuildBankList          uint16 = 0x03E8
	CmsgGuildBankSwapItems     uint16 = 0x03E9
	CmsgGuildBankBuyTab        uint16 = 0x03EA
	CmsgGuildBankUpdateTab     uint16 = 0x03EB
	CmsgGuildBankDepositMoney  uint16 = 0x03EC
	CmsgGuildBankWithdrawMoney uint16 = 0x03ED
	MsgGuildBankLogQuery       uint16 = 0x03EE
	MsgGuildPermissions        uint16 = 0x03FD
	MsgGuildBankMoneyWithdrawn uint16 = 0x03FE
	MsgQueryGuildBankText      uint16 = 0x040A
	CmsgSetGuildBankText       uint16 = 0x040B
)

// GuildBankSlotAuto asks the server to place into the first free bank slot.
const GuildBankSlotAuto uint8 = 0xFF

// GuildBankList is a parsed SMSG_GUILD_BANK_LIST (full or partial).
type GuildBankList struct {
	Money      uint64
	Tab        uint8
	Remaining  int32
	FullUpdate bool
	TabInfos   []GuildBankTabInfo // only when FullUpdate && Tab==0
	Items      []GuildBankListItem
}

// GuildBankTabInfo is name/icon of a purchased bank tab.
type GuildBankTabInfo struct {
	Name string
	Icon string
}

// GuildBankListItem is one slot entry in SMSG_GUILD_BANK_LIST.
type GuildBankListItem struct {
	Slot             uint8
	Entry            uint32
	Flags            int32
	RandomPropertyID int32
	Count            int32
	EnchantmentID    int32
	Charges          uint8
}

// SendGuildBankerActivate opens the guild bank UI for bankerGUID (GO type 34).
// fullUpdate=true requests tab metadata + full item list for tab 0.
func (w *WorldClient) SendGuildBankerActivate(bankerGUID uint64, fullUpdate bool) error {
	buf := make([]byte, 9)
	binary.LittleEndian.PutUint64(buf[0:8], bankerGUID)
	if fullUpdate {
		buf[8] = 1
	}
	return w.sendPacket(CmsgGuildBankerActivate, buf)
}

// SendGuildBankQueryTab requests SMSG_GUILD_BANK_LIST for a tab.
func (w *WorldClient) SendGuildBankQueryTab(bankerGUID uint64, tab uint8, fullUpdate bool) error {
	buf := make([]byte, 10)
	binary.LittleEndian.PutUint64(buf[0:8], bankerGUID)
	buf[8] = tab
	if fullUpdate {
		buf[9] = 1
	}
	return w.sendPacket(CmsgGuildBankQueryTab, buf)
}

// SendGuildBankBuyTab purchases the next bank tab (only guild master).
func (w *WorldClient) SendGuildBankBuyTab(bankerGUID uint64, tab uint8) error {
	buf := make([]byte, 9)
	binary.LittleEndian.PutUint64(buf[0:8], bankerGUID)
	buf[8] = tab
	return w.sendPacket(CmsgGuildBankBuyTab, buf)
}

// SendGuildBankDepositMoney deposits copper from the player into the guild bank.
func (w *WorldClient) SendGuildBankDepositMoney(bankerGUID uint64, amount uint32) error {
	buf := make([]byte, 12)
	binary.LittleEndian.PutUint64(buf[0:8], bankerGUID)
	binary.LittleEndian.PutUint32(buf[8:12], amount)
	return w.sendPacket(CmsgGuildBankDepositMoney, buf)
}

// SendGuildBankWithdrawMoney withdraws copper from the guild bank to the player.
func (w *WorldClient) SendGuildBankWithdrawMoney(bankerGUID uint64, amount uint32) error {
	buf := make([]byte, 12)
	binary.LittleEndian.PutUint64(buf[0:8], bankerGUID)
	binary.LittleEndian.PutUint32(buf[8:12], amount)
	return w.sendPacket(CmsgGuildBankWithdrawMoney, buf)
}

// SendGuildBankDepositItem deposits the inventory item at (bag, bagSlot) into
// bank tab/slot. Use GuildBankSlotAuto (0xFF) for first free bank slot.
// stackCount 0 means the whole stack.
func (w *WorldClient) SendGuildBankDepositItem(bankerGUID uint64, tab, slot, bag, bagSlot uint8, itemEntry, stackCount uint32) error {
	// bankOnly=0 path: banker(8) bankOnly(1) tab(1) slot(1) item(4) autoStore(1)
	// bag(1) bagSlot(1) toChar(1)=0 stack(4)
	buf := make([]byte, 8+1+1+1+4+1+1+1+1+4)
	off := 0
	binary.LittleEndian.PutUint64(buf[off:], bankerGUID)
	off += 8
	buf[off] = 0 // bankOnly
	off++
	buf[off] = tab
	off++
	buf[off] = slot
	off++
	binary.LittleEndian.PutUint32(buf[off:], itemEntry)
	off += 4
	buf[off] = 0 // autoStore
	off++
	buf[off] = bag
	off++
	buf[off] = bagSlot
	off++
	buf[off] = 0 // toChar = deposit
	off++
	binary.LittleEndian.PutUint32(buf[off:], stackCount)
	return w.sendPacket(CmsgGuildBankSwapItems, buf)
}

// SendGuildBankWithdrawItem withdraws the bank item at tab/slot into inventory.
// stackCount 0 means the whole stack.
//
// AC GuildHandler allows NULL_BAG/NULL_SLOT (255/255) for auto inventory placement
// on withdraw. bag=0/slot=0 is equipment and is rejected (EQUIP_ERR).
func (w *WorldClient) SendGuildBankWithdrawItem(bankerGUID uint64, tab, slot uint8, itemEntry, stackCount uint32) error {
	// banker(8) bankOnly(0) tab slot item autoStore(0) bag bagSlot toChar(1) stack
	const nullBag, nullSlot uint8 = 255, 255
	buf := make([]byte, 8+1+1+1+4+1+1+1+1+4)
	off := 0
	binary.LittleEndian.PutUint64(buf[off:], bankerGUID)
	off += 8
	buf[off] = 0 // bankOnly
	off++
	buf[off] = tab
	off++
	buf[off] = slot
	off++
	binary.LittleEndian.PutUint32(buf[off:], itemEntry)
	off += 4
	buf[off] = 0 // autoStore
	off++
	buf[off] = nullBag
	off++
	buf[off] = nullSlot
	off++
	buf[off] = 1 // toChar = withdraw
	off++
	binary.LittleEndian.PutUint32(buf[off:], stackCount)
	return w.sendPacket(CmsgGuildBankSwapItems, buf)
}

// ParseGuildBankList decodes SMSG_GUILD_BANK_LIST.
func ParseGuildBankList(data []byte) (*GuildBankList, error) {
	if len(data) < 8+1+4+1+1 {
		return nil, fmt.Errorf("SMSG_GUILD_BANK_LIST too short: %d", len(data))
	}
	out := &GuildBankList{
		Money:      binary.LittleEndian.Uint64(data[0:8]),
		Tab:        data[8],
		Remaining:  int32(binary.LittleEndian.Uint32(data[9:13])),
		FullUpdate: data[13] != 0,
	}
	off := 14
	if out.FullUpdate && out.Tab == 0 {
		if off >= len(data) {
			return nil, fmt.Errorf("SMSG_GUILD_BANK_LIST missing tab count")
		}
		nTabs := int(data[off])
		off++
		for i := 0; i < nTabs; i++ {
			name, n, err := readCStringBytes(data[off:])
			if err != nil {
				return nil, fmt.Errorf("tab name: %w", err)
			}
			off += n
			icon, n, err := readCStringBytes(data[off:])
			if err != nil {
				return nil, fmt.Errorf("tab icon: %w", err)
			}
			off += n
			out.TabInfos = append(out.TabInfos, GuildBankTabInfo{Name: name, Icon: icon})
		}
	}
	if off >= len(data) {
		return nil, fmt.Errorf("SMSG_GUILD_BANK_LIST missing item count")
	}
	nItems := int(data[off])
	off++
	for i := 0; i < nItems; i++ {
		if off >= len(data) {
			return nil, fmt.Errorf("SMSG_GUILD_BANK_LIST item[%d] truncated", i)
		}
		slot := data[off]
		off++
		if off+4 > len(data) {
			return nil, fmt.Errorf("SMSG_GUILD_BANK_LIST item[%d] entry truncated", i)
		}
		entry := binary.LittleEndian.Uint32(data[off : off+4])
		off += 4
		item := GuildBankListItem{Slot: slot, Entry: entry}
		if entry == 0 {
			out.Items = append(out.Items, item)
			continue
		}
		// flags, randomPropertyID, [suffix], count, enchant, charges, gems
		need := 4 + 4 + 4 + 4 + 1 + 1
		if off+need > len(data) {
			return nil, fmt.Errorf("SMSG_GUILD_BANK_LIST item[%d] body truncated", i)
		}
		item.Flags = int32(binary.LittleEndian.Uint32(data[off : off+4]))
		off += 4
		item.RandomPropertyID = int32(binary.LittleEndian.Uint32(data[off : off+4]))
		off += 4
		if item.RandomPropertyID != 0 {
			if off+4 > len(data) {
				return nil, fmt.Errorf("SMSG_GUILD_BANK_LIST item[%d] suffix truncated", i)
			}
			off += 4 // suffix factor ignored
		}
		item.Count = int32(binary.LittleEndian.Uint32(data[off : off+4]))
		off += 4
		item.EnchantmentID = int32(binary.LittleEndian.Uint32(data[off : off+4]))
		off += 4
		item.Charges = data[off]
		off++
		nGems := int(data[off])
		off++
		for g := 0; g < nGems; g++ {
			if off+5 > len(data) {
				return nil, fmt.Errorf("SMSG_GUILD_BANK_LIST item[%d] gem truncated", i)
			}
			off += 1 + 4 // idx + enchant id
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}

// ParseGuildBankMoneyWithdrawn decodes MSG_GUILD_BANK_MONEY_WITHDRAWN (remaining copper today, -1 unlimited).
func ParseGuildBankMoneyWithdrawn(data []byte) (int32, error) {
	if len(data) < 4 {
		return 0, fmt.Errorf("MSG_GUILD_BANK_MONEY_WITHDRAWN too short: %d", len(data))
	}
	return int32(binary.LittleEndian.Uint32(data[0:4])), nil
}

func readCStringBytes(data []byte) (string, int, error) {
	for i, b := range data {
		if b == 0 {
			return string(data[:i]), i + 1, nil
		}
	}
	return "", 0, fmt.Errorf("unterminated cstring")
}

// GameObjectGUID builds a 3.3.5a HighGuid::GameObject ObjectGuid (entry + counter).
func GameObjectGUID(entry, counter uint32) uint64 {
	if counter == 0 {
		return 0
	}
	return uint64(counter) | (uint64(entry) << 24) | (uint64(0xF110) << 48)
}

// CreatureGUID builds a 3.3.5a HighGuid::Unit ObjectGuid (entry + counter).
func CreatureGUID(entry, counter uint32) uint64 {
	if counter == 0 {
		return 0
	}
	return uint64(counter) | (uint64(entry) << 24) | (uint64(0xF130) << 48)
}
