// Package luaengine provides a Lua scripting runtime for the WoW bot,
// using github.com/Shopify/go-lua. It exposes bot actions and queries to
// Lua scripts and allows runtime behavior modification.
//
// This is part of github.com/walkline/AzerothGhost.
// Existing script syntax (on_tick, bot.* functions) is preserved.
package luaengine

import (
	"fmt"
	"sync"
	"time"

	lua "github.com/Shopify/go-lua"

	"github.com/walkline/AzerothGhost/ai/behaviortree"
)

// BotAPI is the interface that the Lua engine uses to interact with the bot.
type BotAPI interface {
	// Movement
	GetPosition() (x, y, z, o float32)
	MoveTo(x, y, z float32) error
	StopMoving() error
	IsMoving() bool

	// Combat
	AttackTarget(guid uint64) error
	StopAttack() error
	SetTarget(guid uint64) error
	CastSpell(spellID uint32, targetGUID uint64) error
	IsSpellReady(spellID uint32) bool
	GetHealth() (current, max uint32)
	GetPower() (current, max uint32)
	GetLevel() uint32
	InCombat() bool
	IsAlive() bool
	GetTargetGUID() uint64

	// Objects
	GetNearbyUnits(maxDist float32) []UnitInfo
	GetNearbyPlayers(maxDist float32) []UnitInfo
	GetUnitInfo(guid uint64) *UnitInfo

	// Actions
	SendChat(message string) error
	SendCommand(command string) error
	SendGuildCommand(command string) error
	Loot(guid uint64) error
	LootAll(guid uint64) error

	// Logging
	Log(format string, args ...interface{})

	// Extended for Lua AI framework (scripts/ai/*)
	GetClass() uint8
	HasAuraOn(guid uint64, spellID uint32) bool
	CanCast(spellID uint32, targetGUID uint64) bool
	GetPetGUID() uint64
	PetAttack(target uint64)
	GetStance() int
	GetOwnGUID() uint64  // own character GUID for faction/self checks
	SetLevel(level uint32) // force level for scenario prep (GM .level may not update internal state immediately)

	GetPowerType() uint8
	IsBehindTarget(targetGUID uint64) bool

	// Facing for proper melee combat (server rejects swings on bad facing / requires facing target)
	GetFacing() float32
	SetFacing(o float32) error
	FaceTarget(guid uint64) bool
	SetSheathed(state uint32) error

	// ValidationMode reports whether the bot is running with heavy validation
	// instrumentation enabled. Must be cheap to call every tick when false.
	// See AZEROTHGHOST_E2E_QUALITY_ASSURANCE_PLAN.md (Performance Isolation section).
	ValidationMode() bool

	// ConsumeTeleport returns true once after a completed near/far teleport
	// (summon, MSG_MOVE_TELEPORT, SMSG_NEW_WORLD). Scripts should interrupt sticky
	// chase/combat state and restart from the new position.
	ConsumeTeleport() bool
}

// UnitInfo is a simplified view of a nearby unit passed to Lua.
type UnitInfo struct {
	GUID      uint64
	Entry     uint32
	Health    uint32
	MaxHealth uint32
	Level     uint32
	PosX      float32
	PosY      float32
	PosZ      float32
	IsAlive   bool
	IsPlayer  bool
	Distance  float32
	Name      string
	Faction   uint32
	NPCFlags  uint32
	Flags     uint32
	DynFlags  uint32
	Lootable  bool
}

// Engine wraps a Lua state and provides methods to load/run scripts.
type Engine struct {
	mu      sync.Mutex
	state   *lua.State
	bot     BotAPI
	tree    *behaviortree.Tree
	actions map[string]func(bb *behaviortree.Blackboard) behaviortree.Status

	// Script-defined tick function name
	tickFunc string
}

// NewEngine creates a new Lua engine bound to the given bot API.
func NewEngine(bot BotAPI) *Engine {
	e := &Engine{
		bot:      bot,
		actions:  make(map[string]func(bb *behaviortree.Blackboard) behaviortree.Status),
		tickFunc: "on_tick",
	}
	e.initState()
	return e
}

func (e *Engine) initState() {
	e.state = lua.NewState()
	lua.OpenLibraries(e.state)

	// Register bot API functions
	e.registerBotFunctions()
}

