package client

import (
	"bytes"
	"strings"
	"testing"
)

func TestBuildCastSpellGUIDPayloadUnit(t *testing.T) {
	got := buildCastSpellGUIDPayload(133, spellTargetFlagUnit, 0xAABBCCDDEEFF0011)
	want := []byte{
		0x00,
		0x85, 0x00, 0x00, 0x00,
		0x00,
		0x02, 0x00, 0x00, 0x00,
		0xFD, 0x11, 0xFF, 0xEE, 0xDD, 0xCC, 0xBB, 0xAA,
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("unit cast payload = % X, want % X", got, want)
	}
}

func TestBuildCastSpellGUIDPayloadGameObject(t *testing.T) {
	got := buildCastSpellGUIDPayload(3365, spellTargetFlagGameObject, 0xF110000000123456)
	want := []byte{
		0x00,
		0x25, 0x0D, 0x00, 0x00,
		0x00,
		0x00, 0x08, 0x00, 0x00,
		0xC7, 0x56, 0x34, 0x12, 0x10, 0xF1,
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("gameobject cast payload = % X, want % X", got, want)
	}
}

func TestBuildCastSpellGUIDPayloadSelf(t *testing.T) {
	got := buildCastSpellGUIDPayload(687, spellTargetFlagSelf, 0)
	want := []byte{
		0x00,
		0xAF, 0x02, 0x00, 0x00,
		0x00,
		0x00, 0x00, 0x00, 0x00,
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("self cast payload = % X, want % X", got, want)
	}
}

func TestCastSpellOnGameObjectRejectsZeroGUID(t *testing.T) {
	w := NewWorldClient("test", nil, nil)
	err := w.CastSpellOnGameObject(3365, 0)
	if err == nil || !strings.Contains(err.Error(), "target GUID is 0") {
		t.Fatalf("CastSpellOnGameObject error = %v, want zero GUID error", err)
	}
}
