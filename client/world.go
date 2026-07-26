// Package client implements the core World of Warcraft 3.3.5a protocol clients.
//
// AuthClient performs SRP6 authentication against an authserver and returns
// realm list information plus the session key.
//
// WorldClient manages the encrypted world session, character operations
// (enum/create/delete/login), object tracking, movement, combat, spells,
// chat, and exposes callbacks for higher layers (bot, testkit, orchestrator).
//
// This is the pure protocol layer with no AI, navigation, or DB coupling.
// It is intended for use both by the AzerothGhost bot runtime and directly
// in integration tests.
package client

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rc4"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"net"
	"strings"
	"sync"
	"time"
)

// Opcodes (subset needed for the bot)
const (
	SmsgAuthChallenge uint16 = 0x01EC
	CmsgAuthSession   uint16 = 0x01ED
	SmsgAuthResponse  uint16 = 0x01EE

	CmsgCharEnum    uint16 = 0x0037
	SmsgCharEnum    uint16 = 0x003B
	CmsgCharCreate  uint16 = 0x0036
	SmsgCharCreate  uint16 = 0x003A
	CmsgCharDelete  uint16 = 0x0038
	SmsgCharDelete  uint16 = 0x003C
	CmsgPlayerLogin uint16 = 0x003D

	SmsgLoginVerifyWorld uint16 = 0x0236

	CmsgPing uint16 = 0x01DC
	SmsgPong uint16 = 0x01DD

	SmsgTimeSyncReq  uint16 = 0x0390
	CmsgTimeSyncResp uint16 = 0x0391

	CmsgMessageChat uint16 = 0x0095
	SmsgMessageChat uint16 = 0x0096

	CmsgTextEmote uint16 = 0x0104
	SmsgTextEmote uint16 = 0x0105
	SmsgEmote     uint16 = 0x0103

	MsgMoveJump             uint16 = 0x00BB
	MsgMoveFallLand         uint16 = 0x00C9
	MsgMoveHeartbeat        uint16 = 0x00EE
	MsgMoveStartForward     uint16 = 0x00B5
	MsgMoveStop             uint16 = 0x00B7
	MsgMoveStartBackward    uint16 = 0x00B6
	MsgMoveStartStrafeLeft  uint16 = 0x00B8
	MsgMoveStartStrafeRight uint16 = 0x00B9
	MsgMoveStopStrafe       uint16 = 0x00BA
	MsgMoveStartTurnLeft    uint16 = 0x00BC
	MsgMoveStartTurnRight   uint16 = 0x00BD
	MsgMoveStopTurn         uint16 = 0x00BE
	MsgMoveSetFacing        uint16 = 0x00DA
	MsgMoveTeleportAck      uint16 = 0x00C7
	SmsgMoveKnockBack       uint16 = 0x00EF
	SmsgMoveTeleport        uint16 = 0x00C5

	CmsgSetActiveMover uint16 = 0x026A
	CmsgLogoutRequest  uint16 = 0x004B
	SmsgLogoutResponse uint16 = 0x004C
	SmsgLogoutComplete uint16 = 0x004D

	SmsgWardenData uint16 = 0x02E6
	CmsgWardenData uint16 = 0x02E7

	CmsgReadyForAccountDataTimes uint16 = 0x04FF
	SmsgAccountDataTimes         uint16 = 0x0209
	CmsgRealmSplit               uint16 = 0x038C
	SmsgRealmSplit               uint16 = 0x038B

	SmsgAddonInfo     uint16 = 0x02EF
	SmsgTutorialFlags uint16 = 0x00FD
	SmsgCancelCombat  uint16 = 0x014E

	CmsgCompleteCinematic   uint16 = 0x00FC
	CmsgNextCinematicCamera uint16 = 0x00FB

	// Combat opcodes
	CmsgAttackSwing         uint16 = 0x0141
	CmsgAttackStop          uint16 = 0x0142
	SmsgAttackStart         uint16 = 0x0143
	SmsgAttackStop          uint16 = 0x0144
	SmsgAttackSwingNotInRange uint16 = 0x0145
	SmsgAttackSwingBadFacing  uint16 = 0x0146
	SmsgAttackSwingDeadTarget uint16 = 0x0148
	SmsgAttackSwingCantAttack uint16 = 0x0149
	SmsgAttackerStateUpdate uint16 = 0x014A

	// Spell opcodes
	CmsgCastSpell     uint16 = 0x012E
	SmsgSpellStart    uint16 = 0x0131
	SmsgSpellGo       uint16 = 0x0132
	SmsgSpellFailure  uint16 = 0x0133
	SmsgSpellCooldown uint16 = 0x0134
	SmsgCastFailed    uint16 = 0x0130  // server error for failed casts (may be sent on bad target)
	SmsgInitialSpells uint16 = 0x012A
	SmsgCooldownEvent uint16 = 0x0135
	SmsgClearCooldown uint16 = 0x01DE
	CmsgCancelCast    uint16 = 0x012F
	CmsgCancelAura    uint16 = 0x0133

	// Update object opcodes
	SmsgUpdateObject         uint16 = 0x00A9
	SmsgDestroyObject        uint16 = 0x00AA
	SmsgCompressedUpdate     uint16 = 0x01F6
	SmsgMonsterMove          uint16 = 0x00DD
	SmsgMonsterMoveTransport uint16 = 0x02AE
	SmsgCompressedMoves      uint16 = 0x02FB
	SmsgMultipleMoves        uint16 = 0x051E

	// Loot opcodes
	CmsgLoot                uint16 = 0x015D
	CmsgLootMoney           uint16 = 0x015E
	CmsgLootRelease         uint16 = 0x015F
	SmsgLootResponse        uint16 = 0x0160
	CmsgAutostoreLootItem   uint16 = 0x0108
	SmsgLootRemoved         uint16 = 0x0162
	SmsgLootReleaseResponse uint16 = 0x0161

	// Item / inventory opcodes
	CmsgAutoequipItem uint16 = 0x010A

	// Quest opcodes
	CmsgQuestgiverHello         uint16 = 0x0184
	SmsgQuestgiverQuestList     uint16 = 0x0185
	CmsgQuestgiverAcceptQuest   uint16 = 0x0189
	CmsgQuestgiverCompleteQuest uint16 = 0x018A
	CmsgQuestgiverChooseReward  uint16 = 0x018E
	SmsgQuestgiverQuestDetails  uint16 = 0x0188
	SmsgQuestgiverRequestItems  uint16 = 0x018B
	SmsgQuestgiverOfferReward   uint16 = 0x018D
	SmsgQuestupdateComplete     uint16 = 0x01A8

	// Group opcodes
	CmsgGroupInvite        uint16 = 0x006E
	SmsgGroupInvite        uint16 = 0x006F
	CmsgGroupAccept        uint16 = 0x0072
	CmsgGroupSetLeader     uint16 = 0x0078
	SmsgGroupList          uint16 = 0x007D
	SmsgPartyCommandResult uint16 = 0x007F

	// Level up
	SmsgLevelupInfo uint16 = 0x01D4

	// Death handling
	CmsgRepopRequest  uint16 = 0x015A
	CmsgReclaimCorpse uint16 = 0x01D2

	// Target / selection
	CmsgSetSelection uint16 = 0x013D

	// Misc
	SmsgNewWorld        uint16 = 0x003E
	SmsgTransferPending uint16 = 0x003F
	// MsgMoveWorldportAck (CMSG_WORLD_TELEPORT response / WORLDPORT_ACK) — 0x00DC
	MsgMoveWorldportAck uint16 = 0x00DC
	SmsgAiReaction      uint16 = 0x013C
	SmsgPowerUpdate     uint16 = 0x0480

	// Speed change
	SmsgForceRunSpeedChange    uint16 = 0x00E2
	CmsgForceRunSpeedChangeAck uint16 = 0x00E3

	// Name query
	CmsgNameQuery             uint16 = 0x0050
	SmsgNameQueryResponse     uint16 = 0x0051
	CmsgCreatureQuery         uint16 = 0x0060
	SmsgCreatureQueryResponse uint16 = 0x0061

	// Set sheathed
	CmsgSetSheathed uint16 = 0x01E0

	// Sheath states
	SheathStateUnarmed uint32 = 0
	SheathStateMelee   uint32 = 1
	SheathStateRanged  uint32 = 2

	// Aura updates (SMSG_AURA_UPDATE / SMSG_AURA_UPDATE_ALL).
	// These are the primary way AC delivers per-slot aura info (not just value fields).
	// See AzerothCore SpellAuras.cpp AuraApplication::BuildUpdatePacket.
	SmsgAuraUpdate    uint16 = 0x0496
	SmsgAuraUpdateAll uint16 = 0x0495
)

// Chat message types
const (
	ChatMsgSay   uint32 = 0x01
	ChatMsgParty uint32 = 0x02
	ChatMsgRaid  uint32 = 0x03
	ChatMsgGuild uint32 = 0x04
	ChatMsgYell  uint32 = 0x06
	ChatMsgEmote uint32 = 0x08
)

// Laugh emote ID
const (
	TextEmoteLaugh uint32 = 0x3C // 60 = LAUGH
)

// Language
const (
	LangUniversal uint32 = 0x00
	LangOrcish    uint32 = 0x01
	LangCommon    uint32 = 0x07
)

// Movement speeds from AzerothCore Unit.cpp (yards per second)
const (
	BaseSpeedWalk     float32 = 2.5
	BaseSpeedRun      float32 = 7.0
	BaseSpeedRunBack  float32 = 4.5
	BaseSpeedSwim     float32 = 4.722222
	BaseSpeedSwimBack float32 = 2.5
	BaseSpeedFlight   float32 = 7.0
)

// Movement flags
const (
	MoveFlagNone        uint32 = 0x00000000
	MoveFlagForward     uint32 = 0x00000001
	MoveFlagBackward    uint32 = 0x00000002
	MoveFlagStrafeLeft  uint32 = 0x00000004
	MoveFlagStrafeRight uint32 = 0x00000008
	MoveFlagTurnLeft    uint32 = 0x00000010
	MoveFlagTurnRight   uint32 = 0x00000020
	MoveFlagFalling     uint32 = 0x00001000
	MoveFlagSwimming    uint32 = 0x00200000
	MoveFlagFlying      uint32 = 0x02000000
)

// ObjectType constants
const (
	ObjectTypeObject    uint8 = 0
	ObjectTypeItem      uint8 = 1
	ObjectTypeContainer uint8 = 2
	ObjectTypeUnit      uint8 = 3
	ObjectTypePlayer    uint8 = 4
	ObjectTypeGameObj   uint8 = 5
	ObjectTypeDynObj    uint8 = 6
	ObjectTypeCorpse    uint8 = 7
)

// Update types
const (
	UpdateTypeValues            uint8 = 0
	UpdateTypeMovement          uint8 = 1
	UpdateTypeCreateObject      uint8 = 2
	UpdateTypeCreateObject2     uint8 = 3
	UpdateTypeOutOfRangeObjects uint8 = 4
	UpdateTypeNearObjects       uint8 = 5
)

// Unit fields from AzerothCore UpdateFields.h (OBJECT_END = 0x0006)
const (
	ObjectFieldGUID          = 0x0000 // 2 uint32s
	ObjectFieldType          = 0x0002
	UnitFieldEntry           = 0x0003 // OBJECT_FIELD_ENTRY
	UnitFieldTarget          = 0x0012 // OBJECT_END + 0x000C = 0x0012 (2 uint32s = GUID)
	UnitFieldBytes0          = 0x0017 // OBJECT_END + 0x0011 = 0x0017 (race, class, gender, powertype)
	UnitFieldHealth          = 0x0018 // OBJECT_END + 0x0012
	UnitFieldPower1          = 0x0019 // OBJECT_END + 0x0013 (mana/rage/energy)
	UnitFieldMaxHealth       = 0x0020 // OBJECT_END + 0x001A
	UnitFieldMaxPower1       = 0x0021 // OBJECT_END + 0x001B
	UnitFieldLevel           = 0x0036 // OBJECT_END + 0x0030
	UnitFieldFaction         = 0x0037 // OBJECT_END + 0x0031 (faction template)
	UnitFieldFlags           = 0x003B // OBJECT_END + 0x0035
	UnitFieldFlags2          = 0x003C // OBJECT_END + 0x0036
	UnitFieldDisplayID       = 0x0043 // OBJECT_END + 0x003D
	UnitFieldNativeDisplayID = 0x0044 // OBJECT_END + 0x003E
	UnitDynamicFlags         = 0x004F // OBJECT_END + 0x0049
	UnitNPCFlags             = 0x0052 // OBJECT_END + 0x004C

	// UNIT_FIELD_AURASTATE (bitmask of aura states). Fast path hint only;
	// authoritative aura list comes from SMSG_AURA_UPDATE* packets.
	UnitFieldAuraState = 0x003C // OBJECT_END + 0x0036 (typical); adjust if your build differs
)

// From AC SharedDefines.h - dyn flags for dead/lootable corpses
const (
	UnitDynflagLootable = 0x0001
	UnitDynflagDead     = 0x0020
)

// UnitFlags
const (
	UnitFlagInCombat      uint32 = 0x00080000
	UnitFlagNotAttackable   uint32 = 0x00000002
	UnitFlagNotAttackable1  uint32 = 0x00000080
	UnitFlagImmuneToPC      uint32 = 0x00000100
	UnitFlagNotAttackable2  uint32 = 0x00010000
	UnitFlagNotSelectable   uint32 = 0x02000000
	UnitFlagDisarmed      uint32 = 0x00200000
	UnitFlagPacified      uint32 = 0x00020000
	UnitFlagStunned       uint32 = 0x00040000
	UnitFlagDead          uint32 = 0x20000000
	UnitFlagTaxiFlight    uint32 = 0x00100000  // from AC UnitDefines.h - must skip for attack (see _IsValidAttackTarget)
)

// CharEnumEntry holds character data from SMSG_CHAR_ENUM
type CharEnumEntry struct {
	GUID  uint64
	Name  string
	Race  uint8
	Class uint8
	Level uint8
}

// WorldObject represents a tracked object in the game world
type WorldObject struct {
	GUID   uint64
	TypeID uint8 // ObjectType*
	Entry  uint32
	Values map[uint16]uint32

	// valuesMu protects concurrent access to Values (and derived) from the packet read goroutine
	// and the bot AI goroutine(s). Prevents data races on map and stale health/flags views
	// (e.g. seeing positive health on actually dead mobs killed by others).
	valuesMu sync.RWMutex

	// Position data
	PosX, PosY, PosZ float32
	Orientation      float32
	MapID            uint32

	// Last time we received a position update for this object (MonsterMove or movement block).
	// Used to detect stale positions for targets (e.g. mob wandered but we stopped receiving updates).
	LastPosUpdate time.Time

	// Last time we received *any* update for this object (values, movement, etc.).
	// Indicates the server still considers it visible to us.
	LastSeen time.Time

	// Movement interpolation
	DestX, DestY, DestZ    float32
	IsMoving               bool
	MoveStartTime          time.Time
	MoveDuration           time.Duration
	StartX, StartY, StartZ float32

	// Derived convenience fields
	Name     string
	IsPlayer bool

	// Aura state populated from SMSG_AURA_UPDATE* packets (and AURASTATE values).
	// Protected by aurasMu. We track spell IDs only for HasAura / GetActiveAuras
	// needs of testkit (and bot Lua). Full slot/caster/duration info can be added later.
	aurasMu     sync.RWMutex
	activeAuras map[uint32]struct{}
	// slot -> current spellID in that aura slot. Enables correct removal when server
	// sends spellID=0 for a previously occupied slot (SMSG_AURA_UPDATE).
	auraSlots map[uint8]uint32
}

// Clone returns a deep copy of the WorldObject. This gives callers (e.g. AI logic)
// their own private version of the world state snapshot, reducing races with
// concurrent packet updates to the live cache.
func (o *WorldObject) Clone() *WorldObject {
	if o == nil {
		return nil
	}
	o.valuesMu.RLock()
	vals := make(map[uint16]uint32, len(o.Values))
	for k, v := range o.Values {
		vals[k] = v
	}
	o.valuesMu.RUnlock()

	// Snapshot the *current* estimated pose (not the spline segment start).
	// AI/Lua chase paths use Pos*; if we copy raw Pos* for a moving unit they
	// path to the create/MONSTER_MOVE start instead of where the unit is now.
	ix, iy, iz := o.InterpolatedPosition()

	clone := &WorldObject{
		GUID:          o.GUID,
		TypeID:        o.TypeID,
		Entry:         o.Entry,
		Values:        vals,
		PosX:          ix,
		PosY:          iy,
		PosZ:          iz,
		Orientation:   o.Orientation,
		MapID:         o.MapID,
		LastPosUpdate: o.LastPosUpdate,
		LastSeen:      o.LastSeen,
		// Keep spline endpoints so callers can still lead-target if desired.
		DestX:         o.DestX,
		DestY:         o.DestY,
		DestZ:         o.DestZ,
		IsMoving:      o.IsMoving,
		MoveStartTime: o.MoveStartTime,
		MoveDuration:  o.MoveDuration,
		StartX:        o.StartX,
		StartY:        o.StartY,
		StartZ:        o.StartZ,
		Name:          o.Name,
		IsPlayer:      o.IsPlayer,
	}

	// Copy aura snapshot (under read lock).
	o.aurasMu.RLock()
	if len(o.activeAuras) > 0 {
		clone.activeAuras = make(map[uint32]struct{}, len(o.activeAuras))
		for id := range o.activeAuras {
			clone.activeAuras[id] = struct{}{}
		}
	}
	if len(o.auraSlots) > 0 {
		clone.auraSlots = make(map[uint8]uint32, len(o.auraSlots))
		for slot, id := range o.auraSlots {
			clone.auraSlots[slot] = id
		}
	}
	o.aurasMu.RUnlock()

	return clone
}

// value returns a field under lock to avoid data races with packet updates.
func (o *WorldObject) value(field uint16) uint32 {
	o.valuesMu.RLock()
	defer o.valuesMu.RUnlock()
	return o.Values[field]
}

// Value returns a raw update field value (thread-safe). For bot AI and advanced
// consumers. Prefer typed helpers (Health(), IsAlive(), Level(), HasAura()...) when possible.
func (o *WorldObject) Value(field uint16) uint32 {
	return o.value(field)
}

// setValue sets under lock.
func (o *WorldObject) setValue(field uint16, v uint32) {
	o.valuesMu.Lock()
	defer o.valuesMu.Unlock()
	o.Values[field] = v
}

// Health returns the current health of the object, or 0 if unknown.
func (o *WorldObject) Health() uint32 {
	return o.value(UnitFieldHealth)
}

// MaxHealth returns the maximum health of the object, or 0 if unknown.
func (o *WorldObject) MaxHealth() uint32 {
	return o.value(UnitFieldMaxHealth)
}

// Level returns the level of the unit, or 0 if unknown.
func (o *WorldObject) Level() uint32 {
	return o.value(UnitFieldLevel)
}

// IsAlive returns true if the object appears to be alive.
func (o *WorldObject) IsAlive() bool {
	flags := o.value(UnitFieldFlags)
	if flags&UnitFlagDead != 0 {
		return false
	}
	dynFlags := o.value(UnitDynamicFlags)
	if dynFlags&(UnitDynflagDead|UnitDynflagLootable) != 0 {
		return false
	}
	h := o.Health()
	mh := o.MaxHealth()
	// If we know the max health (object data received) and current is 0, it's dead
	if mh > 0 && h == 0 {
		return false
	}
	if h > 0 {
		return true
	}
	return flags&UnitFlagDead == 0
}

