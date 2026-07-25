package client

import "testing"

func TestOpcodeNameKnown(t *testing.T) {
	if OpcodeName(SmsgAttackSwingBadFacing) != "SMSG_ATTACKSWING_BADFACING" {
		t.Fatalf("got %s", OpcodeName(SmsgAttackSwingBadFacing))
	}
	if OpcodeName(CmsgCastSpell) != "CMSG_CAST_SPELL" {
		t.Fatalf("got %s", OpcodeName(CmsgCastSpell))
	}
}

func TestOpcodeNameUnknownHex(t *testing.T) {
	n := OpcodeName(0xABCD)
	if n != "0xABCD" {
		t.Fatalf("got %s", n)
	}
}

func TestIsHighValueTraceOpcode(t *testing.T) {
	if !IsHighValueTraceOpcode(SmsgSpellGo) {
		t.Fatal("SPELL_GO should be high value")
	}
	if !IsHighValueTraceOpcode(SmsgAttackSwingNotInRange) {
		t.Fatal("swing error should be high value")
	}
	// Update object flood should not be traced by default
	if IsHighValueTraceOpcode(SmsgUpdateObject) {
		t.Fatal("UPDATE_OBJECT should not be high-value trace")
	}
	if IsHighValueTraceOpcode(SmsgMonsterMove) {
		t.Fatal("MONSTER_MOVE should not be high-value trace")
	}
}