func (e *Engine) registerBotFunctions() {
	L := e.state

	// bot table
	L.NewTable()

	// bot.log(msg)
	e.setFunc("log", func(l *lua.State) int {
		msg, _ := l.ToString(1)
		e.bot.Log("[Lua] %s", msg)
		return 0
	})

	// bot.now_ms() -> wall-clock milliseconds (prefer over os.clock for cooldowns)
	e.setFunc("now_ms", func(l *lua.State) int {
		l.PushNumber(float64(time.Now().UnixMilli()))
		return 1
	})

	// bot.get_position() -> x, y, z, o
	e.setFunc("get_position", func(l *lua.State) int {
		x, y, z, o := e.bot.GetPosition()
		l.PushNumber(float64(x))
		l.PushNumber(float64(y))
		l.PushNumber(float64(z))
		l.PushNumber(float64(o))
		return 4
	})

	// bot.get_facing() -> number (current orientation in radians)
	e.setFunc("get_facing", func(l *lua.State) int {
		o := e.bot.GetFacing()
		l.PushNumber(float64(o))
		return 1
	})

	// bot.set_facing(o)
	e.setFunc("set_facing", func(l *lua.State) int {
		o, _ := l.ToNumber(1)
		_ = e.bot.SetFacing(float32(o))
		return 0
	})

	// bot.face_target(guid) -> bool  (computes and sets facing toward the unit)
	e.setFunc("face_target", func(l *lua.State) int {
		g := parseGUID(l, 1)
		ok := e.bot.FaceTarget(g)
		l.PushBoolean(ok)
		return 1
	})

	// bot.set_sheath(state)  -- 0 = unsheathed (melee), required for proper combat start on some servers
	e.setFunc("set_sheath", func(l *lua.State) int {
		st, _ := l.ToNumber(1)
		_ = e.bot.SetSheathed(uint32(st))
		return 0
	})

	// bot.move_to(x, y, z) -> bool
	e.setFunc("move_to", func(l *lua.State) int {
		x, _ := l.ToNumber(1)
		y, _ := l.ToNumber(2)
		z, _ := l.ToNumber(3)
		err := e.bot.MoveTo(float32(x), float32(y), float32(z))
		l.PushBoolean(err == nil)
		return 1
	})

	// bot.stop_moving()
	e.setFunc("stop_moving", func(l *lua.State) int {
		e.bot.StopMoving()
		return 0
	})

	// bot.is_moving() -> bool
	e.setFunc("is_moving", func(l *lua.State) int {
		l.PushBoolean(e.bot.IsMoving())
		return 1
	})

	// bot.attack(guid)  -- accepts number or string (for 64-bit GUID safety)
	e.setFunc("attack", func(l *lua.State) int {
		g := parseGUID(l, 1)
		err := e.bot.AttackTarget(g)
		l.PushBoolean(err == nil)
		return 1
	})

	// bot.stop_attack()
	e.setFunc("stop_attack", func(l *lua.State) int {
		e.bot.StopAttack()
		return 0
	})

	// bot.set_target(guid) -- accepts number or string
	e.setFunc("set_target", func(l *lua.State) int {
		g := parseGUID(l, 1)
		e.bot.SetTarget(g)
		return 0
	})

	// bot.cast_spell(spellID, targetGUID) -> bool   (GUID as number or string)
	e.setFunc("cast_spell", func(l *lua.State) int {
		spellID, _ := l.ToNumber(1)
		tg := parseGUID(l, 2)
		err := e.bot.CastSpell(uint32(spellID), tg)
		l.PushBoolean(err == nil)
		return 1
	})

	// bot.is_spell_ready(spellID) -> bool
	e.setFunc("is_spell_ready", func(l *lua.State) int {
		spellID, _ := l.ToNumber(1)
		ready := e.bot.IsSpellReady(uint32(spellID))
		l.PushBoolean(ready)
		return 1
	})

	// bot.get_health() -> current, max
	e.setFunc("get_health", func(l *lua.State) int {
		cur, max := e.bot.GetHealth()
		l.PushNumber(float64(cur))
		l.PushNumber(float64(max))
		return 2
	})

	// bot.get_power() -> current, max
	e.setFunc("get_power", func(l *lua.State) int {
		cur, max := e.bot.GetPower()
		l.PushNumber(float64(cur))
		l.PushNumber(float64(max))
		return 2
	})

	// bot.get_level() -> number
	e.setFunc("get_level", func(l *lua.State) int {
		l.PushNumber(float64(e.bot.GetLevel()))
		return 1
	})

	// bot.set_level(level)  force for scenario (after .level GM)
	e.setFunc("set_level", func(l *lua.State) int {
		lvl, _ := l.ToNumber(1)
		e.bot.SetLevel(uint32(lvl))
		return 0
	})

	// bot.in_combat() -> bool
	e.setFunc("in_combat", func(l *lua.State) int {
		l.PushBoolean(e.bot.InCombat())
		return 1
	})

	// bot.is_alive() -> bool
	e.setFunc("is_alive", func(l *lua.State) int {
		l.PushBoolean(e.bot.IsAlive())
		return 1
	})

	// bot.get_target() -> guid as string (to preserve 64-bit precision for all GUIDs)
	e.setFunc("get_target", func(l *lua.State) int {
		l.PushString(fmt.Sprintf("%d", e.bot.GetTargetGUID()))
		return 1
	})

	// bot.get_nearby_units(maxDist) -> table of units
	e.setFunc("get_nearby_units", func(l *lua.State) int {
		dist, _ := l.ToNumber(1)
		if dist <= 0 {
			dist = 30
		}
		units := e.bot.GetNearbyUnits(float32(dist))
		l.NewTable()
		for i, u := range units {
			l.PushNumber(float64(i + 1))
			pushUnitInfo(l, &u)
			l.SetTable(-3)
		}
		return 1
	})

	// bot.get_nearby_players(maxDist) -> table of units
	e.setFunc("get_nearby_players", func(l *lua.State) int {
		dist, _ := l.ToNumber(1)
		if dist <= 0 {
			dist = 30
		}
		players := e.bot.GetNearbyPlayers(float32(dist))
		l.NewTable()
		for i, u := range players {
			l.PushNumber(float64(i + 1))
			pushUnitInfo(l, &u)
			l.SetTable(-3)
		}
		return 1
	})

	// bot.get_unit(guid) -> unit table or nil  (GUID number or string)
	e.setFunc("get_unit", func(l *lua.State) int {
		g := parseGUID(l, 1)
		info := e.bot.GetUnitInfo(g)
		if info == nil {
			l.PushNil()
		} else {
			pushUnitInfo(l, info)
		}
		return 1
	})

	// bot.send_chat(message)
	e.setFunc("send_chat", func(l *lua.State) int {
		msg, _ := l.ToString(1)
		e.bot.SendChat(msg)
		return 0
	})

	// bot.send_command(command)
	e.setFunc("send_command", func(l *lua.State) int {
		cmd, _ := l.ToString(1)
		e.bot.SendCommand(cmd)
		return 0
	})

	// bot.send_guild_command(command)  -- for commands while dead (guild chat often allowed)
	e.setFunc("send_guild_command", func(l *lua.State) int {
		cmd, _ := l.ToString(1)
		e.bot.SendGuildCommand(cmd)
		return 0
	})

	// bot.loot(guid)  -- GUID number or string
	e.setFunc("loot", func(l *lua.State) int {
		g := parseGUID(l, 1)
		e.bot.Loot(g)
		return 0
	})

	// bot.loot_all(guid)
	e.setFunc("loot_all", func(l *lua.State) int {
		g := parseGUID(l, 1)
		e.bot.LootAll(g)
		return 0
	})

	// === Extended for advanced Lua AI (init.lua, class/*, generic/*) ===
	e.setFunc("get_class", func(l *lua.State) int {
		l.PushNumber(float64(e.bot.GetClass()))
		return 1
	})

	// bot.get_own_guid() -> string (full 64-bit safe)
	e.setFunc("get_own_guid", func(l *lua.State) int {
		l.PushString(fmt.Sprintf("%d", e.bot.GetOwnGUID()))
		return 1
	})

	// bot.get_faction() -> number (faction template id)
	e.setFunc("get_faction", func(l *lua.State) int {
		// use own GUID unit info
		own := e.bot.GetOwnGUID()
		if u := e.bot.GetUnitInfo(own); u != nil {
			l.PushNumber(float64(u.Faction))
			return 1
		}
		l.PushNumber(0)
		return 1
	})

	// bot.is_enemy(guid) -> bool  (opposite faction player or hostile)
	e.setFunc("is_enemy", func(l *lua.State) int {
		g := parseGUID(l, 1)
		u := e.bot.GetUnitInfo(g)
		if u == nil || !u.IsPlayer {
			l.PushBoolean(false)
			return 1
		}
		my := float64(0)
		if mu := e.bot.GetUnitInfo(e.bot.GetOwnGUID()); mu != nil {
			my = float64(mu.Faction)
		}
		// simple opposite check; Lua side can refine with faction lists
		opp := (u.Faction != 0 && u.Faction != uint32(my))
		l.PushBoolean(opp)
		return 1
	})

	e.setFunc("has_aura_on", func(l *lua.State) int {
		g := parseGUID(l, 1)
		sp, _ := l.ToNumber(2)
		has := e.bot.HasAuraOn(g, uint32(sp))
		l.PushBoolean(has)
		return 1
	})

	e.setFunc("can_cast", func(l *lua.State) int {
		sp, _ := l.ToNumber(1)
		tg := parseGUID(l, 2)
		ok := e.bot.CanCast(uint32(sp), tg)
		l.PushBoolean(ok)
		return 1
	})

	e.setFunc("get_pet_guid", func(l *lua.State) int {
		l.PushNumber(float64(e.bot.GetPetGUID()))
		return 1
	})

	e.setFunc("pet_attack", func(l *lua.State) int {
		tg := parseGUID(l, 1)
		e.bot.PetAttack(tg)
		return 0
	})

	e.setFunc("get_stance", func(l *lua.State) int {
		l.PushNumber(float64(e.bot.GetStance()))
		return 1
	})

	e.setFunc("get_power_type", func(l *lua.State) int {
		l.PushNumber(float64(e.bot.GetPowerType()))
		return 1
	})

	e.setFunc("is_behind_target", func(l *lua.State) int {
		g := parseGUID(l, 1) // accepts guid or none (0 => use current target)
		l.PushBoolean(e.bot.IsBehindTarget(g))
		return 1
	})

	// Cheap query for validation tooling. Scripts can guard expensive work:
	// if bot.validation_mode() then ... end
	// Must return false (near zero cost) for all regular / scaled runs.
	e.setFunc("validation_mode", func(l *lua.State) int {
		l.PushBoolean(e.bot.ValidationMode())
		return 1
	})

	// bot.consume_teleport() -> bool
	// True once after summon / near teleport / worldport completes. Clear sticky
	// AI state and resume from bot.get_position().
	e.setFunc("consume_teleport", func(l *lua.State) int {
		l.PushBoolean(e.bot.ConsumeTeleport())
		return 1
	})

	L.SetGlobal("bot")
}