// IsUnit returns true if the object type is unit (NPC) or player.
func (o *WorldObject) IsUnit() bool {
	return o.TypeID == ObjectTypeUnit || o.TypeID == ObjectTypePlayer
}

// InterpolatedPosition returns the estimated current position for a moving object.
// Pure read: does not mutate the object (safe under RLock / concurrent AI ticks).
// For create-time splines and MONSTER_MOVE, Pos* is the segment start; callers that
// path to Pos* without interpolating run to a stale "old" location.
func (o *WorldObject) InterpolatedPosition() (float32, float32, float32) {
	if o == nil {
		return 0, 0, 0
	}
	if !o.IsMoving || o.MoveDuration <= 0 {
		return o.PosX, o.PosY, o.PosZ
	}
	elapsed := time.Since(o.MoveStartTime)
	if elapsed >= o.MoveDuration {
		return o.DestX, o.DestY, o.DestZ
	}
	if elapsed <= 0 {
		return o.StartX, o.StartY, o.StartZ
	}
	t := float32(elapsed.Seconds()) / float32(o.MoveDuration.Seconds())
	ix := o.StartX + (o.DestX-o.StartX)*t
	iy := o.StartY + (o.DestY-o.StartY)*t
	iz := o.StartZ + (o.DestZ-o.StartZ)*t
	return ix, iy, iz
}

// HasKnownPosition reports whether we have ever received a real position for this object.
// Stubs created by aura packets (etc.) stay at 0,0,0 until a movement/create block arrives.
func (o *WorldObject) HasKnownPosition() bool {
	if o == nil {
		return false
	}
	if !o.LastPosUpdate.IsZero() {
		return true
	}
	// Non-zero coords without a timestamp still count (tests / synthetic objects).
	return o.PosX != 0 || o.PosY != 0 || o.PosZ != 0
}

// resetMovementInterp clears spline interpolation without touching Pos*/LastPosUpdate.
// Used on CREATE_OBJECT so a re-create never inherits a previous segment's Dest.
func (o *WorldObject) resetMovementInterp() {
	o.IsMoving = false
	o.MoveDuration = 0
	o.MoveStartTime = time.Time{}
	o.DestX, o.DestY, o.DestZ = 0, 0, 0
	o.StartX, o.StartY, o.StartZ = 0, 0, 0
}

// DistanceTo computes 3D distance to another position, using interpolated position for moving objects.
func (o *WorldObject) DistanceTo(x, y, z float32) float32 {
	ox, oy, oz := o.InterpolatedPosition()
	dx := ox - x
	dy := oy - y
	dz := oz - z
	return float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))
}

// HasAura reports whether spellID is currently applied to this object
// (populated from SMSG_AURA_UPDATE* packets and UNIT_FIELD_AURASTATE).
func (o *WorldObject) HasAura(spellID uint32) bool {
	if o == nil {
		return false
	}
	o.aurasMu.RLock()
	defer o.aurasMu.RUnlock()
	if o.activeAuras == nil {
		return false
	}
	_, ok := o.activeAuras[spellID]
	return ok
}

// GetActiveAuras returns a snapshot of active spell IDs on the object.
func (o *WorldObject) GetActiveAuras() []uint32 {
	if o == nil {
		return nil
	}
	o.aurasMu.RLock()
	defer o.aurasMu.RUnlock()
	if len(o.activeAuras) == 0 {
		return nil
	}
	out := make([]uint32, 0, len(o.activeAuras))
	for id := range o.activeAuras {
		out = append(out, id)
	}
	return out
}

// setAuraForSlot records that a slot now holds spellID (or 0 to clear the slot).
// Maintains both the set (for fast HasAura) and the slot map (for accurate removes).
func (o *WorldObject) setAuraForSlot(slot uint8, spellID uint32) {
	o.aurasMu.Lock()
	defer o.aurasMu.Unlock()
	if o.activeAuras == nil {
		o.activeAuras = make(map[uint32]struct{})
	}
	if o.auraSlots == nil {
		o.auraSlots = make(map[uint8]uint32)
	}

	// If this slot previously held a different spell, drop the old one from the set.
	if prev, had := o.auraSlots[slot]; had && prev != 0 && prev != spellID {
		delete(o.activeAuras, prev)
	}

	if spellID == 0 {
		delete(o.auraSlots, slot)
		return
	}
	o.auraSlots[slot] = spellID
	o.activeAuras[spellID] = struct{}{}
}

// removeAura removes a specific spell aura (best-effort fallback when slot info not available).
func (o *WorldObject) removeAura(spellID uint32) {
	o.aurasMu.Lock()
	defer o.aurasMu.Unlock()
	if o.activeAuras != nil {
		delete(o.activeAuras, spellID)
	}
	// Also clean any slot that references it (defensive).
	if o.auraSlots != nil {
		for s, id := range o.auraSlots {
			if id == spellID {
				delete(o.auraSlots, s)
			}
		}
	}
}

// clearAuras removes all aura state for this object (on destroy/out-of-range or login).
func (o *WorldObject) clearAuras() {
	o.aurasMu.Lock()
	defer o.aurasMu.Unlock()
	o.activeAuras = nil
	o.auraSlots = nil
}

// SpellCooldown tracks a spell's cooldown expiry
type SpellCooldown struct {
	SpellID   uint32
	ExpiresAt time.Time
}

// KnownSpell represents a spell the bot knows
type KnownSpell struct {
	SpellID uint32
	Active  bool
}

// LootItem represents an item available for looting
type LootItem struct {
	Index    uint8
	ItemID   uint32
	Quantity uint32
}

// WorldClient handles the world server protocol
type WorldClient struct {
	conn       net.Conn
	readBuf    *bufio.Reader // buffered for fewer syscalls on readPacket (critical for 500+ bots)
	username   string
	sessionKey []byte

	encryptServer *rc4.Cipher
	encryptClient *rc4.Cipher
	encrypted     bool

	sendMu sync.Mutex
	moveMu sync.Mutex

	charGUID                      uint64
	timeSyncCounter               uint32
	posX, posY, posZ, orientation float32
	mapID                         uint32
	moveSpeed                     float32
	currentMoveFlags              uint32 // last used for movement (to use in acks etc)

	// Cached packed GUID to avoid repeated allocation in hot send path
	packedGUID []byte
	decompBuf  []byte // reusable for compressed updates

	loginDone  chan struct{}
	logoutDone chan struct{}

	stopChan chan struct{}
	stopped  bool

	lastError error

	// For movement debug logging (use observer client to log other player's packets)
	movDebugMu   sync.Mutex
	lastMovDebug map[uint64]struct {
		ts      uint32
		x, y, z float32
		wall    time.Time
	}

	// Object tracking
	objectsMu sync.RWMutex
	objects   map[uint64]*WorldObject

	// Known spells
	spellsMu    sync.RWMutex
	knownSpells map[uint32]*KnownSpell

	// Cooldowns
	cooldownsMu sync.RWMutex
	cooldowns   map[uint32]*SpellCooldown

	// Combat state
	combatMu      sync.RWMutex
	inCombat      bool
	targetGUID    uint64
	attackingGUID uint64

	// Player stats
	statsMu   sync.RWMutex
	health    uint32
	maxHealth uint32
	power     uint32
	maxPower  uint32
	level     uint32

	// Protocol session phase (AC STATUS_* analogue for bots)
	phaseMu              sync.RWMutex
	phase                SessionPhase
	lastTimeSyncCounter  uint32
	timeSyncResponses    uint64
	worldportAcksSent    uint64
	teleportAcksSent     uint64

	// Callbacks
	logFunc            func(format string, args ...interface{})
	OnCharList         func(chars []CharEnumEntry)
	OnCharCreateResult func(data []byte)
	OnChatMessage      func(senderName, message string, msgType uint8)
	OnLevelUp          func(newLevel uint32)
	OnDeath            func()
	OnKill             func(victimGUID uint64)
	OnObjectUpdate     func(guid uint64, obj *WorldObject)
	OnObjectRemove     func(guid uint64)
	OnLootOpened  func(lootGUID uint64, items []LootItem)
	OnCombatStart func(attackerGUID, victimGUID uint64)
	OnCombatStop  func()
	// OnInvalidTarget is fired only for terminal rejects (dead / cant-attack).
	// Prefer OnAttackReject for full taxonomy (transient vs terminal).
	OnInvalidTarget func(victimGUID uint64)
	// OnAttackReject fires for every classified attack-side server reject so bots
	// can approach/face on transient errors instead of marking the unit dead.
	OnAttackReject func(r AttackReject)
	// OnPacket is invoked for every received world opcode (before handlers).
	OnPacket func(opcode uint16, data []byte)
	// OnPacketSend is invoked for every outbound world opcode (after build, before write).
	OnPacketSend func(opcode uint16, data []byte)
	// OnSpellCastResult reports SPELL_GO (success) or SPELL_FAILURE / CAST_FAILED.
	// failReason is 0 on success; otherwise the server reason byte when available.
	OnSpellCastResult func(spellID uint32, success bool, failReason uint8)
	// OnServerRelocate fires when the server forcibly moves the player (charge,
	// blink, knockback, monster-move spline on self). Bot must abort local paths
	// or updateMovement will write pre-relocate coords back over the new pose.
	OnServerRelocate func(x, y, z, o float32, reason string)
	// OnSessionPhase fires on every session phase transition.
	OnSessionPhase func(c SessionPhaseChange)
	// OnProtocolWarning fires when we send gameplay opcodes outside PhaseInWorld.
	OnProtocolWarning func(msg string, opcode uint16, phase SessionPhase)
	// TraceLogOpcodes enables noisy console dumps of combat/spell server opcodes.
	// Off by default; bots enable under --validation-mode --trace-packets.
	TraceLogOpcodes bool
}

// NewWorldClient creates a world client
func NewWorldClient(username string, sessionKey []byte, logFunc func(string, ...interface{})) *WorldClient {
	return &WorldClient{
		username:    strings.ToUpper(username),
		sessionKey:  sessionKey,
		loginDone:   make(chan struct{}),
		logoutDone:  make(chan struct{}),
		stopChan:    make(chan struct{}),
		logFunc:     logFunc,
		objects:     make(map[uint64]*WorldObject),
		knownSpells: make(map[uint32]*KnownSpell),
		cooldowns:   make(map[uint32]*SpellCooldown),
		moveSpeed:   BaseSpeedRun,
		lastMovDebug: make(map[uint64]struct {
			ts      uint32
			x, y, z float32
			wall    time.Time
		}),
	}
}

// Connect connects to the world server
func (w *WorldClient) Connect(worldAddr string) error {
	// Prefer loopback when realmlist points at this host's LAN IP (avoids hairpin NAT).
	worldAddr = strings.Replace(worldAddr, "192.168.178.110", "127.0.0.1", -1)

	var err error
	w.conn, err = net.DialTimeout("tcp", worldAddr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("connect to worldserver: %w", err)
	}

	// Apply TCP socket optimizations
	if tcp, ok := w.conn.(*net.TCPConn); ok {
		// Disable Nagle's algorithm - send packets immediately
		// Critical for game protocol with small frequent packets
		tcp.SetNoDelay(true)

		// Enable TCP keepalive to detect dead connections faster
		tcp.SetKeepAlive(true)
		tcp.SetKeepAlivePeriod(30 * time.Second)

		// Larger kernel buffers (especially important on macOS with limited ephemeral ports)
		// 128KB read/write buffers reduce syscalls and improve throughput
		tcp.SetReadBuffer(131072)
		tcp.SetWriteBuffer(131072)

		// Enable socket linger with 0 timeout for fast port reclamation
		// This forces RST instead of graceful FIN, reclaiming ports immediately
		// Critical for load testing with thousands of connections
		tcp.SetLinger(0)
	}

	// Use buffered reader to drastically reduce syscalls for header+payload reads.
	// 16KB is good for bursts of SMSG_COMPRESSED_UPDATE with many entities.
	w.readBuf = bufio.NewReaderSize(w.conn, 16384)

	return nil
}

// Run starts reading and processing packets. Blocks until connection closes.
func (w *WorldClient) Run() error {
	// Read SMSG_AUTH_CHALLENGE
	if err := w.handleAuthChallenge(); err != nil {
		w.conn.Close()
		return fmt.Errorf("auth challenge: %w", err)
	}

	// Run the packet reading loop (blocks until connection closes or error)
	w.readLoop()
	return w.lastError
}

func (w *WorldClient) handleAuthChallenge() error {
	// Read server header (unencrypted): size(2 big-endian) + opcode(2 little-endian)
	// Use readBuf to avoid syscall per small read.
	if w.conn != nil {
		_ = w.conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	}
	header := make([]byte, 4)
	if _, err := io.ReadFull(w.readBuf, header); err != nil {
		return err
	}

	size := binary.BigEndian.Uint16(header[0:2]) - 2
	opcode := binary.LittleEndian.Uint16(header[2:4])

	if opcode != SmsgAuthChallenge {
		return fmt.Errorf("expected SMSG_AUTH_CHALLENGE (0x%X), got 0x%X", SmsgAuthChallenge, opcode)
	}

	data := make([]byte, size)
	if _, err := io.ReadFull(w.readBuf, data); err != nil {
		return err
	}

	// Parse: uint32(1) + authSeed(4 bytes) + randomBytes(32 bytes)
	r := bytes.NewReader(data)
	var one uint32
	binary.Read(r, binary.LittleEndian, &one)

	authSeed := make([]byte, 4)
	r.Read(authSeed)

	// Generate client seed
	clientSeed := make([]byte, 4)
	rand.Read(clientSeed)

	// Compute digest: SHA1(username, t(zeros), clientSeed, authSeed, sessionKey)
	t := []byte{0, 0, 0, 0}
	h := sha1.New()
	h.Write([]byte(w.username))
	h.Write(t)
	h.Write(clientSeed)
	h.Write(authSeed)
	h.Write(w.sessionKey)
	digest := h.Sum(nil)

	// Build CMSG_AUTH_SESSION
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint32(12340)) // build
	binary.Write(buf, binary.LittleEndian, uint32(0))     // loginServerID
	buf.Write(append([]byte(w.username), 0))              // null-terminated username
	binary.Write(buf, binary.LittleEndian, uint32(0))     // loginServerType
	buf.Write(clientSeed)
	binary.Write(buf, binary.LittleEndian, uint32(0)) // regionID
	binary.Write(buf, binary.LittleEndian, uint32(0)) // battlegroundID
	binary.Write(buf, binary.LittleEndian, uint32(1)) // realmID
	binary.Write(buf, binary.LittleEndian, uint64(0)) // dosResponse
	buf.Write(digest)

	// Addon info (minimal - just count=0 compressed)
	addonData := buildMinimalAddonInfo()
	buf.Write(addonData)

	w.sendPacketUnencrypted(CmsgAuthSession, buf.Bytes())

	// Set up encryption
	w.setupEncryption()
	w.setPhase(PhaseConnected, "CMSG_AUTH_SESSION+crypto")

	return nil
}

func buildMinimalAddonInfo() []byte {
	// Addon count = 0, compressed with zlib
	// Raw: uint32(0) = 4 bytes saying 0 addons
	// We need to zlib compress: size(uint32) + compressed data
	// Actually let's just send the compressed addon data
	// The server reads: uint32(uncompressed_size) + zlib(addon_data)
	// If size is 0, server skips it
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint32(0)) // decompressed size = 0
	return buf.Bytes()
}

func (w *WorldClient) setupEncryption() {
	serverEncryptionKey := []byte{0xCC, 0x98, 0xAE, 0x04, 0xE8, 0x97, 0xEA, 0xCA, 0x12, 0xDD, 0xC0, 0x93, 0x42, 0x91, 0x53, 0x57}
	clientEncryptionKey := []byte{0xC2, 0xB3, 0x72, 0x3C, 0xC6, 0xAE, 0xD9, 0xB5, 0x34, 0x3C, 0x53, 0xEE, 0x2F, 0x43, 0x67, 0xCE}

	// Server key is used by server to encrypt, so we use it to decrypt
	sMac := hmac.New(sha1.New, serverEncryptionKey)
	sMac.Write(w.sessionKey)
	sKey := sMac.Sum(nil)

	// Client key is used by client to encrypt
	cMac := hmac.New(sha1.New, clientEncryptionKey)
	cMac.Write(w.sessionKey)
	cKey := cMac.Sum(nil)

	w.encryptServer, _ = rc4.NewCipher(sKey)
	w.encryptClient, _ = rc4.NewCipher(cKey)

	// Drop first 1024 bytes (ARC4-drop1024)
	drop := make([]byte, 1024)
	w.encryptServer.XORKeyStream(drop, drop)
	drop = make([]byte, 1024)
	w.encryptClient.XORKeyStream(drop, drop)

	w.encrypted = true
}

func (w *WorldClient) readLoop() {
	defer func() {
		if !w.stopped {
			w.stopped = true
			close(w.stopChan)
		}
	}()

	for {
		opcode, data, err := w.readPacket()
		if err != nil {
			if !w.stopped {
				w.lastError = err
			}
			return
		}

		w.handlePacket(opcode, data)
	}
}

func (w *WorldClient) readPacket() (uint16, []byte, error) {
	// Use buffered reader for vastly fewer syscalls. Each ReadFull here
	// is now a fast buffer op in the common case; the OS read happens
	// in larger chunks (16KB).
	if w.readBuf == nil {
		w.readBuf = bufio.NewReaderSize(w.conn, 16384)
	}

	// Set a read deadline so we don't block forever on dead/half-open connections.
	// The AI loop sends pings every 30s; give enough headroom for lag.
	if w.conn != nil {
		_ = w.conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	}

	var hdr [5]byte // max header size we ever need (large encrypted)

	if w.encrypted {
		// Encrypted header: 4 or 5 bytes.
		if _, err := io.ReadFull(w.readBuf, hdr[:4]); err != nil {
			return 0, nil, fmt.Errorf("read header: %w", err)
		}
		w.encryptServer.XORKeyStream(hdr[:4], hdr[:4])

		isLarge := hdr[0]&0x80 != 0
		var size uint32
		var opcode uint16
		if isLarge {
			if _, err := io.ReadFull(w.readBuf, hdr[4:5]); err != nil {
				return 0, nil, err
			}
			w.encryptServer.XORKeyStream(hdr[4:5], hdr[4:5])
			size = (uint32(hdr[0]&0x7F) << 16) | (uint32(hdr[1]) << 8) | uint32(hdr[2])
			opcode = binary.LittleEndian.Uint16(hdr[3:5])
		} else {
			size = (uint32(hdr[0]) << 8) | uint32(hdr[1])
			opcode = binary.LittleEndian.Uint16(hdr[2:4])
		}

		if size < 2 {
			return opcode, nil, nil
		}
		payloadSize := int(size) - 2
		if payloadSize > 10*1024*1024 {
			return 0, nil, fmt.Errorf("packet too large: %d", payloadSize)
		}
		if payloadSize == 0 {
			return opcode, nil, nil
		}

		// Extend deadline for the (potentially large) payload.
		if w.conn != nil {
			_ = w.conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		}
		data := make([]byte, payloadSize)
		if _, err := io.ReadFull(w.readBuf, data); err != nil {
			return 0, nil, err
		}
		return opcode, data, nil
	}

	// Unencrypted (only auth phase)
	if w.conn != nil {
		_ = w.conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	}
	if _, err := io.ReadFull(w.readBuf, hdr[:1]); err != nil {
		return 0, nil, fmt.Errorf("read first byte: %w", err)
	}

	isLarge := hdr[0]&0x80 != 0
	if isLarge {
		if _, err := io.ReadFull(w.readBuf, hdr[1:5]); err != nil { // 4 more bytes
			return 0, nil, err
		}
		size := (uint32(hdr[0]&0x7F) << 16) | (uint32(hdr[1]) << 8) | uint32(hdr[2])
		opcode := binary.LittleEndian.Uint16(hdr[3:5])

		if size < 2 {
			return opcode, nil, nil
		}
		payloadSize := int(size) - 2
		if payloadSize == 0 {
			return opcode, nil, nil
		}
		data := make([]byte, payloadSize)
		if _, err := io.ReadFull(w.readBuf, data); err != nil {
			return 0, nil, err
		}
		return opcode, data, nil
	}

	// Normal unencrypted header
	if _, err := io.ReadFull(w.readBuf, hdr[1:4]); err != nil { // 1 size + 2 opcode
		return 0, nil, err
	}
	size := (uint32(hdr[0]) << 8) | uint32(hdr[1])
	opcode := binary.LittleEndian.Uint16(hdr[2:4])

	if size < 2 {
		return opcode, nil, nil
	}
	payloadSize := int(size) - 2
	if payloadSize == 0 {
		return opcode, nil, nil
	}
	if w.conn != nil {
		_ = w.conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	}
	data := make([]byte, payloadSize)
	if _, err := io.ReadFull(w.readBuf, data); err != nil {
		return 0, nil, err
	}
	return opcode, data, nil
}

