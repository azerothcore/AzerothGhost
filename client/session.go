package client

import (
	"fmt"
	"time"
)

// SessionPhase mirrors AzerothCore WorldSession status gates that matter for bots.
// Gameplay CMSG (move/cast/attack) is only valid in PhaseInWorld; far teleports
// temporarily require WORLDPORT_ACK before movement is accepted again.
type SessionPhase int

const (
	PhaseNone SessionPhase = iota
	PhaseConnected         // TCP + crypto ready, waiting AUTH_RESPONSE
	PhaseAuthed            // AUTH_OK; char enum/create/login allowed
	PhaseLoading           // CMSG_PLAYER_LOGIN sent; waiting LOGIN_VERIFY_WORLD
	PhaseInWorld           // character in world; normal gameplay
	PhaseFarTransfer       // SMSG_NEW_WORLD received; WORLDPORT_ACK in flight
	PhaseNearTeleport      // SMSG_MOVE_TELEPORT for self; TELEPORT_ACK in flight
	PhaseLogout
)

func (p SessionPhase) String() string {
	switch p {
	case PhaseNone:
		return "none"
	case PhaseConnected:
		return "connected"
	case PhaseAuthed:
		return "authed"
	case PhaseLoading:
		return "loading"
	case PhaseInWorld:
		return "in_world"
	case PhaseFarTransfer:
		return "far_transfer"
	case PhaseNearTeleport:
		return "near_teleport"
	case PhaseLogout:
		return "logout"
	default:
		return "unknown"
	}
}

// SessionPhaseChange is emitted on every phase transition.
type SessionPhaseChange struct {
	From   SessionPhase
	To     SessionPhase
	Reason string
}

// IsInWorld reports whether the character is in a normal gameplay phase.
func (w *WorldClient) IsInWorld() bool {
	w.phaseMu.RLock()
	defer w.phaseMu.RUnlock()
	return w.phase == PhaseInWorld
}

// SessionPhase returns the current protocol phase.
func (w *WorldClient) SessionPhase() SessionPhase {
	w.phaseMu.RLock()
	defer w.phaseMu.RUnlock()
	return w.phase
}

// LastTimeSyncCounter returns the last SMSG_TIME_SYNC_REQ counter we answered.
func (w *WorldClient) LastTimeSyncCounter() uint32 {
	w.phaseMu.RLock()
	defer w.phaseMu.RUnlock()
	return w.lastTimeSyncCounter
}

// TimeSyncResponses returns how many CMSG_TIME_SYNC_RESP we sent.
func (w *WorldClient) TimeSyncResponses() uint64 {
	w.phaseMu.RLock()
	defer w.phaseMu.RUnlock()
	return w.timeSyncResponses
}

func (w *WorldClient) setPhase(to SessionPhase, reason string) {
	w.phaseMu.Lock()
	from := w.phase
	if from == to {
		w.phaseMu.Unlock()
		return
	}
	w.phase = to
	// Self near/far teleport completes when we return to InWorld from a transfer phase.
	// Bump sequence so WaitForTeleportAfter can observe the packet-driven settle without sleeps.
	if to == PhaseInWorld && (from == PhaseNearTeleport || from == PhaseFarTransfer) {
		w.teleportSeq++
	}
	seq := w.teleportSeq
	waiters := append([]chan SessionPhase(nil), w.phaseWaiters...)
	w.phaseMu.Unlock()

	w.log("session phase %s -> %s (%s)", from, to, reason)
	if w.OnSessionPhase != nil {
		w.OnSessionPhase(SessionPhaseChange{From: from, To: to, Reason: reason})
	}
	for _, ch := range waiters {
		select {
		case ch <- to:
		default:
		}
	}
	_ = seq // kept for readability next to teleportSeq bump
}

// TeleportSeq returns how many self near/far teleports have completed (packet-driven).
// Snapshot before a teleport command, then WaitForTeleportAfter.
func (w *WorldClient) TeleportSeq() uint64 {
	w.phaseMu.RLock()
	defer w.phaseMu.RUnlock()
	return w.teleportSeq
}

