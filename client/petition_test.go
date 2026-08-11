package client

import (
	"encoding/binary"
	"testing"
)

func TestParsePetitionShowSignatures(t *testing.T) {
	buf := make([]byte, 21+24)
	binary.LittleEndian.PutUint64(buf[0:8], 0x4000000000000001)
	binary.LittleEndian.PutUint64(buf[8:16], 42)
	binary.LittleEndian.PutUint32(buf[16:20], 7)
	buf[20] = 2
	binary.LittleEndian.PutUint64(buf[21:29], 100)
	binary.LittleEndian.PutUint32(buf[29:33], 0)
	binary.LittleEndian.PutUint64(buf[33:41], 101)
	binary.LittleEndian.PutUint32(buf[41:45], 0)

	got, err := ParsePetitionShowSignatures(buf)
	if err != nil {
		t.Fatal(err)
	}
	if got.PetitionID != 7 || len(got.SignatoryGUIDs) != 2 || got.SignatoryGUIDs[1] != 101 {
		t.Fatalf("unexpected parse: %+v", got)
	}
}

func TestParsePetitionSignResults(t *testing.T) {
	buf := make([]byte, 20)
	binary.LittleEndian.PutUint64(buf[0:8], 1)
	binary.LittleEndian.PutUint64(buf[8:16], 2)
	binary.LittleEndian.PutUint32(buf[16:20], PetitionSignOK)
	got, err := ParsePetitionSignResults(buf)
	if err != nil {
		t.Fatal(err)
	}
	if got.Result != PetitionSignOK || got.PlayerGUID != 2 {
		t.Fatalf("unexpected: %+v", got)
	}
}
