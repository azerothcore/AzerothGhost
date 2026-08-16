package bot

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	mathrand "math/rand"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/azerothcore/AzerothGhost/ai/behaviortree"
	"github.com/azerothcore/AzerothGhost/ai/luaengine"
	"github.com/azerothcore/AzerothGhost/client"
	"github.com/azerothcore/AzerothGhost/gamedata"
	"github.com/azerothcore/AzerothGhost/movement"
	"github.com/azerothcore/AzerothGhost/navigation"
	"github.com/azerothcore/AzerothGhost/scenario"
)

var _ scenario.AIBundle // ensure import is used (AIBundle support wiring)

func openDB(dsn string) (*sql.DB, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// BotStatus represents the current state of a bot
type BotStatus string

const (
	BotStatusIdle           BotStatus = "idle"
	BotStatusAuthenticating BotStatus = "authenticating"
	BotStatusConnecting     BotStatus = "connecting"
	BotStatusInWorld        BotStatus = "in_world"
	BotStatusDone           BotStatus = "done"
	BotStatusError          BotStatus = "error"
)

const (
	wanderRandomPathAttempts    = 8
	wanderRetryCooldown         = 500 * time.Millisecond
	wanderMaxPathLengthFactor   = 3.0
	wanderMaxSimplifiedPathSize = 260
)

// BotResult holds the result of a bot run
type BotResult struct {
	ID     string    `json:"id"`
	Status BotStatus `json:"status"`
	Error  string    `json:"error,omitempty"`
	Level  uint32    `json:"level,omitempty"`
	Kills  int       `json:"kills,omitempty"`
	Deaths int       `json:"deaths,omitempty"`
}

// BotEvent is a notable event that occurred during the bot's life.
type BotEvent struct {
	Time    time.Time `json:"time"`
	Type    string    `json:"type"`
	Message string    `json:"message"`
}

// Bot implements the WoW client bot logic with behavior tree AI.
type Bot struct {
	id     string
	config Config
	status BotStatus
	err    error
	mu     sync.Mutex

	world *client.WorldClient
	nav   navigation.Navigator
	lua   *luaengine.Engine
	tree  *behaviortree.Tree
	bb    *behaviortree.Blackboard

	// Stats
	kills  int
	deaths int
	events []BotEvent

	// Movement state is now fully delegated to the separate MovementController.
	movementMu     sync.Mutex
	moveController *movement.MovementController
	isMoving       bool // mirror for quick checks; controller is source of truth

	// Current pursuit target GUID for sticky chasing of moving creatures.
	// This ensures we keep updating the destination instead of heading to a stale snapshot.
	grindTargetGUID       uint64
	lastPursuitUpdate     time.Time
	lastBetterTargetCheck time.Time
	lastMoveToTargetPos   [3]float32
	lastMoveToTargetTime  time.Time

	// lastPursuedTargetPos keeps the last known position for the current pursuit target
	// so we can continue chasing even if the object temporarily goes out of range or is removed.
	lastPursuedTargetGUID uint64
	lastPursuedTargetPos  [3]float32
	lastPursuedTargetTime time.Time

	lastMovementPacket  time.Time
	lastMoveCommandPos  [3]float32
	lastMoveCommandTime time.Time
	targetCacheGUID     uint64
	targetCacheTime     time.Time

	// Combat state
	lastLootGUID        uint64
	lootMu              sync.Mutex
	lastLootAttemptGUID uint64
	lastLootAttemptAt   time.Time
	lastAttackSwingAt   time.Time
	lastCastTime        time.Time // GCD tracking
	lastVictoryRush     bool      // Victory Rush proc available

	// For unstick from bad/dead targets we selected but never entered real combat with
	currentTargetSetAt  time.Time
	lastEngagedGUID     uint64
	engagedTargetHealth uint32 // health when we first engaged; used to detect no-progress on "live" targets

	// Decision chat throttling (so we can see high-level AI choices in-game without flooding chat)
	lastDecisionChat time.Time

	// Separate throttle for "why I think mob is alive" debug messages in chat
	lastAliveReasonChat time.Time

	// per-bot "known dead" guids. This gives each bot its own version of which objects are dead,
	// even if the live cache from server packets still has positive health (stale 8/55 etc).
	// Once we infer dead (low health no progress, dyn flag, etc.), we keep treating it dead
	// in *this bot's* view until we see a positive health update.
	knownDead   map[uint64]bool
	knownDeadMu sync.Mutex

	// Stop channel
	stopCh chan struct{}

	// Validation (only allocated/wired when config.ValidationMode && ValidationLogPath != "")
	// Follows zero-overhead rule: nothing allocated or written on normal paths.
	validationFile *os.File
	validationEnc  *json.Encoder
	validationMu   sync.Mutex
	validationSeq  uint64

	// Teleport / summon resume: set when near/far transfer returns to in_world.
	// Lua scripts poll via ConsumeTeleport() so AI can fully restart at the new pose.
	teleportMu      sync.Mutex
	teleportPending bool
	teleportReason  string

	// After CAST_FAILED NO_POWER (85), treat spell as not ready briefly so AI
	// does not re-spam the same cast every tick while rage/mana regenerates.
	noPowerMu    sync.Mutex
	noPowerUntil map[uint32]time.Time
}

// myPos returns current player position (convenience to avoid direct field access on client).
func (b *Bot) myPos() (x, y, z float32) {
	x, y, z, _, _ = b.world.Position()
	return
}

// NewBot creates a new bot
func NewBot(id string, config Config) *Bot {
	if config.Race == 0 {
		config.Race = 5
	}
	if config.Class == 0 {
		config.Class = 1
	}
	if config.Mode == "" {
		config.Mode = "grind"
	}
	if config.AITickMs <= 0 {
		config.AITickMs = 200
	}
	// LogDecisionsToChat is respected as provided by caller (CLI default true for convenience,
	// but for high-scale node/orchestrator runs it should be set to false via config/profile/flag).
	// When false, logDecision becomes a near-zero-cost no-op (only the throttle check + return).

	// DisableTargetCache defaults to false (enable the 800ms target cache).
	// Set to true via flag to force fresh scans every tick for debugging stale target issues.

	b := &Bot{
		id:     id,
		config: config,
		status: BotStatusIdle,
		stopCh: make(chan struct{}),
	}
	if config.ValidationMode && config.ValidationLogPath != "" {
		if f, err := os.OpenFile(config.ValidationLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
			b.validationFile = f
			b.validationEnc = json.NewEncoder(f)
		} else {
			// fall back to console note only (do not fail bot)
			fmt.Printf("[validation] failed to open log %s: %v (continuing without file)\n", config.ValidationLogPath, err)
		}
	}
	return b
}

// NewHeadlessBot creates a bot attached to an already-authenticated and logged-in WorldClient.
// It skips auth/char creation/login and the optional DB pre-setup. Useful for integration tests
// and library use.
// The caller is responsible for maintaining the WorldClient lifecycle.
func NewHeadlessBot(wc *client.WorldClient, cfg Config) *Bot {
	if cfg.AITickMs <= 0 {
		cfg.AITickMs = 200
	}
	if cfg.Mode == "" {
		cfg.Mode = "lua" // sensible default for headless/custom AI
	}
	b := &Bot{
		id:     "headless-" + time.Now().Format("150405"),
		config: cfg,
		world:  wc,
		status: BotStatusInWorld,
		stopCh: make(chan struct{}),
	}
	if cfg.ValidationMode && cfg.ValidationLogPath != "" {
		if f, err := os.OpenFile(cfg.ValidationLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
			b.validationFile = f
			b.validationEnc = json.NewEncoder(f)
		}
	}
	b.ensureMovementController()
	b.initNavigation()
	b.wireValidationInstrumentation()
	b.wireTeleportHandling()
	b.wireServerRelocateHandling()

	// Lua engine + AIBundle/LuaCode loading (same rules as full Run path)
	b.lua = luaengine.NewEngine(b)
	if !cfg.AIBundle.IsEmpty() {
		if cfg.AIBundle.Main != "" {
			_ = b.lua.DoString(cfg.AIBundle.Main)
		}
		// Sorted helper keys for deterministic load order.
		keys := make([]string, 0, len(cfg.AIBundle.Helpers))
		for k := range cfg.AIBundle.Helpers {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if code := cfg.AIBundle.Helpers[k]; code != "" {
				_ = b.lua.DoString(code)
			}
		}
		if len(cfg.AIBundle.Data) > 0 {
			b.lua.SetTable("scenario_data", cfg.AIBundle.Data)
		}
		if cfg.AIBundle.TickFunc != "" {
			b.lua.SetTickFunc(cfg.AIBundle.TickFunc)
		}
	} else if cfg.LuaCode != "" {
		_ = b.lua.DoString(cfg.LuaCode)
	} else if cfg.LuaScript != "" {
		_ = b.lua.DoFile(cfg.LuaScript)
	}

	b.bb = behaviortree.NewBlackboard()
	b.bb.Set("mode", cfg.Mode)
	b.bb.Set("setup_done", true)
	b.tree = behaviortree.NewTree(b.buildBehaviorTree())
	b.tree.Blackboard = b.bb

	return b
}

// RunAIOnly runs only the AI loop (assumes world + nav + lua already attached/initialized).
// Does not perform login or full lifecycle management. Pairs with NewHeadlessBot.
func (b *Bot) RunAIOnly() BotResult {
	worldErrCh := make(chan error, 1)
	b.runAILoop(worldErrCh)
	b.closeValidation()
	b.setStatus(BotStatusDone)
	return BotResult{
		ID:     b.id,
		Status: b.status,
		Level:  b.world.PlayerLevel(),
		Kills:  b.kills,
		Deaths: b.deaths,
	}
}

// Run executes the full bot flow: authenticate, connect, enter world, run AI loop.
func (b *Bot) Run() BotResult {
	b.setStatus(BotStatusAuthenticating)
	b.log("Starting bot for %s@%s, char: %s, mode: %s",
		b.config.Username, b.config.AuthServer, b.config.CharacterName, b.config.Mode)

	// Step 1: Authenticate
	authClient := client.NewAuthClient(b.config.Username, b.config.Password)
	realms, err := authClient.Authenticate(b.config.AuthServer)
	if err != nil {
		return b.fail("authentication failed: %v", err)
	}
	if len(realms) == 0 {
		return b.fail("no realms available")
	}

	realmIdx := b.config.RealmIndex
	if realmIdx >= len(realms) {
		realmIdx = 0
	}
	realm := realms[realmIdx]
	b.log("Authenticated. Connecting to realm: %s at %s", realm.Name, realm.Address)

	// Step 2: Connect to worldserver
	b.setStatus(BotStatusConnecting)
	b.world = client.NewWorldClient(b.config.Username, authClient.SessionKey(), b.log)
	// Load-safe default: suppress combat/selection/learn thrash on world client.
	// Validation / packet-trace runs raise verbosity for single-bot debug.
	switch {
	case b.config.EnablePacketTrace:
		b.world.SetLogLevel(client.LogTrace)
	case b.config.ValidationMode:
		b.world.SetLogLevel(client.LogDebug)
	default:
		b.world.SetLogLevel(client.LogWarn)
	}

	if err := b.world.Connect(realm.Address); err != nil {
		return b.fail("connect to worldserver failed: %v", err)
	}

	// Set up callbacks
	charListCh := make(chan []client.CharEnumEntry, 1)
	charCreateCh := make(chan uint8, 1)
	b.world.OnCharList = func(chars []client.CharEnumEntry) {
		select {
		case charListCh <- chars:
		default:
		}
	}
	b.world.OnCharCreateResult = func(data []byte) {
		if len(data) > 0 {
			select {
			case charCreateCh <- data[0]:
			default:
			}
		}
	}
	b.world.OnKill = func(victimGUID uint64) {
		b.kills++
		victim := b.world.GetObject(victimGUID)
		name := fmt.Sprintf("GUID:%d", victimGUID)
		if victim != nil {
			name = fmt.Sprintf("Entry:%d", victim.Entry)
		}
		b.addEvent("kill", "Killed %s (total kills: %d)", name, b.kills)
		b.logDecision("Killed %s (kills=%d)", name, b.kills)
		// detailed OnKill debug removed from console (use chat decisions)

		// Immediately mark dead in cache so IsAlive() and finders see death without waiting for update packets.
		b.world.MarkObjectDead(victimGUID)
		b.markKnownDead(victimGUID)

		// Only set lastLoot if the corpse is close; we do NOT want to path across the world to loot.
		// If far, just clear states and let grind/wander take over.
		setForLoot := false
		if victim != nil {
			px, py, pz, _, _ := b.world.Position()
			d := victim.DistanceTo(px, py, pz)
			// dist log removed from console
			if d <= 12.0 {
				b.lastLootGUID = victimGUID
				setForLoot = true
			}
		} else if b.world.TargetGUID() == victimGUID {
			// No obj but it was our target: don't set far loot
			b.lastLootGUID = 0
		}
		if !setForLoot && b.world.TargetGUID() == victimGUID {
			b.lastLootGUID = 0
		}

		// If this kill was our current target or we were attacking it, clear combat state NOW.
		if b.world.TargetGUID() == victimGUID {
			b.world.ClearTarget()
			b.world.ClearCombat()
			b.world.AttackStop()
			b.stopCurrentMove()
			b.currentTargetSetAt = time.Time{}
			b.lastEngagedGUID = 0
			// cleared log removed from console (chat will show via decisions)
		}

		b.lastVictoryRush = true // Victory Rush proc
	}
	b.world.OnDeath = func() {
		b.deaths++
		b.addEvent("death", "Bot died! (total deaths: %d)", b.deaths)
	}
	b.world.OnLevelUp = func(newLevel uint32) {
		b.addEvent("levelup", "Reached level %d", newLevel)
	}
	b.world.OnCombatStart = func(attacker, victim uint64) {
		if victim == b.world.CharGUID() || attacker == b.world.CharGUID() {
			b.log("Combat started (attacker: %d, victim: %d)", attacker, victim)
		}
	}
	// OnAttackReject is the preferred path: classifies AC swing feedback so we
	// never invent "dead" from BAD_FACING / NOT_IN_RANGE (protocol preconditions).
	b.world.OnAttackReject = func(r client.AttackReject) {
		switch r.Class {
		case client.RejectTerminal:
			if r.GUID != 0 {
				b.markKnownDead(r.GUID)
			}
			b.logDecision("Server reject TERMINAL %s GUID=%d (drop target)", r.Reason, r.GUID)
			// Target/combat already cleared in WorldClient for terminal.
			b.stopCurrentMove()
		case client.RejectTransient:
			// Keep target; close the gap. Lua distance can be stale so we repath
			// on NOT_IN_RANGE even if AI thinks we are already in melee.
			b.logDecision("Server reject TRANSIENT %s GUID=%d (keep target, reapproach/face)", r.Reason, r.GUID)
			if r.GUID != 0 && r.Reason == client.RejectReasonBadFacing {
				_ = b.FaceTarget(r.GUID)
			}
			if r.GUID != 0 && r.Reason == client.RejectReasonNotInRange {
				if t := b.world.GetObject(r.GUID); t != nil {
					tx, ty, tz := t.InterpolatedPosition()
					// Force a fresh path (clear throttle).
					b.lastMoveCommandTime = time.Time{}
					b.moveToPoint(tx, ty, tz)
				}
			}
		default:
			// ATTACK_STOP / unknown: do not markKnownDead (ambiguous).
			b.logDecision("Server reject %s GUID=%d class=%s (no dead mark)", r.Reason, r.GUID, r.Class)
		}
	}
	// Legacy terminal-only callback (still fired by WorldClient for DEAD/CANT).
	b.world.OnInvalidTarget = func(victimGUID uint64) {
		if victimGUID == 0 {
			return
		}
		// OnAttackReject already handled markKnownDead for terminal; keep this
		// as a safety net if something only fires OnInvalidTarget.
		if !b.isKnownDead(victimGUID) {
			b.markKnownDead(victimGUID)
			b.logDecision("OnInvalidTarget GUID=%d (terminal safety net)", victimGUID)
		}
	}
	b.world.OnLootOpened = func(lootGUID uint64, items []client.LootItem) {
		b.handleLootOpened(lootGUID, items)
	}

	// Structured validation timeline + optional packet trace (no-op when disabled).
	b.wireValidationInstrumentation()
	// Summon / .go / portal: interrupt movement+combat and flag Lua to restart AI.
	b.wireTeleportHandling()
	// Charge / blink / server-forced player splines: snap movement controller.
	b.wireServerRelocateHandling()

	// Start world client
	worldErrCh := make(chan error, 1)
	go func() {
		worldErrCh <- b.world.Run()
	}()

	time.Sleep(2 * time.Second)

	// Step 3: Character list
	b.world.SendReadyForAccountDataTimes()
	b.world.SendRealmSplit()
	if err := b.world.RequestCharList(); err != nil {
		return b.fail("request char list failed: %v", err)
	}

	var chars []client.CharEnumEntry
	select {
	case chars = <-charListCh:
	case <-time.After(120 * time.Second):
		return b.fail("timeout waiting for character list")
	}

	// Optional: delete all existing characters before creating (orchestrator or explicit flag only)
	if b.config.DeleteExistingCharacters && len(chars) > 0 {
		b.log("DeleteExistingCharacters enabled: deleting %d existing character(s)...", len(chars))
		for _, ch := range chars {
			if err := b.world.DeleteCharacter(ch.GUID); err != nil {
				b.log("Warning: failed to delete character %s (GUID %d): %v", ch.Name, ch.GUID, err)
			} else {
				b.log("Deleted character %s", ch.Name)
			}
			time.Sleep(150 * time.Millisecond)
		}
		// Refresh list after deletes
		time.Sleep(500 * time.Millisecond)
		b.world.SendReadyForAccountDataTimes()
		b.world.SendRealmSplit()
		if err := b.world.RequestCharList(); err != nil {
			return b.fail("request char list after delete failed: %v", err)
		}
		select {
		case chars = <-charListCh:
		case <-time.After(120 * time.Second):
			return b.fail("timeout waiting for character list after deletes")
		}
		b.log("Character list refreshed after deletes (%d remaining)", len(chars))
	}

	// Step 4: Find or create character
	var charGUID uint64
	found := false
	for _, ch := range chars {
		if strings.EqualFold(ch.Name, b.config.CharacterName) {
			charGUID = ch.GUID
			found = true
			b.log("Found character %s (GUID: %d, Level: %d)", ch.Name, ch.GUID, ch.Level)
			break
		}
	}

	if !found {
		// Generate a highly unique starting name. Orchestrator also generates one,
		// but we keep trying fresh unique names on NAME_IN_USE.
		charName := b.config.CharacterName
		var createResult uint8
		for attempt := 0; attempt < 10; attempt++ {
			if attempt > 0 {
				// Generate a completely fresh unique name instead of just appending.
				// This lets the process (or orchestrator on relaunch) keep trying until success.
				charName = generateUniqueCharName(attempt + int(time.Now().UnixNano()%1000))
			}
			b.log("Creating character %s (attempt %d)...", charName, attempt+1)
			if err := b.world.CreateCharacter(
				charName, b.config.Race, b.config.Class, b.config.Gender,
				0, 0, 0, 0, 0, 0,
			); err != nil {
				return b.fail("create character failed: %v", err)
			}

			select {
			case createResult = <-charCreateCh:
			case <-time.After(120 * time.Second):
				return b.fail("timeout waiting for character creation")
			}

			if createResult == 0x2F { // CHAR_CREATE_SUCCESS
				b.config.CharacterName = charName
				break
			}
			if createResult != 0x32 { // Not CHAR_CREATE_NAME_IN_USE
				return b.fail("character creation failed with code 0x%X", createResult)
			}
			b.log("Name %s already in use, generating a new unique name...", charName)
		}
		if createResult != 0x2F {
			return b.fail("character creation failed after retries, last code 0x%X", createResult)
		}

		b.log("Character created, requesting updated char list")
		b.world.SendReadyForAccountDataTimes()
		b.world.SendRealmSplit()
		if err := b.world.RequestCharList(); err != nil {
			return b.fail("request char list after create failed: %v", err)
		}

		select {
		case chars = <-charListCh:
		case <-time.After(120 * time.Second):
			return b.fail("timeout waiting for char list after create")
		}

		for _, ch := range chars {
			if strings.EqualFold(ch.Name, b.config.CharacterName) {
				charGUID = ch.GUID
				found = true
				break
			}
		}
		if !found {
			return b.fail("character not found after creation")
		}
	}

	// Step 5: Pre-login DB setup (default off for library use)
	if !b.config.SkipCharacterSetup && b.config.AllowDBSetup {
		b.preLoginDBSetup()
	} else if b.config.SkipCharacterSetup {
		b.log("Skipping character setup (headless or library-driven flow)")
	}

	// Step 5b: Login
	b.log("Logging in with character GUID %d", charGUID)
	if err := b.world.LoginCharacter(charGUID); err != nil {
		return b.fail("login character failed: %v", err)
	}

	// Race login completion vs connection error.
	loginErrCh := make(chan error, 1)
	go func() {
		loginErrCh <- b.world.WaitForLogin(120 * time.Second)
	}()
	select {
	case err := <-loginErrCh:
		if err != nil {
			return b.fail("login verify failed: %v", err)
		}
	case err := <-worldErrCh:
		if err != nil {
			return b.fail("world connection died during login: %v", err)
		}
	}

	b.setStatus(BotStatusInWorld)
	x, y, z, o, m := b.world.Position()
	_ = x
	_ = y
	_ = z
	_ = o // position available for movement controller
	b.log("Character in world on map %d", m)

	time.Sleep(1 * time.Second)
	b.world.SetActiveMover(charGUID)
	time.Sleep(500 * time.Millisecond)

	b.ensureMovementController()

	// Wait a bit more for all initial data to arrive
	time.Sleep(1 * time.Second)

	// Complete any cinematic (new characters get a cinematic that may block chat)
	b.world.CompleteCinematic()
	time.Sleep(500 * time.Millisecond)

	// Step 7: Initialize navigation
	b.initNavigation()

	// Snap to real ground height right after entering the world.
	// This prevents the bot from starting under the map due to DB position
	// or login placement not being perfectly on terrain.
	if b.nav != nil {
		x, y, z, _, mapID := b.world.Position()
		probeZ := z + 5.0
		if gh, ok := movementGroundHeight(b.nav, mapID, x, y, probeZ); ok {
			delta := gh - z
			if delta <= 1.0 {
				_, _, _, o, _ := b.world.Position()
				b.world.UpdatePosition(x, y, gh, o)
			}
		}
	} else {
		// no nav available for ground snap
	}

	// Step 8: Initialize Lua engine
	b.lua = luaengine.NewEngine(b)

	// AIBundle / LuaCode / LuaScript loading (precedence: AIBundle.Main > LuaCode > LuaScript file).
	// This enables scenario-driven custom AI as specified in the design.
	if !b.config.AIBundle.IsEmpty() {
		if b.config.AIBundle.Main != "" {
			if err := b.lua.DoString(b.config.AIBundle.Main); err != nil {
				b.log("AIBundle Main load error: %v", err)
			}
		}
		// Sorted helper keys for deterministic load order.
		keys := make([]string, 0, len(b.config.AIBundle.Helpers))
		for k := range b.config.AIBundle.Helpers {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if code := b.config.AIBundle.Helpers[k]; code != "" {
				if err := b.lua.DoString(code); err != nil {
					b.log("AIBundle helper %s load error: %v", k, err)
				}
			}
		}
		if len(b.config.AIBundle.Data) > 0 {
			b.lua.SetTable("scenario_data", b.config.AIBundle.Data)
		}
		if b.config.AIBundle.TickFunc != "" {
			b.lua.SetTickFunc(b.config.AIBundle.TickFunc)
		}
	} else if b.config.LuaCode != "" {
		if err := b.lua.DoString(b.config.LuaCode); err != nil {
			b.log("LuaCode load error: %v", err)
		}
	} else if b.config.LuaScript != "" {
		if err := b.lua.DoFile(b.config.LuaScript); err != nil {
			b.log("Failed to load Lua script: %v", err)
		}
	}

	// Step 9: Build behavior tree
	b.bb = behaviortree.NewBlackboard()
	b.bb.Set("mode", b.config.Mode)
	b.bb.Set("setup_done", true) // Pre-setup already completed
	b.tree = behaviortree.NewTree(b.buildBehaviorTree())
	b.tree.Blackboard = b.bb

	// Step 10: Run AI loop
	b.addEvent("start", "Bot entered world, starting AI loop (mode: %s, level: %d)", b.config.Mode, b.world.PlayerLevel())

	// Log Hogger specifically when found (reduced logging)
	if b.config.Mode == "hogger" {
		b.world.OnObjectUpdate = func(guid uint64, obj *client.WorldObject) {
			if obj.TypeID == client.ObjectTypeUnit && obj.Entry == gamedata.HoggerInfo.Entry {
				b.log("Hogger update: GUID=%d HP=%d/%d Pos=(%.1f,%.1f,%.1f)",
					guid, obj.Health(), obj.MaxHealth(), obj.PosX, obj.PosY, obj.PosZ)
			}
		}
	}

	b.runAILoop(worldErrCh)

	b.world.Close()
	if b.nav != nil {
		b.nav.Close()
	}
	if b.lua != nil {
		b.lua.Close()
	}
	b.closeValidation()

	b.setStatus(BotStatusDone)
	b.log("Bot finished. Kills: %d, Deaths: %d", b.kills, b.deaths)

	return BotResult{
		ID:     b.id,
		Status: BotStatusDone,
		Level:  b.world.PlayerLevel(),
		Kills:  b.kills,
		Deaths: b.deaths,
	}
}

// preLoginDBSetup modifies the character's level, position, spells, and equipment
// in the database BEFORE logging in. This is called after character creation/finding
// but before LoginCharacter. The character must be offline for DB changes to take effect.
func (b *Bot) preLoginDBSetup() {
	var level int
	var posX, posY, posZ float64
	var mapID int
	needsUpdate := false

	_ = b.config // config used below

	switch b.config.Mode {
	case "hogger":
		// Hogger is level 11 elite (rank 1) with ~666 HP.
		// A real player would typically fight him around level 12-15 in a group,
		// or solo at level 15+ with decent gear. We use level 15 with full gear.
		level = 15
		// Spawn near the road between Goldshire and Hogger's area
		// This is a safe area free of hostile mobs
		posX = -9819.0
		posY = 450.0
		posZ = 34.0
		mapID = 0
		needsUpdate = true
	case "dungeon":
		dungeonName := b.config.DungeonName
		if dungeonName == "" {
			dungeonName = "ragefire_chasm"
		}
		info, ok := gamedata.Dungeons[dungeonName]
		if !ok {
			b.log("Pre-login: Unknown dungeon %s", dungeonName)
			return
		}
		level = int((info.MinLevel + info.MaxLevel) / 2)
		posX, posY, posZ = float64(info.EntranceX), float64(info.EntranceY), float64(info.EntranceZ)
		mapID = int(info.EntranceMapID)
		needsUpdate = true
	default:
		// For siege / general load, start at Orgrimmar gate area at level 80.
		// This prevents spawning at racial starts (Teldrassil etc) that can't path to Orgrimmar.
		level = 80
		posX = 1368
		posY = -4373
		posZ = 26.057
		mapID = 1
		// small per-bot jitter so not all on exact same point
		jitter := float64((len(b.config.CharacterName)%10)-5) * 2.0
		posX += jitter
		posY += jitter * 0.5
		needsUpdate = true
	}

	// Always force correct starting map/pos from gamedata for the race, to ensure correct mapID for pathfinding (esp. Blood Elf 530, Draenei 530)
	// This overrides server default if it sends wrong map (e.g. 0) for starting zones.
	// For siege we already set above; only apply race start if not set.
	startMap, startX, startY, startZ := gamedata.RaceStartPosition(b.config.Race)
	if (startMap != 0 || (posX == 0 && posY == 0)) && posX == 0 { // if we have good start data and not overridden
		mapID = int(startMap)
		posX = float64(startX)
		posY = float64(startY)
		posZ = float64(startZ)
		needsUpdate = true
		// forcing race start position for correct map/level in pathfinding
	}

	if !needsUpdate {
		// skipping DB update (position already correct)
		return
	}

	dsn := b.config.CharDBDSN
	if dsn == "" {
		dsn = "acore:acore@tcp(127.0.0.1:3306)/acore_characters"
	}

	db, err := openDB(dsn)
	if err != nil {
		b.log("Pre-login: Cannot connect to DB: %v", err)
		return
	}
	defer db.Close()

	charName := b.config.CharacterName
	b.log("Pre-login: Setting %s to level=%d pos=(%.1f,%.1f,%.1f) map=%d",
		charName, level, posX, posY, posZ, mapID)

	// Update level, position, clear ghost/dead flags, and set full health
	// health=1 is a placeholder; the server will cap it to maxhealth on login
	_, err = db.Exec(
		`UPDATE characters SET level=?, position_x=?, position_y=?, position_z=?, map=?,
		 health=99999, power1=99999, playerFlags=playerFlags&~(16|32)
		 WHERE name=? AND online=0`,
		level, posX, posY, posZ, mapID, charName,
	)
	if err != nil {
		b.log("Pre-login: DB update failed: %v", err)
		return
	}
	_ = charName // logged via normal flow if needed

	// Get character GUID for spell/item setup
	var charGUID uint64
	err = db.QueryRow("SELECT guid FROM characters WHERE name=?", charName).Scan(&charGUID)
	if err != nil {
		b.log("Pre-login: Can't find character GUID: %v", err)
		return
	}

	// Give the character appropriate spells for their level
	b.grantSpells(db, charGUID, uint32(level))

	// Give the character equipment appropriate for their level
	b.grantEquipment(db, charGUID, uint32(level))

	b.log("Pre-login: Setup complete for character GUID %d", charGUID)
}

// grantSpells inserts all necessary warrior spells for the given level.
func (b *Bot) grantSpells(db *sql.DB, charGUID uint64, level uint32) {
	allSpells := []uint32{}
	for id, info := range gamedata.WarriorSpells {
		if info.Level <= level {
			allSpells = append(allSpells, id)
		}
	}
	for _, spellID := range allSpells {
		db.Exec("INSERT IGNORE INTO character_spell (guid, spell, specMask) VALUES (?, ?, 255)", charGUID, spellID)
	}
	// Armor proficiencies
	proficiencies := []uint32{
		9116,  // Shield proficiency
		750,   // Plate Mail (not available until 40 but add anyway)
		8737,  // Mail armor proficiency
		9077,  // Leather proficiency
		9078,  // Cloth proficiency
		196,   // One-Handed Axes
		197,   // Two-Handed Axes
		198,   // One-Handed Maces
		199,   // Two-Handed Maces
		200,   // Polearms
		201,   // One-Handed Swords
		202,   // Two-Handed Swords
		227,   // Staves
		264,   // Bows
		5011,  // Crossbows
		266,   // Guns
		15590, // Fist Weapons
	}
	for _, spellID := range proficiencies {
		db.Exec("INSERT IGNORE INTO character_spell (guid, spell, specMask) VALUES (?, ?, 255)", charGUID, spellID)
	}
	b.log("Pre-login: %d spells + proficiencies added for level %d", len(allSpells), level)
}

// grantEquipment gives the character a set of appropriate gear via direct DB inserts.
// This creates item_instance entries and character_inventory entries for equipped slots.
func (b *Bot) grantEquipment(db *sql.DB, charGUID uint64, level uint32) {
	// Equipment loadout: slot -> itemEntry
	// Using green/blue mail items appropriate for level 15 warrior
	equipment := map[int]uint32{
		// slot 4 = chest: Ironforge Breastplate (entry 6731, mail, armor 198, iLvl 20)
		4: 6731,
		// slot 6 = legs: Foreman's Leggings (entry 2166, mail, armor 147, iLvl 20)
		6: 2166,
		// slot 7 = feet: Silver-linked Footguards (entry 12982, mail, armor 129, iLvl 21)
		7: 12982,
		// slot 8 = wrists: Cavedweller Bracers (entry 14147, mail, armor 78, iLvl 18)
		8: 14147,
		// slot 9 = hands: Polar Gauntlets (entry 7606, mail, armor 109, iLvl 22)
		9: 7606,
		// slot 5 = waist: Stormbringer Belt (entry 12978, mail, armor 104, iLvl 20)
		5: 12978,
		// slot 2 = shoulders: Rough Bronze Shoulders (entry 3480, mail, armor 130, iLvl 22)
		2: 3480,
		// slot 15 = main hand (2H): Rhahk'Zor's Hammer (entry 5187, 2H mace, 45-68 dmg, iLvl 20)
		15: 5187,
	}

	// Check which slots already have items
	rows, err := db.Query(
		"SELECT slot FROM character_inventory WHERE guid=? AND bag=0 AND slot < 19",
		charGUID,
	)
	if err != nil {
		b.log("Pre-login: Failed to query inventory: %v", err)
		return
	}
	equippedSlots := make(map[int]bool)
	for rows.Next() {
		var slot int
		rows.Scan(&slot)
		equippedSlots[slot] = true
	}
	rows.Close()

	// Get the next available item_instance GUID (table has no auto_increment)
	var maxItemGUID uint32
	err = db.QueryRow("SELECT COALESCE(MAX(guid), 0) FROM item_instance").Scan(&maxItemGUID)
	if err != nil {
		b.log("Pre-login: Failed to get max item GUID: %v", err)
		return
	}
	nextItemGUID := maxItemGUID + 1

	enchantments := "0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 "
	itemsAdded := 0
	for slot, itemEntry := range equipment {
		if equippedSlots[slot] {
			continue // Already has an item in this slot
		}

		itemGUID := nextItemGUID
		nextItemGUID++

		// Create item_instance with explicit GUID
		_, err := db.Exec(
			"INSERT INTO item_instance (guid, itemEntry, owner_guid, count, durability, enchantments) VALUES (?, ?, ?, 1, 100, ?)",
			itemGUID, itemEntry, charGUID, enchantments,
		)
		if err != nil {
			b.log("Pre-login: Failed to create item %d (guid %d): %v", itemEntry, itemGUID, err)
			continue
		}

		// Put item in equipped slot
		_, err = db.Exec(
			"INSERT INTO character_inventory (guid, bag, slot, item) VALUES (?, 0, ?, ?)",
			charGUID, slot, itemGUID,
		)
		if err != nil {
			b.log("Pre-login: Failed to equip item %d to slot %d: %v", itemEntry, slot, err)
			db.Exec("DELETE FROM item_instance WHERE guid=?", itemGUID)
			continue
		}
		itemsAdded++
	}
	b.log("Pre-login: Equipped %d items for character", itemsAdded)
}

func (b *Bot) initNavigation() {
	if b.config.PathfindingAddress != "" {
		nav, err := navigation.NewRemoteNavigator(b.config.PathfindingAddress)
		if err != nil {
			b.log("Failed to connect to pathfinding service: %v, falling back to embedded", err)
		} else {
			b.nav = nav
			b.log("Using remote pathfinding service at %s", b.config.PathfindingAddress)
			b.attachNavigatorToMovementController()
			return
		}
	}
	if b.config.DataDir != "" {
		b.nav = navigation.NewEmbeddedNavigator(b.config.DataDir)
		b.log("Using embedded pathfinding with data dir %s", b.config.DataDir)
	} else {
		b.log("No pathfinding configured, movement will be direct")
	}
	b.attachNavigatorToMovementController()
}

func (b *Bot) attachNavigatorToMovementController() {
	if b.nav == nil {
		return
	}
	b.movementMu.Lock()
	defer b.movementMu.Unlock()
	if b.moveController != nil {
		b.moveController.SetNavigator(b.nav)
	}
}

func (b *Bot) ensureMovementController() {
	b.movementMu.Lock()
	defer b.movementMu.Unlock()
	b.ensureMovementControllerLocked()
}

func (b *Bot) ensureMovementControllerLocked() {
	if b.moveController != nil {
		if b.nav != nil {
			b.moveController.SetNavigator(b.nav)
		}
		return
	}
	if b.world == nil {
		return
	}
	sender := movement.NewWorldMovementSender(b.world)
	cfg := movement.DefaultMovementConfig()
	speed := b.world.MoveSpeed()
	if speed <= 0 {
		speed = 7.0
	}
	b.moveController = movement.NewMovementController(sender, speed, b.nav, cfg)
	// Seed the controller with the real position we just got from the server so we don't start at (0,0,0)
	cx, cy, cz, co, _ := b.world.Position()
	b.moveController.InitPositionFromWorld(cx, cy, cz, co)
}

func (b *Bot) runAILoop(worldErrCh chan error) {
	tick := time.Duration(b.config.AITickMs) * time.Millisecond
	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	heartbeatTicker := time.NewTicker(5 * time.Second)
	defer heartbeatTicker.Stop()

	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()
	var pingSeq uint32

	movementStop := make(chan struct{})
	movementDone := make(chan struct{})
	go b.runMovementLoop(movementStop, movementDone)
	defer func() {
		close(movementStop)
		<-movementDone
	}()

	for {
		select {
		case <-b.stopCh:
			return
		case err := <-worldErrCh:
			if err != nil {
				b.log("World connection error: %v", err)
			}
			return
		case <-heartbeatTicker.C:
			// Only force a keepalive when not actively driving movement.
			// Frequent movement HBs are sent from runMovementLoop, independent of AI ticks.
			b.movementMu.Lock()
			if !b.isMoving {
				b.world.SendHeartbeat()
			}
			b.movementMu.Unlock()
		case <-pingTicker.C:
			pingSeq++
			b.world.SendPing(pingSeq)
		case <-ticker.C:
			tickStart := time.Now()
			if b.world != nil {
				// Very verbose position debug on every tick (can be noisy, but useful to catch teleports)
				// px, py, pz, po, pmid := b.world.Position()
				// if time.Since(b.lastDecisionChat) > 2*time.Second {
				// 	b.log("[DEBUG-POS] tick pos: map=%d (%.1f,%.1f,%.1f) o=%.2f", pmid, px, py, pz, po)
				// }
			}
			// Lua tick gets priority
			if b.lua != nil && b.lua.CallTick() {
				elapsed := time.Since(tickStart)
				if elapsed > 300*time.Millisecond {
					ms := elapsed.Milliseconds()
					b.log("AI tick update took %dms (lua path)", ms)
					if b.config.LogDecisionsToChat {
						chat := fmt.Sprintf("[TICK] %dms", ms)
						_ = b.world.SendChatMessage(client.ChatMsgSay, client.LangCommon, chat)
					}
				}
				continue
			}

			// Persistent live pursuit: keep path to current target's live position if far.
			// Throttle re-path to ~300ms to avoid jitter and high CPU from re-SetPath every tick.
			// This prevents stale paths (reaching old pos while mob moved) and smooths movement.
			if guid := b.world.TargetGUID(); guid != 0 {
				if t := b.world.GetObject(guid); t != nil && t.IsAlive() && !b.isKnownDead(guid) {
					tx, ty, tz := t.InterpolatedPosition()
					b.lastPursuedTargetGUID = guid
					b.lastPursuedTargetPos = [3]float32{tx, ty, tz}
					b.lastPursuedTargetTime = time.Now()
					px, py, _ := b.myPos()
					dx := tx - px
					dy := ty - py
					dist2d := float32(math.Sqrt(float64(dx*dx + dy*dy)))
					targetMoved := b.lastMoveToTargetTime.IsZero() ||
						(math.Abs(float64(tx-b.lastMoveToTargetPos[0])) > 3.0 ||
							math.Abs(float64(ty-b.lastMoveToTargetPos[1])) > 3.0 ||
							math.Abs(float64(tz-b.lastMoveToTargetPos[2])) > 3.0)
					if dist2d > 2.5 && (b.lastPursuitUpdate.IsZero() || time.Since(b.lastPursuitUpdate) > 1000*time.Millisecond || targetMoved) {
						b.moveToPoint(tx, ty, tz)
						b.lastPursuitUpdate = time.Now()
						b.lastMoveToTargetPos = [3]float32{tx, ty, tz}
						b.lastMoveToTargetTime = time.Now()
					}
				} else if b.lastPursuedTargetGUID == guid && time.Since(b.lastPursuedTargetTime) < 30*time.Second {
					// Use last known pos to continue pursuit even if object temporarily not visible
					tx, ty, tz := b.lastPursuedTargetPos[0], b.lastPursuedTargetPos[1], b.lastPursuedTargetPos[2]
					px, py, _ := b.myPos()
					dx := tx - px
					dy := ty - py
					dist2d := float32(math.Sqrt(float64(dx*dx + dy*dy)))
					if dist2d > 2.5 {
						b.moveToPoint(tx, ty, tz)
						b.lastMoveToTargetPos = [3]float32{tx, ty, tz}
						b.lastMoveToTargetTime = time.Now()
					}
				}
			}

			// Cleanup: if current target is dead or gone, clear it so we don't stay "looking"
			// at a dead creature and can fall through to wander/explore.
			// Also clear lastLootGUID for dead/gone loot targets so we don't get stuck on them.
			if tg := b.world.TargetGUID(); tg != 0 {
				t := b.world.GetObject(tg)
				if t == nil || !t.IsAlive() {
					b.world.MarkObjectDead(tg)
					b.world.ClearTarget()
					b.world.ClearCombat()
					b.stopCurrentMove()
					if b.lastLootGUID == tg {
						b.lastLootGUID = 0
					}
				} else if t.Value(client.UnitNPCFlags) != 0 || !b.isHostileFaction(t.Value(client.UnitFieldFaction)) {
					// Drop friendly vendors/NPCs even if we had them targeted (data may update late or target came from elsewhere)
					b.world.ClearTarget()
					b.world.ClearCombat()
					b.stopCurrentMove()
					if b.lastLootGUID == tg {
						b.lastLootGUID = 0
					}
				}
			}
			if lg := b.lastLootGUID; lg != 0 {
				obj := b.world.GetObject(lg)
				if obj == nil || !obj.IsAlive() {
					b.lastLootGUID = 0
				}
			}

			// Unstick from a selected target that never led to combat (may be dead on server but cache stale, or unattackable)
			if tg := b.world.TargetGUID(); tg != 0 && !b.currentTargetSetAt.IsZero() {
				if time.Since(b.currentTargetSetAt) > 12*time.Second && !b.world.InCombat() {
					tgo := b.world.GetObject(tg)
					currH := uint32(0)
					if tgo != nil {
						currH = tgo.Health()
					}
					noProgress := b.engagedTargetHealth == 0 || currH >= b.engagedTargetHealth
					if noProgress || tgo == nil || !tgo.IsAlive() {
						b.world.ClearTarget()
						b.world.ClearCombat()
						b.stopCurrentMove()
						b.currentTargetSetAt = time.Time{}
						b.lastEngagedGUID = 0
						b.engagedTargetHealth = 0
					}
				}
			}

			// Behavior tree tick
			// NOTE: We do not do a periodic tick snap here. Paths are ground-snapped
			// before being handed to the MovementController, and decisions use the
			// world pos kept current by the controller.
			b.tree.Tick()

			elapsed := time.Since(tickStart)
			if elapsed > 300*time.Millisecond {
				ms := elapsed.Milliseconds()
				b.log("AI tick update took %dms", ms)
				if b.config.LogDecisionsToChat {
					chat := fmt.Sprintf("[TICK] %dms", ms)
					_ = b.world.SendChatMessage(client.ChatMsgSay, client.LangCommon, chat)
				}
			}

			// Periodic state - only to chat on major decisions, not every tick (console unreadable at scale)
			// removed the every-tick b.log DEBUG STATE spam
			_ = b.world.TargetGUID() // keep variable usage minimal if needed elsewhere

			// Periodic status log removed from console (noisy at scale; use chat decisions)
			// if needed, decisions will surface key state via logDecision to /say

		}
	}
}

func (b *Bot) runMovementLoop(stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)

	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-b.stopCh:
			return
		case <-stop:
			return
		case <-ticker.C:
			b.updateMovement()
		}
	}
}

