package client

import (
	"testing"
	"time"
)

// Smoke tests for the core protocol client extraction.
// Full end-to-end connect/enum/login require a live authserver+worldserver
// (see design doc E2E instructions using real AzerothCore binaries).
// These tests exercise constructors, exported types, and non-network paths.

func TestNewAuthClient(t *testing.T) {
	c := NewAuthClient("testuser", "testpass")
	if c == nil {
		t.Fatal("NewAuthClient returned nil")
	}
	// SessionKey should be nil/empty before Authenticate
	if len(c.SessionKey()) != 0 {
		t.Errorf("expected empty session key before auth, got %d bytes", len(c.SessionKey()))
	}
}

func TestNewWorldClient(t *testing.T) {
	wc := NewWorldClient("testuser", []byte{1, 2, 3}, func(string, ...interface{}) {})
	if wc == nil {
		t.Fatal("NewWorldClient returned nil")
	}
}

func TestCharEnumEntry(t *testing.T) {
	e := CharEnumEntry{
		GUID:  12345,
		Name:  "TestChar",
		Race:  1,
		Class: 1,
		Level: 1,
	}
	if e.Name != "TestChar" {
		t.Error("CharEnumEntry fields not set correctly")
	}
}

func TestWorldObjectBasics(t *testing.T) {
	obj := &WorldObject{
		GUID:   999,
		TypeID: ObjectTypeUnit,
		Entry:  38,
		Values: map[uint16]uint32{
			UnitFieldHealth:    100,
			UnitFieldMaxHealth: 100,
			UnitFieldLevel:     5,
		},
		PosX: 10, PosY: 20, PosZ: 30,
	}
	if !obj.IsUnit() {
		t.Error("expected IsUnit true")
	}
	if obj.Health() != 100 {
		t.Errorf("expected health 100, got %d", obj.Health())
	}
	if obj.Level() != 5 {
		t.Error("level not read from values")
	}
	if !obj.IsAlive() {
		t.Error("expected alive with positive health")
	}

	clone := obj.Clone()
	if clone == nil || clone.GUID != obj.GUID || clone.Health() != 100 {
		t.Error("Clone did not produce equivalent object")
	}
}

func TestWorldObjectIsAlive(t *testing.T) {
	// Dead via flag
	dead := &WorldObject{Values: map[uint16]uint32{UnitFieldFlags: UnitFlagDead}}
	if dead.IsAlive() {
		t.Error("dead flag should make IsAlive false")
	}

	// Zero health + maxhealth known -> dead
	zeroHP := &WorldObject{Values: map[uint16]uint32{
		UnitFieldHealth:    0,
		UnitFieldMaxHealth: 100,
	}}
	if zeroHP.IsAlive() {
		t.Error("zero health with max >0 should be dead")
	}
}

func TestOpcodesExported(t *testing.T) {
	// Spot-check a few key exported opcodes are non-zero (compile-time + sanity)
	if SmsgAuthChallenge == 0 || CmsgAuthSession == 0 {
		t.Error("auth opcodes not set")
	}
	if SmsgCharEnum == 0 || CmsgPlayerLogin == 0 {
		t.Error("char/login opcodes not set")
	}
	if SmsgUpdateObject == 0 {
		t.Error("update opcode not set")
	}
}

func TestRealmInfo(t *testing.T) {
	r := RealmInfo{Name: "TestRealm", Address: "127.0.0.1:8085"}
	if r.Name == "" {
		t.Error("RealmInfo not populated")
	}
}

// Test that callbacks are assignable (they are exported func fields)
func TestCallbacksAssignable(t *testing.T) {
	wc := NewWorldClient("u", nil, nil)
	wc.OnCharList = func(chars []CharEnumEntry) {}
	wc.OnCharCreateResult = func([]byte) {}
	wc.OnChatMessage = func(string, string, uint8) {}
	wc.OnObjectUpdate = func(uint64, *WorldObject) {}
	_ = wc // use it
}

// Basic timing smoke for movement time helpers (internal but exercised via package)
func TestMovementTimeHelpers(t *testing.T) {
	now := getMSTime()
	if now == 0 {
		t.Error("getMSTime returned zero")
	}
	t2 := movementMSTime(time.Now())
	if t2 == 0 {
		t.Error("movementMSTime returned zero")
	}
}

// Smoke for connect path used by enum/login flows (char methods require an active conn
// and will panic on nil conn today; Connect/Auth failure paths are the smoke tests).
func TestConnectDialFailureSmoke(t *testing.T) {
	// Covered by TestConnectAndAuthDialFailure below; this is a marker for "connect/enum/login smoke".
}

// Connect should fail fast for bad address (smoke of dial path used by auth+world login flows).
func TestConnectAndAuthDialFailure(t *testing.T) {
	wc := NewWorldClient("test", []byte{}, nil)
	err := wc.Connect("127.0.0.1:1") // unlikely to have listener, will fail connect
	if err == nil {
		t.Error("Connect to invalid port should fail")
	}

	ac := NewAuthClient("u", "p")
	_, err = ac.Authenticate("127.0.0.1:1")
	if err == nil {
		t.Error("AuthClient.Authenticate to invalid should fail")
	}
}

func TestInterpolatedPosition_DoesNotMutateAndAdvances(t *testing.T) {
	obj := &WorldObject{
		PosX: 0, PosY: 0, PosZ: 0,
		StartX: 0, StartY: 0, StartZ: 0,
		DestX: 100, DestY: 0, DestZ: 0,
		IsMoving: true,
		MoveStartTime: time.Now().Add(-500 * time.Millisecond),
		MoveDuration:  time.Second,
	}
	x, y, z := obj.InterpolatedPosition()
	if x < 40 || x > 60 {
		t.Fatalf("mid-spline x=%v want ~50", x)
	}
	if y != 0 || z != 0 {
		t.Fatalf("y/z = %v,%v", y, z)
	}
	// Pure read: raw Pos* stays at segment start
	if obj.PosX != 0 || !obj.IsMoving {
		t.Fatalf("InterpolatedPosition mutated object: PosX=%v IsMoving=%v", obj.PosX, obj.IsMoving)
	}

	obj.MoveStartTime = time.Now().Add(-2 * time.Second)
	x, _, _ = obj.InterpolatedPosition()
	if x != 100 {
		t.Fatalf("completed spline x=%v want Dest 100", x)
	}
}

func TestClone_UsesInterpolatedPose(t *testing.T) {
	obj := &WorldObject{
		GUID: 1, TypeID: ObjectTypeUnit, Values: map[uint16]uint32{},
		PosX: 0, PosY: 0, PosZ: 0,
		StartX: 0, StartY: 0, StartZ: 0,
		DestX: 100, DestY: 0, DestZ: 0,
		IsMoving: true,
		MoveStartTime: time.Now().Add(-500 * time.Millisecond),
		MoveDuration:  time.Second,
		LastPosUpdate: time.Now(),
	}
	cl := obj.Clone()
	if cl.PosX < 40 || cl.PosX > 60 {
		t.Fatalf("clone PosX=%v should be mid-path, not create start 0", cl.PosX)
	}
}

func TestHasKnownPosition(t *testing.T) {
	stub := &WorldObject{}
	if stub.HasKnownPosition() {
		t.Fatal("zero stub should not have known position")
	}
	stub.LastPosUpdate = time.Now()
	if !stub.HasKnownPosition() {
		t.Fatal("LastPosUpdate should mark known")
	}
}