func (e *Engine) setFunc(name string, fn lua.Function) {
	e.state.PushGoFunction(fn)
	e.state.SetField(-2, name)
}

// parseGUID accepts a number or string from Lua stack (index) and returns uint64.
// This handles full 64-bit GUIDs safely (Lua numbers lose precision above 2^53).
// IMPORTANT: try string first. ToNumber on a large integer *string* will succeed
// but produce a rounded float64 (>2^53 loses bits), leading to wrong GUIDs sent
// in CMSG_SET_SELECTION / CMSG_ATTACKSWING. String path via Sscanf preserves full value.
func parseGUID(l *lua.State, idx int) uint64 {
	if s, ok := l.ToString(idx); ok && s != "" {
		var v uint64
		if _, err := fmt.Sscanf(s, "%d", &v); err == nil {
			return v
		}
		if _, err := fmt.Sscanf(s, "%x", &v); err == nil {
			return v
		}
	}
	if n, ok := l.ToNumber(idx); ok && n > 0 {
		return uint64(n)
	}
	return 0
}

func pushUnitInfo(l *lua.State, u *UnitInfo) {
	l.NewTable()

	// Use string for GUID to avoid Lua double precision loss on 64-bit values (critical for E2E target selection, has_aura_on, set_target etc.)
	l.PushString(fmt.Sprintf("%d", u.GUID))
	l.SetField(-2, "guid")

	l.PushNumber(float64(u.Entry))
	l.SetField(-2, "entry")

	l.PushNumber(float64(u.Health))
	l.SetField(-2, "health")

	l.PushNumber(float64(u.MaxHealth))
	l.SetField(-2, "max_health")

	l.PushNumber(float64(u.Level))
	l.SetField(-2, "level")

	l.PushNumber(float64(u.PosX))
	l.SetField(-2, "x")

	l.PushNumber(float64(u.PosY))
	l.SetField(-2, "y")

	l.PushNumber(float64(u.PosZ))
	l.SetField(-2, "z")

	l.PushBoolean(u.IsAlive)
	l.SetField(-2, "is_alive")

	l.PushBoolean(u.IsPlayer)
	l.SetField(-2, "is_player")

	l.PushNumber(float64(u.Distance))
	l.SetField(-2, "distance")

	l.PushString(u.Name)
	l.SetField(-2, "name")

	l.PushNumber(float64(u.Faction))
	l.SetField(-2, "faction")

	l.PushNumber(float64(u.NPCFlags))
	l.SetField(-2, "npc_flags")

	l.PushNumber(float64(u.Flags))
	l.SetField(-2, "flags")

	l.PushNumber(float64(u.DynFlags))
	l.SetField(-2, "dyn_flags")

	l.PushBoolean(u.Lootable)
	l.SetField(-2, "lootable")
}