// ============================================================
// Behavior tree construction
// ============================================================

func (b *Bot) buildBehaviorTree() behaviortree.Node {
	return behaviortree.NewSelector("root",
		// Priority 1: Handle death
		behaviortree.NewSequence("handle_death",
			behaviortree.NewCondition("is_dead", func(bb *behaviortree.Blackboard) bool {
				return !b.IsAlive() || b.deaths > 0 && b.world.Health() == 0
			}),
			behaviortree.NewAction("release_and_respawn", func(bb *behaviortree.Blackboard) behaviortree.Status {
				deathTime := bb.GetInt("death_time")
				now := int(time.Now().Unix())
				if deathTime == 0 {
					// First tick after death: release spirit
					b.log("Dead, releasing spirit...")
					bb.Set("death_time", now)
					// Stop any movement
					b.stopCurrentMove()
					b.world.AttackStop()
					b.world.RepopRequest()
					return behaviortree.Running
				}
				elapsed := now - deathTime
				if elapsed < 32 {
					// Wait for corpse reclaim timer (30s default in AzerothCore)
					if elapsed%5 == 0 {
						b.log("Waiting to reclaim corpse (%ds elapsed, need 30s)...", elapsed)
					}
					return behaviortree.Running
				}
				// Try to reclaim corpse
				b.log("Reclaiming corpse after %ds...", elapsed)
				b.world.ReclaimCorpse()
				// Reset death tracking after a delay for server to process
				bb.Set("death_time", 0)
				return behaviortree.Success
			}),
		),

		// Priority 2: Loot nearby corpses (only if the corpse is already close; we never path to dead)
		behaviortree.NewSequence("loot_nearby",
			behaviortree.NewCondition("has_lootable_target", func(bb *behaviortree.Blackboard) bool {
				if b.lastLootGUID == 0 {
					return false
				}
				obj := b.world.GetObject(b.lastLootGUID)
				if obj == nil || !obj.IsAlive() {
					return false
				}
				d := obj.DistanceTo(b.myPos())
				closeEnough := d <= 10.0
				if !closeEnough {
					b.logDecision("loot cond: corpse too far dist=%.1f", d)
				}
				return closeEnough
			}),
			behaviortree.NewAction("loot_target", func(bb *behaviortree.Blackboard) behaviortree.Status {
				return b.actionLoot()
			}),
		),

		// Priority 3: Fight current target
		behaviortree.NewSequence("fight_target",
			behaviortree.NewCondition("in_combat", func(bb *behaviortree.Blackboard) bool {
				return b.world.InCombat()
			}),
			behaviortree.NewAction("combat_rotation", func(bb *behaviortree.Blackboard) behaviortree.Status {
				return b.actionCombatRotation()
			}),
		),

		// Priority 4: Mode-specific behavior
		b.buildModeBehavior(),
	)
}