func (w *WorldClient) sendPacketUnencrypted(opcode uint16, data []byte) error {
	w.checkOutboundPhase(opcode)
	if w.OnPacketSend != nil {
		w.OnPacketSend(opcode, data)
	}
	header := make([]byte, 6)
	binary.BigEndian.PutUint16(header[0:2], uint16(len(data)+4))
	binary.LittleEndian.PutUint32(header[2:6], uint32(opcode))

	w.sendMu.Lock()
	defer w.sendMu.Unlock()

	_, err := w.conn.Write(append(header, data...))
	return err
}

func (w *WorldClient) sendPacket(opcode uint16, data []byte) error {
	w.checkOutboundPhase(opcode)
	if w.OnPacketSend != nil {
		w.OnPacketSend(opcode, data)
	}
	// Client sends: size(2, big-endian) + opcode(4, little-endian)
	header := make([]byte, 6)
	binary.BigEndian.PutUint16(header[0:2], uint16(len(data)+4))
	binary.LittleEndian.PutUint32(header[2:6], uint32(opcode))

	w.sendMu.Lock()
	defer w.sendMu.Unlock()

	if w.conn == nil {
		return fmt.Errorf("not connected")
	}

	if w.encrypted {
		w.encryptClient.XORKeyStream(header, header)
	}

	packet := append(header, data...)
	_, err := w.conn.Write(packet)
	return err
}

func (w *WorldClient) handlePacket(opcode uint16, data []byte) {
	// Generic callback for raw packet access
	if w.OnPacket != nil {
		w.OnPacket(opcode, data)
	}

	// Console hex dumps are opt-in (TraceLogOpcodes). Default off to avoid drowning
	// load tests; validation timeline uses OnPacket JSONL instead.
	if w.TraceLogOpcodes && IsHighValueTraceOpcode(opcode) {
		short := data
		if len(short) > 32 {
			short = short[:32]
		}
		w.log("SMSG %s (0x%04X) len=%d data=% x", OpcodeName(opcode), opcode, len(data), short)
	}

	switch opcode {
	case SmsgAuthResponse:
		w.handleAuthResponse(data)
	case SmsgCharEnum:
		w.handleCharEnum(data)
	case SmsgCharCreate:
		if w.OnCharCreateResult != nil {
			w.OnCharCreateResult(data)
		}
	case SmsgLoginVerifyWorld:
		w.handleLoginVerifyWorld(data)
	case SmsgTimeSyncReq:
		w.handleTimeSyncReq(data)
	case SmsgPong:
		// Pong received
	case SmsgWardenData:
		// Ignore warden
	case SmsgLogoutResponse:
		w.handleLogoutResponse(data)
	case SmsgLogoutComplete:
		w.handleLogoutComplete()
	case SmsgCancelCombat:
		w.handleCancelCombat()

	// Combat
	case SmsgAttackStart:
		w.handleAttackStart(data)
	case SmsgAttackStop:
		w.handleAttackStop(data)
	case SmsgAttackSwingNotInRange:
		w.handleAttackSwingError(data, RejectReasonNotInRange, SmsgAttackSwingNotInRange)
	case SmsgAttackSwingBadFacing:
		w.handleAttackSwingError(data, RejectReasonBadFacing, SmsgAttackSwingBadFacing)
	case SmsgAttackSwingDeadTarget:
		w.handleAttackSwingError(data, RejectReasonDeadTarget, SmsgAttackSwingDeadTarget)
	case SmsgAttackSwingCantAttack:
		w.handleAttackSwingError(data, RejectReasonCantAttack, SmsgAttackSwingCantAttack)
	case SmsgAttackerStateUpdate:
		w.handleAttackerStateUpdate(data)

	// Spells
	case SmsgInitialSpells:
		w.handleInitialSpells(data)
	case SmsgSpellGo:
		w.handleSpellGo(data)
	case SmsgSpellFailure:
		w.handleSpellFailure(data)
	case SmsgCastFailed:
		w.handleCastFailed(data)
	case SmsgSpellCooldown:
		w.handleSpellCooldown(data)
	case SmsgCooldownEvent:
		w.handleCooldownEvent(data)
	case SmsgClearCooldown:
		w.handleClearCooldown(data)

	// Auras (primary delivery mechanism for aura state on AC 3.3.5a)
	case SmsgAuraUpdate:
		w.handleAuraUpdate(data)
	case SmsgAuraUpdateAll:
		w.handleAuraUpdateAll(data)

	// Object updates
	case SmsgUpdateObject:
		w.handleUpdateObject(data)
	case SmsgCompressedUpdate:
		w.handleCompressedUpdateObject(data)
	case SmsgDestroyObject:
		w.handleDestroyObject(data)
	case SmsgMonsterMove:
		w.handleMonsterMove(data)
	case SmsgMonsterMoveTransport:
		w.handleMonsterMoveTransport(data)
	case SmsgCompressedMoves:
		w.handleCompressedMoves(data)
	case SmsgMultipleMoves:
		w.handleMultipleMoves(data)
	case MsgMoveTeleportAck:
		w.handleMoveTeleportAck(data)
	case SmsgMoveKnockBack:
		w.handleMoveKnockBack(data)
	case SmsgMoveTeleport:
		w.handleMoveTeleport(data)

	// Direct movement packets (for players and possibly relayed for some units/creatures)
	case MsgMoveStartForward, MsgMoveStop, MsgMoveHeartbeat, MsgMoveSetFacing,
		MsgMoveStartBackward, MsgMoveJump, MsgMoveFallLand,
		MsgMoveStartStrafeLeft, MsgMoveStartStrafeRight, MsgMoveStopStrafe,
		MsgMoveStartTurnLeft, MsgMoveStartTurnRight, MsgMoveStopTurn:
		w.handleMovementPacket(opcode, data)

	// Loot
	case SmsgLootResponse:
		w.handleLootResponse(data)

	// Chat
	case SmsgMessageChat:
		w.handleChatMessage(data)

	// Level up
	case SmsgLevelupInfo:
		w.handleLevelUp(data)

	// Power
	case SmsgPowerUpdate:
		w.handlePowerUpdate(data)

	case SmsgNewWorld:
		w.handleNewWorld(data)
	case SmsgTransferPending:
		w.log("Transfer pending received")

	// Speed
	case SmsgForceRunSpeedChange:
		w.handleForceSpeedChange(data)

	default:
		// Ignore most unhandled opcodes
	}
}

func (w *WorldClient) handleAuthResponse(data []byte) {
	if len(data) == 0 {
		w.log("Auth response: empty data")
		return
	}

	result := data[0]
	if result != 12 { // AUTH_OK = 12
		w.log("Auth response failed with code %d", result)
		w.lastError = fmt.Errorf("world auth failed with code %d", result)
		return
	}

	w.log("World auth successful")
	w.setPhase(PhaseAuthed, "SMSG_AUTH_RESPONSE")
}

func (w *WorldClient) handleCharEnum(data []byte) {
	if len(data) == 0 {
		return
	}

	r := bytes.NewReader(data)
	count, _ := r.ReadByte()

	w.log("Character enum: %d characters", count)

	var chars []CharEnumEntry
	for i := byte(0); i < count; i++ {
		entry := w.parseCharEnumEntry(r)
		if entry != nil {
			chars = append(chars, *entry)
		}
	}

	if w.OnCharList != nil {
		w.OnCharList(chars)
	}
}

func (w *WorldClient) parseCharEnumEntry(r *bytes.Reader) *CharEnumEntry {
	entry := &CharEnumEntry{}

	if err := binary.Read(r, binary.LittleEndian, &entry.GUID); err != nil {
		return nil
	}

	// Read null-terminated name
	var nameBytes []byte
	for {
		b, err := r.ReadByte()
		if err != nil || b == 0 {
			break
		}
		nameBytes = append(nameBytes, b)
	}
	entry.Name = string(nameBytes)

	// race(1) + class(1) + gender(1) + skin(1) + face(1) + hairStyle(1) + hairColor(1) + facialHair(1) + level(1)
	var race, class, gender, skin, face, hairStyle, hairColor, facialHair, level uint8
	binary.Read(r, binary.LittleEndian, &race)
	binary.Read(r, binary.LittleEndian, &class)
	binary.Read(r, binary.LittleEndian, &gender)
	binary.Read(r, binary.LittleEndian, &skin)
	binary.Read(r, binary.LittleEndian, &face)
	binary.Read(r, binary.LittleEndian, &hairStyle)
	binary.Read(r, binary.LittleEndian, &hairColor)
	binary.Read(r, binary.LittleEndian, &facialHair)
	binary.Read(r, binary.LittleEndian, &level)

	entry.Race = race
	entry.Class = class
	entry.Level = level

	// zone(4) + map(4) + x(4) + y(4) + z(4)
	skip := make([]byte, 20)
	r.Read(skip)

	// guildID(4)
	skip = make([]byte, 4)
	r.Read(skip)

	// charFlags(4)
	skip = make([]byte, 4)
	r.Read(skip)

	// customizationFlags(4) (at_login flags)
	skip = make([]byte, 4)
	r.Read(skip)

	// firstLogin(1)
	r.ReadByte()

	// petDisplayID(4) + petLevel(4) + petFamily(4)
	skip = make([]byte, 12)
	r.Read(skip)

	// Equipment: 23 slots * (displayID(4) + inventoryType(1) + enchantAura(4)) = 23 * 9 = 207
	skip = make([]byte, 23*9)
	r.Read(skip)

	return entry
}

func (w *WorldClient) handleLoginVerifyWorld(data []byte) {
	// Parse: mapID(4) + posX(4) + posY(4) + posZ(4) + orientation(4)
	if len(data) >= 20 {
		r := bytes.NewReader(data)
		var mapID uint32
		binary.Read(r, binary.LittleEndian, &mapID)
		binary.Read(r, binary.LittleEndian, &w.posX)
		binary.Read(r, binary.LittleEndian, &w.posY)
		binary.Read(r, binary.LittleEndian, &w.posZ)
		binary.Read(r, binary.LittleEndian, &w.orientation)
		w.mapID = mapID
		w.log("Login verified map=%d pos=(%.1f,%.1f,%.1f)", mapID, w.posX, w.posY, w.posZ)
	} else {
		w.log("Login verified - character is in world!")
	}
	w.setPhase(PhaseInWorld, "SMSG_LOGIN_VERIFY_WORLD")
	select {
	case <-w.loginDone:
	default:
		close(w.loginDone)
	}
}

func (w *WorldClient) handleTimeSyncReq(data []byte) {
	if len(data) < 4 {
		return
	}
	counter := binary.LittleEndian.Uint32(data[0:4])

	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, counter)
	binary.Write(buf, binary.LittleEndian, uint32(getMSTime()))

	w.phaseMu.Lock()
	w.lastTimeSyncCounter = counter
	w.timeSyncResponses++
	w.phaseMu.Unlock()

	w.sendPacket(CmsgTimeSyncResp, buf.Bytes())
}

func (w *WorldClient) handleLogoutResponse(data []byte) {
	// SMSG_LOGOUT_RESPONSE: reason(4) + instant(1)
	if len(data) >= 5 {
		reason := binary.LittleEndian.Uint32(data[0:4])
		if reason != 0 {
			w.log("Logout denied with reason %d", reason)
		}
	}
}

func (w *WorldClient) handleLogoutComplete() {
	w.log("Logout complete")
	w.stopped = true
	w.setPhase(PhaseLogout, "SMSG_LOGOUT_COMPLETE")
	select {
	case <-w.logoutDone:
	default:
		close(w.logoutDone)
	}
}

// Public methods for bot actions

// RequestCharList sends CMSG_CHAR_ENUM
func (w *WorldClient) RequestCharList() error {
	return w.sendPacket(CmsgCharEnum, nil)
}

// SendReadyForAccountDataTimes sends CMSG_READY_FOR_ACCOUNT_DATA_TIMES
func (w *WorldClient) SendReadyForAccountDataTimes() error {
	return w.sendPacket(CmsgReadyForAccountDataTimes, nil)
}

// SendRealmSplit sends CMSG_REALM_SPLIT
func (w *WorldClient) SendRealmSplit() error {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint32(0xFFFFFFFF))
	return w.sendPacket(CmsgRealmSplit, buf.Bytes())
}

// CreateCharacter sends CMSG_CHAR_CREATE
func (w *WorldClient) CreateCharacter(name string, race, class, gender, skin, face, hairStyle, hairColor, facialHair, outfitID uint8) error {
	buf := new(bytes.Buffer)
	buf.Write(append([]byte(name), 0))
	buf.WriteByte(race)
	buf.WriteByte(class)
	buf.WriteByte(gender)
	buf.WriteByte(skin)
	buf.WriteByte(face)
	buf.WriteByte(hairStyle)
	buf.WriteByte(hairColor)
	buf.WriteByte(facialHair)
	buf.WriteByte(outfitID)
	return w.sendPacket(CmsgCharCreate, buf.Bytes())
}

// DeleteCharacter sends CMSG_CHAR_DELETE for the given GUID.
// Call RequestCharList before/after as needed to refresh the list.
// This should only be used when explicitly allowed (orchestrator or --delete-existing-chars).
func (w *WorldClient) DeleteCharacter(guid uint64) error {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, guid)
	return w.sendPacket(CmsgCharDelete, buf.Bytes())
}

// LoginCharacter sends CMSG_PLAYER_LOGIN
func (w *WorldClient) LoginCharacter(guid uint64) error {
	w.charGUID = guid
	// Precompute packed GUID once (hot path optimization for sends)
	w.packedGUID = make([]byte, 0, 9)
	packGUID := make([]byte, 9)
	packGUID[0] = 0
	size := 1
	g := guid
	for i := uint8(0); g != 0; i++ {
		if g&0xFF != 0 {
			packGUID[0] |= 1 << i
			packGUID[size] = byte(g & 0xFF)
			size++
		}
		g >>= 8
	}
	w.packedGUID = append(w.packedGUID, packGUID[:size]...)
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, guid)
	w.setPhase(PhaseLoading, "CMSG_PLAYER_LOGIN")
	return w.sendPacket(CmsgPlayerLogin, buf.Bytes())
}

// WaitForLogin waits until the SMSG_LOGIN_VERIFY_WORLD has arrived (signaling successful world entry
// after LoginCharacter). It is safe to call multiple times.
func (w *WorldClient) WaitForLogin(timeout time.Duration) error {
	select {
	case <-w.loginDone:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("timeout waiting for login verify world")
	}
}

// SetActiveMover sends CMSG_SET_ACTIVE_MOVER
func (w *WorldClient) SetActiveMover(guid uint64) error {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, guid)
	return w.sendPacket(CmsgSetActiveMover, buf.Bytes())
}

// SendChatMessage sends a chat message
func (w *WorldClient) SendChatMessage(msgType, lang uint32, message string) error {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, msgType)
	binary.Write(buf, binary.LittleEndian, lang)
	buf.Write(append([]byte(message), 0))
	return w.sendPacket(CmsgMessageChat, buf.Bytes())
}

// SendTextEmote sends a text emote (e.g., laugh)
func (w *WorldClient) SendTextEmote(emoteID uint32, targetGUID uint64) error {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, emoteID)
	binary.Write(buf, binary.LittleEndian, uint32(0xFFFFFFFF)) // emote num (not used)
	binary.Write(buf, binary.LittleEndian, targetGUID)
	return w.sendPacket(CmsgTextEmote, buf.Bytes())
}

// SendJump sends a jump movement packet
func (w *WorldClient) SendJump() error {
	buf := new(bytes.Buffer)
	writePackedGUID(buf, w.charGUID)
	binary.Write(buf, binary.LittleEndian, uint32(0x00001000)) // movementFlags: MOVEMENTFLAG_FALLING
	binary.Write(buf, binary.LittleEndian, uint16(0))          // movementFlags2
	binary.Write(buf, binary.LittleEndian, uint32(getMSTime()))
	// Position x, y, z, orientation - use stored position if available
	binary.Write(buf, binary.LittleEndian, w.posX)
	binary.Write(buf, binary.LittleEndian, w.posY)
	binary.Write(buf, binary.LittleEndian, w.posZ)
	binary.Write(buf, binary.LittleEndian, w.orientation)
	// fallTime
	binary.Write(buf, binary.LittleEndian, uint32(0))
	// Jump data (present because MOVEMENTFLAG_FALLING is set)
	binary.Write(buf, binary.LittleEndian, float32(7.96)) // zspeed
	binary.Write(buf, binary.LittleEndian, float32(0.0))  // sinAngle
	binary.Write(buf, binary.LittleEndian, float32(1.0))  // cosAngle
	binary.Write(buf, binary.LittleEndian, float32(0.0))  // xyspeed
	return w.sendPacket(MsgMoveJump, buf.Bytes())
}

// SendHeartbeat sends a movement heartbeat (stationary, no movement flags)
func (w *WorldClient) SendHeartbeat() error {
	w.moveMu.Lock()
	defer w.moveMu.Unlock()

	return w.sendHeartbeatLocked()
}

func (w *WorldClient) SendHeartbeatAt(x, y, z, o float32) error {
	return w.SendHeartbeatAtTime(x, y, z, o, time.Time{})
}

func (w *WorldClient) SendHeartbeatAtTime(x, y, z, o float32, at time.Time) error {
	w.moveMu.Lock()
	defer w.moveMu.Unlock()

	w.setPositionLocked(x, y, z, o)
	return w.sendHeartbeatLockedAt(movementMSTime(at))
}

func (w *WorldClient) sendHeartbeatLocked() error {
	return w.sendHeartbeatLockedAt(getMSTime())
}

