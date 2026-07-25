package client

// OpcodeName returns a human-readable name for known 3.3.5a opcodes.
// Unknown values are returned as hex (e.g. "0x1234").
func OpcodeName(op uint16) string {
	if n, ok := opcodeNames[op]; ok {
		return n
	}
	return sprintfOpcode(op)
}

func sprintfOpcode(op uint16) string {
	const hexdigits = "0123456789ABCDEF"
	return string([]byte{
		'0', 'x',
		hexdigits[op>>12], hexdigits[(op>>8)&0xF],
		hexdigits[(op>>4)&0xF], hexdigits[op&0xF],
	})
}

// IsHighValueTraceOpcode reports opcodes worth recording under --trace-packets.
// Excludes flood-prone update/move streams that drown the timeline.
func IsHighValueTraceOpcode(op uint16) bool {
	switch op {
	// Auth / login / transfer
	case SmsgAuthChallenge, CmsgAuthSession, SmsgAuthResponse,
		CmsgCharEnum, SmsgCharEnum, CmsgCharCreate, SmsgCharCreate,
		CmsgPlayerLogin, SmsgLoginVerifyWorld,
		SmsgNewWorld, SmsgTransferPending,
		SmsgTimeSyncReq, CmsgTimeSyncResp,
		MsgMoveTeleportAck, SmsgMoveTeleport, SmsgMoveKnockBack:
		return true
	// Combat
	case CmsgAttackSwing, CmsgAttackStop,
		SmsgAttackStart, SmsgAttackStop,
		SmsgAttackSwingNotInRange, SmsgAttackSwingBadFacing,
		SmsgAttackSwingDeadTarget, SmsgAttackSwingCantAttack,
		SmsgAttackerStateUpdate, SmsgCancelCombat, SmsgAiReaction,
		CmsgSetSelection, CmsgSetSheathed:
		return true
	// Spells / auras
	// Note: CmsgCancelAura shares 0x0133 with SmsgSpellFailure on 3.3.5a — one case covers both.
	case CmsgCastSpell, CmsgCancelCast,
		SmsgCastFailed, SmsgSpellStart, SmsgSpellGo, SmsgSpellFailure,
		SmsgSpellCooldown, SmsgCooldownEvent, SmsgClearCooldown,
		SmsgInitialSpells, SmsgAuraUpdate, SmsgAuraUpdateAll:
		return true
	// Sparse movement (not every heartbeat flood in all builds — still useful)
	case MsgMoveStartForward, MsgMoveStop, MsgMoveSetFacing,
		MsgMoveJump, MsgMoveFallLand,
		MsgMoveStartStrafeLeft, MsgMoveStartStrafeRight, MsgMoveStopStrafe,
		MsgMoveStartTurnLeft, MsgMoveStartTurnRight, MsgMoveStopTurn:
		return true
	// Heartbeats: include so repath/choppy issues are visible, but they are frequent.
	case MsgMoveHeartbeat:
		return true
	// Speed force (ACK mismatches kick)
	case SmsgForceRunSpeedChange, CmsgForceRunSpeedChangeAck:
		return true
	// Loot (validation scripts)
	case CmsgLoot, SmsgLootResponse, CmsgLootRelease, SmsgLootReleaseResponse:
		return true
	default:
		return false
	}
}