func (b *Bot) buildModeBehavior() behaviortree.Node {
	switch b.config.Mode {
	case "hogger":
		return b.buildHoggerBehavior()
	case "dungeon":
		return b.buildDungeonBehavior()
	case "idle":
		return behaviortree.NewAction("idle", func(bb *behaviortree.Blackboard) behaviortree.Status {
			return behaviortree.Running
		})
	case "lua":
		return behaviortree.NewAction("lua_control", func(bb *behaviortree.Blackboard) behaviortree.Status {
			return behaviortree.Running
		})
	default: // "grind"
		return b.buildGrindBehavior()
	}
}

func (b *Bot) buildGrindBehavior() behaviortree.Node {
	return behaviortree.NewSelector("grind",
		// Find and attack nearby mobs
		behaviortree.NewSequence("find_and_fight",
			behaviortree.NewCondition("find_target", func(bb *behaviortree.Blackboard) bool {
				// Stick to current pursuit target even if it has moved out of initial scan range.
				// This prevents the bot from committing to an old position and waiting there
				// while the creature has walked away.
				if b.grindTargetGUID != 0 {
					if t := b.world.GetObject(b.grindTargetGUID); t != nil && t.IsAlive() && !b.isKnownDead(b.grindTargetGUID) {
						// re-validate not friendly
						if t.Value(client.UnitNPCFlags) == 0 && b.isHostileFaction(t.Value(client.UnitFieldFaction)) {
							return true
						}
					}
					b.grindTargetGUID = 0
				}

				target := b.findBestTarget(38)
				if target != nil {
					b.grindTargetGUID = target.GUID
					d := target.DistanceTo(b.myPos())
					deadF := (target.Values[client.UnitFieldFlags] & client.UnitFlagDead) != 0
					b.sendAliveReasonChat("FOUND target Entry=%d dist=%.1fyd: h=%d/%d IsAlive=%v deadFlag=%v f=0x%x npc=0x%x fac=%d",
						target.Entry, d, target.Health(), target.MaxHealth(), target.IsAlive(), deadF, target.Values[client.UnitFieldFlags], target.Values[client.UnitNPCFlags], target.Values[client.UnitFieldFaction])
					return true
				}
				b.logDecision("No suitable target, wandering to find mobs")
				return false
			}),
			behaviortree.NewAction("engage_target", func(bb *behaviortree.Blackboard) behaviortree.Status {
				return b.actionEngageTarget(b.grindTargetGUID)
			}),
		),

		// Wander to find mobs
		behaviortree.NewAction("wander", func(bb *behaviortree.Blackboard) behaviortree.Status {
			return b.actionWander()
		}),
	)
}

