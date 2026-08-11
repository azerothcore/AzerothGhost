package bot

import (
	"github.com/walkline/AzerothGhost/scenario"
)

// Config holds configuration for a bot instance.
// This is the public config for AzerothGhost.
// It loosens coupling: DB setup is opt-in; headless usage is supported;
// Lua can be provided inline (LuaCode) or via AIBundle for scenarios.
type Config struct {
	Username      string `json:"username"`
	Password      string `json:"password"`
	AuthServer    string `json:"auth_server"`
	CharacterName string `json:"character_name"`
	RealmIndex    int    `json:"realm_index"`

	Race   uint8 `json:"race"`
	Class  uint8 `json:"class"`
	Gender uint8 `json:"gender"`

	// Navigation
	DataDir            string `json:"data_dir"`            // root containing mmaps/, maps/, vmaps/
	PathfindingAddress string `json:"pathfinding_address"` // optional remote (deprioritized)

	// Lua
	LuaScript string `json:"lua_script"` // path to .lua file (DoFile)
	LuaCode   string `json:"lua_code"`   // inline script (DoString). Precedence: AIBundle > LuaCode > LuaScript

	// AIBundle carries richer scenario-distributed AI (Main + Helpers + Data + TickFunc).
	// When non-empty, takes precedence for initial AI payload.
	AIBundle scenario.AIBundle `json:"ai_bundle"`

	// Behavior mode (preserved for backward compat with existing load tests)
	Mode string `json:"mode"` // "grind", "hogger", "dungeon", "idle", "lua"

	// AI tick interval (ms). Default 200.
	AITickMs int `json:"ai_tick_ms"`

	// Dungeon for "dungeon" mode.
	DungeonName string `json:"dungeon_name"`

	// DeleteExistingCharacters: before creating target char, delete others on account.
	DeleteExistingCharacters bool `json:"delete_existing_characters"`

	// LogDecisionsToChat emits major AI decisions via /say (throttled).
	LogDecisionsToChat bool `json:"log_decisions_to_chat"`

	// DisableTargetCache forces fresh target scans every tick (debug).
	DisableTargetCache bool `json:"disable_target_cache"`

	// === Validation / Observability tooling (must be zero-cost when disabled) ===

	// ValidationMode enables heavier instrumentation only used during E2E quality runs:
	// - Structured decision + outcome logging to ValidationLogPath (JSONL)
	// - Ring buffers for recent cast results / aura deltas (for post-run assertions)
	// - Optional packet tracing
	// When false (the default for regular runs), these paths allocate nothing and do minimal work.
	ValidationMode bool `json:"validation_mode"`

	// ValidationLogPath when set (and ValidationMode true) writes structured events
	// (decisions, cast results, aura changes, value snapshots) for later analysis.
	// Example: "validation/warrior-rend-001.jsonl". Empty = disabled.
	ValidationLogPath string `json:"validation_log_path"`

	// EnablePacketTrace when true + ValidationMode, logs or records selected high-value
	// opcodes (SPELL_GO, AURA_UPDATE*, ATTACKERSTATE, etc.) with timestamps.
	// Expensive; off by default.
	EnablePacketTrace bool `json:"enable_packet_trace"`

	// EnableDetailedAuras when true keeps richer per-aura metadata (duration, stacks, caster).
	// The basic spellID set for HasAura is always maintained (cheap). Detailed is opt-in.
	EnableDetailedAuras bool `json:"enable_detailed_auras"`

	// === Loosened coupling flags ===

	// SkipCharacterSetup skips the entire auth + char enum/create/login flow.
	// Use together with NewHeadlessBot when you already performed login via client.WorldClient.
	SkipCharacterSetup bool `json:"skip_character_setup"`

	// AllowDBSetup gates the old preLoginDBSetup (level/pos/spells/gear via direct MySQL).
	// Default false for library and test use. When true + CharDBDSN provided, the
	// legacy DB path remains available for load-test modes that relied on it.
	AllowDBSetup bool `json:"allow_db_setup"`

	// CharDBDSN is the DSN for acore_characters used only when AllowDBSetup is true.
	CharDBDSN string `json:"char_db_dsn"`
}

// Option is a functional option for NewBot.
type Option func(*Config)

// WithDataDir sets the pathfinding data directory.
func WithDataDir(dir string) Option {
	return func(c *Config) { c.DataDir = dir }
}

// WithLuaCode provides inline Lua code (higher precedence than LuaScript file).
func WithLuaCode(code string) Option {
	return func(c *Config) { c.LuaCode = code }
}

// WithAIBundle provides a full scenario AIBundle.
func WithAIBundle(b scenario.AIBundle) Option {
	return func(c *Config) { c.AIBundle = b }
}

// WithSkipCharacterSetup requests that Run() skips auth/char/login.
func WithSkipCharacterSetup(skip bool) Option {
	return func(c *Config) { c.SkipCharacterSetup = skip }
}

// WithAllowDBSetup enables the legacy pre-login DB writes (off by default).
func WithAllowDBSetup(allow bool) Option {
	return func(c *Config) { c.AllowDBSetup = allow }
}

// WithBehaviorProvider can be used in future to inject a custom Behavior.
// Placeholder for the interface described in the design.
func WithBehaviorProvider(_ interface{}) Option {
	return func(c *Config) { /* wired in later iterations */ }
}

// DefaultConfig returns a Config with common defaults applied.
func DefaultConfig() Config {
	return Config{
		Race:               5,
		Class:              1,
		Mode:               "grind",
		AITickMs:           200,
		LogDecisionsToChat: false, // Safe default for regular / scaled runs. Enable explicitly for observation.
	}
}