// opcodeNames is the subset of opcodes this client uses.
var opcodeNames = map[uint16]string{
	SmsgAuthChallenge: "SMSG_AUTH_CHALLENGE",
	CmsgAuthSession:   "CMSG_AUTH_SESSION",
	SmsgAuthResponse:  "SMSG_AUTH_RESPONSE",
	CmsgCharEnum:      "CMSG_CHAR_ENUM",
	SmsgCharEnum:      "SMSG_CHAR_ENUM",
	CmsgCharCreate:    "CMSG_CHAR_CREATE",
	SmsgCharCreate:    "SMSG_CHAR_CREATE",
	CmsgCharDelete:    "CMSG_CHAR_DELETE",
	SmsgCharDelete:    "SMSG_CHAR_DELETE",
	CmsgPlayerLogin:   "CMSG_PLAYER_LOGIN",
	SmsgLoginVerifyWorld: "SMSG_LOGIN_VERIFY_WORLD",
	CmsgPing:          "CMSG_PING",
	SmsgPong:          "SMSG_PONG",
	SmsgTimeSyncReq:   "SMSG_TIME_SYNC_REQ",
	CmsgTimeSyncResp:  "CMSG_TIME_SYNC_RESP",
	CmsgMessageChat:   "CMSG_MESSAGECHAT",
	SmsgMessageChat:   "SMSG_MESSAGECHAT",
	MsgMoveJump:       "MSG_MOVE_JUMP",
	MsgMoveFallLand:   "MSG_MOVE_FALL_LAND",
	MsgMoveHeartbeat:  "MSG_MOVE_HEARTBEAT",
	MsgMoveStartForward: "MSG_MOVE_START_FORWARD",
	MsgMoveStop:       "MSG_MOVE_STOP",
	MsgMoveStartBackward: "MSG_MOVE_START_BACKWARD",
	MsgMoveStartStrafeLeft: "MSG_MOVE_START_STRAFE_LEFT",
	MsgMoveStartStrafeRight: "MSG_MOVE_START_STRAFE_RIGHT",
	MsgMoveStopStrafe: "MSG_MOVE_STOP_STRAFE",
	MsgMoveStartTurnLeft: "MSG_MOVE_START_TURN_LEFT",
	MsgMoveStartTurnRight: "MSG_MOVE_START_TURN_RIGHT",
	MsgMoveStopTurn:   "MSG_MOVE_STOP_TURN",
	MsgMoveSetFacing:  "MSG_MOVE_SET_FACING",
	MsgMoveTeleportAck: "MSG_MOVE_TELEPORT_ACK",
	SmsgMoveKnockBack: "SMSG_MOVE_KNOCK_BACK",
	SmsgMoveTeleport:  "SMSG_MOVE_TELEPORT",
	CmsgSetActiveMover: "CMSG_SET_ACTIVE_MOVER",
	CmsgLogoutRequest: "CMSG_LOGOUT_REQUEST",
	SmsgLogoutResponse: "SMSG_LOGOUT_RESPONSE",
	SmsgLogoutComplete: "SMSG_LOGOUT_COMPLETE",
	SmsgCancelCombat:  "SMSG_CANCEL_COMBAT",
	CmsgAttackSwing:   "CMSG_ATTACKSWING",
	CmsgAttackStop:    "CMSG_ATTACKSTOP",
	SmsgAttackStart:   "SMSG_ATTACKSTART",
	SmsgAttackStop:    "SMSG_ATTACKSTOP",
	SmsgAttackSwingNotInRange: "SMSG_ATTACKSWING_NOTINRANGE",
	SmsgAttackSwingBadFacing:  "SMSG_ATTACKSWING_BADFACING",
	SmsgAttackSwingDeadTarget: "SMSG_ATTACKSWING_DEADTARGET",
	SmsgAttackSwingCantAttack: "SMSG_ATTACKSWING_CANT_ATTACK",
	SmsgAttackerStateUpdate:   "SMSG_ATTACKERSTATEUPDATE",
	CmsgCastSpell:     "CMSG_CAST_SPELL",
	SmsgSpellStart:    "SMSG_SPELL_START",
	SmsgSpellGo:       "SMSG_SPELL_GO",
	SmsgSpellFailure:  "SMSG_SPELL_FAILURE",
	SmsgSpellCooldown: "SMSG_SPELL_COOLDOWN",
	SmsgCastFailed:    "SMSG_CAST_FAILED",
	SmsgInitialSpells: "SMSG_INITIAL_SPELLS",
	SmsgCooldownEvent: "SMSG_COOLDOWN_EVENT",
	SmsgClearCooldown: "SMSG_CLEAR_COOLDOWN",
	CmsgCancelCast:    "CMSG_CANCEL_CAST",
	SmsgUpdateObject:  "SMSG_UPDATE_OBJECT",
	SmsgDestroyObject: "SMSG_DESTROY_OBJECT",
	SmsgCompressedUpdate: "SMSG_COMPRESSED_UPDATE_OBJECT",
	CmsgLoot:          "CMSG_LOOT",
	SmsgLootResponse:  "SMSG_LOOT_RESPONSE",
	CmsgLootRelease:   "CMSG_LOOT_RELEASE",
	SmsgLootReleaseResponse: "SMSG_LOOT_RELEASE_RESPONSE",
	CmsgSetSelection:  "CMSG_SET_SELECTION",
	SmsgNewWorld:      "SMSG_NEW_WORLD",
	SmsgTransferPending: "SMSG_TRANSFER_PENDING",
	MsgMoveWorldportAck: "MSG_MOVE_WORLDPORT_ACK",
	SmsgAiReaction:    "SMSG_AI_REACTION",
	SmsgPowerUpdate:   "SMSG_POWER_UPDATE",
	SmsgForceRunSpeedChange: "SMSG_FORCE_RUN_SPEED_CHANGE",
	CmsgForceRunSpeedChangeAck: "CMSG_FORCE_RUN_SPEED_CHANGE_ACK",
	CmsgSetSheathed:   "CMSG_SETSHEATHED",
	SmsgAuraUpdate:    "SMSG_AURA_UPDATE",
	SmsgAuraUpdateAll: "SMSG_AURA_UPDATE_ALL",
	SmsgLevelupInfo:   "SMSG_LEVELUP_INFO",
}