func (b *Bot) buildHoggerBehavior() behaviortree.Node {
	var hoggerGUID uint64
	hoggerKilled := false
	battleShoutUsed := false
	return behaviortree.NewSelector("hogger_hunt",
		// Priority 1: If Hogger is killed, we're done
		behaviortree.NewSequence("hogger_done_check",
			behaviortree.NewCondition("hogger_killed", func(bb *behaviortree.Blackboard) bool {
				return hoggerKilled
			}),
			behaviortree.NewAction("celebrate", func(bb *behaviortree.Blackboard) behaviortree.Status {
				b.addEvent("hogger_kill", "Hogger has been slain!")
				return behaviortree.Running // Stay alive for logging
			}),
		),

		// Priority 2: Find and fight Hogger
		behaviortree.NewSequence("find_hogger",
			behaviortree.NewCondition("hogger_visible", func(bb *behaviortree.Blackboard) bool {
				units := b.world.GetNearbyUnits(80)
				for _, u := range units {
					if u.Entry == gamedata.HoggerInfo.Entry {
						if u.IsAlive() {
							hoggerGUID = u.GUID
							return true
						}
						// Hogger dead = we killed him (or he died)
						hoggerKilled = true
					}
				}
				return false
			}),
			behaviortree.NewAction("attack_hogger", func(bb *behaviortree.Blackboard) behaviortree.Status {
				// Pre-combat: Battle Shout (6673). Never cast 2457 here — that is Battle Stance.
				if !battleShoutUsed {
					if b.world.IsSpellReady(6673) {
						b.log("Pre-combat: casting Battle Shout")
						b.world.CastSpell(6673, 0)
					}
					battleShoutUsed = true
				}

				target := b.world.GetObject(hoggerGUID)
				if target == nil || !target.IsAlive() {
					return behaviortree.Failure
				}

				dist := target.DistanceTo(b.myPos())

				// Use Charge if in range (8-25 yards) and not in combat
				if dist >= 8 && dist <= 25 && !b.world.InCombat() && b.world.IsSpellReady(100) {
					b.log("Charging Hogger! dist=%.1f", dist)
					b.world.SetTarget(hoggerGUID)
					b.world.CastSpell(100, hoggerGUID) // Charge
					return behaviortree.Running
				}

				return b.actionEngageTarget(hoggerGUID)
			}),
		),

		// Priority 3: Fight anything that attacks us (clear adds before Hogger)
		behaviortree.NewSequence("fight_attackers",
			behaviortree.NewCondition("being_attacked", func(bb *behaviortree.Blackboard) bool {
				return b.world.InCombat()
			}),
			behaviortree.NewAction("fight_back", func(bb *behaviortree.Blackboard) behaviortree.Status {
				return b.actionCombatRotation()
			}),
		),

		// Priority 4: Move toward Hogger's spawn while waiting
		behaviortree.NewAction("move_to_hogger_area", func(bb *behaviortree.Blackboard) behaviortree.Status {
			hx := float32(gamedata.HoggerInfo.PosX)
			hy := float32(gamedata.HoggerInfo.PosY)
			hz := float32(gamedata.HoggerInfo.PosZ)
			px, py, _ := b.myPos()
			dist := float32(math.Sqrt(float64(
				(hx-px)*(hx-px) +
					(hy-py)*(hy-py))))
			if dist > 10 {
				b.moveToPoint(hx, hy, hz)
				return behaviortree.Running
			}
			return b.actionWander()
		}),
	)
}

func (b *Bot) buildDungeonBehavior() behaviortree.Node {
	var dungeonTargetGUID uint64
	// Setup is already done in preSetup(), just fight and explore
	return behaviortree.NewSelector("dungeon",
		// Priority 1: Fight current target in combat
		behaviortree.NewSequence("dungeon_in_combat",
			behaviortree.NewCondition("in_combat", func(bb *behaviortree.Blackboard) bool {
				return b.world.InCombat()
			}),
			behaviortree.NewAction("dungeon_combat_rotation", func(bb *behaviortree.Blackboard) behaviortree.Status {
				return b.actionCombatRotation()
			}),
		),

		// Priority 2: Find and engage enemies
		behaviortree.NewSequence("find_dungeon_mob",
			behaviortree.NewCondition("enemy_in_range", func(bb *behaviortree.Blackboard) bool {
				target := b.findBestTarget(40)
				if target != nil {
					dungeonTargetGUID = target.GUID
					return true
				}
				return false
			}),
			behaviortree.NewAction("fight_dungeon_mob", func(bb *behaviortree.Blackboard) behaviortree.Status {
				return b.actionEngageTarget(dungeonTargetGUID)
			}),
		),

		// Priority 3: Explore dungeon
		behaviortree.NewAction("explore_dungeon", func(bb *behaviortree.Blackboard) behaviortree.Status {
			return b.actionWander()
		}),
	)
}

// ============================================================
// Bot actions (used by behavior tree)
// ============================================================

func (b *Bot) findBestTarget(maxDist float32) *client.WorldObject {
	// noisy enter log removed - console unreadable at scale.
	// decisions + alive reasons go via logDecision / sendAliveReasonChat to chat.
	now := time.Now()

	// When DisableTargetCache is set we always do a full fresh scan.
	// This helps when the bot is attacking dead creatures (stale "alive" in cache)
	// or wandering while live mobs are nearby (stale "no target" decision).
	if !b.config.DisableTargetCache && b.targetCacheGUID != 0 {
		// Always fetch fresh object to get current position and state.
		// Using cached object snapshot was causing stale positions for moving mobs,
		// leading to attacking "mobs that were not there" or at old locations.
		c := b.world.GetObject(b.targetCacheGUID)
		if c == nil {
			b.targetCacheGUID = 0
		} else if b.isKnownDead(c.GUID) {
			if c.Health() > 0 {
				b.clearKnownDead(c.GUID)
			} else {
				b.targetCacheGUID = 0
			}
		} else if !c.IsAlive() {
			b.targetCacheGUID = 0
		} else if now.Sub(b.targetCacheTime) < 800*time.Millisecond {
			d := c.DistanceTo(b.myPos())
			if d <= maxDist {
				// Re-check npc flags / faction in case data updated since cache
				npcf := c.Value(client.UnitNPCFlags)
				fac := c.Value(client.UnitFieldFaction)
				if npcf != 0 || !b.isHostileFaction(fac) {
					b.targetCacheGUID = 0
				} else {
					return c
				}
			}
		}
	}

	units := b.world.GetNearbyUnits(maxDist)
	skippedDead := 0
	skippedNotHostile := 0
	skippedFriendlyNPC := 0
	skippedLevel := 0
	skippedLowHP := 0

	var candidates []*client.WorldObject
	for _, u := range units {
		if b.isKnownDead(u.GUID) {
			if u.Health() > 0 {
				b.clearKnownDead(u.GUID)
				// fall through, treat as possibly alive now
			} else {
				skippedDead++
				continue
			}
		}
		if !u.IsAlive() {
			skippedDead++
			continue
		}
		flags := u.Value(client.UnitFieldFlags)
		if flags&client.UnitFlagNotAttackable != 0 ||
			flags&client.UnitFlagTaxiFlight != 0 ||
			flags&client.UnitFlagNotAttackable1 != 0 ||
			flags&client.UnitFlagNotAttackable2 != 0 ||
			flags&client.UnitFlagNotSelectable != 0 {
			skippedNotHostile++
			continue
		}

		// Skip NPCs that have interaction flags (quest givers, vendors, trainers, guards, gossip, etc.).
		// These are almost always friendly and should never be attacked.
		npcFlags := u.Value(client.UnitNPCFlags)
		if npcFlags != 0 {
			skippedFriendlyNPC++
			continue
		}

		faction := u.Value(client.UnitFieldFaction)
		hostile := b.isHostileFaction(faction)
		if !hostile {
			skippedNotHostile++
			continue
		}
		ulevel := u.Level()
		myLevel := b.world.PlayerLevel()
		// Allow a bit wider level range so fresh low-level bots in starting zones
		// can actually find and prioritize killing appropriate mobs instead of
		// only wandering.
		if ulevel > myLevel+6 {
			skippedLevel++
			continue
		}
		if u.MaxHealth() <= 1 {
			skippedLowHP++
			continue
		}
		candidates = append(candidates, u)
	}

	var best *client.WorldObject
	if len(candidates) > 0 {
		// Pick a random candidate to spread bots across different mobs.
		// This prevents all bots targeting the same mob and its position at the time of selection (outdated location).
		idx := mathrand.Intn(len(candidates))
		best = candidates[idx]
	}

	if best != nil {
		bestDist := best.DistanceTo(b.myPos())
		// send useful selection info to chat instead of console spam
		b.logDecision("findBestTarget chose Entry=%d dist=%.1f (skipped dead=%d notHostile=%d friendlyNPC=%d level=%d lowHP=%d total=%d)", best.Entry, bestDist, skippedDead, skippedNotHostile, skippedFriendlyNPC, skippedLevel, skippedLowHP, len(units))
	} else {
		b.logDecision("findBestTarget no target (skipped dead=%d notHostile=%d friendlyNPC=%d level=%d lowHP=%d total=%d)", skippedDead, skippedNotHostile, skippedFriendlyNPC, skippedLevel, skippedLowHP, len(units))
	}

	b.targetCacheGUID = 0
	if best != nil {
		b.targetCacheGUID = best.GUID
	}
	b.targetCacheTime = now
	return best
}

// isHostileFaction checks if a faction template ID is hostile to the player.
// Uses known hostile faction IDs from AzerothCore's factiontemplate_dbc.
func (b *Bot) isHostileFaction(factionTemplate uint32) bool {
	// Explicit friendly / neutral factions that should NEVER be attacked
	// (quest NPCs, vendors, guards, city factions, trainers, etc.)
	switch factionTemplate {
	case 35: // "Friendly" - commonly used for many helpful NPCs
		return false
	case 11, 12, 13: // Common city/guard factions (Stormwind, Ironforge, etc.)
		return false
	case 55, 57, 59, 60: // Other common friendly/neutral
		return false
	case 4, 5, 6, 161, 162: // Additional common starting area / city friendly factions
		return false
	}

	// Known hostile faction templates from AzerothCore:
	// These have FACTION_TEMPLATE_FLAG_HOSTILE_BY_DEFAULT or are Monster factions
	switch factionTemplate {
	case 7: // Defias Brotherhood
		return true
	case 14: // Monster (generic hostile)
		return true
	case 16: // Monster (hostile to all)
		return true
	case 17: // Defias Brotherhood
		return true
	case 20: // Redridge Gnolls
		return true
	case 21: // Gnoll - Riverpaw
		return true
	case 22: // Undead, Scourge
		return true
	case 24: // Beast - Ravager
		return true
	case 25: // Monster (Kobolds etc)
		return true
	case 26: // Defias
		return true
	case 28: // Murloc
		return true
	case 29: // Gnoll - Shadowhide
		return true
	case 32: // Monster (Diseased wolves etc)
		return true
	case 33: // Gnoll - Mosshide
		return true
	case 34: // Monster (hostile to alliance)
		return true
	case 45: // Ogre
		return true
	case 48: // Pirate
		return true
	case 49: // Dalaran
		return true
	case 51: // Syndicate
		return true
	case 54: // Murloc (hostile)
		return true
	case 57: // Lost Ones
		return true
	case 66: // Blackrock
		return true
	case 73: // Dark Iron Dwarves
		return true
	case 80: // Blackfathom
		return true
	case 83: // Scorpid
		return true
	case 87: // Bloodsail Buccaneers
		return true
	case 90: // Burning Blade
		return true
	case 93: // Flamekin
		return true
	case 168: // Enemy (generic)
		return true
	}
	// Default to hostile.
	// Combined with the npcFlags check above (which skips almost all friendly NPCs),
	// this lets us attack wild monsters even if their faction ID is not in the explicit list.
	return true
}

func (b *Bot) actionEngageTarget(guid uint64) behaviortree.Status {
	// Prefer fresh scan but throttle to avoid CPU from full scans + pathing every tick.
	// Check for better target only every 2s or so.
	if b.lastBetterTargetCheck.IsZero() || time.Since(b.lastBetterTargetCheck) > 2*time.Second {
		b.lastBetterTargetCheck = time.Now()
		if fresh := b.findBestTarget(40); fresh != nil && fresh.IsAlive() {
			freshDist := fresh.DistanceTo(b.myPos())
			oldDist := float32(9999)
			currentObj := b.world.GetObject(guid)
			if currentObj != nil {
				oldDist = currentObj.DistanceTo(b.myPos())
			}
			currentAlive := currentObj != nil && currentObj.IsAlive()
			// Switch if the passed guid is bad or the fresh one is significantly closer/better.
			// Note: GetObject can return nil if the object was destroyed/expired (e.g. dead mob cleaned up).
			if guid == 0 || !currentAlive || freshDist < oldDist*0.8 {
				guid = fresh.GUID
			}
		}
	}
	b.grindTargetGUID = guid

	target := b.world.GetObject(guid)
	if target == nil || !target.IsAlive() || b.isKnownDead(guid) {
		b.stopCurrentMove()
		b.world.ClearTarget()
		b.world.ClearCombat()
		b.markKnownDead(guid)
		b.grindTargetGUID = 0
		return behaviortree.Failure
	}
	if target.Value(client.UnitNPCFlags) != 0 || !b.isHostileFaction(target.Value(client.UnitFieldFaction)) {
		b.sendAliveReasonChat("ABORT friendly NPC GUID=%d Entry=%d npc=0x%x fac=%d", guid, target.Entry, target.Value(client.UnitNPCFlags), target.Value(client.UnitFieldFaction))
		b.world.ClearTarget()
		b.world.ClearCombat()
		b.stopCurrentMove()
		b.grindTargetGUID = 0
		return behaviortree.Failure
	}
	// Print to chat the reason we think it's alive (for debugging dead creature attacks)
	deadF := (target.Value(client.UnitFieldFlags) & client.UnitFlagDead) != 0
	b.sendAliveReasonChat("ENGAGE GUID=%d Entry=%d: h=%d/%d IsAlive=%v deadFlag=%v flags=0x%x npc=0x%x fac=%d",
		guid, target.Entry, target.Health(), target.MaxHealth(), target.IsAlive(), deadF,
		target.Value(client.UnitFieldFlags), target.Value(client.UnitNPCFlags), target.Value(client.UnitFieldFaction))

	// Unstick: if we have been "looking at" this target for >6s without entering real combat, drop it.
	// Prevents standing forever on dead/stuck/unattackable mobs that our cache still thinks alive.
	age := time.Duration(0)
	if !b.currentTargetSetAt.IsZero() {
		age = time.Since(b.currentTargetSetAt)
	}
	currHealth := uint32(0)
	if target != nil {
		currHealth = target.Health()
	}
	noProgress := b.engagedTargetHealth == 0 || currHealth >= b.engagedTargetHealth

	// Act faster on low health targets that show no progress (likely dead but health not updated to 0 in cache)
	unstickThreshold := 12 * time.Second
	if currHealth > 0 && currHealth < 20 {
		unstickThreshold = 2500 * time.Millisecond
	}
	if age > unstickThreshold {
		if noProgress {
			if currHealth > 0 && currHealth < 20 {
				b.markKnownDead(guid)
				b.logDecision("Forcing dead on low-health no-progress target h=%d", currHealth)
			}
			b.logDecision("Unsticking from target (no progress)")
			b.world.ClearTarget()
			b.world.ClearCombat()
			b.stopCurrentMove()
			b.currentTargetSetAt = time.Time{}
			b.lastEngagedGUID = 0
			b.engagedTargetHealth = 0
			b.grindTargetGUID = 0
			return behaviortree.Failure
		}
	}
	if b.lastEngagedGUID == guid {
		// re-tick debug moved to chat decisions only
	}

	// Use interpolated position for moving targets
	tx, ty, tz := target.InterpolatedPosition()
	dx := tx - func() float32 { x, _, _, _, _ := b.world.Position(); return x }()
	dy := ty - func() float32 { _, y, _, _, _ := b.world.Position(); return y }()
	dist2d := float32(math.Sqrt(float64(dx*dx + dy*dy)))
	dist := target.DistanceTo(b.myPos()) // 3D for logs etc.

	// Use Charge if in range (8-25 yards) and not in combat
	if dist >= 8 && dist <= 25 && !b.world.InCombat() && b.world.IsSpellReady(100) {
		b.log("Charging target Entry=%d GUID=%d dist=%.1f", target.Entry, guid, dist)
		b.logDecision("Charging target (Entry=%d)", target.Entry)
		b.world.SetTarget(guid)
		if b.lastEngagedGUID != guid {
			b.currentTargetSetAt = time.Now()
			b.lastEngagedGUID = guid
			b.engagedTargetHealth = target.Health()
		}
		b.world.CastSpell(100, guid) // Charge
		return behaviortree.Running
	}

	// Set target early so we are attacking while closing the gap
	b.world.SetTarget(guid)
	if b.lastEngagedGUID != guid {
		b.currentTargetSetAt = time.Now()
		b.lastEngagedGUID = guid
		if target != nil {
			b.engagedTargetHealth = target.Health()
		}
	}

	// If too far in horizontal, move closer (follow to within 2 yards XY). Use 2D so height diffs don't stop pursuit.
	if dist2d > 2.0 {
		b.logDecision("Moving toward target (dist2d=%.1fyd, 3d=%.1f)", dist2d, dist)
		b.moveToPoint(tx, ty, tz)
		return behaviortree.Running
	}

	// Stop moving, start attacking
	if b.movementActive() {
		px, py, _ := b.myPos()
		dx = tx - px
		dy = ty - py
		facing := float32(math.Atan2(float64(dy), float64(dx)))
		b.world.SetFacing(facing)
		b.stopCurrentMove()
	}

	// Face the target
	px, py, _ := b.myPos()
	dx = tx - px
	dy = ty - py
	facing := float32(math.Atan2(float64(dy), float64(dx)))
	_, _, _, o, _ := b.world.Position()
	if math.Abs(float64(o-facing)) > 0.1 {
		b.world.SetFacing(facing)
	}

	// Only claim "melee engage" when actually close. Otherwise it's pursuit.
	curDist := target.DistanceTo(b.myPos())
	playerX, playerY, playerZ := b.myPos()
	mobX, mobY, mobZ := tx, ty, tz
	if curDist <= 3.0 {
		b.sendAliveReasonChat("ENGAGE melee GUID=%d Entry=%d: h=%d/%d IsAlive=%v flags=0x%x dist=%.1f player=(%.1f,%.1f,%.1f) mob=(%.1f,%.1f,%.1f)",
			guid, target.Entry, target.Health(), target.MaxHealth(), target.IsAlive(), target.Values[client.UnitFieldFlags], curDist, playerX, playerY, playerZ, mobX, mobY, mobZ)
		b.log("ATTACK ENGAGE melee GUID=%d Entry=%d dist=%.1f player=(%.1f,%.1f,%.1f) mob=(%.1f,%.1f,%.1f)", guid, target.Entry, curDist, playerX, playerY, playerZ, mobX, mobY, mobZ)
	}
	// Debug to catch attacking non-existing or far mob (console for visibility)
	if obj := b.world.GetObject(guid); obj == nil {
		b.log("ATTACK on missing object GUID=%d dist=%.1f player=(%.1f,%.1f,%.1f) mob=(%.1f,%.1f,%.1f)", guid, curDist, playerX, playerY, playerZ, mobX, mobY, mobZ)
	} else if !obj.IsAlive() {
		b.log("ATTACK on dead object GUID=%d dist=%.1f player=(%.1f,%.1f,%.1f) mob=(%.1f,%.1f,%.1f)", guid, curDist, playerX, playerY, playerZ, mobX, mobY, mobZ)
	}
	b.world.AttackSwing(guid)

	return behaviortree.Running
}