func (w *WorldClient) sendHeartbeatLockedAt(ts uint32) error {
	w.currentMoveFlags = 0
	buf := new(bytes.Buffer)
	if len(w.packedGUID) > 0 {
		buf.Write(w.packedGUID)
	} else {
		writePackedGUID(buf, w.charGUID)
	}
	binary.Write(buf, binary.LittleEndian, uint32(0)) // movementFlags: none
	binary.Write(buf, binary.LittleEndian, uint16(0)) // movementFlags2
	binary.Write(buf, binary.LittleEndian, ts)
	binary.Write(buf, binary.LittleEndian, w.posX)
	binary.Write(buf, binary.LittleEndian, w.posY)
	binary.Write(buf, binary.LittleEndian, w.posZ)
	binary.Write(buf, binary.LittleEndian, w.orientation)
	binary.Write(buf, binary.LittleEndian, uint32(0)) // fallTime
	pkt := buf.Bytes()
	return w.sendPacket(MsgMoveHeartbeat, pkt)
}

// SendMovementHeartbeat sends a movement heartbeat while moving (with forward flag)
func (w *WorldClient) SendMovementHeartbeat() error {
	w.moveMu.Lock()
	defer w.moveMu.Unlock()

	return w.sendMovementHeartbeatLocked()
}

func (w *WorldClient) SendMovementHeartbeatAt(x, y, z, o float32) error {
	return w.SendMovementHeartbeatAtTime(x, y, z, o, time.Time{})
}

func (w *WorldClient) SendMovementHeartbeatAtTime(x, y, z, o float32, at time.Time) error {
	w.moveMu.Lock()
	defer w.moveMu.Unlock()

	w.setPositionLocked(x, y, z, o)
	return w.sendMovementHeartbeatLockedAt(movementMSTime(at))
}

func (w *WorldClient) sendMovementHeartbeatLocked() error {
	return w.sendMovementHeartbeatLockedAt(getMSTime())
}

func (w *WorldClient) sendMovementHeartbeatLockedAt(ts uint32) error {
	w.currentMoveFlags = MoveFlagForward
	buf := new(bytes.Buffer)
	if len(w.packedGUID) > 0 {
		buf.Write(w.packedGUID)
	} else {
		writePackedGUID(buf, w.charGUID)
	}
	binary.Write(buf, binary.LittleEndian, uint32(MoveFlagForward)) // movementFlags: forward
	binary.Write(buf, binary.LittleEndian, uint16(0))               // movementFlags2
	binary.Write(buf, binary.LittleEndian, ts)
	binary.Write(buf, binary.LittleEndian, w.posX)
	binary.Write(buf, binary.LittleEndian, w.posY)
	binary.Write(buf, binary.LittleEndian, w.posZ)
	binary.Write(buf, binary.LittleEndian, w.orientation)
	binary.Write(buf, binary.LittleEndian, uint32(0)) // fallTime
	pkt := buf.Bytes()
	//w.log("[MOV] send 0x%04X flags=0x%08X ts=%d pos=(%.3f,%.3f,%.3f) o=%.3f", MsgMoveHeartbeat, MoveFlagForward, ts, w.posX, w.posY, w.posZ, w.orientation)
	return w.sendPacket(MsgMoveHeartbeat, pkt)
}

// SendMovementHeartbeatWithJump sends a forward heartbeat that also has MOVEMENTFLAG_FALLING
// and includes the jump/fall trajectory data (zspeed, sin/cos angle, xyspeed).
// This matches packets seen in manual movement logs for the initial move while a small drop/fall is occurring.
func (w *WorldClient) SendMovementHeartbeatWithJump(fallTime uint32, zspeed, sinAngle, cosAngle, xyspeed float32) error {
	w.moveMu.Lock()
	defer w.moveMu.Unlock()

	return w.sendMovementHeartbeatWithJumpLocked(fallTime, zspeed, sinAngle, cosAngle, xyspeed)
}

func (w *WorldClient) SendMovementHeartbeatWithJumpAt(x, y, z, o float32, fallTime uint32, zspeed, sinAngle, cosAngle, xyspeed float32) error {
	return w.SendMovementHeartbeatWithJumpAtTime(x, y, z, o, time.Time{}, fallTime, zspeed, sinAngle, cosAngle, xyspeed)
}

func (w *WorldClient) SendMovementHeartbeatWithJumpAtTime(x, y, z, o float32, at time.Time, fallTime uint32, zspeed, sinAngle, cosAngle, xyspeed float32) error {
	w.moveMu.Lock()
	defer w.moveMu.Unlock()

	w.setPositionLocked(x, y, z, o)
	return w.sendMovementHeartbeatWithJumpLockedAt(movementMSTime(at), fallTime, zspeed, sinAngle, cosAngle, xyspeed)
}

func (w *WorldClient) sendMovementHeartbeatWithJumpLocked(fallTime uint32, zspeed, sinAngle, cosAngle, xyspeed float32) error {
	return w.sendMovementHeartbeatWithJumpLockedAt(getMSTime(), fallTime, zspeed, sinAngle, cosAngle, xyspeed)
}

func (w *WorldClient) sendMovementHeartbeatWithJumpLockedAt(ts uint32, fallTime uint32, zspeed, sinAngle, cosAngle, xyspeed float32) error {
	flags := MoveFlagForward | MoveFlagFalling
	w.currentMoveFlags = flags
	buf := new(bytes.Buffer)
	if len(w.packedGUID) > 0 {
		buf.Write(w.packedGUID)
	} else {
		writePackedGUID(buf, w.charGUID)
	}
	binary.Write(buf, binary.LittleEndian, uint32(flags))
	binary.Write(buf, binary.LittleEndian, uint16(0)) // flags2
	binary.Write(buf, binary.LittleEndian, ts)
	binary.Write(buf, binary.LittleEndian, w.posX)
	binary.Write(buf, binary.LittleEndian, w.posY)
	binary.Write(buf, binary.LittleEndian, w.posZ)
	binary.Write(buf, binary.LittleEndian, w.orientation)
	binary.Write(buf, binary.LittleEndian, fallTime)
	// Jump / falling data (present because MOVEMENTFLAG_FALLING is set)
	binary.Write(buf, binary.LittleEndian, zspeed)
	binary.Write(buf, binary.LittleEndian, sinAngle)
	binary.Write(buf, binary.LittleEndian, cosAngle)
	binary.Write(buf, binary.LittleEndian, xyspeed)
	return w.sendPacket(MsgMoveHeartbeat, buf.Bytes())
}

// writeMovementBody writes the movement info body (flags + pos etc) without leading packed GUID.
// Used for ACK packets which have their own prefix (guid + counter).
func (w *WorldClient) writeMovementBody(buf *bytes.Buffer, moveFlags uint32) {
	binary.Write(buf, binary.LittleEndian, moveFlags)
	binary.Write(buf, binary.LittleEndian, uint16(0)) // flags2
	binary.Write(buf, binary.LittleEndian, uint32(getMSTime()))
	binary.Write(buf, binary.LittleEndian, w.posX)
	binary.Write(buf, binary.LittleEndian, w.posY)
	binary.Write(buf, binary.LittleEndian, w.posZ)
	binary.Write(buf, binary.LittleEndian, w.orientation)
	binary.Write(buf, binary.LittleEndian, uint32(0)) // fallTime
	// For basic ground movement we omit transport/pitch/jump/spline extras.
}

func (w *WorldClient) sendForceSpeedAck(ackOpcode uint16, counter uint32, speed float32) {
	w.moveMu.Lock()
	defer w.moveMu.Unlock()

	buf := new(bytes.Buffer)
	writePackedGUID(buf, w.charGUID)
	binary.Write(buf, binary.LittleEndian, counter)
	w.writeMovementBody(buf, w.currentMoveFlags)
	binary.Write(buf, binary.LittleEndian, speed)
	w.sendPacket(ackOpcode, buf.Bytes())
}

func (w *WorldClient) handleForceSpeedChange(data []byte) {
	if len(data) < 10 {
		return
	}
	r := bytes.NewReader(data)
	_, err := readPackedGUID(r)
	if err != nil {
		return
	}
	var counter uint32
	binary.Read(r, binary.LittleEndian, &counter)

	// For RUN there is an extra uint8(0) before the float.
	// Read enough for u8 + f32 or just f32.
	var b [5]byte
	if n, err := io.ReadFull(r, b[:]); err == nil && n >= 5 {
		newspeed := math.Float32frombits(binary.LittleEndian.Uint32(b[1:5]))
		w.moveSpeed = newspeed
		w.log("Force speed change: speed=%.4f counter=%d", newspeed, counter)
		w.sendForceSpeedAck(CmsgForceRunSpeedChangeAck, counter, newspeed)
	} else if n, err := io.ReadFull(r, b[:4]); err == nil && n >= 4 {
		newspeed := math.Float32frombits(binary.LittleEndian.Uint32(b[:4]))
		w.moveSpeed = newspeed
		w.log("Force speed change: speed=%.4f counter=%d", newspeed, counter)
		w.sendForceSpeedAck(CmsgForceRunSpeedChangeAck, counter, newspeed)
	}
}

// SendPing sends CMSG_PING
func (w *WorldClient) SendPing(seq uint32) error {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, seq)
	binary.Write(buf, binary.LittleEndian, uint32(0)) // latency
	return w.sendPacket(CmsgPing, buf.Bytes())
}

// SendLogout sends CMSG_LOGOUT_REQUEST
func (w *WorldClient) SendLogout() error {
	return w.sendPacket(CmsgLogoutRequest, nil)
}

// WaitForLogout waits for the logout to complete
func (w *WorldClient) WaitForLogout(timeout time.Duration) error {
	select {
	case <-w.logoutDone:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("logout timeout")
	case <-w.stopChan:
		return w.lastError
	}
}

// CompleteCinematic sends CMSG_COMPLETE_CINEMATIC
func (w *WorldClient) CompleteCinematic() error {
	return w.sendPacket(CmsgCompleteCinematic, nil)
}

// NextCinematicCamera sends CMSG_NEXT_CINEMATIC_CAMERA
func (w *WorldClient) NextCinematicCamera() error {
	return w.sendPacket(CmsgNextCinematicCamera, nil)
}

// RepopRequest sends CMSG_REPOP_REQUEST (release spirit after death)
func (w *WorldClient) RepopRequest() error {
	w.log("Releasing spirit (CMSG_REPOP_REQUEST)")
	return w.sendPacket(CmsgRepopRequest, []byte{0}) // 1 byte expected by server
}

// ReclaimCorpse sends CMSG_RECLAIM_CORPSE (resurrect at corpse)
func (w *WorldClient) ReclaimCorpse() error {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, w.charGUID)
	w.log("Reclaiming corpse (CMSG_RECLAIM_CORPSE)")
	return w.sendPacket(CmsgReclaimCorpse, buf.Bytes())
}

// Close closes the connection and unblocks any pending reads.
func (w *WorldClient) Close() {
	w.sendMu.Lock()
	defer w.sendMu.Unlock()
	w.stopped = true
	if w.conn != nil {
		// Closing the conn will unblock any ReadFull in readPacket/readLoop.
		_ = w.conn.Close()
	}
}

// StopChan returns the channel that is closed when the connection stops
func (w *WorldClient) StopChan() <-chan struct{} {
	return w.stopChan
}

// Stop requests the client to stop. It also closes the underlying connection
// so that a blocked readPacket/readLoop unblocks promptly.
func (w *WorldClient) Stop() {
	w.stopped = true
	w.Close()
}

func (w *WorldClient) log(format string, args ...interface{}) {
	if w.logFunc != nil {
		w.logFunc(format, args...)
	}
}

func getMSTime() uint32 {
	return uint32(time.Now().UnixMilli() & 0xFFFFFFFF)
}

func movementMSTime(t time.Time) uint32 {
	if t.IsZero() {
		return getMSTime()
	}
	return uint32(t.UnixMilli() & 0xFFFFFFFF)
}

func crandRead(b []byte) {
	rand.Read(b)
}

// writePackedGUID writes a packed GUID to the buffer
func writePackedGUID(buf *bytes.Buffer, guid uint64) {
	packGUID := make([]byte, 9)
	packGUID[0] = 0
	size := 1

	for i := uint8(0); guid != 0; i++ {
		if guid&0xFF != 0 {
			packGUID[0] |= 1 << i
			packGUID[size] = byte(guid & 0xFF)
			size++
		}
		guid >>= 8
	}

	buf.Write(packGUID[:size])
}

// readPackedGUID reads a packed GUID from a reader
func readPackedGUID(r io.Reader) (uint64, error) {
	var maskBuf [1]byte
	if _, err := io.ReadFull(r, maskBuf[:]); err != nil {
		return 0, err
	}
	mask := maskBuf[0]
	if mask == 0 {
		return 0, nil
	}

	var guid uint64
	var b [1]byte
	for i := uint8(0); i < 8; i++ {
		if mask&(1<<i) != 0 {
			if _, err := io.ReadFull(r, b[:]); err != nil {
				return 0, err
			}
			guid |= uint64(b[0]) << (i * 8)
		}
	}
	return guid, nil
}

// ============================================================
// Combat methods
// ============================================================

// AttackSwing sends CMSG_ATTACKSWING to start melee attacking a target
func (w *WorldClient) AttackSwing(targetGUID uint64) error {
	// Log with current pos/facing so we can correlate with any following SMSG_ATTACKSWING_* "target incorrect" packet
	px, py, pz, po := w.posX, w.posY, w.posZ, w.orientation
	w.log("CMSG_ATTACKSWING target=%d from (%.1f,%.1f,%.1f) facing=%.2f", targetGUID, px, py, pz, po)
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, targetGUID)
	w.combatMu.Lock()
	w.attackingGUID = targetGUID
	w.inCombat = true   // Optimistic: we initiated attack on (what we believe is) a valid target. Server will send STOP or swing error if incorrect (we now handle those).
	w.combatMu.Unlock()
	return w.sendPacket(CmsgAttackSwing, buf.Bytes())
}

// AttackStop sends CMSG_ATTACKSTOP to stop attacking
func (w *WorldClient) AttackStop() error {
	w.combatMu.Lock()
	w.attackingGUID = 0
	w.inCombat = false
	w.combatMu.Unlock()
	return w.sendPacket(CmsgAttackStop, nil)
}

// SetTarget sends CMSG_SET_SELECTION to set the current target
func (w *WorldClient) SetTarget(targetGUID uint64) error {
	w.log("CMSG_SET_SELECTION target=%d", targetGUID)
	w.combatMu.Lock()
	w.targetGUID = targetGUID
	w.combatMu.Unlock()
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, targetGUID)
	return w.sendPacket(CmsgSetSelection, buf.Bytes())
}

// CastSpell sends CMSG_CAST_SPELL for the given spell targeting a unit
func (w *WorldClient) CastSpell(spellID uint32, targetGUID uint64) error {
	buf := new(bytes.Buffer)
	buf.WriteByte(0) // castCount
	binary.Write(buf, binary.LittleEndian, spellID)
	buf.WriteByte(0) // castFlags

	// Target flags: unit target
	if targetGUID != 0 {
		binary.Write(buf, binary.LittleEndian, uint32(0x0002)) // TARGET_FLAG_UNIT
		writePackedGUID(buf, targetGUID)
	} else {
		binary.Write(buf, binary.LittleEndian, uint32(0x0000)) // TARGET_FLAG_SELF
	}

	return w.sendPacket(CmsgCastSpell, buf.Bytes())
}

// CastSpellAtPosition sends a spell targeted at a position
func (w *WorldClient) CastSpellAtPosition(spellID uint32, x, y, z float32) error {
	buf := new(bytes.Buffer)
	buf.WriteByte(0)
	binary.Write(buf, binary.LittleEndian, spellID)
	buf.WriteByte(0)
	binary.Write(buf, binary.LittleEndian, uint32(0x0020)) // TARGET_FLAG_DEST_LOCATION
	binary.Write(buf, binary.LittleEndian, x)
	binary.Write(buf, binary.LittleEndian, y)
	binary.Write(buf, binary.LittleEndian, z)
	return w.sendPacket(CmsgCastSpell, buf.Bytes())
}

// Loot sends CMSG_LOOT to loot a unit/object
func (w *WorldClient) Loot(guid uint64) error {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, guid)
	return w.sendPacket(CmsgLoot, buf.Bytes())
}

// LootMoney sends CMSG_LOOT_MONEY to collect money from loot
func (w *WorldClient) LootMoney() error {
	return w.sendPacket(CmsgLootMoney, nil)
}

// LootItem sends CMSG_AUTOSTORE_LOOT_ITEM to take a loot item by slot
func (w *WorldClient) LootItem(slot uint8) error {
	buf := new(bytes.Buffer)
	buf.WriteByte(slot)
	return w.sendPacket(CmsgAutostoreLootItem, buf.Bytes())
}

// LootRelease sends CMSG_LOOT_RELEASE to close the loot window
func (w *WorldClient) LootRelease(guid uint64) error {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, guid)
	return w.sendPacket(CmsgLootRelease, buf.Bytes())
}

// SendGMCommand sends a chat message that is a GM command (e.g., ".gm on")
func (w *WorldClient) SendGMCommand(command string) error {
	// GM commands are typically sent as a chat message starting with '.' when the
	// account has sufficient GM level (or via account_access in the DB). The server
	// intercepts it before normal chat processing for authorized users.
	w.log("Sending GM command: %s (opcode=0x%04X encrypted=%v)", command, CmsgMessageChat, w.encrypted)
	err := w.SendChatMessage(ChatMsgSay, LangCommon, command)
	if err != nil {
		w.log("GM command send error: %v", err)
	}
	return err
}

// SendGuildChatMessage sends a message to guild chat (works for many servers even while dead).
func (w *WorldClient) SendGuildChatMessage(message string) error {
	w.log("Sending guild chat: %s", message)
	return w.SendChatMessage(ChatMsgGuild, LangCommon, message)
}

// SendGuildCommand sends a GM-style command via guild chat channel (preferred for .revive etc while dead).
func (w *WorldClient) SendGuildCommand(command string) error {
	w.log("Sending GUILD command: %s (via guild chat)", command)
	return w.SendChatMessage(ChatMsgGuild, LangCommon, command)
}

// Teleport attempts to move the player to the given location.
// Preferred implementation for tests uses a GM command (requires GM rights).
// Falls back or can be extended to use CMSG_WORLD_TELEPORT + ack handling.
func (w *WorldClient) Teleport(mapID uint32, x, y, z, o float32) error {
	// Use .go for coordinate teleport (supported in most 3.3.5a GM command sets).
	cmd := fmt.Sprintf(".go %.2f %.2f %.2f %d", x, y, z, mapID)
	return w.SendGMCommand(cmd)
}


// GroupInvite sends CMSG_GROUP_INVITE to invite a player by name
func (w *WorldClient) GroupInvite(playerName string) error {
	buf := new(bytes.Buffer)
	buf.Write(append([]byte(playerName), 0))
	return w.sendPacket(CmsgGroupInvite, buf.Bytes())
}

// GroupAccept sends CMSG_GROUP_ACCEPT to accept a group invitation
func (w *WorldClient) GroupAccept() error {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint32(0))
	return w.sendPacket(CmsgGroupAccept, buf.Bytes())
}

// NameQuery sends CMSG_NAME_QUERY to look up a player name
func (w *WorldClient) NameQuery(guid uint64) error {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, guid)
	return w.sendPacket(CmsgNameQuery, buf.Bytes())
}

// CreatureQuery sends CMSG_CREATURE_QUERY to look up a creature name
func (w *WorldClient) CreatureQuery(entry uint32, guid uint64) error {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, entry)
	binary.Write(buf, binary.LittleEndian, guid)
	return w.sendPacket(CmsgCreatureQuery, buf.Bytes())
}

// SetSheathed sets sheath state: 0=unsheathed, 1=melee, 2=ranged
func (w *WorldClient) SetSheathed(state uint32) error {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, state)
	return w.sendPacket(CmsgSetSheathed, buf.Bytes())
}

