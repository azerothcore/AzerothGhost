package client

import (
	"encoding/binary"
	"testing"
)

func TestClassifyAttackSwingReason(t *testing.T) {
	cases := []struct {
		reason string
		want   RejectClass
	}{
		{RejectReasonNotInRange, RejectTransient},
		{RejectReasonBadFacing, RejectTransient},
		{RejectReasonDeadTarget, RejectTerminal},
		{RejectReasonCantAttack, RejectTerminal},
		{RejectReasonAttackStop, RejectUnknown},
		{"SOMETHING_ELSE", RejectUnknown},
	}
	for _, tc := range cases {
		got := ClassifyAttackSwingReason(tc.reason)
		if got != tc.want {
			t.Errorf("ClassifyAttackSwingReason(%q)=%v want %v", tc.reason, got, tc.want)
		}
	}
}

func TestAttackRejectHelpers(t *testing.T) {
	tr := AttackReject{Class: RejectTransient, Reason: RejectReasonBadFacing}
	if !tr.IsTransient() || tr.IsTerminal() {
		t.Fatal("transient helpers wrong")
	}
	term := AttackReject{Class: RejectTerminal, Reason: RejectReasonDeadTarget}
	if !term.IsTerminal() || term.IsTransient() {
		t.Fatal("terminal helpers wrong")
	}
}

func TestHandleAttackSwingError_TransientKeepsTargetAlive(t *testing.T) {
	w := NewWorldClient("u", nil, func(string, ...interface{}) {})
	const victim uint64 = 0xAABBCCDD11223344
	w.targetGUID = victim
	w.attackingGUID = victim
	w.objects[victim] = &WorldObject{
		GUID: victim,
		Values: map[uint16]uint32{
			UnitFieldHealth:    80,
			UnitFieldMaxHealth: 100,
		},
	}

	var rejects []AttackReject
	w.OnAttackReject = func(r AttackReject) { rejects = append(rejects, r) }
	invalidFired := false
	w.OnInvalidTarget = func(uint64) { invalidFired = true }

	payload := make([]byte, 8)
	binary.LittleEndian.PutUint64(payload, victim)
	w.handleAttackSwingError(payload, RejectReasonBadFacing, SmsgAttackSwingBadFacing)

	if invalidFired {
		t.Fatal("OnInvalidTarget must not fire for BAD_FACING")
	}
	if len(rejects) != 1 || rejects[0].Class != RejectTransient || rejects[0].Reason != RejectReasonBadFacing {
		t.Fatalf("unexpected rejects: %+v", rejects)
	}
	if w.TargetGUID() != victim {
		t.Fatalf("transient reject must keep target, got %d", w.TargetGUID())
	}
	obj := w.GetObject(victim)
	if obj == nil || !obj.IsAlive() || obj.Health() != 80 {
		t.Fatalf("transient reject must not mark object dead, health=%v", obj)
	}
	if w.attackingGUID != 0 {
		t.Fatal("local auto-attack should stop after swing reject")
	}
}

func TestHandleAttackSwingError_TerminalMarksDead(t *testing.T) {
	w := NewWorldClient("u", nil, func(string, ...interface{}) {})
	const victim uint64 = 99
	w.targetGUID = victim
	w.attackingGUID = victim
	w.objects[victim] = &WorldObject{
		GUID: victim,
		Values: map[uint16]uint32{
			UnitFieldHealth:    50,
			UnitFieldMaxHealth: 100,
		},
	}

	var rejects []AttackReject
	w.OnAttackReject = func(r AttackReject) { rejects = append(rejects, r) }
	var invalidGUID uint64
	w.OnInvalidTarget = func(g uint64) { invalidGUID = g }

	payload := make([]byte, 8)
	binary.LittleEndian.PutUint64(payload, victim)
	w.handleAttackSwingError(payload, RejectReasonDeadTarget, SmsgAttackSwingDeadTarget)

	if invalidGUID != victim {
		t.Fatalf("OnInvalidTarget want %d got %d", victim, invalidGUID)
	}
	if len(rejects) != 1 || rejects[0].Class != RejectTerminal {
		t.Fatalf("unexpected rejects: %+v", rejects)
	}
	if w.TargetGUID() != 0 {
		t.Fatal("terminal reject must clear target")
	}
	obj := w.GetObject(victim)
	if obj == nil || obj.IsAlive() {
		t.Fatal("terminal DEAD_TARGET must mark object dead")
	}
}

func TestHandleAttackSwingError_NotInRangeNoDeadMark(t *testing.T) {
	w := NewWorldClient("u", nil, func(string, ...interface{}) {})
	const victim uint64 = 7
	w.targetGUID = victim
	w.objects[victim] = &WorldObject{
		GUID: victim,
		Values: map[uint16]uint32{
			UnitFieldHealth:    10,
			UnitFieldMaxHealth: 10,
		},
	}
	payload := make([]byte, 8)
	binary.LittleEndian.PutUint64(payload, victim)
	w.handleAttackSwingError(payload, RejectReasonNotInRange, SmsgAttackSwingNotInRange)

	if w.TargetGUID() != victim {
		t.Fatal("NOT_IN_RANGE should keep target")
	}
	if !w.GetObject(victim).IsAlive() {
		t.Fatal("NOT_IN_RANGE must not invent death")
	}
}

func TestHandleAttackStop_DoesNotMarkDead(t *testing.T) {
	w := NewWorldClient("u", nil, func(string, ...interface{}) {})
	w.charGUID = 0x100
	const victim uint64 = 55
	w.targetGUID = victim
	w.attackingGUID = victim
	w.objects[victim] = &WorldObject{
		GUID: victim,
		Values: map[uint16]uint32{
			UnitFieldHealth:    40,
			UnitFieldMaxHealth: 100,
		},
	}

	var rejects []AttackReject
	w.OnAttackReject = func(r AttackReject) { rejects = append(rejects, r) }

	// Packed GUIDs: simplified path needs enough bytes for readPackedGUID.
	// Use raw 8-byte LE GUIDs wrapped as packed with mask 0xFF (all bytes present).
	// readPackedGUID expects mask + bytes; build minimal packs.
	payload := packGUIDForTest(w.charGUID)
	payload = append(payload, packGUIDForTest(victim)...)
	w.handleAttackStop(payload)

	if !w.GetObject(victim).IsAlive() {
		t.Fatal("ATTACK_STOP must not mark victim dead")
	}
	if len(rejects) != 1 || rejects[0].Class != RejectUnknown || rejects[0].Reason != RejectReasonAttackStop {
		t.Fatalf("unexpected rejects: %+v", rejects)
	}
	if w.attackingGUID != 0 {
		t.Fatal("attackingGUID should clear on attack stop")
	}
}

// packGUIDForTest builds a packed GUID with full 8-byte mask for tests.
func packGUIDForTest(guid uint64) []byte {
	out := []byte{0xFF}
	for i := 0; i < 8; i++ {
		out = append(out, byte(guid>>(8*i)))
	}
	return out
}
