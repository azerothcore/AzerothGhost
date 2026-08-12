package client

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestParseTradeStatus_BeginTrade(t *testing.T) {
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, TradeStatusBeginTrade)
	_ = binary.Write(buf, binary.LittleEndian, uint64(0xAABBCCDDEEFF0011))
	info, err := ParseTradeStatus(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if info.Status != TradeStatusBeginTrade {
		t.Fatalf("status=%d", info.Status)
	}
	if info.TraderGUID != 0xAABBCCDDEEFF0011 {
		t.Fatalf("trader=%x", info.TraderGUID)
	}
}

func TestParseTradeStatus_Complete(t *testing.T) {
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, TradeStatusTradeComplete)
	info, err := ParseTradeStatus(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if info.Status != TradeStatusTradeComplete {
		t.Fatalf("status=%d", info.Status)
	}
	if TradeStatusName(info.Status) != "TRADE_COMPLETE" {
		t.Fatalf("name=%s", TradeStatusName(info.Status))
	}
}

func TestParseLootStartRoll(t *testing.T) {
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, uint64(99))
	_ = binary.Write(buf, binary.LittleEndian, uint32(0))    // map
	_ = binary.Write(buf, binary.LittleEndian, uint32(2))    // slot
	_ = binary.Write(buf, binary.LittleEndian, uint32(2589)) // linen
	_ = binary.Write(buf, binary.LittleEndian, uint32(0))
	_ = binary.Write(buf, binary.LittleEndian, uint32(0))
	_ = binary.Write(buf, binary.LittleEndian, uint32(1))
	_ = binary.Write(buf, binary.LittleEndian, uint32(60000))
	_ = binary.Write(buf, binary.LittleEndian, uint8(7))
	r, err := ParseLootStartRoll(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if r.ItemID != 2589 || r.ItemSlot != 2 || r.CountdownMS != 60000 {
		t.Fatalf("%+v", r)
	}
}

func TestParseLootRollWon(t *testing.T) {
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, uint64(1))
	_ = binary.Write(buf, binary.LittleEndian, uint32(0))
	_ = binary.Write(buf, binary.LittleEndian, uint32(19019))
	_ = binary.Write(buf, binary.LittleEndian, uint32(0))
	_ = binary.Write(buf, binary.LittleEndian, uint32(0))
	_ = binary.Write(buf, binary.LittleEndian, uint64(42))
	_ = binary.Write(buf, binary.LittleEndian, uint8(77))
	_ = binary.Write(buf, binary.LittleEndian, RollNeed)
	r, err := ParseLootRollWon(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if r.WinnerGUID != 42 || r.ItemID != 19019 || r.RollType != RollNeed {
		t.Fatalf("%+v", r)
	}
}