// QuestgiverHello sends CMSG_QUESTGIVER_HELLO
func (w *WorldClient) QuestgiverHello(npcGUID uint64) error {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, npcGUID)
	return w.sendPacket(CmsgQuestgiverHello, buf.Bytes())
}

// QuestgiverAcceptQuest sends CMSG_QUESTGIVER_ACCEPT_QUEST
func (w *WorldClient) QuestgiverAcceptQuest(npcGUID uint64, questID uint32) error {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, npcGUID)
	binary.Write(buf, binary.LittleEndian, questID)
	binary.Write(buf, binary.LittleEndian, uint32(0))
	return w.sendPacket(CmsgQuestgiverAcceptQuest, buf.Bytes())
}

// QuestgiverCompleteQuest sends CMSG_QUESTGIVER_COMPLETE_QUEST
func (w *WorldClient) QuestgiverCompleteQuest(npcGUID uint64, questID uint32) error {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, npcGUID)
	binary.Write(buf, binary.LittleEndian, questID)
	return w.sendPacket(CmsgQuestgiverCompleteQuest, buf.Bytes())
}

// QuestgiverChooseReward sends CMSG_QUESTGIVER_CHOOSE_REWARD
func (w *WorldClient) QuestgiverChooseReward(npcGUID uint64, questID uint32, reward uint32) error {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, npcGUID)
	binary.Write(buf, binary.LittleEndian, questID)
	binary.Write(buf, binary.LittleEndian, reward)
	return w.sendPacket(CmsgQuestgiverChooseReward, buf.Bytes())
}

// ============================================================
// Movement methods
// ============================================================

// MoveForward starts forward movement toward a destination.
// The caller is responsible for sending MsgMoveStop when arriving.
func (w *WorldClient) MoveForward() error {
	return w.sendMovement(MsgMoveStartForward, MoveFlagForward)
}

func (w *WorldClient) MoveForwardAt(x, y, z, o float32) error {
	return w.MoveForwardAtTime(x, y, z, o, time.Time{})
}

func (w *WorldClient) MoveForwardAtTime(x, y, z, o float32, at time.Time) error {
	return w.sendMovementAtTime(MsgMoveStartForward, MoveFlagForward, x, y, z, o, at)
}

// MoveStop stops all movement
func (w *WorldClient) MoveStop() error {
	return w.sendMovement(MsgMoveStop, MoveFlagNone)
}

func (w *WorldClient) MoveStopAt(x, y, z, o float32) error {
	return w.MoveStopAtTime(x, y, z, o, time.Time{})
}

func (w *WorldClient) MoveStopAtTime(x, y, z, o float32, at time.Time) error {
	return w.sendMovementAtTime(MsgMoveStop, MoveFlagNone, x, y, z, o, at)
}

// SetFacing sets the character's facing orientation
func (w *WorldClient) SetFacing(orientation float32) error {
	return w.sendMovementWithFacing(MsgMoveSetFacing, MoveFlagNone, orientation)
}

func (w *WorldClient) SetFacingAt(x, y, z, o float32) error {
	return w.SetFacingAtTime(x, y, z, o, time.Time{})
}

func (w *WorldClient) SetFacingAtTime(x, y, z, o float32, at time.Time) error {
	return w.sendMovementAtTime(MsgMoveSetFacing, MoveFlagNone, x, y, z, o, at)
}

// SetFacingMoving sets facing while moving (includes forward flag, like real client)
func (w *WorldClient) SetFacingMoving(orientation float32) error {
	return w.sendMovementWithFacing(MsgMoveSetFacing, MoveFlagForward, orientation)
}

func (w *WorldClient) SetFacingMovingAt(x, y, z, o float32) error {
	return w.SetFacingMovingAtTime(x, y, z, o, time.Time{})
}

func (w *WorldClient) SetFacingMovingAtTime(x, y, z, o float32, at time.Time) error {
	return w.sendMovementAtTime(MsgMoveSetFacing, MoveFlagForward, x, y, z, o, at)
}

func (w *WorldClient) sendMovement(opcode uint16, moveFlags uint32) error {
	w.moveMu.Lock()
	defer w.moveMu.Unlock()

	return w.sendMovementLocked(opcode, moveFlags)
}

func (w *WorldClient) sendMovementWithFacing(opcode uint16, moveFlags uint32, orientation float32) error {
	w.moveMu.Lock()
	defer w.moveMu.Unlock()

	w.orientation = orientation
	return w.sendMovementLocked(opcode, moveFlags)
}

func (w *WorldClient) sendMovementAt(opcode uint16, moveFlags uint32, x, y, z, o float32) error {
	return w.sendMovementAtTime(opcode, moveFlags, x, y, z, o, time.Time{})
}

func (w *WorldClient) sendMovementAtTime(opcode uint16, moveFlags uint32, x, y, z, o float32, at time.Time) error {
	w.moveMu.Lock()
	defer w.moveMu.Unlock()

	w.setPositionLocked(x, y, z, o)
	return w.sendMovementLockedAt(opcode, moveFlags, movementMSTime(at))
}

func (w *WorldClient) sendMovementLocked(opcode uint16, moveFlags uint32) error {
	return w.sendMovementLockedAt(opcode, moveFlags, getMSTime())
}

func (w *WorldClient) sendMovementLockedAt(opcode uint16, moveFlags uint32, ts uint32) error {
	w.currentMoveFlags = moveFlags
	buf := new(bytes.Buffer)
	if len(w.packedGUID) > 0 {
		buf.Write(w.packedGUID)
	} else {
		writePackedGUID(buf, w.charGUID)
	}
	binary.Write(buf, binary.LittleEndian, moveFlags)
	binary.Write(buf, binary.LittleEndian, uint16(0)) // movementFlags2
	binary.Write(buf, binary.LittleEndian, ts)
	binary.Write(buf, binary.LittleEndian, w.posX)
	binary.Write(buf, binary.LittleEndian, w.posY)
	binary.Write(buf, binary.LittleEndian, w.posZ)
	binary.Write(buf, binary.LittleEndian, w.orientation)
	binary.Write(buf, binary.LittleEndian, uint32(0)) // fallTime
	pkt := buf.Bytes()
	return w.sendPacket(opcode, pkt)
}

func (w *WorldClient) setPositionLocked(x, y, z, o float32) {
	w.posX = x
	w.posY = y
	w.posZ = z
	w.orientation = o
}

// UpdatePosition locally updates the stored position.
func (w *WorldClient) UpdatePosition(x, y, z, o float32) {
	w.moveMu.Lock()
	defer w.moveMu.Unlock()

	w.setPositionLocked(x, y, z, o)
}

// Position returns the current position of the character.
func (w *WorldClient) Position() (x, y, z, o float32, mapID uint32) {
	w.moveMu.Lock()
	defer w.moveMu.Unlock()

	return w.posX, w.posY, w.posZ, w.orientation, w.mapID
}

// CharGUID returns the GUID of the logged-in character.
func (w *WorldClient) CharGUID() uint64 {
	return w.charGUID
}

// ============================================================
// Query methods
// ============================================================

// GetObject returns a tracked object by GUID, or nil if not found.
// It returns a clone so the caller gets its own private snapshot of the world state.
func (w *WorldClient) GetObject(guid uint64) *WorldObject {
	w.objectsMu.RLock()
	obj := w.objects[guid]
	w.objectsMu.RUnlock()
	if obj == nil {
		return nil
	}
	return obj.Clone()
}

// GetNearbyUnits returns all tracked units (NPCs) within maxDist yards.
// Returns clones so each bot/AI gets its own private version of the world state
// (helps with races and different layers/views per connection).
func (w *WorldClient) GetNearbyUnits(maxDist float32) []*WorldObject {
	w.objectsMu.RLock()
	var raw []*WorldObject
	for _, obj := range w.objects {
		if obj.TypeID != ObjectTypeUnit {
			continue
		}
		// Skip aura/combat stubs and creates that have not delivered a position yet.
		// Chasing (0,0,0) or a never-updated spawn is a common "run to empty ground" bug.
		if !obj.HasKnownPosition() {
			continue
		}
		if obj.DistanceTo(w.posX, w.posY, w.posZ) <= maxDist {
			raw = append(raw, obj)
		}
	}
	w.objectsMu.RUnlock()

	result := make([]*WorldObject, len(raw))
	for i, obj := range raw {
		result[i] = obj.Clone()
	}
	return result
}

// GetNearbyPlayers returns all tracked players within maxDist yards.
// Returns clones for private per-bot world view.
func (w *WorldClient) GetNearbyPlayers(maxDist float32) []*WorldObject {
	w.objectsMu.RLock()
	var raw []*WorldObject
	for _, obj := range w.objects {
		if obj.TypeID == ObjectTypePlayer && obj.GUID != w.charGUID && obj.DistanceTo(w.posX, w.posY, w.posZ) <= maxDist {
			raw = append(raw, obj)
		}
	}
	w.objectsMu.RUnlock()

	result := make([]*WorldObject, len(raw))
	for i, obj := range raw {
		result[i] = obj.Clone()
	}
	return result
}

// InCombat returns whether the bot thinks it is in combat.
func (w *WorldClient) InCombat() bool {
	w.combatMu.RLock()
	defer w.combatMu.RUnlock()
	return w.inCombat
}

// TargetGUID returns the current target GUID.
func (w *WorldClient) TargetGUID() uint64 {
	w.combatMu.RLock()
	defer w.combatMu.RUnlock()
	return w.targetGUID
}

// AttackingGUID returns the GUID we last sent CMSG_ATTACKSWING for (0 if not swinging).
func (w *WorldClient) AttackingGUID() uint64 {
	w.combatMu.RLock()
	defer w.combatMu.RUnlock()
	return w.attackingGUID
}

// ClearTarget clears the current target selection.
func (w *WorldClient) ClearTarget() {
	w.combatMu.Lock()
	w.targetGUID = 0
	w.combatMu.Unlock()
}

// ClearCombat clears the combat state.
func (w *WorldClient) ClearCombat() {
	w.combatMu.Lock()
	w.inCombat = false
	w.attackingGUID = 0
	w.combatMu.Unlock()
}

// MarkObjectDead forces health=0 for the given GUID in the object cache.
// Used on kill prediction/confirmation so IsAlive() and BT decisions see death immediately
// instead of waiting for the next values update (prevents chasing/looking at dead).
func (w *WorldClient) MarkObjectDead(guid uint64) {
	w.objectsMu.Lock()
	if obj := w.objects[guid]; obj != nil {
		obj.setValue(UnitFieldHealth, 0)
		obj.setValue(UnitFieldFlags, obj.value(UnitFieldFlags)|UnitFlagDead)
		obj.setValue(UnitDynamicFlags, obj.value(UnitDynamicFlags)|UnitDynflagDead)
	}
	w.objectsMu.Unlock()
}

// ApplyLocalDamage applies damage seen in AttackerStateUpdate packets to the local object cache.
// This helps when the server doesn't promptly send UNIT_FIELD_HEALTH=0 updates for mobs killed
// by other players/bots (common in multi-bot scenarios). We locally simulate the health reduction
// so IsAlive() and targeting see death faster, avoiding the "8/55 on dead mob" problem.
func (w *WorldClient) ApplyLocalDamage(guid uint64, damage uint32) {
	if damage == 0 {
		return
	}
	w.objectsMu.Lock()
	if obj := w.objects[guid]; obj != nil {
		h := obj.value(UnitFieldHealth)
		if h > damage {
			obj.setValue(UnitFieldHealth, h-damage)
		} else {
			obj.setValue(UnitFieldHealth, 0)
			obj.setValue(UnitFieldFlags, obj.value(UnitFieldFlags)|UnitFlagDead)
			obj.setValue(UnitDynamicFlags, obj.value(UnitDynamicFlags)|UnitDynflagDead)
		}
	}
	w.objectsMu.Unlock()
}

// Health returns current player health.
func (w *WorldClient) Health() uint32 {
	w.statsMu.RLock()
	defer w.statsMu.RUnlock()
	return w.health
}

// MaxHealth returns max player health.
func (w *WorldClient) MaxHealth() uint32 {
	w.statsMu.RLock()
	defer w.statsMu.RUnlock()
	return w.maxHealth
}

func (w *WorldClient) Power() (current, max uint32) {
	w.statsMu.RLock()
	defer w.statsMu.RUnlock()
	return w.power, w.maxPower
}

// PlayerLevel returns the bot's current level.
func (w *WorldClient) PlayerLevel() uint32 {
	w.statsMu.RLock()
	defer w.statsMu.RUnlock()
	return w.level
}

// SetLevelForTest forces the internal level (used by scenario prep when GM .level packet update may lag or not fire the expected handler).
func (w *WorldClient) SetLevelForTest(l uint32) {
	w.statsMu.Lock()
	w.level = l
	w.statsMu.Unlock()
}

// IsSpellReady returns true if the spell is known and off cooldown.
func (w *WorldClient) IsSpellReady(spellID uint32) bool {
	w.spellsMu.RLock()
	sp, known := w.knownSpells[spellID]
	w.spellsMu.RUnlock()
	if !known || !sp.Active {
		return false
	}

	w.cooldownsMu.RLock()
	cd, onCooldown := w.cooldowns[spellID]
	w.cooldownsMu.RUnlock()
	if onCooldown && time.Now().Before(cd.ExpiresAt) {
		return false
	}

	return true
}

// KnowsSpell returns true if the bot has learned the given spell.
func (w *WorldClient) KnowsSpell(spellID uint32) bool {
	w.spellsMu.RLock()
	defer w.spellsMu.RUnlock()
	sp, ok := w.knownSpells[spellID]
	return ok && sp.Active
}

// MoveSpeed returns the current movement speed.
func (w *WorldClient) MoveSpeed() float32 {
	return w.moveSpeed
}

// ============================================================
// Packet handlers for combat / spells / objects
// ============================================================

func (w *WorldClient) handleCancelCombat() {
	w.combatMu.Lock()
	w.inCombat = false
	w.combatMu.Unlock()
	if w.OnCombatStop != nil {
		w.OnCombatStop()
	}
}

func (w *WorldClient) handleAttackStart(data []byte) {
	if len(data) < 16 {
		return
	}
	attacker := binary.LittleEndian.Uint64(data[0:8])
	victim := binary.LittleEndian.Uint64(data[8:16])
	w.log("SMSG_ATTACK_START: attacker=%d victim=%d myGUID=%d attacking=%d", attacker, victim, w.charGUID, w.attackingGUID)
	lowMy := w.charGUID & 0xFFFFFFFF
	lowAttacker := attacker & 0xFFFFFFFF
	lowVictim := victim & 0xFFFFFFFF
	if attacker == w.charGUID || victim == w.charGUID || lowAttacker == lowMy || lowVictim == lowMy {
		w.combatMu.Lock()
		w.inCombat = true
		if (victim == w.charGUID || lowVictim == lowMy) && w.targetGUID == 0 {
			w.targetGUID = attacker
		}
		w.combatMu.Unlock()
	}
	// Also set if the start involves the target we are currently trying to attack (helps when GUID forms differ or server reports the engagement)
	if w.attackingGUID != 0 && (attacker == w.attackingGUID || victim == w.attackingGUID || (attacker&0xFFFFFFFF) == (w.attackingGUID&0xFFFFFFFF) || (victim&0xFFFFFFFF) == (w.attackingGUID&0xFFFFFFFF)) {
		w.combatMu.Lock()
		w.inCombat = true
		w.combatMu.Unlock()
	}
	if w.OnCombatStart != nil {
		w.OnCombatStart(attacker, victim)
	}
}

func (w *WorldClient) handleAttackStop(data []byte) {
	if len(data) < 4 {
		return
	}
	r := bytes.NewReader(data)
	attackerGUID, _ := readPackedGUID(r)
	victimGUID, _ := readPackedGUID(r)

	// SMSG_ATTACK_STOP alone is ambiguous: normal end of swing, invalid target,
	// or death. Do NOT invent death here — terminal state comes from
	// SMSG_ATTACKSWING_DEAD_TARGET / CANT_ATTACK, health updates, or OnKill.
	if attackerGUID != w.charGUID && (attackerGUID&0xFFFFFFFF) != (w.charGUID&0xFFFFFFFF) {
		return
	}

	w.combatMu.Lock()
	w.attackingGUID = 0
	w.combatMu.Unlock()

	guid := victimGUID
	if guid == 0 {
		guid = w.targetGUID
	}

	// No victim payload often means "could not resolve enemy" — drop selection
	// so we re-acquire, but still do not mark dead (might be facing/range/phase).
	if victimGUID == 0 && w.targetGUID != 0 {
		w.ClearTarget()
		w.ClearCombat()
	}

	w.emitAttackReject(AttackReject{
		GUID:   guid,
		Reason: RejectReasonAttackStop,
		Class:  RejectUnknown,
		Opcode: SmsgAttackStop,
	})
}

// handleAttackSwingError handles SMSG_ATTACKSWING_* notifications from AC when
// CMSG_ATTACKSWING preconditions fail (range, facing, dead, cant-attack).
// Transient reasons keep the target; terminal reasons mark dead and clear.
func (w *WorldClient) handleAttackSwingError(data []byte, reason string, opcode uint16) {
	class := ClassifyAttackSwingReason(reason)
	w.log("SMSG_ATTACKSWING_%s class=%s (len=%d) data=% x", reason, class, len(data), data)

	victim := uint64(0)
	if len(data) >= 8 {
		// Many swing error packets carry the victim as raw uint64 GUID (like ATTACKSTART).
		victim = binary.LittleEndian.Uint64(data[0:8])
	} else if len(data) > 0 {
		r := bytes.NewReader(data)
		if g, err := readPackedGUID(r); err == nil && g != 0 {
			victim = g
		}
	}
	if victim == 0 {
		victim = w.targetGUID
		if victim == 0 {
			victim = w.attackingGUID
		}
	}
	if victim != 0 {
		w.log("  swing error victim GUID=%d (current target=%d attacking=%d)", victim, w.targetGUID, w.attackingGUID)
	}

	// Always stop local auto-attack swing; posture correction may re-issue ATTACKSWING.
	w.combatMu.Lock()
	w.attackingGUID = 0
	w.combatMu.Unlock()

	switch class {
	case RejectTerminal:
		if victim != 0 {
			w.MarkObjectDead(victim)
		}
		w.ClearTarget()
		w.ClearCombat()
		if w.OnInvalidTarget != nil && victim != 0 {
			w.OnInvalidTarget(victim)
		}
	case RejectTransient:
		// Keep target + selection so AI can approach / face and retry.
		// Do not MarkObjectDead — that was the choppy "phantom dead mobs" bug.
	default:
		// Unknown: clear combat flag only, leave target unless none.
		w.combatMu.Lock()
		w.inCombat = false
		w.combatMu.Unlock()
	}

	w.emitAttackReject(AttackReject{
		GUID:   victim,
		Reason: reason,
		Class:  class,
		Opcode: opcode,
	})
}

func (w *WorldClient) emitAttackReject(r AttackReject) {
	if w.OnAttackReject != nil {
		w.OnAttackReject(r)
	}
}