func (b *Bot) actionCombatRotation() behaviortree.Status {
	targetGUID := b.world.TargetGUID()
	// console enter log removed - use chat for decisions
	b.logDecision("In combat rotation on target")

	// Opportunistic fresh target selection while in combat rotation.
	// Prevents continuing to "attack" a creature that died while we weren't looking.
	if targetGUID != 0 {
		cur := b.world.GetObject(targetGUID)
		if cur == nil || !cur.IsAlive() || b.isKnownDead(targetGUID) {
			b.world.MarkObjectDead(targetGUID)
			b.markKnownDead(targetGUID)
			targetGUID = 0
		} else if cur.Value(client.UnitNPCFlags) != 0 || !b.isHostileFaction(cur.Value(client.UnitFieldFaction)) {
			b.sendAliveReasonChat("ABORT combat friendly NPC GUID=%d Entry=%d npc=0x%x fac=%d", targetGUID, cur.Entry, cur.Value(client.UnitNPCFlags), cur.Value(client.UnitFieldFaction))
			b.world.ClearTarget()
			b.world.ClearCombat()
			targetGUID = 0
		}
	}
	if targetGUID == 0 {
		if best := b.findBestTarget(35); best != nil {
			targetGUID = best.GUID
			b.world.SetTarget(targetGUID)
			b.currentTargetSetAt = time.Now()
			b.lastEngagedGUID = targetGUID
			deadF := (best.Values[client.UnitFieldFlags] & client.UnitFlagDead) != 0
			b.sendAliveReasonChat("COMBAT picked GUID=%d Entry=%d: h=%d/%d IsAlive=%v deadFlag=%v f=0x%x npc=0x%x",
				targetGUID, best.Entry, best.Health(), best.MaxHealth(), best.IsAlive(), deadF, best.Values[client.UnitFieldFlags], best.Values[client.UnitNPCFlags])
			b.log("ATTACK COMBAT picked GUID=%d Entry=%d", targetGUID, best.Entry)
		}
	}
	if targetGUID == 0 {
		// No target but in combat - find what's attacking us
		newTarget := b.findBestTarget(30)
		if newTarget != nil {
			b.world.SetTarget(newTarget.GUID)
			if b.lastEngagedGUID != newTarget.GUID {
				b.currentTargetSetAt = time.Now()
				b.lastEngagedGUID = newTarget.GUID
				b.engagedTargetHealth = newTarget.Health()
			}
			px, py, pz := b.myPos()
			mx, my, mz := newTarget.PosX, newTarget.PosY, newTarget.PosZ
			b.log("ATTACK (new combat target) GUID=%d Entry=%d player=(%.1f,%.1f,%.1f) mob=(%.1f,%.1f,%.1f)", newTarget.GUID, newTarget.Entry, px, py, pz, mx, my, mz)
			b.world.AttackSwing(newTarget.GUID)
			return behaviortree.Running
		}
		b.world.ClearTarget()
		b.world.ClearCombat()
		return behaviortree.Failure
	}

	target := b.world.GetObject(targetGUID)
	if target == nil || !target.IsAlive() || b.isKnownDead(targetGUID) {
		d := float32(999)
		if target != nil {
			d = target.DistanceTo(b.myPos())
		}
		b.logDecision("Target dead, switching to next action")
		b.world.AttackStop()
		b.world.ClearTarget()
		b.world.ClearCombat()
		b.stopCurrentMove()
		b.markKnownDead(targetGUID)
		// Only set lastLoot for opportunistic close loot. Do not run to corpses.
		if target != nil && d <= 12.0 {
			b.lastLootGUID = targetGUID
		} else {
			b.lastLootGUID = 0
		}

		// Check if something else is attacking us
		newTarget := b.findBestTarget(30)
		if newTarget != nil {
			b.world.SetTarget(newTarget.GUID)
			if b.lastEngagedGUID != newTarget.GUID {
				b.currentTargetSetAt = time.Now()
				b.lastEngagedGUID = newTarget.GUID
				b.engagedTargetHealth = newTarget.Health()
			}
			px, py, pz := b.myPos()
			mx, my, mz := newTarget.PosX, newTarget.PosY, newTarget.PosZ
			b.log("ATTACK (new combat target) GUID=%d Entry=%d player=(%.1f,%.1f,%.1f) mob=(%.1f,%.1f,%.1f)", newTarget.GUID, newTarget.Entry, px, py, pz, mx, my, mz)
			b.world.AttackSwing(newTarget.GUID)
			return behaviortree.Running
		}
		// No more valid target: return Failure so grind/wander can run.
		return behaviortree.Failure
	}

	// If we haven't received *any* update (values/movement) for the current combat target recently,
	// the server may no longer consider it visible to us, or its position is stale because the mob
	// is wandering and we stopped receiving MonsterMove (e.g. due to our reported player pos making
	// the mob out of our update range on server, even if in reality it's near).
	// Drop it so the tree can re-evaluate and pick a target with fresh position data.
	if !target.LastSeen.IsZero() && time.Since(target.LastSeen) > 5*time.Second {
		b.logDecision("Combat target stale (no update >5s) GUID=%d Entry=%d, dropping to re-acquire", targetGUID, target.Entry)
		b.world.ClearTarget()
		b.world.ClearCombat()
		b.stopCurrentMove()
		return behaviortree.Failure
	}

	if target != nil && (target.Values[client.UnitNPCFlags] != 0 || !b.isHostileFaction(target.Values[client.UnitFieldFaction])) {
		b.sendAliveReasonChat("ABORT combat friendly GUID=%d Entry=%d npc=0x%x fac=%d", targetGUID, target.Entry, target.Values[client.UnitNPCFlags], target.Values[client.UnitFieldFaction])
		b.world.ClearTarget()
		b.world.ClearCombat()
		b.stopCurrentMove()
		return behaviortree.Failure
	}

	// Log to chat why we believe this target is alive (debug for dead creature attacks)
	deadF := (target.Value(client.UnitFieldFlags) & client.UnitFlagDead) != 0
	curDist := target.DistanceTo(b.myPos())
	// Use interpolated early for debug
	tx, ty, tz := target.InterpolatedPosition()
	if curDist <= 5.0 {
		px, py, pz := b.myPos()
		mx, my, mz := tx, ty, tz
		b.sendAliveReasonChat("COMBAT GUID=%d Entry=%d: h=%d/%d IsAlive=%v deadFlag=%v flags=0x%x npc=0x%x fac=%d dist=%.1f player=(%.1f,%.1f,%.1f) mob=(%.1f,%.1f,%.1f)",
			targetGUID, target.Entry, target.Health(), target.MaxHealth(), target.IsAlive(), deadF,
			target.Value(client.UnitFieldFlags), target.Value(client.UnitNPCFlags), target.Value(client.UnitFieldFaction), curDist, px, py, pz, mx, my, mz)
		b.log("ATTACK COMBAT GUID=%d Entry=%d dist=%.1f player=(%.1f,%.1f,%.1f) mob=(%.1f,%.1f,%.1f)", targetGUID, target.Entry, curDist, px, py, pz, mx, my, mz)
	} else {
		b.logDecision("COMBAT far from target GUID=%d dist=%.1f (still chasing)", targetGUID, curDist)
	}

	// Quick drop for low health no-progress (stale health on dead mob)
	if target.Health() > 0 && target.Health() < 20 {
		if !b.currentTargetSetAt.IsZero() && time.Since(b.currentTargetSetAt) > 3*time.Second {
			if b.engagedTargetHealth == 0 || target.Health() >= b.engagedTargetHealth {
				b.markKnownDead(targetGUID)
				b.logDecision("COMBAT quick unstick low health stale target")
				b.world.ClearTarget()
				b.world.ClearCombat()
				b.stopCurrentMove()
				return behaviortree.Failure
			}
		}
	}

	// Use interpolated position for moving targets (computed earlier for debug)
	dx := tx - func() float32 { x, _, _, _, _ := b.world.Position(); return x }()
	dy := ty - func() float32 { _, y, _, _, _ := b.world.Position(); return y }()
	dist2d := float32(math.Sqrt(float64(dx*dx + dy*dy)))

	// Face and approach target if needed (follow to within 2 yards XY, re-follow if target moves).
	// Use 2D for the decision so Z differences (hills, interp) don't cause "stuck far but combat".
	if dist2d > 2.0 {
		b.moveToPoint(tx, ty, tz)
		return behaviortree.Running
	}

	// Stop if we were moving
	if b.movementActive() {
		b.stopCurrentMove()
	}

	// Face target
	facing := float32(math.Atan2(float64(dy), float64(dx)))
	if math.Abs(float64(func() float32 { _, _, _, o, _ := b.world.Position(); return o }()-facing)) > 0.1 {
		b.world.SetFacing(facing)
	}

	// Continue auto-attack
	playerX, playerY, playerZ := b.myPos()
	mobX, mobY, mobZ := tx, ty, tz
	if obj := b.world.GetObject(targetGUID); obj == nil || !obj.IsAlive() {
		b.log("ATTACK on missing/dead in combat GUID=%d player=(%.1f,%.1f,%.1f) mob=(%.1f,%.1f,%.1f)", targetGUID, playerX, playerY, playerZ, mobX, mobY, mobZ)
	}
	b.world.AttackSwing(targetGUID)

	// Use abilities based on class
	b.useCombatAbilities(targetGUID, target)

	return behaviortree.Running
}

func (b *Bot) useCombatAbilities(targetGUID uint64, target *client.WorldObject) {
	// Respect GCD (1.5 seconds)
	if time.Since(b.lastCastTime) < 1500*time.Millisecond {
		return
	}

	level := b.world.PlayerLevel()

	// Use Victory Rush if available (proc from killing blow)
	if b.lastVictoryRush && b.world.IsSpellReady(34428) {
		b.log("Casting Victory Rush (proc)")
		b.world.CastSpell(34428, targetGUID)
		b.lastCastTime = time.Now()
		b.lastVictoryRush = false
		return
	}

	spells := gamedata.GetSpellPriority(b.config.Class, level)

	for _, spellID := range spells {
		info, ok := gamedata.WarriorSpells[spellID]
		if !ok {
			continue
		}

		if info.Level > level {
			continue
		}

		// Skip Execute if target isn't low health (< 20%)
		if spellID == 5308 && target.Health() > target.MaxHealth()/5 {
			continue
		}

		// Check range
		dist := target.DistanceTo(b.myPos())
		if info.Range > 0 && dist > info.Range {
			continue
		}
		if info.MinRange > 0 && dist < info.MinRange {
			continue
		}

		if b.world.IsSpellReady(spellID) {
			b.log("Casting %s (ID=%d) on target", info.Name, spellID)
			b.world.CastSpell(spellID, targetGUID)
			b.lastCastTime = time.Now()
			return // One ability per GCD
		}
	}
}

func (b *Bot) actionLoot() behaviortree.Status {
	if b.lastLootGUID == 0 {
		return behaviortree.Failure
	}
	guid := b.lastLootGUID
	obj := b.world.GetObject(guid)
	d := float32(999)
	if obj != nil {
		d = obj.DistanceTo(b.myPos())
	}
	// loot enter debug suppressed from console

	// Only loot if the corpse is very close (within melee range ~8).
	// If far we already decided not to set it, but double-check and clear here.
	if obj != nil {
		if d > 10.0 {
			// too far loot decision will be in chat via logDecision if needed
			b.lastLootGUID = 0
			b.stopCurrentMove()
			return behaviortree.Failure
		}
	} else {
		// gone loot to chat
		b.lastLootGUID = 0
		return behaviortree.Failure
	}

	b.lastLootGUID = 0
	b.logDecision("Looting corpse")
	b.world.Loot(guid)
	// The SMSG_LOOT_RESPONSE will trigger handleLootOpened which performs the actual looting + release.
	return behaviortree.Success
}

func (b *Bot) handleLootOpened(lootGUID uint64, items []client.LootItem) {
	// Auto-loot all items without blocking sleeps (non-blocking for the read loop)
	for _, item := range items {
		b.world.LootItem(item.Index)
	}
	b.world.LootMoney()
	b.world.LootRelease(lootGUID)
}

