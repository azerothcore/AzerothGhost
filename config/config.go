package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// CLIConfig holds the merged configuration for azghost CLI, node, orchestrator and scenario modes.
type CLIConfig struct {
	Profile  string `yaml:"profile"`
	ConfigFile string `yaml:"-"`

	// Connection
	Username   string `yaml:"username" json:"username"`
	Password   string `yaml:"password" json:"password"`
	AuthServer string `yaml:"auth_server" json:"auth_server"`

	// Character / play
	CharName  string `yaml:"char_name" json:"character_name"`
	RealmIndex int   `yaml:"realm_index" json:"realm_index"`
	Race      int    `yaml:"race" json:"race"`
	Class     int    `yaml:"class" json:"class"`
	Gender    uint8  `yaml:"gender" json:"gender"`

	// Behavior
	BotMode     string `yaml:"bot_mode" json:"mode"`
	DungeonName string `yaml:"dungeon" json:"dungeon_name"`
	LuaScript   string `yaml:"lua_script" json:"lua_script"`
	LuaCode     string `yaml:"lua_code" json:"lua_code"`

	// Data & pathfinding (embedded by default)
	DataDir            string `yaml:"data_dir" json:"data_dir"`
	PathfindingAddress string `yaml:"pathfinding_addr" json:"pathfinding_addr"`

	// Behavior flags
	DeleteExistingChars bool `yaml:"delete_existing_chars" json:"delete_existing_chars"`
	LogDecisionsToChat  bool `yaml:"log_decisions_to_chat" json:"log_decisions_to_chat"`
	DisableTargetCache  bool `yaml:"disable_target_cache" json:"disable_target_cache"`

	// Validation tooling flags (see bot.Config for details). These are off by default
	// so regular high-scale runs have no performance impact.
	ValidationMode      bool   `yaml:"validation_mode" json:"validation_mode"`
	ValidationLogPath   string `yaml:"validation_log" json:"validation_log"`
	EnablePacketTrace   bool   `yaml:"enable_packet_trace" json:"enable_packet_trace"`
	EnableDetailedAuras bool   `yaml:"enable_detailed_auras" json:"enable_detailed_auras"`

	// Node / server
	Listen string `yaml:"listen" json:"listen"`

	// Orchestrator / scenario
	NumBots            int    `yaml:"num_bots" json:"num_bots"`
	Nodes              string `yaml:"nodes" json:"nodes"`
	AccountPrefix      string `yaml:"account_prefix" json:"account_prefix"`
	AccountPassword    string `yaml:"account_password" json:"account_password"`
	DBDSN              string        `yaml:"db_dsn" json:"db_dsn"`
	Duration           time.Duration `yaml:"duration"`
	SpawnRateLimit     int           `yaml:"spawn_rate_limit"`
	SpawnRateInterval  time.Duration `yaml:"spawn_rate_interval"`

	// Internal
	AuthDBDSN      string `yaml:"auth_db_dsn"`
	CharactersDBDSN string `yaml:"characters_db_dsn"`
}

// Load merges profile (yaml), config file, env and defaults.
// Profile is looked up in common locations.
func Load(profile, cfgPath string) (CLIConfig, error) {
	c := defaultCLIConfig()

	// Try explicit config
	if cfgPath != "" {
		if err := loadYAML(cfgPath, &c); err != nil {
			return c, err
		}
		c.ConfigFile = cfgPath
	}

	// Profile
	if profile != "" {
		c.Profile = profile
		p := findProfile(profile)
		if p != "" {
			if err := loadYAML(p, &c); err == nil {
				// ok
			} else {
				fmt.Fprintf(os.Stderr, "[config] warning: failed to load profile %s: %v\n", p, err)
			}
		} else {
			fmt.Fprintf(os.Stderr, "[config] warning: profile %q not found (looked in .azghost/profiles/ and ~/.config/azghost/profiles/)\n", profile)
		}
	} else if envP := os.Getenv("AZGHOST_PROFILE"); envP != "" {
		c.Profile = envP
		p := findProfile(envP)
		if p != "" {
			_ = loadYAML(p, &c)
		}
	}

	// Env overrides (basic)
	if v := os.Getenv("AZGHOST_AUTH_SERVER"); v != "" {
		c.AuthServer = v
	}
	if v := os.Getenv("AZGHOST_DATA_DIR"); v != "" {
		c.DataDir = v
	}
	if v := os.Getenv("AZGHOST_USERNAME"); v != "" {
		c.Username = v
	}
	if v := os.Getenv("AZGHOST_PASSWORD"); v != "" {
		c.Password = v
	}
	if v := os.Getenv("AZGHOST_CHAR_NAME"); v != "" {
		c.CharName = v
	}
	// ... (add more env as needed; design specifies the pattern)

	// Defaults for local dev if nothing set
	if c.AuthServer == "" {
		c.AuthServer = "127.0.0.1:3724"
	}
	if c.DataDir == "" {
		if d := os.Getenv("AC_DATA"); d != "" {
			c.DataDir = d
		}
	}

	return c, nil
}

func defaultCLIConfig() CLIConfig {
	return CLIConfig{
		AuthServer:         "127.0.0.1:3724",
		BotMode:            "grind",
		Listen:             ":8080",
		NumBots:            1,
		AccountPrefix:      "loadbot",
		AccountPassword:    "loadbot",
		SpawnRateLimit:     5,
		SpawnRateInterval:  time.Second,
		LogDecisionsToChat: true, // convenient default for single-bot CLI observation
		// Validation* fields default false/empty → no perf cost on regular runs
	}
}

func findProfile(name string) string {
	// Allow users to pass the filename with extension, e.g. --profile local-ac.yml
	base := strings.TrimSuffix(name, ".yml")
	base = strings.TrimSuffix(base, ".yaml")

	// Also allow passing a direct path to a profile file
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		if _, err := os.Stat(name); err == nil {
			return name
		}
	}

	candidates := []string{
		".azghost/profiles/" + base + ".yml",
		".azghost/profiles/" + base + ".yaml",
		filepath.Join(os.Getenv("HOME"), ".config/azghost/profiles/"+base+".yml"),
		filepath.Join(os.Getenv("HOME"), ".config/azghost/profiles/"+base+".yaml"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func loadYAML(path string, out *CLIConfig) error {
	// Minimal loader for now (profiles can be supported via env or future expansion).
	// Avoids external dep for initial build after restore.
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	// Very basic key: value parser for common fields (sufficient for E2E profile use).
	s := string(b)
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.Index(line, ":"); idx > 0 {
			k := strings.TrimSpace(line[:idx])
			v := strings.Trim(strings.TrimSpace(line[idx+1:]), `"'`)
			switch k {
			case "auth_server", "auth-server":
				out.AuthServer = v
			case "data_dir", "data-dir":
				out.DataDir = v
			case "username":
				out.Username = v
			case "password":
				out.Password = v
			case "char_name", "char-name", "character_name":
				out.CharName = v
			case "account_prefix", "account-prefix":
				out.AccountPrefix = v
			case "account_password", "account-password":
				out.AccountPassword = v
			case "num_bots", "num-bots":
				if n, err := strconv.Atoi(v); err == nil {
					out.NumBots = n
				}
			case "nodes":
				out.Nodes = v
			}
		}
	}
	return nil
}