func (w *WorldClient) handleAttackerStateUpdate(data []byte) {
	if len(data) < 20 {
		return
	}
	r := bytes.NewReader(data)
	var hitInfo uint32
	binary.Read(r, binary.LittleEndian, &hitInfo)
	attackerGUID, _ := readPackedGUID(r)
	victimGUID, _ := readPackedGUID(r)
	var totalDamage uint32
	binary.Read(r, binary.LittleEndian, &totalDamage)

	// Track our outgoing damage and check for kills (pre-damage view)
	if attackerGUID == w.charGUID && totalDamage > 0 {
		victim := w.GetObject(victimGUID)
		if victim != nil {
			h := victim.Health()
			w.log("Dealt %d damage to GUID %d Entry=%d (HP: %d)", totalDamage, victimGUID, victim.Entry, h)
			if h > 0 && totalDamage >= h {
				w.log("Killed target GUID %d Entry=%d", victimGUID, victim.Entry)
				if w.OnKill != nil {
					w.OnKill(victimGUID)
				}
			}
		} else {
			w.log("Dealt %d damage to GUID %d (no object data)", totalDamage, victimGUID)
		}
	}

	// Apply seen damage locally to the victim's object cache *after* our prediction.
	// Critical for multi-bot: when others kill a mob, server may not send health=0 promptly
	// to this connection. Local damage application makes our IsAlive()/targeting see 0 health.
	if victimGUID != 0 && totalDamage > 0 {
		w.ApplyLocalDamage(victimGUID, totalDamage)
	}

	// If local application brought health to 0, force dead mark on cache so IsAlive sees it immediately.
	if victimGUID != 0 {
		v := w.GetObject(victimGUID)
		if v != nil && v.Health() == 0 {
			w.MarkObjectDead(victimGUID)
		}
	}

	// Check if we are the victim - set combat state and track damage
	if victimGUID == w.charGUID && totalDamage > 0 {
		// Always set combat flag when taking damage
		w.combatMu.Lock()
		w.inCombat = true
		if w.targetGUID == 0 {
			w.targetGUID = attackerGUID
		}
		w.combatMu.Unlock()

		w.statsMu.Lock()
		w.log("Took %d damage from attacker GUID %d (HP: %d/%d)", totalDamage, attackerGUID, w.health, w.maxHealth)
		if w.health > 0 && totalDamage >= w.health {
			w.health = 0
			w.statsMu.Unlock()
			w.log("Bot has died! (killed by GUID %d)", attackerGUID)
			if w.OnDeath != nil {
				w.OnDeath()
			}
		} else if w.health > totalDamage {
			w.health -= totalDamage
			w.statsMu.Unlock()
		} else {
			w.statsMu.Unlock()
		}
	}
}

func (w *WorldClient) handleInitialSpells(data []byte) {
	if len(data) < 3 {
		return
	}
	r := bytes.NewReader(data)
	r.ReadByte() // talentSpec
	var spellCount uint16
	binary.Read(r, binary.LittleEndian, &spellCount)

	w.spellsMu.Lock()
	for i := uint16(0); i < spellCount; i++ {
		var spellID uint32
		binary.Read(r, binary.LittleEndian, &spellID)
		var unk uint16
		binary.Read(r, binary.LittleEndian, &unk) // slot index or flags

		w.knownSpells[spellID] = &KnownSpell{SpellID: spellID, Active: unk == 0}
	}
	w.spellsMu.Unlock()
	w.log("Received %d initial spells", spellCount)
}

func (w *WorldClient) handleSpellGo(data []byte) {
	if len(data) < 4 {
		return
	}
	r := bytes.NewReader(data)
	casterGUID, _ := readPackedGUID(r)
	_, _ = readPackedGUID(r) // casterUnit
	var castID uint8
	binary.Read(r, binary.LittleEndian, &castID)
	var spellID uint32
	binary.Read(r, binary.LittleEndian, &spellID)

	if casterGUID == w.charGUID {
		if w.OnSpellCastResult != nil {
			w.OnSpellCastResult(spellID, true, 0)
		}
	}
}

func (w *WorldClient) handleSpellFailure(data []byte) {
	if len(data) < 10 {
		return
	}
	r := bytes.NewReader(data)
	casterGUID, _ := readPackedGUID(r)
	var castID uint8
	binary.Read(r, binary.LittleEndian, &castID)
	var spellID uint32
	binary.Read(r, binary.LittleEndian, &spellID)
	var reason uint8
	binary.Read(r, binary.LittleEndian, &reason)

	if casterGUID == w.charGUID {
		w.log("Spell %d FAILED (reason=%d)", spellID, reason)
		if w.OnSpellCastResult != nil {
			w.OnSpellCastResult(spellID, false, reason)
		}
	}
}

// handleCastFailed processes SMSG_CAST_FAILED (client-facing cast result).
// Layout (3.3.5a): castCount(u8), spellId(u32), result(u8), optional args.
func (w *WorldClient) handleCastFailed(data []byte) {
	if len(data) < 6 {
		return
	}
	castCount := data[0]
	spellID := binary.LittleEndian.Uint32(data[1:5])
	reason := data[5]
	w.log("SMSG_CAST_FAILED spell=%d reason=%d castCount=%d", spellID, reason, castCount)
	if w.OnSpellCastResult != nil {
		w.OnSpellCastResult(spellID, false, reason)
	}
}

func (w *WorldClient) handleSpellCooldown(data []byte) {
	if len(data) < 12 {
		return
	}
	r := bytes.NewReader(data)
	var guid uint64
	binary.Read(r, binary.LittleEndian, &guid)

	if guid != w.charGUID {
		return
	}

	w.cooldownsMu.Lock()
	for r.Len() >= 8 {
		var spellID uint32
		var cdTime uint32
		binary.Read(r, binary.LittleEndian, &spellID)
		binary.Read(r, binary.LittleEndian, &cdTime)

		w.cooldowns[spellID] = &SpellCooldown{
			SpellID:   spellID,
			ExpiresAt: time.Now().Add(time.Duration(cdTime) * time.Millisecond),
		}
	}
	w.cooldownsMu.Unlock()
}

func (w *WorldClient) handleCooldownEvent(data []byte) {
	if len(data) < 12 {
		return
	}
	var spellID uint32
	var guid uint64
	r := bytes.NewReader(data)
	binary.Read(r, binary.LittleEndian, &spellID)
	binary.Read(r, binary.LittleEndian, &guid)

	if guid == w.charGUID {
		w.cooldownsMu.Lock()
		delete(w.cooldowns, spellID)
		w.cooldownsMu.Unlock()
	}
}

func (w *WorldClient) handleClearCooldown(data []byte) {
	if len(data) < 12 {
		return
	}
	var spellID uint32
	var guid uint64
	r := bytes.NewReader(data)
	binary.Read(r, binary.LittleEndian, &spellID)
	binary.Read(r, binary.LittleEndian, &guid)

	if guid == w.charGUID {
		w.cooldownsMu.Lock()
		delete(w.cooldowns, spellID)
		w.cooldownsMu.Unlock()
	}
}

// handleAuraUpdate handles SMSG_AURA_UPDATE (incremental single-target aura slot updates).
// Packet (per AC): packed target GUID, then repeated:
//   uint8 slot
//   uint32 spellId (or 0 to remove/clear the slot)
//   [if spellId != 0]:
//     uint8 flags
//     uint8 casterLevel
//     uint8 stackOrCharges
//     [if not self-cast flag] packed caster GUID
//     [if has duration] uint32 maxDuration, uint32 duration
func (w *WorldClient) handleAuraUpdate(data []byte) {
	r := bytes.NewReader(data)
	targetGUID, err := readPackedGUID(r)
	if err != nil || targetGUID == 0 {
		return
	}
	w.objectsMu.RLock()
	obj := w.objects[targetGUID]
	w.objectsMu.RUnlock()
	if obj == nil {
		// We may learn about auras on objects we don't fully track yet; ignore or create stub?
		// For testkit we primarily care about tracked objects; create a minimal stub.
		obj = w.getOrCreateObject(targetGUID)
	}

	for {
		var slot uint8
		if err := binary.Read(r, binary.LittleEndian, &slot); err != nil {
			break
		}
		var spellID uint32
		if err := binary.Read(r, binary.LittleEndian, &spellID); err != nil {
			break
		}

		// Read the fixed aura header (we don't currently use flags/level/stack for HasAura).
		var flags, casterLevel, stackOrCharges uint8
		if err := binary.Read(r, binary.LittleEndian, &flags); err != nil {
			break
		}
		if err := binary.Read(r, binary.LittleEndian, &casterLevel); err != nil {
			break
		}
		if err := binary.Read(r, binary.LittleEndian, &stackOrCharges); err != nil {
			break
		}

		// If not caster flag (bit 2 in typical AC flags?), a packed caster GUID follows.
		// We simply skip it for now.
		const AFLAG_NOT_CASTER = 0x02 // conservative; actual mask per AC may vary
		if flags&AFLAG_NOT_CASTER != 0 {
			if _, err := readPackedGUID(r); err != nil {
				break
			}
		}

		// Duration block may be present depending on flags. We attempt a non-blocking peek
		// by trying to read; on short packet we just stop. This is best-effort.
		// Real impl would inspect flags for DURATION etc. For HasAura we only need spellID.
		var maxDur, dur uint32
		_ = binary.Read(r, binary.LittleEndian, &maxDur)
		_ = binary.Read(r, binary.LittleEndian, &dur)

		// Use slot-aware update so that spellID==0 correctly clears the previous occupant.
		obj.setAuraForSlot(slot, spellID)

		// Notify observers (testkit can poll, or we could add OnAura).
		if w.OnObjectUpdate != nil {
			w.OnObjectUpdate(targetGUID, obj.Clone())
		}
	}
}

// handleAuraUpdateAll handles SMSG_AURA_UPDATE_ALL (full aura list for a target).
// Similar structure repeated until end of packet or a terminator slot.
func (w *WorldClient) handleAuraUpdateAll(data []byte) {
	r := bytes.NewReader(data)
	targetGUID, err := readPackedGUID(r)
	if err != nil || targetGUID == 0 {
		return
	}
	w.objectsMu.RLock()
	obj := w.objects[targetGUID]
	w.objectsMu.RUnlock()
	if obj == nil {
		obj = w.getOrCreateObject(targetGUID)
	}
	// Clear previous state for a full snapshot.
	obj.clearAuras()

	for {
		var slot uint8
		if err := binary.Read(r, binary.LittleEndian, &slot); err != nil {
			break
		}
		var spellID uint32
		if err := binary.Read(r, binary.LittleEndian, &spellID); err != nil {
			break
		}

		var flags, casterLevel, stackOrCharges uint8
		_ = binary.Read(r, binary.LittleEndian, &flags)
		_ = binary.Read(r, binary.LittleEndian, &casterLevel)
		_ = binary.Read(r, binary.LittleEndian, &stackOrCharges)

		const AFLAG_NOT_CASTER = 0x02
		if flags&AFLAG_NOT_CASTER != 0 {
			if _, err := readPackedGUID(r); err != nil {
				break
			}
		}
		var maxDur, dur uint32
		_ = binary.Read(r, binary.LittleEndian, &maxDur)
		_ = binary.Read(r, binary.LittleEndian, &dur)

		// For full snapshot we can still use the slot map for future remove fidelity.
		obj.setAuraForSlot(slot, spellID)
	}

	if w.OnObjectUpdate != nil {
		w.OnObjectUpdate(targetGUID, obj.Clone())
	}
}

func (w *WorldClient) handleLootResponse(data []byte) {
	if len(data) < 14 {
		return
	}
	r := bytes.NewReader(data)
	var lootGUID uint64
	binary.Read(r, binary.LittleEndian, &lootGUID)

	var lootType uint8
	binary.Read(r, binary.LittleEndian, &lootType)
	var gold uint32
	binary.Read(r, binary.LittleEndian, &gold)
	var itemCount uint8
	binary.Read(r, binary.LittleEndian, &itemCount)

	items := make([]LootItem, 0, itemCount)
	for i := uint8(0); i < itemCount; i++ {
		item := LootItem{}
		binary.Read(r, binary.LittleEndian, &item.Index)
		binary.Read(r, binary.LittleEndian, &item.ItemID)
		binary.Read(r, binary.LittleEndian, &item.Quantity)

		// Skip: displayID(4) + randomSuffix(4) + randomPropertyID(4) + slotType(1)
		skip := make([]byte, 13)
		r.Read(skip)

		items = append(items, item)
	}

	if w.OnLootOpened != nil {
		w.OnLootOpened(lootGUID, items)
	}
}

func (w *WorldClient) handleChatMessage(data []byte) {
	if len(data) < 8 {
		return
	}
	r := bytes.NewReader(data)
	var msgType uint8
	binary.Read(r, binary.LittleEndian, &msgType)
	var lang uint32
	binary.Read(r, binary.LittleEndian, &lang)

	// Read sender GUID
	var senderGUID uint64
	binary.Read(r, binary.LittleEndian, &senderGUID)
	var unk uint32
	binary.Read(r, binary.LittleEndian, &unk)

	// For SAY, YELL, etc. the format is:
	// targetGUID(8) + msgLen(4) + msg(null-terminated) + chatTag(1)
	switch msgType {
	case 0x00, 0x01, 0x06: // SAY, PARTY, YELL
		var targetGUID uint64
		binary.Read(r, binary.LittleEndian, &targetGUID)
		var msgLen uint32
		binary.Read(r, binary.LittleEndian, &msgLen)
		if msgLen > 0 && msgLen < 4096 {
			msgBytes := make([]byte, msgLen)
			r.Read(msgBytes)
			msg := strings.TrimRight(string(msgBytes), "\x00")
			senderName := ""
			obj := w.GetObject(senderGUID)
			if obj != nil {
				senderName = obj.Name
			}
			if w.OnChatMessage != nil {
				w.OnChatMessage(senderName, msg, msgType)
			}
		}
	case 0xFF: // CHAT_MSG_SYSTEM (system messages)
		// System messages have different format
		var targetGUID uint64
		binary.Read(r, binary.LittleEndian, &targetGUID)
		var msgLen uint32
		binary.Read(r, binary.LittleEndian, &msgLen)
		if msgLen > 0 && msgLen < 4096 {
			msgBytes := make([]byte, msgLen)
			r.Read(msgBytes)
			msg := strings.TrimRight(string(msgBytes), "\x00")
			w.log("System: %s", msg)
		}
	}
}

func (w *WorldClient) handleLevelUp(data []byte) {
	if len(data) < 4 {
		return
	}
	newLevel := binary.LittleEndian.Uint32(data[0:4])
	w.statsMu.Lock()
	w.level = newLevel
	w.statsMu.Unlock()
	w.log("LEVEL UP! Now level %d", newLevel)
	if w.OnLevelUp != nil {
		w.OnLevelUp(newLevel)
	}
}

func (w *WorldClient) handlePowerUpdate(data []byte) {
	if len(data) < 12 {
		return
	}
	r := bytes.NewReader(data)
	guid, _ := readPackedGUID(r)
	if guid != w.charGUID {
		return
	}
	var powerType uint8
	binary.Read(r, binary.LittleEndian, &powerType)
	var value uint32
	binary.Read(r, binary.LittleEndian, &value)

	if powerType == 0 { // mana
		w.statsMu.Lock()
		w.power = value
		w.statsMu.Unlock()
	}
}

// ============================================================
// SMSG_UPDATE_OBJECT handling
// ============================================================

func (w *WorldClient) handleCompressedUpdateObject(data []byte) {
	if len(data) < 4 {
		return
	}
	decompressedSize := binary.LittleEndian.Uint32(data[0:4])
	if decompressedSize > 10*1024*1024 {
		return
	}

	zr, err := zlib.NewReader(bytes.NewReader(data[4:]))
	if err != nil {
		return
	}
	defer zr.Close()

	if cap(w.decompBuf) < int(decompressedSize) {
		w.decompBuf = make([]byte, decompressedSize)
	}
	decompressed := w.decompBuf[:decompressedSize]
	n, err := io.ReadFull(zr, decompressed)
	if err != nil && n == 0 {
		return
	}

	w.handleUpdateObject(decompressed[:n])
}

func (w *WorldClient) handleUpdateObject(data []byte) {
	if len(data) < 4 {
		return
	}
	r := bytes.NewReader(data)
	var blockCount uint32
	binary.Read(r, binary.LittleEndian, &blockCount)

	for i := uint32(0); i < blockCount && r.Len() > 0; i++ {
		var updateType uint8
		if err := binary.Read(r, binary.LittleEndian, &updateType); err != nil {
			return
		}

		switch updateType {
		case UpdateTypeValues:
			guid, err := readPackedGUID(r)
			if err != nil {
				return
			}
			if guid == w.charGUID {
				w.readValuesUpdate(r, guid)
			} else if w.GetObject(guid) != nil {
				// only process values updates for objects we are tracking (units/NPCs for targeting/health)
				// other players' updates are skipped to save CPU when many players visible
				w.readValuesUpdate(r, guid)
			} else {
				w.skipValuesUpdate(r)
			}

		case UpdateTypeMovement:
			// Movement only update for existing object (e.g. position/orient for units)
			guid, err := readPackedGUID(r)
			if err != nil {
				return
			}
			w.objectsMu.RLock()
			master := w.objects[guid]
			w.objectsMu.RUnlock()
			if master != nil {
				w.readMovementUpdate(r, master)
			} else {
				w.skipMovementUpdate(r)
			}

		case UpdateTypeCreateObject, UpdateTypeCreateObject2:
			guid, err := readPackedGUID(r)
			if err != nil {
				return
			}
			var objTypeID uint8
			if err := binary.Read(r, binary.LittleEndian, &objTypeID); err != nil {
				return
			}

			if objTypeID == ObjectTypePlayer && guid != w.charGUID {
				// For other players (the main source of packet volume with 500 in zone),
				// skip parsing entirely. We only need to consume bytes to stay in sync.
				// We still track self + NPCs/units for AI targeting.
				w.skipMovementUpdate(r)
				w.skipValuesUpdate(r)
			} else {
				obj := w.getOrCreateObject(guid)
				// Preserve a fresher MONSTER_MOVE pose if create is delayed in the stream
				// (move/aura stubs often exist before CREATE_OBJECT is parsed).
				preX, preY, preZ := obj.PosX, obj.PosY, obj.PosZ
				preLast := obj.LastPosUpdate
				hadPrePos := obj.HasKnownPosition()
				preMoving := obj.IsMoving
				preDestX, preDestY, preDestZ := obj.DestX, obj.DestY, obj.DestZ
				preStartX, preStartY, preStartZ := obj.StartX, obj.StartY, obj.StartZ
				preMoveStart, preMoveDur := obj.MoveStartTime, obj.MoveDuration

				obj.TypeID = objTypeID
				obj.IsPlayer = objTypeID == ObjectTypePlayer
				// Drop prior spline so GUID reuse never keeps an old Dest.
				obj.resetMovementInterp()

				w.readMovementUpdate(r, obj)
				w.readValuesUpdate(r, guid)

				if hadPrePos {
					if !obj.HasKnownPosition() {
						// Create had no usable position block — keep pre-create pose.
						obj.PosX, obj.PosY, obj.PosZ = preX, preY, preZ
						obj.LastPosUpdate = preLast
						if preMoving {
							obj.IsMoving = true
							obj.DestX, obj.DestY, obj.DestZ = preDestX, preDestY, preDestZ
							obj.StartX, obj.StartY, obj.StartZ = preStartX, preStartY, preStartZ
							obj.MoveStartTime, obj.MoveDuration = preMoveStart, preMoveDur
						}
					} else if !obj.IsMoving && !preLast.IsZero() && time.Since(preLast) < 2*time.Second {
						// Stationary delayed CREATE often carries an older spawn/home pose
						// while MONSTER_MOVE already put the unit on a live path.
						ddx := obj.PosX - preX
						ddy := obj.PosY - preY
						ddz := obj.PosZ - preZ
						if ddx*ddx+ddy*ddy+ddz*ddz > 16 { // >4 yards
							obj.PosX, obj.PosY, obj.PosZ = preX, preY, preZ
							obj.LastPosUpdate = preLast
							if preMoving {
								obj.IsMoving = true
								obj.DestX, obj.DestY, obj.DestZ = preDestX, preDestY, preDestZ
								obj.StartX, obj.StartY, obj.StartZ = preStartX, preStartY, preStartZ
								obj.MoveStartTime, obj.MoveDuration = preMoveStart, preMoveDur
							}
						}
					}
				}
			}

		case UpdateTypeOutOfRangeObjects:
			var count uint32
			binary.Read(r, binary.LittleEndian, &count)
			for j := uint32(0); j < count; j++ {
				guid, err := readPackedGUID(r)
				if err != nil {
					return
				}
				w.removeObject(guid)
			}

		case UpdateTypeNearObjects:
			// list of near objects, consume guids to keep sync (format may vary)
			var count uint32
			binary.Read(r, binary.LittleEndian, &count)
			for j := uint32(0); j < count; j++ {
				_, _ = readPackedGUID(r)
			}

		default:
			// Unknown update type, bail
			return
		}
	}
}

