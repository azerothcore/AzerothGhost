package client

import (
	"encoding/binary"
	"testing"
)

func TestParseGuildBankListFullTab0(t *testing.T) {
	// money=100, tab=0, remaining=-1, full=1, 1 tab ("Vault","INV_Misc"), 1 item slot0 entry 2589 count 5
	buf := make([]byte, 0, 64)
	putU64 := func(v uint64) {
		var b [8]byte
		binary.LittleEndian.PutUint64(b[:], v)
		buf = append(buf, b[:]...)
	}
	putU32 := func(v uint32) {
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], v)
		buf = append(buf, b[:]...)
	}
	putI32 := func(v int32) { putU32(uint32(v)) }

	putU64(100)
	buf = append(buf, 0) // tab
	putI32(-1)
	buf = append(buf, 1) // full
	buf = append(buf, 1) // n tabs
	buf = append(buf, append([]byte("Vault"), 0)...)
	buf = append(buf, append([]byte("INV_Misc"), 0)...)
	buf = append(buf, 1) // n items
	buf = append(buf, 0) // slot
	putU32(2589)
	putI32(0) // flags
	putI32(0) // random prop
	putI32(5) // count
	putI32(0) // enchant
	buf = append(buf, 0) // charges
	buf = append(buf, 0) // gems

	got, err := ParseGuildBankList(buf)
	if err != nil {
		t.Fatal(err)
	}
	if got.Money != 100 || !got.FullUpdate || len(got.TabInfos) != 1 || got.TabInfos[0].Name != "Vault" {
		t.Fatalf("unexpected header: %+v", got)
	}
	if len(got.Items) != 1 || got.Items[0].Entry != 2589 || got.Items[0].Count != 5 {
		t.Fatalf("unexpected items: %+v", got.Items)
	}
}

func TestParseGuildBankListEmptySlotPartial(t *testing.T) {
	buf := make([]byte, 0, 32)
	var b8 [8]byte
	binary.LittleEndian.PutUint64(b8[:], 0)
	buf = append(buf, b8[:]...)
	buf = append(buf, 0) // tab
	var b4 [4]byte
	binary.LittleEndian.PutUint32(b4[:], 0)
	buf = append(buf, b4[:]...)
	buf = append(buf, 0) // partial
	buf = append(buf, 1) // 1 item
	buf = append(buf, 3) // slot 3
	binary.LittleEndian.PutUint32(b4[:], 0)
	buf = append(buf, b4[:]...) // entry 0

	got, err := ParseGuildBankList(buf)
	if err != nil {
		t.Fatal(err)
	}
	if got.FullUpdate || len(got.Items) != 1 || got.Items[0].Slot != 3 || got.Items[0].Entry != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestGameObjectGUID(t *testing.T) {
	// Stormwind vault sample: entry 187329 counter 41911
	g := GameObjectGUID(187329, 41911)
	if g == 0 {
		t.Fatal("zero")
	}
	if (g>>48)&0xFFFF != 0xF110 {
		t.Fatalf("high=%x", g>>48)
	}
	if uint32((g>>24)&0xFFFFFF) != 187329 {
		t.Fatalf("entry")
	}
	if uint32(g&0xFFFFFF) != 41911 {
		t.Fatalf("counter")
	}
}