func (b *Bot) actionWander() behaviortree.Status {
	// console enter log removed (too noisy with 1000 bots)
	//b.logDecision("Wandering to explore for mobs")

	// Re-check for targets while wandering. This lets us exit wander quickly when
	// mobs appear (the Sequence may have been resumed on the wander action).
	if best := b.findBestTarget(35); best != nil && best.IsAlive() {
		// (debug log removed)
		return behaviortree.Failure // let selector try find_and_fight again next tick
	}

	// If the controller is actively moving toward a wander point, keep going until it finishes.
	// The MovementController handles its own arrival detection and will stop when it reaches the destination.
	if b.movementActive() {
		return behaviortree.Running
	}

	// Find a random nearby point using pathfinding.
	// Use a reasonably large radius so we actually explore the world instead of
	// orbiting a tiny local area. Random points + pathing + chaining gives
	// varied traversal instead of tight circles or repeated loops.
	if b.nav != nil {
		now := time.Now()
		if b.bb != nil {
			if v, ok := b.bb.Get("wander_retry_after"); ok {
				if retryAfter, ok := v.(time.Time); ok && now.Before(retryAfter) {
					return behaviortree.Running
				}
			}
		}

		x, y, z, _, mapID := b.world.Position()

		// Snap to real ground before deciding on a random wander point.
		// Use small relative probe based on current Z (from path or pos) to stay on correct floor/level.
		// High fixed probe can pick upper floors in multi-level areas (e.g. under second floor).
		probeZ := z + 5.0
		if gh, ok := movementGroundHeight(b.nav, mapID, x, y, probeZ); ok {
			delta := gh - z
			if delta > 1.0 {
				// would snap up significantly - keep current to avoid floor teleport
			} else if math.Abs(float64(delta)) > 0.5 {
				z = gh
			} else {
				z = gh
			}
		}

		radius := float32(70)
		if mapID == 1 || mapID == 530 { // Kalimdor or Blood Elf start (Eversong 530) - worse navmesh, tighter to avoid under/over map
			radius = 40
		}
		current := navigation.Point3D{X: x, Y: y, Z: z}
		pts := findWanderPath(b.nav, mapID, current, radius)
		if len(pts) > 1 {
			if b.bb != nil {
				b.bb.Delete("wander_retry_after")
			}
			// Use the points directly from the random generator (they already include height correction
			// via internal FindPath/GetPolyHeight). This ensures random movement uses the generator's
			// corrected heights, and points are only on valid mmaps-connected areas (prevents climbing
			// rocks/areas the navmesh does not allow).
			b.movementMu.Lock()
			b.ensureMovementControllerLocked()
			if b.moveController != nil {
				// Snap first point to current for handoff (current already snapped above)
				if len(pts) > 0 {
					pts[0] = current
				}
				b.moveController.SetPath(pts, time.Now(), func() float32 { _, _, _, o, _ := b.world.Position(); return o }(), mapID)
				b.isMoving = true
			}
			b.movementMu.Unlock()
			return behaviortree.Running
		}

		if b.bb != nil {
			b.bb.Set("wander_retry_after", now.Add(wanderRetryCooldown))
		}
		return behaviortree.Running
	}

	// Fallback (no nav): pick a varied random direction + decent distance (15-45yd).
	// Using high-res time bits for angle + varying length breaks repetitive circular
	// paths. Each re-wander (when close to previous dest) goes somewhere new.
	x, y, z := b.myPos()
	seed := time.Now().UnixNano()
	angle := float64(seed%62831853) / 10000000.0 // good distribution
	dist := 15.0 + float64(seed%31)              // 15-45 yd steps
	newX := x + float32(math.Cos(angle))*float32(dist)
	newY := y + float32(math.Sin(angle))*float32(dist)
	b.moveToPoint(newX, newY, z)
	return behaviortree.Running
}

func findWanderPath(nav navigation.Navigator, mapID uint32, center navigation.Point3D, radius float32) []navigation.Point3D {
	if nav == nil {
		return nil
	}

	for attempt := 0; attempt < wanderRandomPathAttempts; attempt++ {
		attemptRadius := wanderAttemptRadius(radius, attempt)
		result, err := nav.FindRandomPath(mapID, center, attemptRadius)
		if err != nil || result == nil || !result.Found || len(result.Points) <= 1 {
			continue
		}

		pts := simplifyAndDensifyPath(result.Points, 3.0, 1.0)
		snapPathToGround(nav, mapID, pts)
		if len(pts) > 0 {
			pts[0] = center
		}
		if wanderPathUsable(center, pts) {
			return pts
		}
	}

	return nil
}

func wanderAttemptRadius(radius float32, attempt int) float32 {
	switch attempt % 4 {
	case 1:
		radius *= 0.75
	case 2:
		radius *= 0.5
	case 3:
		radius *= 1.25
	}
	if radius < 8 {
		return 8
	}
	return radius
}

func wanderPathUsable(center navigation.Point3D, pts []navigation.Point3D) bool {
	if len(pts) < 2 || len(pts) > wanderMaxSimplifiedPathSize {
		return false
	}

	for _, p := range pts {
		if math.IsNaN(float64(p.X)) || math.IsNaN(float64(p.Y)) || math.IsNaN(float64(p.Z)) ||
			math.IsInf(float64(p.X), 0) || math.IsInf(float64(p.Y), 0) || math.IsInf(float64(p.Z), 0) {
			return false
		}
	}

	straight := center.DistanceTo2D(pts[len(pts)-1])
	if straight < 2.0 {
		return false
	}
	pathLen := pathLength2D(pts)
	if straight > 5.0 && pathLen > straight*wanderMaxPathLengthFactor {
		return false
	}

	return true
}

func pathLength2D(pts []navigation.Point3D) float32 {
	var length float32
	for i := 1; i < len(pts); i++ {
		length += pts[i-1].DistanceTo2D(pts[i])
	}
	return length
}

// ============================================================
// Movement system
// ============================================================

func (b *Bot) moveToPoint(x, y, z float32) {
	b.ensureMovementController()

	now := time.Now()
	requestedDest := [3]float32{x, y, z}
	px, py, pz, po, mapID := b.world.Position()

	b.movementMu.Lock()
	if b.moveController == nil {
		b.movementMu.Unlock()
		// Fallback: at least stop any old movement
		b.world.MoveStop()
		return
	}

	if b.moveController.IsMoving() {
		b.moveController.Update(now)
		b.isMoving = b.moveController.IsMoving()
		cx, cy, cz, co := b.moveController.CurrentPosition()
		px, py, pz, po = cx, cy, cz, co
		b.world.UpdatePosition(cx, cy, cz, co)
	}

	// Throttle repaths hard while walking. Live mob chase calls move_to every
	// AI tick with a slightly new XYZ; re-SetPath is what makes movement look
	// like constant direction changes.
	if b.isMoving && !b.lastMoveCommandTime.IsZero() {
		since := now.Sub(b.lastMoveCommandTime)
		ddx := x - b.lastMoveCommandPos[0]
		ddy := y - b.lastMoveCommandPos[1]
		ddz := z - b.lastMoveCommandPos[2]
		destMoved2 := ddx*ddx + ddy*ddy

		// Sticky: same-ish dest for 1.5s — never repath.
		if destMoved2 < 25.0 && math.Abs(float64(ddz)) < 5.0 && since < 1500*time.Millisecond {
			b.movementMu.Unlock()
			return
		}
		// Moderate mob drift: repath at most ~0.67Hz.
		if destMoved2 < 64.0 && math.Abs(float64(ddz)) < 8.0 && since < 1200*time.Millisecond {
			b.movementMu.Unlock()
			return
		}
		// Absolute floor while moving.
		if since < 700*time.Millisecond {
			b.movementMu.Unlock()
			return
		}
		// Path already ends near requested dest.
		if ex, ey, ez, ok := b.moveController.Destination(); ok {
			edx, edy, edz := ex-x, ey-y, ez-z
			if edx*edx+edy*edy < 16.0 && math.Abs(float64(edz)) < 5.0 {
				b.movementMu.Unlock()
				return
			}
		}
	}
	b.movementMu.Unlock()

	// Before any movement decisions, snap both current position and target to real
	// ground height using a small Z offset. Querying GetHeight from slightly above
	// the expected ground is required to get accurate terrain height.
	if b.nav != nil {
		// Snap current position to ground if needed (small corrections only; avoid jumping levels).
		probeZ := pz + 5.0
		if gh, ok := movementGroundHeight(b.nav, mapID, px, py, probeZ); ok {
			delta := gh - pz
			if delta > 1.0 {
				// would snap up significantly (possible upper floor) - keep current Z
			} else if math.Abs(float64(delta)) > 0.5 {
				pz = gh
			} else {
				pz = gh
			}
		}
		targetProbe := z + 5.0
		if gh, ok := movementGroundHeight(b.nav, mapID, x, y, targetProbe); ok {
			delta := gh - z
			if math.Abs(float64(delta)) > 2.0 {
				// large difference - trust the provided target Z (e.g. from random generator or mob)
				gh = z
			} else if math.Abs(float64(delta)) > 0.5 {
				z = gh
			} else {
				z = gh
			}
		}
	}

	// Clamp target Z for pursuit to avoid oscillation: bots trying to go much higher/lower
	// than current ground then snapping back. Limit delta to ~5 yards.
	deltaZ := z - pz
	if deltaZ > 5.0 {
		z = pz + 5.0
	} else if deltaZ < -5.0 {
		z = pz - 5.0
	}

	current := navigation.Point3D{X: px, Y: py, Z: pz}

	// If we have nav, compute a proper path (real pathfinding as required for movement).
	var pts []navigation.Point3D
	if b.nav != nil {
		result, err := b.nav.FindPath(mapID, current, navigation.Point3D{X: x, Y: y, Z: z})
		if err == nil && result != nil && result.Found && len(result.Points) > 1 {
			pts = simplifyAndDensifyPath(result.Points, 3.0, 1.0)
			// detect crazy path (common in Durotar bad navmesh)
			straight := current.DistanceTo2D(navigation.Point3D{X: x, Y: y, Z: z})
			plen := float32(0)
			for j := 1; j < len(pts); j++ {
				plen += pts[j-1].DistanceTo2D(pts[j])
			}
			// Reject huge detours (navmesh artifacts): more than 2× straight line
			// or absurdly long paths send the bot running across the zone.
			if straight > 1.0 && (plen > straight*2.0 || plen > straight+40 || len(pts) > 60) {
				b.log("crazy path rejected (straight=%.1f path=%.1f n=%d) — direct line",
					straight, plen, len(pts))
				pts = simplifyAndDensifyPath([]navigation.Point3D{current, {X: x, Y: y, Z: z}}, 3.0, 1.0)
			}
		} else {
			b.logDecision("No path found to target")
		}
	}

	if len(pts) == 0 {
		if b.nav != nil {
			b.logDecision("Nav present but no valid path to target - falling back to direct line (may clip trees/obstacles!)")
		}
		// Direct fallback
		pts = simplifyAndDensifyPath([]navigation.Point3D{current, {X: x, Y: y, Z: z}}, 3.0, 1.0)
	}

	// Ensure first point is exactly current for smooth handoff
	if len(pts) > 0 {
		pts[0] = current
	}

	if b.nav != nil {
		snapPathToGround(b.nav, mapID, pts)
		if len(pts) > 0 {
			pts[0] = current
		}
	}

	// Hand off to the (now only) movement implementation.
	// The controller handles time-based following, 500ms HBs, and explicit turns at direction changes.
	b.movementMu.Lock()
	b.ensureMovementControllerLocked()
	if b.moveController != nil {
		b.moveController.SetPath(pts, now, po, mapID)
		b.isMoving = true
		b.lastMoveCommandPos = requestedDest
		b.lastMoveCommandTime = now
	}
	b.movementMu.Unlock()

	// The controller will emit the appropriate START / HB packets.
}

type terrainHeightNavigator interface {
	GetTerrainHeight(mapID uint32, x, y float32) (float32, bool)
}

func movementGroundHeight(nav navigation.Navigator, mapID uint32, x, y, fallbackHintZ float32) (float32, bool) {
	gh, _, ok := movementGroundHeightWithSource(nav, mapID, x, y, fallbackHintZ)
	return gh, ok
}

func movementGroundHeightWithSource(nav navigation.Navigator, mapID uint32, x, y, fallbackHintZ float32) (float32, bool, bool) {
	if gh, ok := nav.GetHeight(mapID, x, y, fallbackHintZ); ok {
		return gh, false, true
	}
	if terrainNav, ok := nav.(terrainHeightNavigator); ok {
		if gh, ok := terrainNav.GetTerrainHeight(mapID, x, y); ok {
			return gh, true, true
		}
	}
	return 0, false, false
}

func snapPathToGround(nav navigation.Navigator, mapID uint32, pts []navigation.Point3D) {
	if nav == nil {
		return
	}
	for i := range pts {
		origZ := pts[i].Z
		gh, _, ok := movementGroundHeightWithSource(nav, mapID, pts[i].X, pts[i].Y, origZ)
		if !ok || math.IsNaN(float64(gh)) || math.IsInf(float64(gh), 0) {
			continue
		}
		pts[i].Z = gh
	}
}

// simplifyAndDensifyPath reduces zig-zags (simplify collinear) and adds intermediate points
// for smoother following on uneven terrain like Durotar hills. This helps make path following
// look less "crazy" when navmesh produces sparse or jagged paths.
func simplifyAndDensifyPath(pts []navigation.Point3D, maxStep, collinearTol float32) []navigation.Point3D {
	if len(pts) < 2 {
		return pts
	}
	// densify first (use 2D horizontal distance for step size; Z will be ground-snapped live)
	dense := []navigation.Point3D{pts[0]}
	for i := 1; i < len(pts); i++ {
		p0 := pts[i-1]
		p1 := pts[i]
		d := p0.DistanceTo2D(p1)
		if d > maxStep && d > 0.001 {
			n := int(d / maxStep)
			for k := 1; k <= n; k++ {
				t := float32(k) / float32(n+1)
				dense = append(dense, navigation.Point3D{
					X: p0.X + (p1.X-p0.X)*t,
					Y: p0.Y + (p1.Y-p0.Y)*t,
					Z: p0.Z + (p1.Z-p0.Z)*t, // interp; snapPathToGround corrects to terrain
				})
			}
		}
		dense = append(dense, p1)
	}
	if len(dense) <= 2 {
		return dense
	}
	// simplify collinear-ish points (keep changes in direction or steep Z)
	simp := []navigation.Point3D{dense[0]}
	for i := 1; i < len(dense)-1; i++ {
		a := simp[len(simp)-1]
		b := dense[i]
		c := dense[i+1]
		abx := b.X - a.X
		aby := b.Y - a.Y
		abz := b.Z - a.Z
		bcx := c.X - b.X
		bcy := c.Y - b.Y
		bcz := c.Z - b.Z
		// 3D cross product mag
		crx := aby*bcz - abz*bcy
		cry := abz*bcx - abx*bcz
		crz := abx*bcy - aby*bcx
		cross := float32(math.Sqrt(float64(crx*crx + cry*cry + crz*crz)))
		if cross > collinearTol || math.Abs(float64(abz)) > 1.5 || math.Abs(float64(bcz)) > 1.5 {
			simp = append(simp, b)
		}
	}
	simp = append(simp, dense[len(dense)-1])
	return simp
}

// stopCurrentMove aborts any in-progress path without necessarily sending a stop packet.
// Called on kill of target etc. to prevent continuing to run toward a now-dead mob's last position.
func (b *Bot) stopCurrentMove() {
	b.movementMu.Lock()
	defer b.movementMu.Unlock()
	b.isMoving = false
	b.lastMoveCommandTime = time.Time{}
	if b.moveController != nil {
		b.moveController.Stop(time.Now())
	}
}

// wireTeleportHandling installs a session-phase hook that reacts to near teleports
// (summon, MSG_MOVE_TELEPORT) and far transfers (SMSG_NEW_WORLD).
// Must run after wireValidationInstrumentation so both callbacks chain correctly.
func (b *Bot) wireTeleportHandling() {
	if b == nil || b.world == nil {
		return
	}
	prev := b.world.OnSessionPhase
	b.world.OnSessionPhase = func(c client.SessionPhaseChange) {
		if prev != nil {
			prev(c)
		}
		// Transfer in flight: drop local path immediately so the movement loop
		// cannot keep extrapolating from the pre-teleport pose.
		if c.To == client.PhaseNearTeleport || c.To == client.PhaseFarTransfer {
			b.abortMovementForTeleport()
			return
		}
		// Transfer complete: snap to new pose, clear combat, flag Lua restart.
		if c.To == client.PhaseInWorld &&
			(c.From == client.PhaseNearTeleport || c.From == client.PhaseFarTransfer) {
			b.handleTeleportResume(c.Reason)
		}
	}
}

// abortMovementForTeleport silently cancels any active path (no MoveStop at old coords).
func (b *Bot) abortMovementForTeleport() {
	b.movementMu.Lock()
	defer b.movementMu.Unlock()
	b.isMoving = false
	b.lastMoveCommandTime = time.Time{}
	if b.moveController != nil {
		b.moveController.AbortSilent()
	}
}