func (w *WorldClient) readMovementUpdate(r *bytes.Reader, obj *WorldObject) {
	var updateFlags uint16
	if err := binary.Read(r, binary.LittleEndian, &updateFlags); err != nil {
		return
	}

	// UPDATEFLAG_LIVING = 0x20
	if updateFlags&0x20 != 0 {
		var moveFlags uint32
		binary.Read(r, binary.LittleEndian, &moveFlags)
		var moveFlags2 uint16
		binary.Read(r, binary.LittleEndian, &moveFlags2)
		var timestamp uint32
		binary.Read(r, binary.LittleEndian, &timestamp)

		binary.Read(r, binary.LittleEndian, &obj.PosX)
		binary.Read(r, binary.LittleEndian, &obj.PosY)
		binary.Read(r, binary.LittleEndian, &obj.PosZ)
		binary.Read(r, binary.LittleEndian, &obj.Orientation)

		obj.LastPosUpdate = time.Now()
		obj.LastSeen = time.Now()

		// Transport GUID (MOVEMENTFLAG_ONTRANSPORT = 0x00000200)
		if moveFlags&0x00000200 != 0 {
			tGUID, _ := readPackedGUID(r)
			_ = tGUID
			// transX(4) + transY(4) + transZ(4) + transO(4) + transTime(4) + transSeat(1)
			r.Seek(21, io.SeekCurrent)
			// MOVEMENTFLAG2_INTERPOLATED_MOVEMENT = 0x0400
			if moveFlags2&0x0400 != 0 {
				var extraTime uint32
				binary.Read(r, binary.LittleEndian, &extraTime)
			}
		}

		// MOVEMENTFLAG_SWIMMING (0x00200000) or MOVEMENTFLAG_FLYING (0x02000000) or MOVEMENTFLAG2_ALWAYS_ALLOW_PITCHING (0x0020)
		if moveFlags&(0x00200000|0x02000000) != 0 || moveFlags2&0x0020 != 0 {
			var pitch float32
			binary.Read(r, binary.LittleEndian, &pitch)
		}

		var fallTime uint32
		binary.Read(r, binary.LittleEndian, &fallTime)

		// MOVEMENTFLAG_FALLING (0x00001000)
		if moveFlags&0x00001000 != 0 {
			r.Seek(16, io.SeekCurrent) // zSpeed(4) + sinAngle(4) + cosAngle(4) + xySpeed(4)
		}

		// MOVEMENTFLAG_SPLINE_ELEVATION
		if moveFlags&0x04000000 != 0 {
			var splineElev float32
			binary.Read(r, binary.LittleEndian, &splineElev)
		}

		// Movement speeds: walk, run, runBack, swim, swimBack, flight, flightBack, turnRate, pitchRate
		if obj.GUID == w.charGUID {
			speeds := make([]float32, 9)
			for si := 0; si < 9; si++ {
				binary.Read(r, binary.LittleEndian, &speeds[si])
			}
			w.moveSpeed = speeds[1] // run speed
		} else {
			r.Seek(36, io.SeekCurrent)
		}

		// Spline data (MOVEMENTFLAG_SPLINE_ENABLED = 0x08000000)
		if moveFlags&0x08000000 != 0 {
			if obj.TypeID == ObjectTypeUnit && obj.GUID != w.charGUID {
				w.parseCreateSplineData(r, obj)
			} else {
				w.skipSplineData(r)
			}
		}
	} else if updateFlags&0x0100 != 0 {
		// UPDATEFLAG_POSITION (0x0100) - non-living objects with position updates
		tGUID, _ := readPackedGUID(r) // transport GUID (packed, may be just 0x00)
		_ = tGUID
		binary.Read(r, binary.LittleEndian, &obj.PosX)
		binary.Read(r, binary.LittleEndian, &obj.PosY)
		binary.Read(r, binary.LittleEndian, &obj.PosZ)
		obj.LastPosUpdate = time.Now()
		obj.LastSeen = time.Now()
		// transport offsets (or duplicate of position if no transport)
		var tx, ty, tz float32
		binary.Read(r, binary.LittleEndian, &tx)
		binary.Read(r, binary.LittleEndian, &ty)
		binary.Read(r, binary.LittleEndian, &tz)
		binary.Read(r, binary.LittleEndian, &obj.Orientation)
		var facing float32
		binary.Read(r, binary.LittleEndian, &facing)
	} else if updateFlags&0x0040 != 0 {
		// UPDATEFLAG_STATIONARY_POSITION (0x0040)
		binary.Read(r, binary.LittleEndian, &obj.PosX)
		binary.Read(r, binary.LittleEndian, &obj.PosY)
		binary.Read(r, binary.LittleEndian, &obj.PosZ)
		obj.LastPosUpdate = time.Now()
		obj.LastSeen = time.Now()
		binary.Read(r, binary.LittleEndian, &obj.Orientation)
	}

	// UPDATEFLAG_UNKNOWN (0x0008)
	if updateFlags&0x0008 != 0 {
		var unk uint32
		binary.Read(r, binary.LittleEndian, &unk)
	}

	// UPDATEFLAG_LOWGUID (0x0010)
	if updateFlags&0x0010 != 0 {
		var lowGUID uint32
		binary.Read(r, binary.LittleEndian, &lowGUID)
	}

	// UPDATEFLAG_HAS_TARGET (0x0004)
	if updateFlags&0x0004 != 0 {
		_, _ = readPackedGUID(r)
	}

	// UPDATEFLAG_TRANSPORT (0x0002)
	if updateFlags&0x0002 != 0 {
		var transportTime uint32
		binary.Read(r, binary.LittleEndian, &transportTime)
	}

	// UPDATEFLAG_VEHICLE (0x0080)
	if updateFlags&0x0080 != 0 {
		var vehicleID uint32
		binary.Read(r, binary.LittleEndian, &vehicleID)
		var vehicleOrient float32
		binary.Read(r, binary.LittleEndian, &vehicleOrient)
	}

	// UPDATEFLAG_ROTATION (0x0200) for gameobjects
	if updateFlags&0x0200 != 0 {
		var rotation int64
		binary.Read(r, binary.LittleEndian, &rotation)
	}
}

func (w *WorldClient) skipSplineData(r *bytes.Reader) {
	var splineFlags uint32
	binary.Read(r, binary.LittleEndian, &splineFlags)

	// SPLINEFLAG_FINAL_ANGLE = 0x00040000
	if splineFlags&0x00040000 != 0 {
		var angle float32
		binary.Read(r, binary.LittleEndian, &angle)
	} else if splineFlags&0x00020000 != 0 {
		// SPLINEFLAG_FINAL_TARGET
		var targetGUID uint64
		binary.Read(r, binary.LittleEndian, &targetGUID)
	} else if splineFlags&0x00010000 != 0 {
		// SPLINEFLAG_FINAL_POINT
		r.Seek(12, io.SeekCurrent)
	}

	var timePassed uint32
	binary.Read(r, binary.LittleEndian, &timePassed)
	var duration uint32
	binary.Read(r, binary.LittleEndian, &duration)
	var splineID uint32
	binary.Read(r, binary.LittleEndian, &splineID)

	// 3.3.3 additions
	var unk1 float32
	binary.Read(r, binary.LittleEndian, &unk1)
	var unk2 float32
	binary.Read(r, binary.LittleEndian, &unk2)
	var unk3 uint32
	binary.Read(r, binary.LittleEndian, &unk3)
	var unk4 uint32
	binary.Read(r, binary.LittleEndian, &unk4)

	var splineCount uint32
	binary.Read(r, binary.LittleEndian, &splineCount)

	// Read waypoints - use seek to avoid allocs
	r.Seek(int64(splineCount)*12, io.SeekCurrent)

	// Spline mode
	var splineMode uint8
	binary.Read(r, binary.LittleEndian, &splineMode)

	// Final point
	r.Seek(12, io.SeekCurrent)
}

// parseCreateSplineData parses the spline data from UPDATE_OBJECT create movement block for units
// and sets the movement state for interpolation. This ensures we capture initial movement
// for creatures created while moving, so their positions are up-to-date rather than frozen
// at create time (which was causing targeting of outdated locations).
func (w *WorldClient) parseCreateSplineData(r *bytes.Reader, obj *WorldObject) {
	var splineFlags uint32
	if err := binary.Read(r, binary.LittleEndian, &splineFlags); err != nil {
		return
	}

	// Handle final facing (angle, target, or point)
	if splineFlags&0x00040000 != 0 { // FINAL_ANGLE
		var angle float32
		binary.Read(r, binary.LittleEndian, &angle)
	} else if splineFlags&0x00020000 != 0 { // FINAL_TARGET
		var tgt uint64
		binary.Read(r, binary.LittleEndian, &tgt)
	} else if splineFlags&0x00010000 != 0 { // FINAL_POINT
		var fx, fy, fz float32
		binary.Read(r, binary.LittleEndian, &fx)
		binary.Read(r, binary.LittleEndian, &fy)
		binary.Read(r, binary.LittleEndian, &fz)
	}

	var timePassed, duration, splineID uint32
	binary.Read(r, binary.LittleEndian, &timePassed)
	binary.Read(r, binary.LittleEndian, &duration)
	binary.Read(r, binary.LittleEndian, &splineID)

	// 3.3.3 fields (duration_mod, duration_mod_next, vertical_accel, effect_start_time)
	var durationMod1, durationMod2, verticalAccel float32
	var effectStartTime uint32
	binary.Read(r, binary.LittleEndian, &durationMod1)
	binary.Read(r, binary.LittleEndian, &durationMod2)
	binary.Read(r, binary.LittleEndian, &verticalAccel)
	binary.Read(r, binary.LittleEndian, &effectStartTime)

	var nodeCount uint32
	binary.Read(r, binary.LittleEndian, &nodeCount)

	var lastX, lastY, lastZ float32
	origNodeCount := nodeCount
	if nodeCount > 1024 {
		// Guard against bogus huge nodeCount from misparsed data (CPU/reader exhaustion risk)
		nodeCount = 1024
	}

	if nodeCount > 0 {
		// We only need the last point for destination. Seek over all but the last to avoid CPU on bogus huge counts.
		if nodeCount > 1 {
			r.Seek(int64((nodeCount-1)*12), io.SeekCurrent)
		}
		binary.Read(r, binary.LittleEndian, &lastX)
		binary.Read(r, binary.LittleEndian, &lastY)
		binary.Read(r, binary.LittleEndian, &lastZ)
	}

	var mode uint8
	binary.Read(r, binary.LittleEndian, &mode)

	// final dest
	var fdX, fdY, fdZ float32
	binary.Read(r, binary.LittleEndian, &fdX)
	binary.Read(r, binary.LittleEndian, &fdY)
	binary.Read(r, binary.LittleEndian, &fdZ)

	// Use the final dest if present, else last node
	destX, destY, destZ := fdX, fdY, fdZ
	if origNodeCount > 0 && destX == 0 && destY == 0 && destZ == 0 {
		destX, destY, destZ = lastX, lastY, lastZ
	}

	obj.StartX = obj.PosX
	obj.StartY = obj.PosY
	obj.StartZ = obj.PosZ
	obj.DestX = destX
	obj.DestY = destY
	obj.DestZ = destZ
	obj.IsMoving = (origNodeCount > 0 || splineFlags != 0)
	obj.MoveStartTime = time.Now().Add(-time.Duration(timePassed) * time.Millisecond)
	obj.MoveDuration = time.Duration(duration) * time.Millisecond
}

// skipMovementUpdate consumes a movement block without allocating or storing anything.
// Used for other players/objects when we only need to keep the protocol stream in sync.
func (w *WorldClient) skipMovementUpdate(r *bytes.Reader) {
	var updateFlags uint16
	if err := binary.Read(r, binary.LittleEndian, &updateFlags); err != nil {
		return
	}

	if updateFlags&0x20 != 0 {
		// LIVING
		var moveFlags uint32
		binary.Read(r, binary.LittleEndian, &moveFlags)
		var moveFlags2 uint16
		binary.Read(r, binary.LittleEndian, &moveFlags2)
		var timestamp uint32
		binary.Read(r, binary.LittleEndian, &timestamp)

		r.Seek(16, io.SeekCurrent) // x y z o

		if moveFlags&0x00000200 != 0 {
			_, _ = readPackedGUID(r)
			r.Seek(21, io.SeekCurrent)
			if moveFlags2&0x0400 != 0 {
				var extra uint32
				binary.Read(r, binary.LittleEndian, &extra)
			}
		}
		if moveFlags&(0x00200000|0x02000000) != 0 || moveFlags2&0x0020 != 0 {
			var pitch float32
			binary.Read(r, binary.LittleEndian, &pitch)
		}
		var fallTime uint32
		binary.Read(r, binary.LittleEndian, &fallTime)
		if moveFlags&0x00001000 != 0 {
			r.Seek(16, io.SeekCurrent)
		}
		if moveFlags&0x04000000 != 0 {
			var se float32
			binary.Read(r, binary.LittleEndian, &se)
		}
		// 9 speeds
		r.Seek(36, io.SeekCurrent)
		if moveFlags&0x08000000 != 0 {
			w.skipSplineData(r)
		}
	} else if updateFlags&0x0100 != 0 {
		// UPDATEFLAG_POSITION
		_, _ = readPackedGUID(r)
		// x y z tx ty tz o facing (8 floats)
		r.Seek(32, io.SeekCurrent)
	} else if updateFlags&0x0040 != 0 {
		// STATIONARY_POSITION
		r.Seek(16, io.SeekCurrent)
	}

	if updateFlags&0x0008 != 0 {
		var unk uint32
		binary.Read(r, binary.LittleEndian, &unk)
	}
	if updateFlags&0x0010 != 0 {
		var lg uint32
		binary.Read(r, binary.LittleEndian, &lg)
	}
	if updateFlags&0x0004 != 0 {
		_, _ = readPackedGUID(r)
	}
	if updateFlags&0x0002 != 0 {
		_, _ = readPackedGUID(r)
	}
	if updateFlags&0x0200 != 0 {
		var rot int64
		binary.Read(r, binary.LittleEndian, &rot)
	}
}

// skipValuesUpdate consumes a values update block (mask + values) without storing or callbacks.
func (w *WorldClient) skipValuesUpdate(r *bytes.Reader) {
	var blockCount uint8
	if err := binary.Read(r, binary.LittleEndian, &blockCount); err != nil || blockCount == 0 {
		return
	}
	var mask [32]uint32
	totalValues := 0
	for i := uint8(0); i < blockCount && i < 32; i++ {
		binary.Read(r, binary.LittleEndian, &mask[i])
		for b := 0; b < 32; b++ {
			if mask[i]&(1<<uint(b)) != 0 {
				totalValues++
			}
		}
	}
	if totalValues > 0 {
		r.Seek(int64(totalValues)*4, io.SeekCurrent)
	}
}

func (w *WorldClient) readValuesUpdate(r *bytes.Reader, guid uint64) {
	// Read update mask
	var blockCount uint8
	if err := binary.Read(r, binary.LittleEndian, &blockCount); err != nil {
		return
	}

	if blockCount == 0 {
		return
	}

	mask := make([]uint32, blockCount)
	for i := uint8(0); i < blockCount; i++ {
		binary.Read(r, binary.LittleEndian, &mask[i])
	}

	obj := w.getOrCreateObject(guid)
	obj.LastSeen = time.Now()

	for i := uint8(0); i < blockCount; i++ {
		for bit := uint8(0); bit < 32; bit++ {
			if mask[i]&(1<<bit) != 0 {
				fieldIndex := uint16(i)*32 + uint16(bit)
				var value uint32
				binary.Read(r, binary.LittleEndian, &value)

				obj.setValue(fieldIndex, value)

				// Update derived fields
				switch fieldIndex {
				case UnitFieldEntry:
					obj.Entry = value
				case UnitDynamicFlags:
					if (value & (UnitDynflagDead | UnitDynflagLootable)) != 0 {
						// Server told us it's a dead/lootable corpse - force health 0 in our cache
						// even if a health value wasn't sent or is stale positive.
						obj.setValue(UnitFieldHealth, 0)
					}
				case UnitFieldHealth:
					if guid == w.charGUID {
						w.statsMu.Lock()
						oldHealth := w.health
						w.health = value
						w.statsMu.Unlock()
						if value == 0 && oldHealth > 0 {
							w.log("Bot has died! (health went to 0 in update)")
							if w.OnDeath != nil {
								w.OnDeath()
							}
						}
					}
				case UnitFieldMaxHealth:
					if guid == w.charGUID {
						w.statsMu.Lock()
						w.maxHealth = value
						w.statsMu.Unlock()
					}
				case UnitFieldPower1:
					if guid == w.charGUID {
						w.statsMu.Lock()
						w.power = value
						w.statsMu.Unlock()
					}
				case UnitFieldMaxPower1:
					if guid == w.charGUID {
						w.statsMu.Lock()
						w.maxPower = value
						w.statsMu.Unlock()
					}
				case UnitFieldLevel:
					if guid == w.charGUID {
						w.statsMu.Lock()
						w.level = value
						w.statsMu.Unlock()
					}
				case UnitFieldAuraState:
					// Aura state bitmask received. We do not map bits->spellIDs here
					// (states are engine-defined). SMSG_AURA_UPDATE* are authoritative.
					_ = value
				}
			}
		}
	}

	if w.OnObjectUpdate != nil {
		w.OnObjectUpdate(guid, obj)
	}
}

func (w *WorldClient) handleDestroyObject(data []byte) {
	if len(data) < 8 {
		return
	}
	guid := binary.LittleEndian.Uint64(data[0:8])
	w.removeObject(guid)
}

func (w *WorldClient) isSelfGUID(guid uint64) bool {
	if w.charGUID == 0 || guid == 0 {
		return false
	}
	return guid == w.charGUID || (guid&0xFFFFFFFF) == (w.charGUID&0xFFFFFFFF)
}

// applySelfServerRelocate updates local player pose and notifies the bot so the
// movement controller cannot keep heartbeating pre-relocate coordinates (Charge
// would otherwise "rubber-band" back to the cast position).
// Pose writes go through moveMu (same path as UpdatePosition). After the
// OnServerRelocate callback aborts the controller we re-assert pose so a
// concurrent updateMovement cannot leave pre-relocate coords published.
func (w *WorldClient) applySelfServerRelocate(x, y, z float32, reason string) {
	w.moveMu.Lock()
	o := w.orientation
	w.setPositionLocked(x, y, z, o)
	cb := w.OnServerRelocate
	w.moveMu.Unlock()
	if cb != nil {
		cb(x, y, z, o, reason)
	}
	w.moveMu.Lock()
	w.setPositionLocked(x, y, z, o)
	w.moveMu.Unlock()
}

