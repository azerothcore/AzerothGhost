package client

import (
	"encoding/binary"
	"testing"
)

func TestHandleLearnedSpellUpdatesKnowsSpell(t *testing.T) {
	w := &WorldClient{knownSpells: make(map[uint32]*KnownSpell)}
	if w.KnowsSpell(688) {
		t.Fatal("expected unknown before learn packet")
	}
	buf := make([]byte, 6)
	binary.LittleEndian.PutUint32(buf[0:4], 688)
	// uint16 unk = 0
	w.handleLearnedSpell(buf)
	if !w.KnowsSpell(688) {
		t.Fatal("expected KnowsSpell(688) after SMSG_LEARNED_SPELL")
	}
}

func TestHandleRemovedSpellClearsKnowsSpell(t *testing.T) {
	w := &WorldClient{knownSpells: map[uint32]*KnownSpell{
		688: {SpellID: 688, Active: true},
	}}
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, 688)
	w.handleRemovedSpell(buf)
	if w.KnowsSpell(688) {
		t.Fatal("expected unknown after SMSG_REMOVED_SPELL")
	}
}

func TestHandleSupercededSpellSwapsActive(t *testing.T) {
	w := &WorldClient{knownSpells: map[uint32]*KnownSpell{
		100: {SpellID: 100, Active: true},
	}}
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint32(buf[0:4], 100) // old
	binary.LittleEndian.PutUint32(buf[4:8], 200) // new
	w.handleSupercededSpell(buf)
	if w.KnowsSpell(100) {
		t.Fatal("old rank should be inactive")
	}
	if !w.KnowsSpell(200) {
		t.Fatal("new rank should be active")
	}
}