// handleTeleportResume runs when the session returns to in_world after a summon/teleport.
// Interrupts combat + movement, snaps the movement controller, and flags Lua AI to restart.
func (b *Bot) handleTeleportResume(reason string) {
	if b.world == nil {
		return
	}
	x, y, z, o, mapID := b.world.Position()
	b.log("Teleport resume (%s): map=%d pos=(%.1f,%.1f,%.1f) — interrupt everything, restart AI",
		reason, mapID, x, y, z)

	// Local combat interrupt first (always). Best-effort CMSG when socket is live.
	b.world.ClearTarget()
	b.world.ClearCombat()
	_ = b.world.AttackStop()
	_ = b.world.SetTarget(0)

	b.movementMu.Lock()
	b.isMoving = false
	b.lastMoveCommandTime = time.Time{}
	b.lastMoveCommandPos = [3]float32{}
	b.ensureMovementControllerLocked()
	if b.moveController != nil {
		b.moveController.AbortAndSnap(x, y, z, o)
	}
	b.movementMu.Unlock()

	// Drop Go-side pursuit / loot sticky state so built-in AI cannot chase old coords.
	b.grindTargetGUID = 0
	b.lastPursuedTargetGUID = 0
	b.lastPursuedTargetPos = [3]float32{}
	b.lastPursuedTargetTime = time.Time{}
	b.lastPursuitUpdate = time.Time{}
	b.lastMoveToTargetPos = [3]float32{}
	b.lastMoveToTargetTime = time.Time{}
	b.lastAttackSwingAt = time.Time{}
	b.currentTargetSetAt = time.Time{}
	b.lastEngagedGUID = 0
	b.engagedTargetHealth = 0
	b.lastLootGUID = 0
	b.lastLootAttemptGUID = 0
	b.lastLootAttemptAt = time.Time{}

	b.teleportMu.Lock()
	b.teleportPending = true
	b.teleportReason = reason
	b.teleportMu.Unlock()

	if b.validationEnc != nil {
		b.logValidation("teleport", map[string]interface{}{
			"reason": reason,
			"map":    mapID,
			"x":      x,
			"y":      y,
			"z":      z,
		})
	}
	// Do not call into Lua here (world read loop). Scripts poll bot.consume_teleport()
	// on the next AI tick and fully restart sticky state from the new pose.
}

// ConsumeTeleport returns true once after a completed summon/teleport so Lua can
// clear sticky state and re-settle at the new position.
func (b *Bot) ConsumeTeleport() bool {
	b.teleportMu.Lock()
	defer b.teleportMu.Unlock()
	if !b.teleportPending {
		return false
	}
	b.teleportPending = false
	b.teleportReason = ""
	return true
}

// Spells that forcibly relocate the caster (Charge, Intercept, Blink, …).
// On SPELL_GO we abort local pathing; the final pose comes from MONSTER_MOVE.
var relocateOnCastSpells = map[uint32]struct{}{
	100:   {}, // Charge
	20252: {}, // Intercept
	3411:  {}, // Intervene
	1953:  {}, // Blink
}

// wireServerRelocateHandling snaps the movement controller when the server
// relocates the player (Charge is the common case: without this the controller
// keeps simulating the pre-charge path and rubber-bands the bot home).
func (b *Bot) wireServerRelocateHandling() {
	if b == nil || b.world == nil {
		return
	}
	prevReloc := b.world.OnServerRelocate
	b.world.OnServerRelocate = func(x, y, z, o float32, reason string) {
		b.log("Server relocate (%s): pos=(%.1f,%.1f,%.1f) — abort local path", reason, x, y, z)
		b.movementMu.Lock()
		b.isMoving = false
		b.lastMoveCommandTime = time.Time{}
		b.lastMoveCommandPos = [3]float32{}
		b.ensureMovementControllerLocked()
		if b.moveController != nil {
			b.moveController.AbortAndSnap(x, y, z, o)
		}
		b.movementMu.Unlock()
		// Clear sticky pursuit dest so we re-path from the new pose.
		b.lastPursuitUpdate = time.Time{}
		b.lastMoveToTargetTime = time.Time{}
		if prevReloc != nil {
			prevReloc(x, y, z, o, reason)
		}
	}

	prevSpell := b.world.OnSpellCastResult
	b.world.OnSpellCastResult = func(spellID uint32, success bool, failReason uint8) {
		if success {
			if _, ok := relocateOnCastSpells[spellID]; ok {
				// Drop local path immediately; MONSTER_MOVE will snap pose.
				b.abortMovementForTeleport()
				b.logDecision("RELOCATE_SPELL id=%d — abort path pending server pose", spellID)
			}
		} else if failReason == 85 { // SPELL_FAILED_NO_POWER
			b.noteSpellNoPower(spellID)
			b.logDecision("NO_POWER spell=%d — block re-cast 1.5s", spellID)
		}
		if prevSpell != nil {
			prevSpell(spellID, success, failReason)
		}
	}
}

func (b *Bot) updateMovement() {
	b.movementMu.Lock()
	defer b.movementMu.Unlock()

	if b.moveController == nil {
		return
	}

	b.moveController.Update(time.Now())
	b.isMoving = b.moveController.IsMoving()

	// Sync position from controller ONLY if it has actually started a path (travelDist > 0 or isMoving).
	// Otherwise we would overwrite the correct login position with struct-zero (0,0,0).
	if b.moveController.TravelDist() > 0 || b.isMoving {
		cx, cy, cz, co := b.moveController.CurrentPosition()
		b.world.UpdatePosition(cx, cy, cz, co)
	}
}

func (b *Bot) movementActive() bool {
	b.movementMu.Lock()
	defer b.movementMu.Unlock()
	return b.isMoving
}

// ============================================================
// LuaEngine BotAPI implementation
// ============================================================

func (b *Bot) GetPosition() (x, y, z, o float32) {
	x, y, z, o, _ = b.world.Position()
	return
}

func (b *Bot) GetFacing() float32 {
	_, _, _, o, _ := b.world.Position()
	return o
}

func (b *Bot) SetFacing(o float32) error {
	// Preserve forward motion when Lua adjusts facing mid-path.
	if b.movementActive() {
		return b.world.SetFacingMoving(o)
	}
	return b.world.SetFacing(o)
}

func (b *Bot) FaceTarget(guid uint64) bool {
	u := b.GetUnitInfo(guid)
	if u == nil {
		return false
	}
	px, py, _, po := b.GetPosition()
	dx := u.PosX - px
	dy := u.PosY - py
	fo := float32(math.Atan2(float64(dy), float64(dx)))
	// Skip micro facing updates — spam breaks auto-attack anim and jerks movement.
	d := fo - po
	for d > math.Pi {
		d -= 2 * math.Pi
	}
	for d < -math.Pi {
		d += 2 * math.Pi
	}
	if math.Abs(float64(d)) < 0.35 { // ~20 degrees
		return true
	}
	// While pathing, keep FORWARD flag; standing melee uses plain set facing.
	if b.movementActive() {
		_ = b.world.SetFacingMoving(fo)
	} else {
		_ = b.world.SetFacing(fo)
	}
	return true
}

func (b *Bot) SetSheathed(state uint32) error {
	return b.world.SetSheathed(state)
}

func (b *Bot) MoveTo(x, y, z float32) error {
	b.moveToPoint(x, y, z)
	return nil
}

func (b *Bot) StopMoving() error {
	if !b.movementActive() {
		// Already stopped — do not re-send MoveStop (interrupts attack anim / idles).
		return nil
	}
	b.stopCurrentMove()
	return nil
}

// generateUniqueCharName produces a highly unique WoW-legal (alphabetic) name.
// Used when the requested name is taken so we can keep trying fresh ones.
func generateUniqueCharName(seed int) string {
	consonantStarts := []string{
		"Ar", "Br", "Cr", "Dr", "El", "Fr", "Gr", "Hr", "Ir", "Kr", "Lr", "Mr", "Nr", "Or", "Pr", "Rr", "Sr", "Tr", "Ur", "Vr", "Wr", "Zr",
		"Al", "Bl", "Cl", "Fl", "Gl", "Kl", "Ll", "Ml", "Pl", "Sl", "Tl", "Vl", "Wl", "Yl",
		"An", "Bn", "Cn", "Dn", "Fn", "Gn", "Kn", "Ln", "Mn", "Nn", "Pn", "Sn", "Tn", "Vn", "Wn", "Yn",
		"Ak", "Bk", "Ck", "Dk", "Fk", "Gk", "Hk", "Kk", "Lk", "Mk", "Nk", "Pk", "Sk", "Tk", "Vk", "Wk", "Yk",
		"Ag", "Bg", "Cg", "Dg", "Fg", "Gg", "Hg", "Kg", "Lg", "Mg", "Ng", "Pg", "Sg", "Tg", "Vg", "Wg", "Yg",
		"Ad", "Bd", "Cd", "Dd", "Fd", "Gd", "Hd", "Kd", "Ld", "Md", "Nd", "Pd", "Sd", "Td", "Vd", "Wd", "Yd",
		"Th", "Sh", "Ch", "Ph", "Wh", "Qu", "St", "Sp", "Sk", "Sm", "Sn", "Sw", "Tw", "Tr", "Dr", "Gr", "Kr", "Pr", "Br", "Fr", "Cl", "Fl", "Gl", "Pl", "Sl", "Bl",
	}
	midSyls := []string{
		"ar", "er", "ir", "or", "ur", "yr", "al", "el", "il", "ol", "ul", "yl",
		"an", "en", "in", "on", "un", "yn", "ak", "ek", "ik", "ok", "uk", "yk",
		"ag", "eg", "ig", "og", "ug", "yg", "ad", "ed", "id", "od", "ud", "yd",
		"ath", "eth", "ith", "oth", "uth", "yth", "ash", "esh", "ish", "osh", "ush", "ysh",
		"ra", "re", "ri", "ro", "ru", "ry", "la", "le", "li", "lo", "lu", "ly",
		"ma", "me", "mi", "mo", "mu", "my", "na", "ne", "ni", "no", "nu", "ny",
		"sa", "se", "si", "so", "su", "sy", "ta", "te", "ti", "to", "tu", "ty",
		"va", "ve", "vi", "vo", "vu", "vy", "za", "ze", "zi", "zo", "zu", "zy",
	}
	endSyls := []string{
		"ar", "er", "ir", "or", "ur", "ath", "eth", "ith", "oth", "uth",
		"an", "en", "in", "on", "un", "ak", "ek", "ik", "ok", "uk",
		"al", "el", "il", "ol", "ul", "ad", "ed", "id", "od", "ud",
		"as", "es", "is", "os", "us", "and", "end", "ind", "ond", "und",
		"ard", "erd", "ird", "ord", "urd", "ion", "eon", "ian", "aan", "oon",
	}

	rb := make([]byte, 2)
	rand.Read(rb)
	r1 := int(rb[0]) + seed
	r2 := int(rb[1]) + seed*7

	part1 := consonantStarts[r1%len(consonantStarts)]
	part2 := midSyls[r2%len(midSyls)]
	part3 := midSyls[(r1+r2)%len(midSyls)]
	part4 := endSyls[(r1*37+r2)%len(endSyls)]

	name := part1 + part2 + part3 + part4
	if len(name) > 12 {
		name = name[:12]
	}
	if len(name) < 3 {
		name += "ar"
	}
	if len(name) > 0 {
		name = strings.ToUpper(name[:1]) + strings.ToLower(name[1:])
	}
	return name
}

func (b *Bot) AttackTarget(guid uint64) error {
	if b.world == nil {
		return nil
	}
	// Avoid CMSG_SET_SELECTION spam when already on this target.
	if b.world.TargetGUID() != guid {
		_ = b.world.SetTarget(guid)
	}
	// Keep auto-attack alive: re-issue ATTACKSWING at most every 1.5s while on
	// the same target. Calling only once often loses server-side swing after
	// range/facing glitches or MoveStop, so the client shows no attack anim.
	if b.world.AttackingGUID() == guid && !b.lastAttackSwingAt.IsZero() &&
		time.Since(b.lastAttackSwingAt) < 1500*time.Millisecond {
		return nil
	}
	b.logDecision("ATTACK_SWING guid=%d", guid)
	b.lastAttackSwingAt = time.Now()
	return b.world.AttackSwing(guid)
}

func (b *Bot) StopAttack() error {
	return b.world.AttackStop()
}

func (b *Bot) SetTarget(guid uint64) error {
	if b.world != nil && b.world.TargetGUID() == guid {
		return nil
	}
	return b.world.SetTarget(guid)
}

// IsMoving reports whether the movement controller is mid-path.
func (b *Bot) IsMoving() bool {
	return b.movementActive()
}

func (b *Bot) CastSpell(spellID uint32, targetGUID uint64) error {
	b.logDecision("CAST_SPELL id=%d target=%d", spellID, targetGUID)
	// Do NOT abort the chase path on cast attempt. Charge often CAST_FAILs
	// (stance/range/path); aborting here froze bots mid-pull so they only
	// swung NOT_IN_RANGE. Path is dropped on SPELL_GO success (relocate spells)
	// and on self MONSTER_MOVE (OnServerRelocate).
	return b.world.CastSpell(spellID, targetGUID)
}

func (b *Bot) IsSpellReady(spellID uint32) bool {
	b.noPowerMu.Lock()
	if b.noPowerUntil != nil {
		if until, ok := b.noPowerUntil[spellID]; ok {
			if time.Now().Before(until) {
				b.noPowerMu.Unlock()
				return false
			}
			delete(b.noPowerUntil, spellID)
		}
	}
	b.noPowerMu.Unlock()
	if b.world == nil {
		return false
	}
	return b.world.IsSpellReady(spellID)
}

// noteSpellNoPower blocks a spell in IsSpellReady for a short window after
// SMSG_CAST_FAILED reason 85 (SPELL_FAILED_NO_POWER).
func (b *Bot) noteSpellNoPower(spellID uint32) {
	if spellID == 0 {
		return
	}
	b.noPowerMu.Lock()
	if b.noPowerUntil == nil {
		b.noPowerUntil = make(map[uint32]time.Time)
	}
	b.noPowerUntil[spellID] = time.Now().Add(1500 * time.Millisecond)
	b.noPowerMu.Unlock()
}

func (b *Bot) GetHealth() (current, max uint32) {
	return b.world.Health(), b.world.MaxHealth()
}

func (b *Bot) GetPower() (current, max uint32) {
	return b.world.Power()
}

func (b *Bot) GetLevel() uint32 {
	return b.world.PlayerLevel()
}

func (b *Bot) GetClass() uint8 {
	return b.config.Class
}

func (b *Bot) GetOwnGUID() uint64 {
	if b.world != nil {
		return b.world.CharGUID()
	}
	return 0
}

func (b *Bot) SetLevel(level uint32) {
	if b.world != nil {
		b.world.SetLevelForTest(level) // internal
	}
}

func (b *Bot) GetRace() uint8 {
	return b.config.Race
}

func (b *Bot) GetPowerType() uint8 {
	// Common power types: 0=mana, 1=rage, 2=focus, 3=energy, 6=runic power
	// For simplicity, derive from class. Can be improved by reading UnitFieldBytes0.
	switch b.config.Class {
	case 1: // Warrior
		return 1 // rage
	case 2: // Paladin
		return 0 // mana
	case 3: // Hunter
		return 2 // focus (in WotLK)
	case 4: // Rogue
		return 3 // energy
	case 5: // Priest
		return 0
	case 6: // DK
		return 6 // runic
	case 7: // Shaman
		return 0
	case 8: // Mage
		return 0
	case 9: // Warlock
		return 0
	case 11: // Druid
		return 0 // or 3 in cat, but default mana
	default:
		return 0
	}
}

func (b *Bot) InCombat() bool {
	return b.world.InCombat()
}

func (b *Bot) IsAlive() bool {
	h := b.world.Health()
	mh := b.world.MaxHealth()
	// If we haven't received health data yet, assume alive
	if h == 0 && mh == 0 {
		return true
	}
	if h == 0 {
		return false
	}
	// Also check death count - if deaths > 0 and health is still 0, we're dead
	return h > 0
}

func (b *Bot) GetTargetGUID() uint64 {
	return b.world.TargetGUID()
}

func (b *Bot) GetNearbyUnits(maxDist float32) []luaengine.UnitInfo {
	objects := b.world.GetNearbyUnits(maxDist)
	result := make([]luaengine.UnitInfo, 0, len(objects))
	for _, obj := range objects {
		result = append(result, b.worldObjToUnitInfo(obj))
	}
	return result
}

