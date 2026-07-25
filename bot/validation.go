package bot

import (
	"encoding/hex"
	"fmt"

	"github.com/walkline/AzerothGhost/client"
)

// wireValidationInstrumentation attaches JSONL timeline hooks to the world client.
// Zero-cost when ValidationMode is off or the validation log file is not open.
//
// Timeline event types (field "type"):
//   - phase: bot status change
//   - decision: AI /logDecision
//   - reject: classified attack swing / attack-stop feedback
//   - cast: SPELL_GO / SPELL_FAILURE / CAST_FAILED
//   - cmsg / smsg: high-value opcodes when EnablePacketTrace
//   - event: kill / death / levelup
func (b *Bot) wireValidationInstrumentation() {
	if b == nil || b.world == nil || !b.config.ValidationMode || b.validationEnc == nil {
		return
	}

	b.world.TraceLogOpcodes = b.config.EnablePacketTrace

	// --- Reject timeline (always when validation log is open) ---
	prevReject := b.world.OnAttackReject
	b.world.OnAttackReject = func(r client.AttackReject) {
		b.logValidation("reject", map[string]interface{}{
			"reason": r.Reason,
			"class":  r.Class.String(),
			"guid":   fmt.Sprintf("%d", r.GUID),
			"opcode": client.OpcodeName(r.Opcode),
			"opcode_id": r.Opcode,
		})
		if prevReject != nil {
			prevReject(r)
		}
	}

	// --- Cast outcomes ---
	prevCast := b.world.OnSpellCastResult
	b.world.OnSpellCastResult = func(spellID uint32, success bool, failReason uint8) {
		fields := map[string]interface{}{
			"spell_id": spellID,
			"success":  success,
		}
		if !success {
			fields["fail_reason"] = failReason
		}
		b.logValidation("cast", fields)
		if prevCast != nil {
			prevCast(spellID, success, failReason)
		}
	}

	// --- Lifecycle events already partially set; wrap kill/death/level ---
	prevKill := b.world.OnKill
	b.world.OnKill = func(victimGUID uint64) {
		b.logValidation("event", map[string]interface{}{
			"kind": "kill",
			"guid": fmt.Sprintf("%d", victimGUID),
		})
		if prevKill != nil {
			prevKill(victimGUID)
		}
	}
	prevDeath := b.world.OnDeath
	b.world.OnDeath = func() {
		b.logValidation("event", map[string]interface{}{"kind": "death"})
		if prevDeath != nil {
			prevDeath()
		}
	}
	prevLevel := b.world.OnLevelUp
	b.world.OnLevelUp = func(newLevel uint32) {
		b.logValidation("event", map[string]interface{}{
			"kind":  "levelup",
			"level": newLevel,
		})
		if prevLevel != nil {
			prevLevel(newLevel)
		}
	}

	// --- Packet trace (opt-in via EnablePacketTrace) ---
	if b.config.EnablePacketTrace {
		b.world.OnPacket = func(opcode uint16, data []byte) {
			if !client.IsHighValueTraceOpcode(opcode) {
				return
			}
			b.logValidation("smsg", packetTraceFields(opcode, data))
		}
		b.world.OnPacketSend = func(opcode uint16, data []byte) {
			if !client.IsHighValueTraceOpcode(opcode) {
				return
			}
			b.logValidation("cmsg", packetTraceFields(opcode, data))
		}
		b.logValidation("meta", map[string]interface{}{
			"msg":          "packet_trace_enabled",
			"trace_filter": "high_value",
		})
	}

	// Session phase transitions (protocol STATUS_* analogue)
	prevPhase := b.world.OnSessionPhase
	b.world.OnSessionPhase = func(c client.SessionPhaseChange) {
		b.logValidation("phase", map[string]interface{}{
			"from":   c.From.String(),
			"to":     c.To.String(),
			"reason": c.Reason,
			"kind":   "session",
		})
		if prevPhase != nil {
			prevPhase(c)
		}
	}
	prevWarn := b.world.OnProtocolWarning
	b.world.OnProtocolWarning = func(msg string, opcode uint16, phase client.SessionPhase) {
		b.logValidation("protocol_warn", map[string]interface{}{
			"msg":    msg,
			"opcode": client.OpcodeName(opcode),
			"phase":  phase.String(),
		})
		if prevWarn != nil {
			prevWarn(msg, opcode, phase)
		}
	}

	b.logValidation("meta", map[string]interface{}{
		"msg":           "validation_timeline_start",
		"packet_trace":  b.config.EnablePacketTrace,
		"detailed_aura": b.config.EnableDetailedAuras,
	})
}

func packetTraceFields(opcode uint16, data []byte) map[string]interface{} {
	const maxHex = 48
	short := data
	truncated := false
	if len(short) > maxHex {
		short = short[:maxHex]
		truncated = true
	}
	fields := map[string]interface{}{
		"opcode":    client.OpcodeName(opcode),
		"opcode_id": opcode,
		"len":       len(data),
		"data_hex":  hex.EncodeToString(short),
	}
	if truncated {
		fields["data_truncated"] = true
	}
	return fields
}
