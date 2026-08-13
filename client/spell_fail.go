package client

import "fmt"

// SpellFailReasonName maps 3.3.5a SpellCastResult codes (AC SharedDefines) to short names.
// Common e2e/AI reasons are covered; unknown codes fall back to REASON_N.
func SpellFailReasonName(reason uint8) string {
	// Subset of SpellCastResult — keep greppable short tokens (no SPELL_FAILED_ prefix).
	names := map[uint8]string{
		0:   "SUCCESS",
		1:   "AFFECTING_COMBAT",
		12:  "BAD_TARGETS",
		22:  "CASTER_AURASTATE",
		23:  "CASTER_DEAD",
		27:  "DONT_REPORT",
		33:  "FIZZLE",
		38:  "IMMUNE",
		40:  "INTERRUPTED",
		41:  "INTERRUPTED_COMBAT",
		46:  "LEVEL_REQUIREMENT",
		47:  "LINE_OF_SIGHT",
		51:  "MOVING",
		56:  "NOPATH",
		60:  "NOT_HERE",
		61:  "NOT_INFRONT",
		62:  "NOT_IN_CONTROL",
		63:  "NOT_KNOWN",
		67:  "NOT_READY",
		69:  "NOT_STANDING",
		73:  "NOT_WHILE_GHOST",
		78:  "NO_COMBO_POINTS",
		85:  "NO_POWER",
		93:  "ONLY_OUTDOORS",
		97:  "OUT_OF_RANGE",
		100: "REAGENTS",
		105: "SPELL_IN_PROGRESS",
		108: "STUNNED",
		109: "TARGETS_DEAD",
		111: "TARGET_AURASTATE",
		130: "TOTEM_CATEGORY",
		166: "NOT_IN_BATTLEGROUND",
		168: "ONLY_IN_ARENA",
		172: "CUSTOM_ERROR",
	}
	if s, ok := names[reason]; ok {
		return s
	}
	return fmt.Sprintf("REASON_%d", reason)
}