// WaitForSessionPhase blocks until SessionPhase equals want, or timeout.
// Driven by phase transitions (SMSG_AUTH_RESPONSE, LOGIN_VERIFY_WORLD, teleports, …).
func (w *WorldClient) WaitForSessionPhase(want SessionPhase, timeout time.Duration) error {
	if w.SessionPhase() == want {
		return nil
	}
	ch := make(chan SessionPhase, 16)
	w.phaseMu.Lock()
	w.phaseWaiters = append(w.phaseWaiters, ch)
	// Recheck under lock so we cannot miss a transition that happened after the first check.
	if w.phase == want {
		w.phaseMu.Unlock()
		w.removePhaseWaiter(ch)
		return nil
	}
	w.phaseMu.Unlock()
	defer w.removePhaseWaiter(ch)

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case p := <-ch:
			if p == want {
				return nil
			}
		case <-timer.C:
			return fmt.Errorf("timeout waiting for session phase %s (have %s)", want, w.SessionPhase())
		case <-w.stopChan:
			return fmt.Errorf("client stopped while waiting for session phase %s", want)
		}
	}
}

// WaitForTeleportAfter waits until TeleportSeq advances past afterSeq (self tele complete).
// Use after GM `.go` / far transfer; near-tele and NEW_WORLD both bump the sequence.
func (w *WorldClient) WaitForTeleportAfter(afterSeq uint64, timeout time.Duration) error {
	if w.TeleportSeq() > afterSeq {
		return nil
	}
	// Teleport completion always lands in PhaseInWorld; also watch phase so a stalled
	// tele does not hang past the session dying.
	ch := make(chan SessionPhase, 16)
	w.phaseMu.Lock()
	w.phaseWaiters = append(w.phaseWaiters, ch)
	if w.teleportSeq > afterSeq {
		w.phaseMu.Unlock()
		w.removePhaseWaiter(ch)
		return nil
	}
	w.phaseMu.Unlock()
	defer w.removePhaseWaiter(ch)

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		if w.TeleportSeq() > afterSeq {
			return nil
		}
		select {
		case <-ch:
			if w.TeleportSeq() > afterSeq {
				return nil
			}
		case <-timer.C:
			return fmt.Errorf("timeout waiting for teleport after seq=%d (have seq=%d phase=%s)",
				afterSeq, w.TeleportSeq(), w.SessionPhase())
		case <-w.stopChan:
			return fmt.Errorf("client stopped while waiting for teleport")
		}
	}
}

func (w *WorldClient) removePhaseWaiter(ch chan SessionPhase) {
	w.phaseMu.Lock()
	defer w.phaseMu.Unlock()
	out := w.phaseWaiters[:0]
	for _, wch := range w.phaseWaiters {
		if wch != ch {
			out = append(out, wch)
		}
	}
	w.phaseWaiters = out
}

// requiresInWorldGameplay is true for opcodes AC will STATUS-drop outside in-world.
func requiresInWorldGameplay(opcode uint16) bool {
	switch opcode {
	case CmsgAttackSwing, CmsgAttackStop, CmsgCastSpell, CmsgCancelCast,
		CmsgSetSelection, CmsgSetSheathed, CmsgLoot, CmsgLootRelease,
		MsgMoveStartForward, MsgMoveStop, MsgMoveHeartbeat, MsgMoveSetFacing,
		MsgMoveJump, MsgMoveFallLand,
		MsgMoveStartStrafeLeft, MsgMoveStartStrafeRight, MsgMoveStopStrafe,
		MsgMoveStartTurnLeft, MsgMoveStartTurnRight, MsgMoveStopTurn,
		MsgMoveStartBackward:
		return true
	default:
		return false
	}
}

// checkOutboundPhase logs protocol warnings when we send gameplay in a bad phase.
// Does not block the send (login race / edge cases); observability only.
func (w *WorldClient) checkOutboundPhase(opcode uint16) {
	if !requiresInWorldGameplay(opcode) {
		return
	}
	ph := w.SessionPhase()
	if ph == PhaseInWorld {
		return
	}
	msg := "gameplay opcode while not in_world"
	if w.OnProtocolWarning != nil {
		w.OnProtocolWarning(msg, opcode, ph)
	}
	w.log("PROTOCOL_WARN: %s opcode=%s phase=%s", msg, OpcodeName(opcode), ph)
}