func (w *WorldClient) handleMonsterMove(data []byte) {
	if len(data) < 16 {
		return
	}
	r := bytes.NewReader(data)
	guid, err := readPackedGUID(r)
	if err != nil {
		return
	}

	// uint8 flag (sets/unsets MOVEMENTFLAG2_UNK7 0x40)
	var unk8 uint8
	if err := binary.Read(r, binary.LittleEndian, &unk8); err != nil {
		return
	}

	// Current position
	var posX, posY, posZ float32
	binary.Read(r, binary.LittleEndian, &posX)
	binary.Read(r, binary.LittleEndian, &posY)
	binary.Read(r, binary.LittleEndian, &posZ)

	selfMove := w.isSelfGUID(guid)

	// Accept MONSTER_MOVE before CREATE_OBJECT so delayed creates do not leave us
	// stuck with a later create's older spawn/start pose only.
	obj := w.getOrCreateObject(guid)
	if obj.TypeID == 0 {
		// Likely a unit; refined when CREATE arrives.
		obj.TypeID = ObjectTypeUnit
	}

	// Always update to the current position from the packet
	obj.PosX = posX
	obj.PosY = posY
	obj.PosZ = posZ
	obj.LastPosUpdate = time.Now()
	obj.LastSeen = time.Now()

	// splineId (uint32)
	var splineID uint32
	if err := binary.Read(r, binary.LittleEndian, &splineID); err != nil {
		return
	}

	// MonsterMoveType (uint8): 0=Normal, 1=Stop, 2=FacingSpot, 3=FacingTarget, 4=FacingAngle
	var moveType uint8
	if err := binary.Read(r, binary.LittleEndian, &moveType); err != nil {
		return
	}

	if moveType == 1 { // MonsterMoveStop
		obj.IsMoving = false
		obj.MoveDuration = 0
		// snap to the stop pos
		obj.StartX = posX
		obj.StartY = posY
		obj.StartZ = posZ
		obj.DestX = posX
		obj.DestY = posY
		obj.DestZ = posZ
		if selfMove {
			w.applySelfServerRelocate(posX, posY, posZ, "monster_move_stop")
		}
		return
	}

	// Skip facing data depending on move type
	switch moveType {
	case 2: // FacingSpot: 3 floats
		var fx, fy, fz float32
		binary.Read(r, binary.LittleEndian, &fx)
		binary.Read(r, binary.LittleEndian, &fy)
		binary.Read(r, binary.LittleEndian, &fz)
	case 3: // FacingTarget: uint64
		var ftarget uint64
		binary.Read(r, binary.LittleEndian, &ftarget)
	case 4: // FacingAngle: float32
		var fangle float32
		binary.Read(r, binary.LittleEndian, &fangle)
	}

	// splineFlags (uint32)
	var splineFlags uint32
	if err := binary.Read(r, binary.LittleEndian, &splineFlags); err != nil {
		return
	}

	// animation (if flag 0x00000100 - Animation)
	if splineFlags&0x00000100 != 0 {
		var animID uint8
		var animStartTime uint32
		binary.Read(r, binary.LittleEndian, &animID)
		binary.Read(r, binary.LittleEndian, &animStartTime)
	}

	// duration (uint32)
	var duration uint32
	if err := binary.Read(r, binary.LittleEndian, &duration); err != nil {
		return
	}

	// parabolic (if flag 0x00000200 - Parabolic)
	if splineFlags&0x00000200 != 0 {
		var verticalAccel float32
		var effectStartTime uint32
		binary.Read(r, binary.LittleEndian, &verticalAccel)
		binary.Read(r, binary.LittleEndian, &effectStartTime)
	}

	// Read waypoints to get destination
	var waypointCount uint32
	if err := binary.Read(r, binary.LittleEndian, &waypointCount); err != nil {
		return
	}

	if waypointCount == 0 {
		if selfMove {
			w.applySelfServerRelocate(posX, posY, posZ, "monster_move_empty")
		}
		return
	}

	// For CatmullRom (flag 0x00000008), waypoints are full Vector3 positions
	// For linear paths, first point after count is the destination, rest are packed
	var destX, destY, destZ float32
	if splineFlags&0x00000008 != 0 {
		// CatmullRom: read all waypoints, last one is destination
		for i := uint32(0); i < waypointCount; i++ {
			binary.Read(r, binary.LittleEndian, &destX)
			binary.Read(r, binary.LittleEndian, &destY)
			binary.Read(r, binary.LittleEndian, &destZ)
		}
	} else {
		// Linear: destination is the first Vector3 after the count
		binary.Read(r, binary.LittleEndian, &destX)
		binary.Read(r, binary.LittleEndian, &destY)
		binary.Read(r, binary.LittleEndian, &destZ)
	}
	obj.StartX = obj.PosX
	obj.StartY = obj.PosY
	obj.StartZ = obj.PosZ
	obj.DestX = destX
	obj.DestY = destY
	obj.DestZ = destZ
	obj.IsMoving = true
	obj.MoveStartTime = time.Now()
	obj.MoveDuration = time.Duration(duration) * time.Millisecond

	if selfMove {
		// Charge/intercept/etc.: server owns the spline. Snap to the destination so
		// local path following cannot rubber-band us back to the cast origin.
		// Short-duration forced moves should land immediately for AI purposes.
		if duration <= 1500 || duration == 0 {
			w.applySelfServerRelocate(destX, destY, destZ, "monster_move_charge")
		} else {
			w.applySelfServerRelocate(posX, posY, posZ, "monster_move_self")
		}
	}
}

func (w *WorldClient) handleMonsterMoveTransport(data []byte) {
	// SMSG_MONSTER_MOVE_TRANSPORT has transport prefix: after opcode, typically unitGUID? + transGUID packed + seat + then standard monster move data (unk + pos + ...)
	// Since not main case per user, parse prefix and then update pos using similar logic to avoid missing pos updates.
	r := bytes.NewReader(data)
	unitGUID, err := readPackedGUID(r)
	if err != nil {
		return
	}
	transGUID, _ := readPackedGUID(r)
	var seat int8
	binary.Read(r, binary.LittleEndian, &seat)
	_ = transGUID
	_ = seat

	// now at unk8 + pos etc, same as after guid in regular
	var unk8 uint8
	if err := binary.Read(r, binary.LittleEndian, &unk8); err != nil {
		return
	}

	var posX, posY, posZ float32
	binary.Read(r, binary.LittleEndian, &posX)
	binary.Read(r, binary.LittleEndian, &posY)
	binary.Read(r, binary.LittleEndian, &posZ)

	w.objectsMu.RLock()
	obj, ok := w.objects[unitGUID]
	w.objectsMu.RUnlock()
	if !ok || obj == nil {
		return
	}
	obj.PosX = posX
	obj.PosY = posY
	obj.PosZ = posZ
	obj.LastPosUpdate = time.Now()
	obj.LastSeen = time.Now()

	// for full spline, would continue parse, but for pos correctness, pos is set. Skip rest for now to keep sync.
	// To fully support, could call similar parse code, but skip for this.
}

func (w *WorldClient) handleCompressedMoves(data []byte) {
	// (debug log removed)
	// TODO: decompress and parse multiple move packets.
}

func (w *WorldClient) handleMultipleMoves(data []byte) {
	// (debug log removed)
	// Parse multiple movement infos.
}

func (w *WorldClient) handleMovementPacket(opcode uint16, data []byte) {
	if len(data) < 8 {
		return
	}
	r := bytes.NewReader(data)
	guid, err := readPackedGUID(r)
	if err != nil {
		return
	}
	w.objectsMu.RLock()
	obj, ok := w.objects[guid]
	w.objectsMu.RUnlock()
	if !ok || obj == nil {
		// Not tracking, but consume to keep in sync? For now, try to parse pos anyway for debug.
		// Skip to pos.
		var moveFlags uint32
		binary.Read(r, binary.LittleEndian, &moveFlags)
		var moveFlags2 uint16
		binary.Read(r, binary.LittleEndian, &moveFlags2)
		var ts uint32
		binary.Read(r, binary.LittleEndian, &ts)
		var x, y, z, o float32
		binary.Read(r, binary.LittleEndian, &x)
		binary.Read(r, binary.LittleEndian, &y)
		binary.Read(r, binary.LittleEndian, &z)
		binary.Read(r, binary.LittleEndian, &o)
		// (debug log removed)
		return
	}
	// Parse basic movement info to update pos
	var moveFlags uint32
	binary.Read(r, binary.LittleEndian, &moveFlags)
	var moveFlags2 uint16
	binary.Read(r, binary.LittleEndian, &moveFlags2)
	var ts uint32
	binary.Read(r, binary.LittleEndian, &ts)
	var x, y, z, o float32
	binary.Read(r, binary.LittleEndian, &x)
	binary.Read(r, binary.LittleEndian, &y)
	binary.Read(r, binary.LittleEndian, &z)
	binary.Read(r, binary.LittleEndian, &o)
	obj.PosX = x
	obj.PosY = y
	obj.PosZ = z
	obj.Orientation = o
	obj.LastPosUpdate = time.Now()
	obj.LastSeen = time.Now()
	obj.IsMoving = (moveFlags&0x1000) != 0 || (moveFlags&0x4000) != 0 // forward or backward rough
	// (debug log removed)
}

func (w *WorldClient) handleMoveKnockBack(data []byte) {
	if len(data) < 8 {
		return
	}
	r := bytes.NewReader(data)
	guid, _ := readPackedGUID(r)
	w.objectsMu.RLock()
	obj, ok := w.objects[guid]
	w.objectsMu.RUnlock()
	if !ok || obj == nil {
		return
	}
	// packet has more: falltime, x y z speed horiz/vert etc. For pos update, we can try to read new pos if present in structure.
	// Basic: skip some, read possible pos fields. For now, log to see.
	// (debug log removed)
	// To properly update, would parse the knock target pos, but AC may follow with monster move.
	// For this, at least mark.
}

func (w *WorldClient) handleMoveTeleport(data []byte) {
	if len(data) < 8 {
		return
	}
	r := bytes.NewReader(data)
	guid, _ := readPackedGUID(r)

	// Movement info block (same layout used elsewhere)
	var moveFlags uint32
	binary.Read(r, binary.LittleEndian, &moveFlags)
	var moveFlags2 uint16
	binary.Read(r, binary.LittleEndian, &moveFlags2)
	var ts uint32
	binary.Read(r, binary.LittleEndian, &ts)
	var nx, ny, nz, no float32
	binary.Read(r, binary.LittleEndian, &nx)
	binary.Read(r, binary.LittleEndian, &ny)
	binary.Read(r, binary.LittleEndian, &nz)
	binary.Read(r, binary.LittleEndian, &no)

	// Update object cache for any unit
	w.objectsMu.Lock()
	if obj := w.objects[guid]; obj != nil {
		obj.PosX = nx
		obj.PosY = ny
		obj.PosZ = nz
		obj.Orientation = no
		obj.LastPosUpdate = time.Now()
		obj.LastSeen = time.Now()
		obj.IsMoving = false
	}
	w.objectsMu.Unlock()

	// Self near-teleport: update local pose, ACK, resume in-world.
	// Counter is typically present after movement block on some builds; use 0 if absent.
	if guid == w.charGUID || (w.charGUID != 0 && (guid&0xFFFFFFFF) == (w.charGUID&0xFFFFFFFF)) {
		w.setPhase(PhaseNearTeleport, "SMSG_MOVE_TELEPORT")
		w.posX, w.posY, w.posZ, w.orientation = nx, ny, nz, no
		var counter uint32
		_ = binary.Read(r, binary.LittleEndian, &counter)

		ackBuf := new(bytes.Buffer)
		writePackedGUID(ackBuf, w.charGUID)
		binary.Write(ackBuf, binary.LittleEndian, counter)
		binary.Write(ackBuf, binary.LittleEndian, uint32(getMSTime()))
		w.sendPacket(MsgMoveTeleportAck, ackBuf.Bytes())

		w.phaseMu.Lock()
		w.teleportAcksSent++
		w.phaseMu.Unlock()
		w.setPhase(PhaseInWorld, "MSG_MOVE_TELEPORT_ACK")
		_ = w.SendHeartbeat()
	}
}

func (w *WorldClient) handleNewWorld(data []byte) {
	if len(data) < 20 {
		return
	}
	r := bytes.NewReader(data)
	var mapID uint32
	binary.Read(r, binary.LittleEndian, &mapID)
	var newX, newY, newZ, newO float32
	binary.Read(r, binary.LittleEndian, &newX)
	binary.Read(r, binary.LittleEndian, &newY)
	binary.Read(r, binary.LittleEndian, &newZ)
	binary.Read(r, binary.LittleEndian, &newO)

	w.log("SMSG_NEW_WORLD map=%d pos=(%.1f,%.1f,%.1f)", mapID, newX, newY, newZ)
	w.setPhase(PhaseFarTransfer, "SMSG_NEW_WORLD")

	w.mapID = mapID
	w.posX = newX
	w.posY = newY
	w.posZ = newZ
	w.orientation = newO

	// Clear all objects since we changed maps
	w.objectsMu.Lock()
	w.objects = make(map[uint64]*WorldObject)
	w.objectsMu.Unlock()

	// MSG_MOVE_WORLDPORT_ACK — required before AC accepts STATUS_LOGGEDIN gameplay again
	w.sendPacket(MsgMoveWorldportAck, nil)
	w.phaseMu.Lock()
	w.worldportAcksSent++
	w.phaseMu.Unlock()
	w.setPhase(PhaseInWorld, "MSG_MOVE_WORLDPORT_ACK")
}

// handleMoveTeleportAck handles legacy/misrouted MSG_MOVE_TELEPORT_ACK as SMSG
// (kept for compatibility with older packet paths that mirrored the opcode).
func (w *WorldClient) handleMoveTeleportAck(data []byte) {
	if len(data) < 10 {
		return
	}
	r := bytes.NewReader(data)
	guid, _ := readPackedGUID(r)
	if guid != w.charGUID && (guid&0xFFFFFFFF) != (w.charGUID&0xFFFFFFFF) {
		return
	}

	w.setPhase(PhaseNearTeleport, "MSG_MOVE_TELEPORT_ACK(smsg)")

	var counter uint32
	binary.Read(r, binary.LittleEndian, &counter)

	var moveFlags uint32
	binary.Read(r, binary.LittleEndian, &moveFlags)
	var moveFlags2 uint16
	binary.Read(r, binary.LittleEndian, &moveFlags2)
	var moveTime uint32
	binary.Read(r, binary.LittleEndian, &moveTime)
	var newX, newY, newZ, newO float32
	binary.Read(r, binary.LittleEndian, &newX)
	binary.Read(r, binary.LittleEndian, &newY)
	binary.Read(r, binary.LittleEndian, &newZ)
	binary.Read(r, binary.LittleEndian, &newO)

	w.posX = newX
	w.posY = newY
	w.posZ = newZ
	w.orientation = newO

	w.objectsMu.Lock()
	w.objects = make(map[uint64]*WorldObject)
	w.objectsMu.Unlock()

	ackBuf := new(bytes.Buffer)
	writePackedGUID(ackBuf, w.charGUID)
	binary.Write(ackBuf, binary.LittleEndian, counter)
	binary.Write(ackBuf, binary.LittleEndian, uint32(getMSTime()))
	w.sendPacket(MsgMoveTeleportAck, ackBuf.Bytes())
	w.phaseMu.Lock()
	w.teleportAcksSent++
	w.phaseMu.Unlock()
	w.setPhase(PhaseInWorld, "MSG_MOVE_TELEPORT_ACK")
	_ = w.SendHeartbeat()
}

func (w *WorldClient) getOrCreateObject(guid uint64) *WorldObject {
	w.objectsMu.Lock()
	defer w.objectsMu.Unlock()
	obj, ok := w.objects[guid]
	if !ok {
		obj = &WorldObject{
			GUID:   guid,
			Values: make(map[uint16]uint32),
		}
		w.objects[guid] = obj
	}
	return obj
}

func (w *WorldClient) removeObject(guid uint64) {
	w.objectsMu.Lock()
	if obj := w.objects[guid]; obj != nil {
		obj.clearAuras()
	}
	delete(w.objects, guid)
	w.objectsMu.Unlock()
	if w.OnObjectRemove != nil {
		w.OnObjectRemove(guid)
	}
}

// SendPacketRaw exposes the encrypted packet sender for advanced usage.
func (w *WorldClient) SendPacketRaw(opcode uint16, data []byte) error {
	return w.sendPacket(opcode, data)
}

// logMovementPacket logs details of received movement packets (primarily from other players).
// Used to debug smoothness by running an observer client alongside a moving one.
// It logs opcode, GUID, flags, timestamp, position, orientation so deltas/speeds can be analyzed.
func (w *WorldClient) logMovementPacket(opcode uint16, data []byte) {
	if len(data) < 4 {
		return
	}

	r := bytes.NewReader(data)
	guid, err := readPackedGUID(r)
	if err != nil {
		w.log("[MOV] recv 0x%04X bad packed GUID: %v raw=% X", opcode, err, data)
		return
	}

	// Only care about other players' movement for observer analysis (skip self echoes if any)
	if guid == w.charGUID {
		return
	}

	var flags uint32
	var flags2 uint16
	var mtime uint32
	var px, py, pz, po float32

	// Common prefix for most movement packets: flags(4) + flags2(2) + time(4) + x y z o (16)
	if err := binary.Read(r, binary.LittleEndian, &flags); err != nil {
		//w.log("[MOV] recv 0x%04X guid=%d bad flags", opcode, guid)
		return
	}
	binary.Read(r, binary.LittleEndian, &flags2)
	binary.Read(r, binary.LittleEndian, &mtime)
	binary.Read(r, binary.LittleEndian, &px)
	binary.Read(r, binary.LittleEndian, &py)
	binary.Read(r, binary.LittleEndian, &pz)
	binary.Read(r, binary.LittleEndian, &po)

	// Log key info for smoothness analysis: packet ts, pos, to compute inter-packet deltas/speed/jitter from observer POV
	nowWall := time.Now()
	w.movDebugMu.Lock()
	prev, had := w.lastMovDebug[guid]
	deltaT := float64(0)
	deltaD := float64(0)
	estSpeed := float64(0)
	if had {
		deltaT = nowWall.Sub(prev.wall).Seconds()
		dx := px - prev.x
		dy := py - prev.y
		dz := pz - prev.z
		deltaD = math.Sqrt(float64(dx*dx + dy*dy + dz*dz))
		if deltaT > 0 {
			estSpeed = deltaD / deltaT
		}
	}
	w.lastMovDebug[guid] = struct {
		ts      uint32
		x, y, z float32
		wall    time.Time
	}{ts: mtime, x: px, y: py, z: pz, wall: nowWall}
	w.movDebugMu.Unlock()

	w.log("[MOV] recv 0x%04X guid=%d flags=0x%08X f2=0x%04X ts=%d pos=(%.3f,%.3f,%.3f) o=%.3f len=%d dt=%.3fs dd=%.3f speed=%.2f",
		opcode, guid, flags, flags2, mtime, px, py, pz, po, len(data), deltaT, deltaD, estSpeed)
}