// DoString executes Lua code.
func (e *Engine) DoString(code string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return lua.DoString(e.state, code)
}

// DoFile loads and executes a Lua file.
func (e *Engine) DoFile(path string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := lua.LoadFile(e.state, path, ""); err != nil {
		return fmt.Errorf("load lua file %s: %w", path, err)
	}
	return e.state.ProtectedCall(0, lua.MultipleReturns, 0)
}

// CallTick calls the global on_tick function if it exists.
// Returns true if the function was found and called.
func (e *Engine) CallTick() bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.state.Global(e.tickFunc)
	if !e.state.IsFunction(-1) {
		e.state.Pop(1)
		return false
	}
	if err := e.state.ProtectedCall(0, 0, 0); err != nil {
		e.bot.Log("[Lua] tick error: %v", err)
	}
	return true
}

// CallFunction calls a named global Lua function with no arguments.
func (e *Engine) CallFunction(name string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.state.Global(name)
	if !e.state.IsFunction(-1) {
		e.state.Pop(1)
		return fmt.Errorf("lua function %q not found", name)
	}
	return e.state.ProtectedCall(0, 0, 0)
}

// CallFunctionIfExists calls a named global Lua function if present.
// Missing functions are a no-op (used for optional hooks like on_teleport).
func (e *Engine) CallFunctionIfExists(name string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.state.Global(name)
	if !e.state.IsFunction(-1) {
		e.state.Pop(1)
		return nil
	}
	return e.state.ProtectedCall(0, 0, 0)
}

