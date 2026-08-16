package client

import "testing"

func TestPlayerVisibleItemEntryField(t *testing.T) {
	// UNIT_END(0x94) + 0x0087 = 0x011B; stride 2 per slot (entry + enchantment).
	if got := PlayerVisibleItemEntryField(EquipmentSlotHead); got != 0x011B {
		t.Fatalf("head field=%#x want 0x11b", got)
	}
	if got := PlayerVisibleItemEntryField(EquipmentSlotMainHand); got != 0x0139 {
		t.Fatalf("mainhand field=%#x want 0x139", got)
	}
	if got := PlayerVisibleItemEntryField(EquipmentSlotTabard); got != 0x013F {
		t.Fatalf("tabard field=%#x want 0x13f", got)
	}
	if got := PlayerVisibleItemEntryField(EquipmentSlotEnd); got != 0 {
		t.Fatalf("end field=%#x want 0", got)
	}
}

func TestWorldObjectVisibleItem(t *testing.T) {
	o := &WorldObject{Values: map[uint16]uint32{
		PlayerVisibleItemEntryField(EquipmentSlotMainHand): 25,
		PlayerVisibleItemEntryField(EquipmentSlotTabard):   5976,
	}}
	if got := o.VisibleItemEntry(EquipmentSlotMainHand); got != 25 {
		t.Fatalf("mainhand entry=%d want 25", got)
	}
	if got := o.VisibleItemEntry(EquipmentSlotHead); got != 0 {
		t.Fatalf("empty head=%d", got)
	}
	if got := o.VisibleItemEntry(EquipmentSlotEnd); got != 0 {
		t.Fatalf("invalid slot=%d", got)
	}
	slot, ok := o.EquippedSlot(25)
	if !ok || slot != EquipmentSlotMainHand {
		t.Fatalf("EquippedSlot(25)=%d ok=%v", slot, ok)
	}
	if _, ok := o.EquippedSlot(1); ok {
		t.Fatal("unexpected match for missing entry")
	}
	if (*WorldObject)(nil).VisibleItemEntry(0) != 0 {
		t.Fatal("nil object should return 0")
	}
}
