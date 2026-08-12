package client

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestParseGroupList_EmptyDestroyed(t *testing.T) {
	// AC Disband empty list form: type/sub/flags/roles + groupGUID + counter + 0 + leader0
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, uint8(0x10))
	_ = binary.Write(buf, binary.LittleEndian, uint8(0))
	_ = binary.Write(buf, binary.LittleEndian, uint8(0))
	_ = binary.Write(buf, binary.LittleEndian, uint8(0))
	_ = binary.Write(buf, binary.LittleEndian, uint64(0xABCD))
	_ = binary.Write(buf, binary.LittleEndian, uint32(7))
	_ = binary.Write(buf, binary.LittleEndian, uint32(0))
	_ = binary.Write(buf, binary.LittleEndian, uint64(0))

	st, err := ParseGroupList(buf.Bytes())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if st.InGroup {
		t.Fatalf("want not in group")
	}
	if st.MemberCount != 0 {
		t.Fatalf("member count=%d", st.MemberCount)
	}
}

func TestParseGroupList_TwoPlayerParty(t *testing.T) {
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, uint8(0)) // group type
	_ = binary.Write(buf, binary.LittleEndian, uint8(0))
	_ = binary.Write(buf, binary.LittleEndian, uint8(0))
	_ = binary.Write(buf, binary.LittleEndian, uint8(0))
	groupGUID := uint64(0x1111)
	leader := uint64(0x2222)
	mate := uint64(0x3333)
	_ = binary.Write(buf, binary.LittleEndian, groupGUID)
	_ = binary.Write(buf, binary.LittleEndian, uint32(1)) // counter
	_ = binary.Write(buf, binary.LittleEndian, uint32(1)) // other members
	buf.Write(append([]byte("MateName"), 0))
	_ = binary.Write(buf, binary.LittleEndian, mate)
	_ = binary.Write(buf, binary.LittleEndian, uint8(1)) // online
	_ = binary.Write(buf, binary.LittleEndian, uint8(0))
	_ = binary.Write(buf, binary.LittleEndian, uint8(0))
	_ = binary.Write(buf, binary.LittleEndian, uint8(0))
	_ = binary.Write(buf, binary.LittleEndian, leader)
	_ = binary.Write(buf, binary.LittleEndian, uint8(LootMethodGroupLoot))
	_ = binary.Write(buf, binary.LittleEndian, uint64(0))
	_ = binary.Write(buf, binary.LittleEndian, uint8(2))
	_ = binary.Write(buf, binary.LittleEndian, uint8(0)) // dungeon diff
	_ = binary.Write(buf, binary.LittleEndian, uint8(0)) // raid diff
	_ = binary.Write(buf, binary.LittleEndian, uint8(0))

	st, err := ParseGroupList(buf.Bytes())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !st.InGroup || st.MemberCount != 2 {
		t.Fatalf("in=%v size=%d", st.InGroup, st.MemberCount)
	}
	if st.LeaderGUID != leader {
		t.Fatalf("leader=0x%X", st.LeaderGUID)
	}
	if len(st.Members) != 1 || st.Members[0].Name != "MateName" || st.Members[0].GUID != mate {
		t.Fatalf("members=%+v", st.Members)
	}
	if st.LootMethod != LootMethodGroupLoot {
		t.Fatalf("loot=%d", st.LootMethod)
	}
}

func TestMakePetActionButton_Abandon(t *testing.T) {
	// ACT_COMMAND=0x07, COMMAND_ABANDON=3 → 0x07000003
	got := MakePetActionButton(PetCommandAbandon, PetActCommand)
	if got != 0x07000003 {
		t.Fatalf("got 0x%08X", got)
	}
}

func TestAuraStacks_FromSlotUpdate(t *testing.T) {
	obj := &WorldObject{Values: map[uint16]uint32{}}
	obj.setAuraForSlot(1, 12345, 3)
	if !obj.HasAura(12345) {
		t.Fatal("missing aura")
	}
	if obj.AuraStacks(12345) != 3 {
		t.Fatalf("stacks=%d", obj.AuraStacks(12345))
	}
	obj.setAuraForSlot(1, 0, 0)
	if obj.HasAura(12345) || obj.AuraStacks(12345) != 0 {
		t.Fatalf("aura should be gone stacks=%d", obj.AuraStacks(12345))
	}
}

func TestGUIDField(t *testing.T) {
	obj := &WorldObject{Values: map[uint16]uint32{
		UnitFieldSummon:     0x89ABCDEF,
		UnitFieldSummon + 1: 0x01234567,
	}}
	got := obj.GUIDField(UnitFieldSummon)
	want := uint64(0x0123456789ABCDEF)
	if got != want {
		t.Fatalf("got 0x%X want 0x%X", got, want)
	}
}
