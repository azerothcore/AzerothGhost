package client

import (
	"encoding/binary"
	"testing"
)

func TestParseItemPushResult(t *testing.T) {
	buf := make([]byte, 45)
	binary.LittleEndian.PutUint64(buf[0:8], 42)
	binary.LittleEndian.PutUint32(buf[8:12], 1)  // received
	binary.LittleEndian.PutUint32(buf[12:16], 0) // created
	binary.LittleEndian.PutUint32(buf[16:20], 1) // chat
	buf[20] = 255                               // bag
	binary.LittleEndian.PutUint32(buf[21:25], 23)
	binary.LittleEndian.PutUint32(buf[25:29], ItemGuildCharterEntry)
	binary.LittleEndian.PutUint32(buf[29:33], 0)
	binary.LittleEndian.PutUint32(buf[33:37], 0)
	binary.LittleEndian.PutUint32(buf[37:41], 1)
	binary.LittleEndian.PutUint32(buf[41:45], 1)

	got, err := ParseItemPushResult(buf)
	if err != nil {
		t.Fatal(err)
	}
	if got.PlayerGUID != 42 || got.Entry != ItemGuildCharterEntry || got.BagSlot != 255 || got.ItemSlot != 23 || got.Count != 1 {
		t.Fatalf("unexpected: %+v", got)
	}
}

// ItemGuildCharterEntry is Guild Charter (5863) — mirrored for tests without e2eharness import.
const ItemGuildCharterEntry = 5863