// Reload reinitializes the Lua state and reloads a script file.
func (e *Engine) Reload(scriptPath string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.initState()
	if scriptPath != "" {
		if err := lua.LoadFile(e.state, scriptPath, ""); err != nil {
			return err
		}
		return e.state.ProtectedCall(0, lua.MultipleReturns, 0)
	}
	return nil
}

// SetTickFunc sets the name of the Lua function to call on each tick.
func (e *Engine) SetTickFunc(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.tickFunc = name
}

// SetTable sets a global Lua table from a Go map[string]any (used for AIBundle.Data).
// Supports scalars (string, bool, numeric) and nested maps. Slices are supported
// as 1-based indexed tables. Mirrors the NewTable/SetField/Push*/SetField patterns
// from pushUnitInfo and registerBotFunctions.
func (e *Engine) SetTable(name string, data map[string]any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state == nil {
		return
	}
	L := e.state
	L.NewTable()
	setTableFields(L, data)
	L.SetGlobal(name)
}

// setTableFields recursively populates a Lua table (at top of stack) from a Go map.
func setTableFields(L *lua.State, data map[string]any) {
	for k, v := range data {
		L.PushString(k)
		pushValue(L, v)
		L.SetTable(-3)
	}
}

// pushValue pushes a Go value onto the Lua stack as appropriate Lua type.
// Supports scalars and nested map[string]any (and map[string]interface{}).
// Slices/arrays become 1-based Lua tables.
func pushValue(L *lua.State, v any) {
	if v == nil {
		L.PushNil()
		return
	}
	switch val := v.(type) {
	case string:
		L.PushString(val)
	case bool:
		L.PushBoolean(val)
	case int:
		L.PushNumber(float64(val))
	case int8:
		L.PushNumber(float64(val))
	case int16:
		L.PushNumber(float64(val))
	case int32:
		L.PushNumber(float64(val))
	case int64:
		L.PushNumber(float64(val))
	case uint:
		L.PushNumber(float64(val))
	case uint8:
		L.PushNumber(float64(val))
	case uint16:
		L.PushNumber(float64(val))
	case uint32:
		L.PushNumber(float64(val))
	case uint64:
		L.PushNumber(float64(val))
	case float32:
		L.PushNumber(float64(val))
	case float64:
		L.PushNumber(val)
	case map[string]any:
		L.NewTable()
		setTableFields(L, val)
	case map[interface{}]interface{}:
		L.NewTable()
		m := make(map[string]any, len(val))
		for kk, vv := range val {
			if ks, ok := kk.(string); ok {
				m[ks] = vv
			}
		}
		setTableFields(L, m)
	case []any:
		L.NewTable()
		for i, item := range val {
			L.PushNumber(float64(i + 1))
			pushValue(L, item)
			L.SetTable(-3)
		}
	default:
		// Fallback for other scalars (e.g. via interface{}); use string representation.
		L.PushString(fmt.Sprintf("%v", v))
	}
}

// Close releases the Lua state.
func (e *Engine) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.state = nil
}
