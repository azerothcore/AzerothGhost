package client

// RejectClass classifies server combat/cast feedback so upper layers can
// distinguish protocol/precondition misses from true terminal world-state.
//
// Transient: bot should adjust (approach, face, wait) and keep the target.
// Terminal: target is not a valid attack target; clear and do not re-pick soon.
// Unknown: ambiguous (e.g. SMSG_ATTACK_STOP); clear swing state but do not invent death.
type RejectClass int

const (
	RejectUnknown RejectClass = iota
	RejectTransient
	RejectTerminal
)

func (c RejectClass) String() string {
	switch c {
	case RejectTransient:
		return "transient"
	case RejectTerminal:
		return "terminal"
	default:
		return "unknown"
	}
}

// AttackReject is structured feedback from the server (or client inference)
// about why an attack/swing failed.
type AttackReject struct {
	GUID   uint64
	Reason string // e.g. "NOT_IN_RANGE", "BAD_FACING", "DEAD_TARGET", "CANT_ATTACK", "ATTACK_STOP"
	Class  RejectClass
	Opcode uint16
}

// Well-known swing error reason strings (match handleAttackSwingError callers).
const (
	RejectReasonNotInRange = "NOT_IN_RANGE"
	RejectReasonBadFacing  = "BAD_FACING"
	RejectReasonDeadTarget = "DEAD_TARGET"
	RejectReasonCantAttack = "CANT_ATTACK"
	RejectReasonAttackStop = "ATTACK_STOP"
)

// ClassifyAttackSwingReason maps SMSG_ATTACKSWING_* reasons to a reject class.
// Transient preconditions (range/facing) must never be treated as death.
func ClassifyAttackSwingReason(reason string) RejectClass {
	switch reason {
	case RejectReasonNotInRange, RejectReasonBadFacing:
		return RejectTransient
	case RejectReasonDeadTarget, RejectReasonCantAttack:
		return RejectTerminal
	default:
		return RejectUnknown
	}
}

// IsTerminal reports whether this reject should drop the target from the valid pool.
func (r AttackReject) IsTerminal() bool {
	return r.Class == RejectTerminal
}

// IsTransient reports whether the bot should keep the target and correct posture.
func (r AttackReject) IsTransient() bool {
	return r.Class == RejectTransient
}