func (b *Bot) GetNearbyPlayers(maxDist float32) []luaengine.UnitInfo {
	objects := b.world.GetNearbyPlayers(maxDist)
	result := make([]luaengine.UnitInfo, 0, len(objects))
	for _, obj := range objects {
		result = append(result, b.worldObjToUnitInfo(obj))
	}
	return result
}

func (b *Bot) GetUnitInfo(guid uint64) *luaengine.UnitInfo {
	obj := b.world.GetObject(guid)
	if obj == nil {
		return nil
	}
	info := b.worldObjToUnitInfo(obj)
	return &info
}

func (b *Bot) worldObjToUnitInfo(obj *client.WorldObject) luaengine.UnitInfo {
	// Always expose the *current* estimated pose. Raw Pos* is the create/MONSTER_MOVE
	// segment start; chasing it is the classic "run to where the mob used to be" bug.
	px, py, pz := obj.InterpolatedPosition()
	mx, my, mz := b.myPos()
	dx, dy, dz := px-mx, py-my, pz-mz
	dist := float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))
	dyn := obj.Value(client.UnitDynamicFlags)
	return luaengine.UnitInfo{
		GUID:      obj.GUID,
		Entry:     obj.Entry,
		Health:    obj.Health(),
		MaxHealth: obj.MaxHealth(),
		Level:     obj.Level(),
		PosX:      px,
		PosY:      py,
		PosZ:      pz,
		IsAlive:   obj.IsAlive(),
		IsPlayer:  obj.IsPlayer,
		Distance:  dist,
		Name:      obj.Name,
		Faction:   obj.Value(client.UnitFieldFaction),
		NPCFlags:  obj.Value(client.UnitNPCFlags),
		Flags:     obj.Value(client.UnitFieldFlags),
		DynFlags:  dyn,
		Lootable:  dyn&client.UnitDynflagLootable != 0,
	}
}

func (b *Bot) SendChat(message string) error {
	return b.world.SendChatMessage(client.ChatMsgSay, client.LangCommon, message)
}

func (b *Bot) SendCommand(command string) error {
	return b.world.SendGMCommand(command)
}

func (b *Bot) SendGuildCommand(command string) error {
	if b.world == nil {
		return nil
	}
	return b.world.SendGuildCommand(command)
}

func (b *Bot) Loot(guid uint64) error {
	return b.world.Loot(guid)
}

// LootAll opens loot, takes money, and releases without blocking the AI tick.
// Previously this slept 700ms on the AI goroutine, which froze ticks and caused
// loot spam under Lua grind (validation run: 122× CMSG_LOOT in ~45s).
// Server responses arrive asynchronously via OnLootOpened / SMSG_LOOT_*.
func (b *Bot) LootAll(guid uint64) error {
	if b.world == nil || guid == 0 {
		return nil
	}
	// Throttle: at most one open/release cycle per GUID every 3s.
	now := time.Now()
	b.lootMu.Lock()
	if b.lastLootAttemptGUID == guid && now.Sub(b.lastLootAttemptAt) < 3*time.Second {
		b.lootMu.Unlock()
		return nil
	}
	b.lastLootAttemptGUID = guid
	b.lastLootAttemptAt = now
	b.lootMu.Unlock()

	_ = b.world.Loot(guid)
	_ = b.world.LootMoney()
	_ = b.world.LootRelease(guid)
	return nil
}

func (b *Bot) Log(format string, args ...interface{}) {
	b.log(format, args...)
}

// Extended BotAPI methods for the Lua AI framework (warrior rends, pet, stance, aura checks, etc.)
func (b *Bot) HasAuraOn(guid uint64, spellID uint32) bool {
	if b.world == nil {
		return false
	}
	obj := b.world.GetObject(guid)
	if obj == nil {
		return false
	}
	return obj.HasAura(spellID)
}

// warriorRageCost is a minimal 3.3.5 base-cost map so CanCast rejects
// NO_POWER spam when IsSpellReady is still true with 0 rage.
var warriorRageCost = map[uint32]uint32{
	6673:  10, // Battle Shout
	772:   10, // Rend
	78:    15, // Heroic Strike
	7386:  15, // Sunder
	5308:  15, // Execute
	6343:  20, // Thunder Clap
	845:   20, // Cleave
	1680:  25, // Whirlwind
	12294: 30, // Mortal Strike
	23881: 20, // Bloodthirst
	23922: 20, // Shield Slam
	6572:  5,  // Revenge
	7384:  5,  // Overpower
	1715:  10, // Hamstring
	1160:  10, // Demoralizing Shout
}

func (b *Bot) CanCast(spellID uint32, targetGUID uint64) bool {
	if b.world == nil {
		return true // optimistic for headless tests
	}
	// Use Bot.IsSpellReady so noPowerUntil (CAST_FAILED NO_POWER) is honored
	// for callers that only check can_cast without a prior is_spell_ready.
	if !b.IsSpellReady(spellID) {
		return false
	}
	// Power precheck for rage users (warrior). Mana/energy classes skip this map.
	if b.config.Class == 1 { // warrior
		if need, ok := warriorRageCost[spellID]; ok && need > 0 {
			cur, _ := b.world.Power()
			if cur < need {
				return false
			}
		}
	}
	return true
}

func (b *Bot) GetPetGUID() uint64 {
	if b.world == nil {
		return 0
	}
	// Basic discovery for validation/E2E (zero cost common path when no pet).
	// Scan nearby units for a plausible pet: non-player, alive, within close range.
	// Full impl would use SMSG_PET_SPELLS + owner/creator fields from UNIT_FIELD_*.
	units := b.world.GetNearbyUnits(25)
	px, py, pz, _, _ := b.world.Position()
	for _, u := range units {
		if u == nil || u.IsPlayer {
			continue
		}
		d := u.DistanceTo(px, py, pz)
		if u.IsAlive() && u.Health() > 0 && d > 0 && d < 12 {
			// Heuristic good enough for call-pet validation runs in isolated areas.
			return u.GUID
		}
	}
	return 0
}

func (b *Bot) PetAttack(target uint64) {
	if b.world != nil {
		b.log("Lua AI: pet attack request on %d", target)
		// Real implementation would send pet action packet if sidecar supports it.
	}
}

func (b *Bot) GetStance() int {
	// Stance/form often in UNIT_FIELD_BYTES_1 or detectable via auras (battle stance etc).
	// Return 0 (default) ; advanced detection can be added.
	return 0
}

func (b *Bot) IsBehindTarget(targetGUID uint64) bool {
	if b.world == nil {
		return false
	}
	if targetGUID == 0 {
		targetGUID = b.GetTargetGUID()
	}
	if targetGUID == 0 {
		return false
	}
	obj := b.world.GetObject(targetGUID)
	if obj == nil {
		return false
	}
	px, py, _, _ := b.GetPosition()
	tx, ty := obj.PosX, obj.PosY
	to := obj.Orientation

	dx := float64(px - tx)
	dy := float64(py - ty)
	angleToPlayer := math.Atan2(dy, dx)
	delta := angleToPlayer - float64(to)
	// normalize to [-pi, pi]
	for delta > math.Pi {
		delta -= 2 * math.Pi
	}
	for delta < -math.Pi {
		delta += 2 * math.Pi
	}
	absd := math.Abs(delta)
	// Behind if in the rear 180 deg arc (abs delta > pi/2 from target's forward)
	return absd > (math.Pi / 2)
}

// ValidationMode satisfies the luaengine.BotAPI requirement.
// It is the central cheap gate for all heavy validation tooling (structured logs,
// packet traces, ring buffers, detailed aura data, etc.).
// When false (the default for regular operation), callers must do almost no work.
func (b *Bot) ValidationMode() bool {
	return b.config.ValidationMode
}

// markKnownDead marks this GUID as dead in *this bot's* private view of the world.
// This persists even if server packets keep sending positive health (stale cache from death not fully propagated to this connection).
func (b *Bot) markKnownDead(guid uint64) {
	b.knownDeadMu.Lock()
	if b.knownDead == nil {
		b.knownDead = make(map[uint64]bool)
	}
	b.knownDead[guid] = true
	b.knownDeadMu.Unlock()
	b.world.MarkObjectDead(guid) // also force live cache for this bot's worldclient
}

// isKnownDead returns true if this bot has decided the guid is dead in its view.
func (b *Bot) isKnownDead(guid uint64) bool {
	b.knownDeadMu.Lock()
	defer b.knownDeadMu.Unlock()
	return b.knownDead != nil && b.knownDead[guid]
}

// clearKnownDead if we see evidence it's alive (positive health update).
func (b *Bot) clearKnownDead(guid uint64) {
	b.knownDeadMu.Lock()
	if b.knownDead != nil {
		delete(b.knownDead, guid)
	}
	b.knownDeadMu.Unlock()
}

// ============================================================
// Utility methods
// ============================================================

// Stop signals the bot to stop running.
func (b *Bot) Stop() {
	select {
	case <-b.stopCh:
	default:
		close(b.stopCh)
	}
}

// closeValidation closes the validation log writer if open. Safe to call multiple times.
func (b *Bot) closeValidation() {
	b.validationMu.Lock()
	defer b.validationMu.Unlock()
	if b.validationEnc != nil {
		b.logValidationUnlocked("meta", map[string]interface{}{"msg": "validation_timeline_end"})
	}
	if b.validationFile != nil {
		_ = b.validationFile.Close()
		b.validationFile = nil
		b.validationEnc = nil
	}
}

// logValidationUnlocked is used only while validationMu is already held (e.g. close).
func (b *Bot) logValidationUnlocked(typ string, data map[string]interface{}) {
	if b.validationEnc == nil {
		return
	}
	now := time.Now().UTC()
	b.validationSeq++
	rec := map[string]interface{}{
		"ts":   now.Format(time.RFC3339Nano),
		"t_ns": now.UnixNano(),
		"seq":  b.validationSeq,
		"bot":  b.id,
		"type": typ,
	}
	for k, v := range data {
		if k == "type" || k == "ts" || k == "seq" || k == "bot" {
			continue
		}
		rec[k] = v
	}
	_ = b.validationEnc.Encode(rec)
}

// logValidation writes a structured timeline record to the validation JSONL.
// Safe for concurrent use (AI tick + packet reader). No-op when file not open.
//
// Common fields on every line:
//
//	ts (RFC3339Nano UTC), t_ns, seq, bot, type
func (b *Bot) logValidation(typ string, data map[string]interface{}) {
	if b.validationEnc == nil {
		return
	}
	now := time.Now().UTC()
	b.validationMu.Lock()
	defer b.validationMu.Unlock()
	b.validationSeq++
	rec := map[string]interface{}{
		"ts":   now.Format(time.RFC3339Nano),
		"t_ns": now.UnixNano(),
		"seq":  b.validationSeq,
		"bot":  b.id,
		"type": typ,
	}
	for k, v := range data {
		if k == "type" || k == "ts" || k == "seq" || k == "bot" {
			continue
		}
		rec[k] = v
	}
	_ = b.validationEnc.Encode(rec)
}

// LoadLuaScript loads a Lua script at runtime (simple string form).
// For richer updates including helpers/data/tick func switch use LoadAIBundle.
func (b *Bot) LoadLuaScript(code string) error {
	if b.lua == nil {
		return fmt.Errorf("lua engine not initialized")
	}
	return b.lua.DoString(code)
}

// LoadAIBundle applies a full AIBundle (Main + Helpers + Data + TickFunc) at runtime.
// This enables live phase broadcasts and richer scenario AI updates.
// It DoStrings Main then each Helper, sets scenario_data, and overrides the tick func.
// Existing globals from prior loads are not cleared (Lua state is additive for functions).
func (b *Bot) LoadAIBundle(bundle scenario.AIBundle) error {
	if b.lua == nil {
		return fmt.Errorf("lua engine not initialized")
	}
	if bundle.Main != "" {
		if err := b.lua.DoString(bundle.Main); err != nil {
			return fmt.Errorf("AIBundle Main: %w", err)
		}
	}
	// For deterministic application order we iterate keys sorted (helpers may have inter-deps).
	// Simple range is acceptable; advanced callers can order keys themselves in the bundle.
	// Apply helpers in sorted key order for determinism (important for inter-helper dependencies).
	keys := make([]string, 0, len(bundle.Helpers))
	for k := range bundle.Helpers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if code := bundle.Helpers[k]; code != "" {
			if err := b.lua.DoString(code); err != nil {
				return fmt.Errorf("AIBundle helper %s: %w", k, err)
			}
		}
	}
	if len(bundle.Data) > 0 {
		b.lua.SetTable("scenario_data", bundle.Data)
	}
	if bundle.TickFunc != "" {
		b.lua.SetTickFunc(bundle.TickFunc)
	}
	return nil
}

// Events returns the recorded events.
func (b *Bot) Events() []BotEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	result := make([]BotEvent, len(b.events))
	copy(result, b.events)
	return result
}

func (b *Bot) setStatus(s BotStatus) {
	b.mu.Lock()
	b.status = s
	b.mu.Unlock()
	// Timeline phase changes (zero cost when validation log not open).
	if b.validationEnc != nil {
		b.logValidation("phase", map[string]interface{}{"status": string(s)})
	}
}

// Status returns current status
func (b *Bot) Status() BotResult {
	b.mu.Lock()
	defer b.mu.Unlock()
	result := BotResult{
		ID:     b.id,
		Status: b.status,
		Kills:  b.kills,
		Deaths: b.deaths,
	}
	if b.err != nil {
		result.Error = b.err.Error()
	}
	if b.world != nil {
		result.Level = b.world.PlayerLevel()
	}
	return result
}

func (b *Bot) fail(format string, args ...interface{}) BotResult {
	msg := fmt.Sprintf(format, args...)
	b.log("ERROR: %s", msg)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.status = BotStatusError
	b.err = fmt.Errorf("%s", msg)
	if b.world != nil {
		b.world.Close()
	}
	return BotResult{
		ID:     b.id,
		Status: BotStatusError,
		Error:  msg,
	}
}

func (b *Bot) log(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	// Combat thrash lines only in validation / observation / packet-trace —
	// army load must not fmt.Printf every melee tick.
	if strings.HasPrefix(msg, "ATTACK ") {
		if !b.config.ValidationMode && !b.config.LogDecisionsToChat && !b.config.EnablePacketTrace {
			return
		}
	}
	fmt.Printf("[Bot %s] %s\n", b.id, msg)
}

// logDecision logs an important AI/behavior decision both to the console and
// (throttled) as an in-game /say so you can observe what the bot is "thinking"
// while watching it in the world.
func (b *Bot) logDecision(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)

	// Console is unreadable with hundreds/thousands of bots.
	// All decision logging goes to in-game /say chat only.
	// (use b.log(...) explicitly for anything you want in node console)
	if b.config.LogDecisionsToChat {
		if time.Since(b.lastDecisionChat) < 700*time.Millisecond {
			return
		}
		b.lastDecisionChat = time.Now()

		chat := "[AI] " + msg
		if len(chat) > 110 {
			chat = chat[:107] + "..."
		}
		_ = b.world.SendChatMessage(client.ChatMsgSay, client.LangCommon, chat)
	}

	// Structured validation path (zero cost when ValidationMode+path not set)
	if b.config.ValidationMode && b.validationEnc != nil {
		b.logValidation("decision", map[string]interface{}{"msg": msg})
	}
}

// sendAliveReasonChat sends a detailed "why we think this mob is alive" message to in-game chat.
// Gated by LogDecisionsToChat (same observation flag as AI decisions) so army load
// does not flood worldserver chat. Throttled when enabled.
func (b *Bot) sendAliveReasonChat(format string, args ...interface{}) {
	if !b.config.LogDecisionsToChat {
		return
	}
	if time.Since(b.lastAliveReasonChat) < 800*time.Millisecond {
		return
	}
	b.lastAliveReasonChat = time.Now()
	msg := fmt.Sprintf(format, args...)
	if len(msg) > 120 {
		msg = msg[:117] + "..."
	}
	if b.world == nil {
		return
	}
	_ = b.world.SendChatMessage(client.ChatMsgSay, client.LangCommon, "[ALIVE] "+msg)
}

func (b *Bot) addEvent(eventType, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	b.log("[EVENT:%s] %s", eventType, msg)
	b.mu.Lock()
	b.events = append(b.events, BotEvent{
		Time:    time.Now(),
		Type:    eventType,
		Message: msg,
	})
	b.mu.Unlock()
}
