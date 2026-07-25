package client

import "testing"

func TestSessionPhaseString(t *testing.T) {
	if PhaseInWorld.String() != "in_world" {
		t.Fatal(PhaseInWorld.String())
	}
	if PhaseFarTransfer.String() != "far_transfer" {
		t.Fatal(PhaseFarTransfer.String())
	}
}

func TestSetPhaseTransitions(t *testing.T) {
	w := NewWorldClient("u", nil, func(string, ...interface{}) {})
	var changes []SessionPhaseChange
	w.OnSessionPhase = func(c SessionPhaseChange) { changes = append(changes, c) }

	w.setPhase(PhaseConnected, "test")
	w.setPhase(PhaseAuthed, "auth")
	w.setPhase(PhaseLoading, "login")
	w.setPhase(PhaseInWorld, "verify")
	// no-op same phase
	w.setPhase(PhaseInWorld, "again")

	if !w.IsInWorld() {
		t.Fatal("expected in world")
	}
	if w.SessionPhase() != PhaseInWorld {
		t.Fatal(w.SessionPhase())
	}
	if len(changes) != 4 {
		t.Fatalf("expected 4 transitions, got %d %+v", len(changes), changes)
	}
}

func TestCheckOutboundPhaseWarns(t *testing.T) {
	w := NewWorldClient("u", nil, func(string, ...interface{}) {})
	var warned bool
	w.OnProtocolWarning = func(msg string, opcode uint16, phase SessionPhase) {
		warned = true
		if phase != PhaseLoading {
			t.Fatalf("phase %v", phase)
		}
		if opcode != CmsgAttackSwing {
			t.Fatalf("opcode %v", opcode)
		}
	}
	w.setPhase(PhaseLoading, "test")
	w.checkOutboundPhase(CmsgAttackSwing)
	if !warned {
		t.Fatal("expected warning")
	}
	warned = false
	w.setPhase(PhaseInWorld, "ok")
	w.checkOutboundPhase(CmsgAttackSwing)
	if warned {
		t.Fatal("should not warn in_world")
	}
}

func TestRequiresInWorldGameplay(t *testing.T) {
	if !requiresInWorldGameplay(CmsgCastSpell) {
		t.Fatal("cast")
	}
	if requiresInWorldGameplay(CmsgCharEnum) {
		t.Fatal("char enum should be allowed outside world")
	}
}
