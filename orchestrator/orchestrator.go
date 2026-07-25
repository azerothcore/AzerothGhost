package orchestrator

import (
	"crypto/rand"
	"crypto/sha1"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	lua "github.com/Shopify/go-lua"
	_ "github.com/go-sql-driver/mysql"

	"github.com/walkline/AzerothGhost/bot"
	"github.com/walkline/AzerothGhost/scenario"
)

// SRP6 parameters matching AzerothCore's implementation
var (
	srp6N = new(big.Int).SetBytes([]byte{
		0x89, 0x4B, 0x64, 0x5E, 0x89, 0xE1, 0x53, 0x5B, 0xBD, 0xAD, 0x5B, 0x8B,
		0x29, 0x06, 0x50, 0x53, 0x08, 0x01, 0xB1, 0x8E, 0xBF, 0xBF, 0x5E, 0x8F,
		0xAB, 0x3C, 0x82, 0x87, 0x2A, 0x3E, 0x9B, 0xB7,
	})
	srp6G = big.NewInt(7)
)

// Config holds orchestrator configuration.
type Config struct {
	// Database connection in MySQL DSN format: user:pass@tcp(host:port)/dbname
	AuthDBDSN       string `json:"auth_db_dsn"`
	WorldDBDSN      string `json:"world_db_dsn"`
	CharactersDBDSN string `json:"characters_db_dsn"`

	// Auth server address for bot connections
	AuthServerAddr string `json:"auth_server_addr"`

	// List of node addresses (bot runner HTTP endpoints)
	NodeAddresses []string `json:"node_addresses"`

	// Account settings
	AccountPrefix   string `json:"account_prefix"`
	AccountPassword string `json:"account_password"`
	NumBots         int    `json:"num_bots"`

	// Pathfinding
	DataDir            string `json:"data_dir"` // root dir with mmaps/, maps/, vmaps/
	PathfindingAddress string `json:"pathfinding_address"`

	// Bot defaults
	DefaultRace  uint8  `json:"default_race"`
	DefaultClass uint8  `json:"default_class"`
	DefaultMode  string `json:"default_mode"`
	DungeonName  string `json:"dungeon_name"`
	LuaScript    string `json:"lua_script"`
	LuaCode      string `json:"lua_code"` // inline code for scenarios

	// AIBundle support for richer scenario AI distribution
	AIBundle scenario.AIBundle `json:"ai_bundle"`

	// When true (orchestrator default), bots will delete existing characters
	// on the account before creating the target one.
	DeleteExistingCharacters bool `json:"delete_existing_characters"`

	// Rate limiting for spawning bots (to avoid overwhelming auth/world servers)
	// Spawn at most SpawnRateLimit bots per SpawnRateInterval.
	// Example: 100 bots per 2 seconds.
	SpawnRateLimit    int           `json:"spawn_rate_limit"`
	SpawnRateInterval time.Duration `json:"spawn_rate_interval"`

	// Whether launched bots should speak their AI decisions in /say (for debugging).
	LogDecisionsToChat bool `json:"log_decisions_to_chat"`

	// DisableTargetCache tells launched bots to skip the findBestTarget short cache.
	DisableTargetCache bool `json:"disable_target_cache"`

	// Validation tooling flags. These must default false so large-scale load tests have
	// no measurable overhead from validation features.
	ValidationMode      bool   `json:"validation_mode"`
	ValidationLogPath   string `json:"validation_log"`
	EnablePacketTrace   bool   `json:"enable_packet_trace"`
	EnableDetailedAuras bool   `json:"enable_detailed_auras"`
}

// DefaultConfig returns a config with sensible defaults for AzerothCore.
func DefaultConfig() Config {
	return Config{
		AuthDBDSN:       "acore:acore@tcp(127.0.0.1:3306)/acore_auth",
		WorldDBDSN:      "acore:acore@tcp(127.0.0.1:3306)/acore_world",
		CharactersDBDSN: "acore:acore@tcp(127.0.0.1:3306)/acore_characters",
		AuthServerAddr:  "127.0.0.1:3724",
		AccountPrefix:   "loadbot",
		AccountPassword: "loadbot",
		NumBots:         1,
		DefaultRace:     1, // Human
		DefaultClass:    1, // Warrior
		DefaultMode:     "grind",
		// Orchestrator enables clean character creation by default (delete others first)
		DeleteExistingCharacters: true,
		// Default: spawn at most 2 bots every 10 seconds (conservative)
		SpawnRateLimit:    2,
		SpawnRateInterval: 10 * time.Second,
		LogDecisionsToChat: false, // off for scale by default
	}
}

// BotAssignment represents a bot assigned to a node.
type BotAssignment struct {
	NodeAddress   string `json:"node_address"`
	BotID         string `json:"bot_id"`
	AccountName   string `json:"account_name"`
	Password      string `json:"password"`
	CharacterName string `json:"character_name"`
	Race          uint8  `json:"race"`
	Class         uint8  `json:"class"`
	Faction       string `json:"faction,omitempty"` // "alliance" | "horde" - for Orgrimmar siege and similar
}

// TestResult holds the aggregate result of a load test.
type TestResult struct {
	StartTime  time.Time       `json:"start_time"`
	EndTime    time.Time       `json:"end_time"`
	BotResults []BotNodeResult `json:"bot_results"`
	TotalBots  int             `json:"total_bots"`
	Errors     int             `json:"errors"`
}

// BotNodeResult is the result from a single bot on a node.
type BotNodeResult struct {
	BotID  string `json:"bot_id"`
	Status string `json:"status"`
	Level  uint32 `json:"level"`
	Kills  int    `json:"kills"`
	Deaths int    `json:"deaths"`
	Error  string `json:"error,omitempty"`
}

// NodeLaunchRequest is what the orchestrator sends to a node to launch a bot.
// Matches/extends the one in server/ for wire compatibility. Includes
// LuaCode and AIBundle for scenario support.
type NodeLaunchRequest struct {
	BotID               string              `json:"bot_id"`
	Username            string              `json:"username"`
	Password            string              `json:"password"`
	AuthServer          string              `json:"auth_server"`
	CharacterName       string              `json:"character_name"`
	Race                uint8               `json:"race"`
	Class               uint8               `json:"class"`
	Mode                string              `json:"mode"`
	DungeonName         string              `json:"dungeon_name"`
	DataDir             string              `json:"data_dir"`
	PathfindingAddr     string              `json:"pathfinding_addr"`
	LuaScript           string              `json:"lua_script"`
	LuaCode             string              `json:"lua_code"`
	AIBundle            scenario.AIBundle   `json:"ai_bundle"`
	DeleteExistingChars bool                `json:"delete_existing_chars"`
	LogDecisionsToChat  bool                `json:"log_decisions_to_chat"`
	DisableTargetCache  bool                `json:"disable_target_cache"`

	// Validation flags (propagated only when doing quality validation runs)
	ValidationMode      bool   `json:"validation_mode"`
	ValidationLogPath   string `json:"validation_log"`
	EnablePacketTrace   bool   `json:"enable_packet_trace"`
	EnableDetailedAuras bool   `json:"enable_detailed_auras"`
}

// Orchestrator manages account creation and bot distribution.
type Orchestrator struct {
	config Config
	authDB *sql.DB
	mu     sync.Mutex

	assignments []BotAssignment

	// For scenario coordination (basic support)
	lua *lua.State

	// localBots holds in-process bots launched via scenario for local update demo.
	localBots sync.Map // map[string]*bot.Bot
}

// NewOrchestrator creates and initializes an orchestrator.
func NewOrchestrator(config Config) (*Orchestrator, error) {
	o := &Orchestrator{config: config}

	// DB is optional (for library/test use or when accounts pre-created)
	if config.AuthDBDSN != "" {
		db, err := sql.Open("mysql", config.AuthDBDSN)
		if err == nil {
			db.SetMaxOpenConns(5)
			db.SetConnMaxLifetime(5 * time.Minute)
			if db.Ping() == nil {
				o.authDB = db
			} else {
				db.Close()
			}
		}
	}
	return o, nil
}

// PrepareAccounts creates or reuses bot accounts and grants GM rights.
// DB operations are skipped if no authDB connection (optional DB).
func (o *Orchestrator) PrepareAccounts() ([]BotAssignment, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	var assignments []BotAssignment
	nodeCount := len(o.config.NodeAddresses)
	if nodeCount == 0 {
		nodeCount = 1
		o.config.NodeAddresses = []string{"local"}
	}

	skipDB := o.authDB == nil
	if skipDB {
		fmt.Println("[Orchestrator] No DB connection (or DB optional); assuming accounts/characters pre-created or using protocol+GM for setup.")
	}

	for i := 0; i < o.config.NumBots; i++ {
		accountName := fmt.Sprintf("%s%d", o.config.AccountPrefix, i+1)
		password := o.config.AccountPassword
		charName := generateCharName(i)
		nodeAddr := o.config.NodeAddresses[i%nodeCount]
		race, class := chooseStartingRaceClass(i)

		if !skipDB {
			// Create or update account
			if err := o.ensureAccount(accountName, password); err != nil {
				return nil, fmt.Errorf("ensure account %s: %w", accountName, err)
			}

			// Grant GM rights
			if err := o.setGMLevel(accountName, 3); err != nil {
				return nil, fmt.Errorf("set GM for %s: %w", accountName, err)
			}
		}

		assignments = append(assignments, BotAssignment{
			NodeAddress:   nodeAddr,
			BotID:         fmt.Sprintf("bot-%d", i+1),
			AccountName:   accountName,
			Password:      password,
			CharacterName: charName,
			Race:          race,
			Class:         class,
		})
	}

	o.assignments = assignments
	return assignments, nil
}

func (o *Orchestrator) ensureAccount(username, password string) error {
	usernameUpper := strings.ToUpper(username)
	passwordUpper := strings.ToUpper(password)

	// Compute SRP6 salt and verifier (matching AzerothCore format)
	salt, verifier := computeSRP6Registration(usernameUpper, passwordUpper)

	// Check if account exists
	var id int
	err := o.authDB.QueryRow("SELECT id FROM account WHERE username = ?", usernameUpper).Scan(&id)
	if err == sql.ErrNoRows {
		_, err = o.authDB.Exec(
			`INSERT INTO account (username, salt, verifier, expansion) VALUES (?, ?, ?, 2)`,
			usernameUpper, salt, verifier,
		)
		if err != nil {
			return fmt.Errorf("create account: %w", err)
		}
		fmt.Printf("[Orchestrator] Created account: %s\n", usernameUpper)
	} else if err != nil {
		return err
	} else {
		_, err = o.authDB.Exec(
			`UPDATE account SET salt = ?, verifier = ? WHERE username = ?`,
			salt, verifier, usernameUpper,
		)
		if err != nil {
			return fmt.Errorf("update account: %w", err)
		}
		fmt.Printf("[Orchestrator] Reusing account: %s (id=%d)\n", usernameUpper, id)
	}
	return nil
}

// computeSRP6Registration generates salt and verifier matching AzerothCore's SRP6 implementation.
// verifier = g^x mod N where x = SHA1(salt || SHA1(username:password))
func computeSRP6Registration(username, password string) ([]byte, []byte) {
	// Generate random 32-byte salt
	salt := make([]byte, 32)
	rand.Read(salt)

	// x = SHA1(salt || SHA1(username:password))
	credHash := sha1.Sum([]byte(username + ":" + password))
	xInput := make([]byte, 0, 32+20)
	xInput = append(xInput, salt...)
	xInput = append(xInput, credHash[:]...)
	xHash := sha1.Sum(xInput)

	// Convert x to big.Int (little-endian)
	xReversed := make([]byte, len(xHash))
	for i := 0; i < len(xHash); i++ {
		xReversed[i] = xHash[len(xHash)-1-i]
	}
	x := new(big.Int).SetBytes(xReversed)

	// verifier = g^x mod N
	v := new(big.Int).Exp(srp6G, x, srp6N)

	// Convert verifier to 32-byte little-endian array
	vBytes := v.Bytes()
	verifier := make([]byte, 32)
	for i := 0; i < len(vBytes) && i < 32; i++ {
		verifier[i] = vBytes[len(vBytes)-1-i]
	}

	return salt, verifier
}

// generateCharName produces a WoW-legal character name for bot index i.
func generateCharName(i int) string {
	consonantStarts := []string{
		"Ar", "Br", "Cr", "Dr", "El", "Fr", "Gr", "Hr", "Ir", "Jr", "Kr", "Lr", "Mr", "Nr", "Or", "Pr", "Qr", "Rr", "Sr", "Tr", "Ur", "Vr", "Wr", "Xr", "Yr", "Zr",
		"Al", "Bl", "Cl", "Dl", "Fl", "Gl", "Hl", "Kl", "Ll", "Ml", "Nl", "Pl", "Sl", "Tl", "Vl", "Wl", "Yl", "Zl",
		"An", "Bn", "Cn", "Dn", "Fn", "Gn", "Hn", "Kn", "Ln", "Mn", "Nn", "Pn", "Sn", "Tn", "Vn", "Wn", "Yn", "Zn",
		"Ak", "Bk", "Ck", "Dk", "Fk", "Gk", "Hk", "Kk", "Lk", "Mk", "Nk", "Pk", "Sk", "Tk", "Vk", "Wk", "Yk", "Zk",
		"Ag", "Bg", "Cg", "Dg", "Fg", "Gg", "Hg", "Kg", "Lg", "Mg", "Ng", "Pg", "Sg", "Tg", "Vg", "Wg", "Yg", "Zg",
		"Ad", "Bd", "Cd", "Dd", "Fd", "Gd", "Hd", "Kd", "Ld", "Md", "Nd", "Pd", "Sd", "Td", "Vd", "Wd", "Yd", "Zd",
		"Th", "Sh", "Ch", "Ph", "Wh", "Qu", "St", "Sp", "Sk", "Sm", "Sn", "Sw", "Tw", "Tr", "Dr", "Gr", "Kr", "Pr", "Br", "Fr", "Cl", "Fl", "Gl", "Pl", "Sl", "Bl",
	}
	midSyls := []string{
		"ar", "er", "ir", "or", "ur", "yr",
		"al", "el", "il", "ol", "ul", "yl",
		"an", "en", "in", "on", "un", "yn",
		"ak", "ek", "ik", "ok", "uk", "yk",
		"ag", "eg", "ig", "og", "ug", "yg",
		"ad", "ed", "id", "od", "ud", "yd",
		"ath", "eth", "ith", "oth", "uth", "yth",
		"ash", "esh", "ish", "osh", "ush", "ysh",
		"and", "end", "ind", "ond", "und", "ynd",
		"ra", "re", "ri", "ro", "ru", "ry",
		"la", "le", "li", "lo", "lu", "ly",
		"ma", "me", "mi", "mo", "mu", "my",
		"na", "ne", "ni", "no", "nu", "ny",
		"sa", "se", "si", "so", "su", "sy",
		"ta", "te", "ti", "to", "tu", "ty",
		"va", "ve", "vi", "vo", "vu", "vy",
		"za", "ze", "zi", "zo", "zu", "zy",
	}
	endSyls := []string{
		"ar", "er", "ir", "or", "ur",
		"ath", "eth", "ith", "oth", "uth",
		"an", "en", "in", "on", "un",
		"ak", "ek", "ik", "ok", "uk",
		"al", "el", "il", "ol", "ul",
		"ad", "ed", "id", "od", "ud",
		"as", "es", "is", "os", "us",
		"and", "end", "ind", "ond", "und",
		"ard", "erd", "ird", "ord", "urd",
		"ion", "eon", "ian", "aan", "oon",
		"ius", "eus", "ias", "aas", "oos",
	}

	rb := make([]byte, 2)
	rand.Read(rb)
	r1 := int(rb[0])
	r2 := int(rb[1])

	part1 := consonantStarts[r1%len(consonantStarts)]
	part2 := midSyls[r2%len(midSyls)]
	part3 := midSyls[(r1+r2)%len(midSyls)]
	part4 := endSyls[(r1*31+r2)%len(endSyls)]

	name := part1 + part2 + part3 + part4

	if i > 0 {
		extra := midSyls[i%len(midSyls)]
		name = part1 + extra + part2 + part4
	}

	if len(name) > 12 {
		name = name[:12]
	}
	if len(name) < 3 {
		name = name + "ar"
	}

	if len(name) > 0 {
		name = strings.ToUpper(name[:1]) + strings.ToLower(name[1:])
	}

	return name
}

func (o *Orchestrator) setGMLevel(username string, level int) error {
	usernameUpper := strings.ToUpper(username)

	var accountID int
	err := o.authDB.QueryRow("SELECT id FROM account WHERE username = ?", usernameUpper).Scan(&accountID)
	if err != nil {
		return fmt.Errorf("find account %s: %w", usernameUpper, err)
	}

	_, err = o.authDB.Exec(
		`INSERT INTO account_access (id, gmlevel, RealmID) VALUES (?, ?, -1)
		 ON DUPLICATE KEY UPDATE gmlevel = ?`,
		accountID, level, level,
	)
	if err != nil {
		return fmt.Errorf("set GM level: %w", err)
	}
	return nil
}

// LaunchWithRateLimit executes the given launch function for each assignment,
// throttling so that at most SpawnRateLimit bots are started per SpawnRateInterval.
func (o *Orchestrator) LaunchWithRateLimit(assignments []BotAssignment, launchFn func(BotAssignment) error) error {
	if len(assignments) == 0 {
		return nil
	}

	limit := o.config.SpawnRateLimit
	interval := o.config.SpawnRateInterval

	fmt.Printf("[Orchestrator] LaunchWithRateLimit: %d assignments, limit=%d interval=%v\n", len(assignments), limit, interval)

	if limit <= 0 || interval <= 0 {
		fmt.Println("[Orchestrator] Rate limit disabled, launching all immediately")
		for _, a := range assignments {
			if err := launchFn(a); err != nil {
				return err
			}
		}
		return nil
	}

	perBotDelay := interval / time.Duration(limit)
	if perBotDelay < 0 {
		perBotDelay = 0
	}
	fmt.Printf("[Orchestrator] Rate limit active: per-bot delay=%v\n", perBotDelay)

	for i, a := range assignments {
		if i > 0 && perBotDelay > 0 {
			fmt.Printf("[Orchestrator] rate-limit: sleeping %v before bot %d/%d (%s)\n", perBotDelay, i+1, len(assignments), a.BotID)
			time.Sleep(perBotDelay)
		}
		fmt.Printf("[Orchestrator] rate-limit: launching bot %d/%d %s now\n", i+1, len(assignments), a.BotID)
		if err := launchFn(a); err != nil {
			return err
		}
		if i%200 == 0 {
			time.Sleep(time.Second * 5)
		}
	}
	return nil
}

// LaunchBots sends bot configurations to all nodes (remote). Supports LuaCode/AIBundle from config.
func (o *Orchestrator) LaunchBots(assignments []BotAssignment) error {
	remoteAssignments := make([]BotAssignment, 0, len(assignments))
	for _, a := range assignments {
		if a.NodeAddress != "local" {
			remoteAssignments = append(remoteAssignments, a)
		}
	}
	fmt.Printf("[Orchestrator] LaunchBots: %d remote assignments\n", len(remoteAssignments))

	return o.LaunchWithRateLimit(remoteAssignments, func(a BotAssignment) error {
		username := a.AccountName
		password := a.Password
		authServer := o.config.AuthServerAddr
		charName := a.CharacterName

		if username == "" {
			username = fmt.Sprintf("bot%d", 1) // fallback, should not happen
		}
		if password == "" {
			password = "bot"
		}
		if authServer == "" {
			authServer = "127.0.0.1:3724"
		}
		if charName == "" {
			charName = a.BotID
		}

		req := NodeLaunchRequest{
			BotID:               a.BotID,
			Username:            username,
			Password:            password,
			AuthServer:          authServer,
			CharacterName:       charName,
			Race:                a.Race,
			Class:               a.Class,
			Mode:                o.config.DefaultMode,
			DungeonName:         o.config.DungeonName,
			DataDir:             o.config.DataDir,
			PathfindingAddr:     o.config.PathfindingAddress,
			LuaScript:           o.config.LuaScript,
			LuaCode:             o.config.LuaCode,
			AIBundle:            o.config.AIBundle,
			DeleteExistingChars: o.config.DeleteExistingCharacters,
			LogDecisionsToChat:  o.config.LogDecisionsToChat,
			DisableTargetCache:  o.config.DisableTargetCache,
			ValidationMode:      o.config.ValidationMode,
			ValidationLogPath:   o.config.ValidationLogPath,
			EnablePacketTrace:   o.config.EnablePacketTrace,
			EnableDetailedAuras: o.config.EnableDetailedAuras,
		}

		if err := o.sendToNode(a.NodeAddress, "/launch", req); err != nil {
			return fmt.Errorf("launch bot %s on %s: %w", a.BotID, a.NodeAddress, err)
		}
		return nil
	})
}

// LaunchLocal runs bots directly in-process using the bot package (for no-nodes case or local dev).
func (o *Orchestrator) LaunchLocal(assignments []BotAssignment) ([]*bot.Bot, error) {
	var localBots []*bot.Bot

	err := o.LaunchWithRateLimit(assignments, func(a BotAssignment) error {
		cfg := bot.Config{
			Username:                 a.AccountName,
			Password:                 a.Password,
			AuthServer:               o.config.AuthServerAddr,
			CharacterName:            a.CharacterName,
			Race:                     a.Race,
			Class:                    a.Class,
			Mode:                     o.config.DefaultMode,
			DungeonName:              o.config.DungeonName,
			DataDir:                  o.config.DataDir,
			PathfindingAddress:       o.config.PathfindingAddress,
			LuaScript:                o.config.LuaScript,
			LuaCode:                  o.config.LuaCode,
			AIBundle:                 o.config.AIBundle,
			DeleteExistingCharacters: o.config.DeleteExistingCharacters,
			LogDecisionsToChat:       o.config.LogDecisionsToChat,
			DisableTargetCache:       o.config.DisableTargetCache,
			ValidationMode:           o.config.ValidationMode,
			ValidationLogPath:        o.config.ValidationLogPath,
			EnablePacketTrace:        o.config.EnablePacketTrace,
			EnableDetailedAuras:      o.config.EnableDetailedAuras,
			AllowDBSetup:             true, // for siege initial level/pos/gear via DB + GM override
			CharDBDSN:                o.config.CharactersDBDSN,
		}

		b := bot.NewBot(a.BotID, cfg)
		localBots = append(localBots, b)
		go b.Run()
		return nil
	})
	return localBots, err
}

// CollectResults queries all nodes for bot status.
func (o *Orchestrator) CollectResults() []BotNodeResult {
	var results []BotNodeResult

	for _, a := range o.assignments {
		if a.NodeAddress == "local" {
			continue
		}

		result := o.queryNodeStatus(a.NodeAddress, a.BotID)
		results = append(results, result)
	}

	return results
}

// UpdateLuaScripts sends a Lua script update to all running bots via their nodes.
func (o *Orchestrator) UpdateLuaScripts(luaCode string) error {
	for _, a := range o.assignments {
		if a.NodeAddress == "local" {
			continue
		}

		req := map[string]string{
			"bot_id":   a.BotID,
			"lua_code": luaCode,
		}
		if err := o.sendToNode(a.NodeAddress, "/lua", req); err != nil {
			fmt.Printf("[Orchestrator] Failed to update Lua on %s: %v\n", a.NodeAddress, err)
		}
	}
	return nil
}

// Close releases resources.
func (o *Orchestrator) Close() {
	if o.authDB != nil {
		o.authDB.Close()
	}
}

func (o *Orchestrator) sendToNode(nodeAddr, path string, body interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("http://%s%s", nodeAddr, path)
	resp, err := http.Post(url, "application/json", strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("node returned %d: %s", resp.StatusCode, string(bodyBytes))
	}
	return nil
}

func (o *Orchestrator) queryNodeStatus(nodeAddr, botID string) BotNodeResult {
	url := fmt.Sprintf("http://%s/status?id=%s", nodeAddr, botID)
	resp, err := http.Get(url)
	if err != nil {
		return BotNodeResult{BotID: botID, Status: "error", Error: err.Error()}
	}
	defer resp.Body.Close()

	var result BotNodeResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return BotNodeResult{BotID: botID, Status: "error", Error: "decode error"}
	}
	return result
}

// chooseStartingRaceClass returns a race/class pair for bot index i so that
// characters are created on many different starting zones.
func chooseStartingRaceClass(i int) (uint8, uint8) {
	combos := []struct{ Race, Class uint8 }{
		// Alliance
		{1, 1},  // Human Warrior - Northshire
		{1, 2},  // Human Paladin
		{3, 1},  // Dwarf Warrior - Dun Morogh
		{3, 2},  // Dwarf Paladin
		{3, 3},  // Dwarf Hunter
		{4, 1},  // Night Elf Warrior - Teldrassil
		{4, 3},  // Night Elf Hunter
		{4, 11}, // Night Elf Druid
		{7, 1},  // Gnome Warrior - Dun Morogh
		{7, 8},  // Gnome Mage
		{11, 1}, // Draenei Warrior
		{11, 2}, // Draenei Paladin
		{11, 3}, // Draenei Hunter
		{11, 7}, // Draenei Shaman

		// Horde
		{2, 1},  // Orc Warrior - Durotar
		{2, 3},  // Orc Hunter
		{2, 7},  // Orc Shaman
		{8, 1},  // Troll Warrior - Durotar
		{8, 3},  // Troll Hunter
		{8, 7},  // Troll Shaman
		{6, 1},  // Tauren Warrior - Mulgore
		{6, 3},  // Tauren Hunter
		{6, 7},  // Tauren Shaman
		{6, 11}, // Tauren Druid
		{5, 1},  // Undead Warrior - Tirisfal Glades
		{5, 5},  // Undead Priest
		{5, 8},  // Undead Mage
		{10, 2}, // Blood Elf Paladin
		{10, 3}, // Blood Elf Hunter
		{10, 5}, // Blood Elf Priest
		{10, 8}, // Blood Elf Mage
	}
	c := combos[i%len(combos)]
	return c.Race, c.Class
}

// chooseFactionRaceClass returns race/class appropriate for the given faction.
// Used by siege scenario and similar to ensure lore-correct races.
func chooseFactionRaceClass(i int, faction string) (uint8, uint8) {
	f := strings.ToLower(faction)
	alliance := []struct{ Race, Class uint8 }{
		{1, 1}, {1, 2}, {3, 1}, {3, 3}, {4, 1}, {4, 3}, {4, 11}, {7, 8}, {11, 2}, {11, 7},
	}
	horde := []struct{ Race, Class uint8 }{
		{2, 1}, {2, 3}, {2, 7}, {8, 1}, {8, 3}, {6, 1}, {6, 11}, {5, 1}, {5, 5}, {10, 2}, {10, 3}, {10, 5},
	}
	var pool []struct{ Race, Class uint8 }
	if f == "alliance" || f == "a" {
		pool = alliance
	} else {
		pool = horde
	}
	c := pool[i%len(pool)]
	return c.Race, c.Class
}

// --- Scenario host support ---

// registerScenarioFuncs exposes Go callbacks ("orch.*") to the orchestrator Lua scenario script
// following the "Concrete Lua Scenario Host + AIBundle Spec" in the design.
func (o *Orchestrator) registerScenarioFuncs(L *lua.State) {
	// Build "orch" table
	L.NewTable()

	// orch.prepare_accounts() -> table of assignments
	// (ignores group arg, uses orchestrator config)
	o.setOrchFunc(L, "prepare_accounts", func(l *lua.State) int {
		assignments, err := o.PrepareAccounts()
		if err != nil {
			l.PushString("prepare error: " + err.Error())
			l.Error()
			return 0
		}
		l.NewTable()
		for i, a := range assignments {
			l.NewTable()
			l.PushString(a.BotID)
			l.SetField(-2, "bot_id")
			l.PushString(a.AccountName)
			l.SetField(-2, "account")
			l.PushString(a.CharacterName)
			l.SetField(-2, "char_name")
			l.PushNumber(float64(a.Race))
			l.SetField(-2, "race")
			l.PushNumber(float64(a.Class))
			l.SetField(-2, "class")
			l.PushString(a.NodeAddress)
			l.SetField(-2, "node")
			l.RawSetInt(-2, i+1)
		}
		return 1
	})

	// orch.launch_group([node], [group], aiCodeOrBundle)
	// aiCodeOrBundle may be:
	//   - string (treated as LuaCode / simple main)
	//   - table {main=..., helpers={...}, data={...}, tick_func=...}
	// Full AIBundle support + proper bundle construction.
	o.setOrchFunc(L, "launch_group", func(l *lua.State) int {
		node := ""
		// arg1 may be node string
		if l.IsString(1) {
			node, _ = l.ToString(1)
		}

		// Find the AI payload in arguments (string or table). Support group table too.
		var bundle scenario.AIBundle
		aiCode := ""
		for pos := 3; pos >= 1; pos-- {
			if l.IsString(pos) {
				aiCode, _ = l.ToString(pos)
				bundle = scenario.AIBundle{Main: aiCode, TickFunc: "on_tick"}
				break
			}
			if l.IsTable(pos) {
				bundle = luaValueToBundle(l, pos)
				if bundle.Main != "" {
					aiCode = bundle.Main // for back-compat LuaCode field
				}
				break
			}
		}
		// If no bundle yet but we have a group table at arg2 that may contain .ai
		if bundle.IsEmpty() && l.IsTable(2) {
			// look inside group.ai
			l.Field(2, "ai")
			if !l.IsNil(-1) {
				bundle = luaValueToBundle(l, l.Top())
				if bundle.Main != "" {
					aiCode = bundle.Main
				}
			}
			l.Pop(1)
		}

		asgs := o.assignments
		if len(asgs) == 0 {
			var err error
			asgs, err = o.PrepareAccounts()
			if err != nil {
				fmt.Printf("[Orchestrator] prepare fallback error: %v\n", err)
				return 0
			}
		}

		for _, a := range asgs {
			if node != "" && node != "local" && a.NodeAddress != node && a.NodeAddress != "local" {
				// simplistic: if explicit node filter, skip non-matches
				if node != a.NodeAddress {
					continue
				}
			}
			req := NodeLaunchRequest{
				BotID:               a.BotID,
				Username:            a.AccountName,
				Password:            a.Password,
				AuthServer:          o.config.AuthServerAddr,
				CharacterName:       a.CharacterName,
				Race:                a.Race,
				Class:               a.Class,
				Mode:                o.config.DefaultMode,
				DataDir:             o.config.DataDir,
				PathfindingAddr:     o.config.PathfindingAddress,
				LuaCode:             aiCode,
				AIBundle:            bundle,
				DeleteExistingChars: o.config.DeleteExistingCharacters,
				LogDecisionsToChat:  o.config.LogDecisionsToChat,
				DisableTargetCache:  o.config.DisableTargetCache,
				ValidationMode:      o.config.ValidationMode,
				ValidationLogPath:   o.config.ValidationLogPath,
				EnablePacketTrace:   o.config.EnablePacketTrace,
				EnableDetailedAuras: o.config.EnableDetailedAuras,
			}
			if a.NodeAddress == "local" || node == "local" || node == "" {
				cfg := bot.Config{
					Username:                 a.AccountName,
					Password:                 a.Password,
					AuthServer:               o.config.AuthServerAddr,
					CharacterName:            a.CharacterName,
					Race:                     a.Race,
					Class:                    a.Class,
					Mode:                     o.config.DefaultMode,
					DataDir:                  o.config.DataDir,
					PathfindingAddress:       o.config.PathfindingAddress,
					LuaCode:                  aiCode,
					AIBundle:                 bundle,
					DeleteExistingCharacters: o.config.DeleteExistingCharacters,
					AllowDBSetup:             true,
					CharDBDSN:                o.config.CharactersDBDSN,
				}
				b := bot.NewBot(a.BotID, cfg)
				o.localBots.Store(a.BotID, b)
				go b.Run()
			} else {
				addr := a.NodeAddress
				if node != "" {
					addr = node
				}
				_ = o.sendToNode(addr, "/launch", req)
			}
		}
		return 0
	})

	// orch.send_lua_update(botID|"all", codeOrBundle)
	// Enhanced: accepts string (lua_code) or table (full ai_bundle).
	o.setOrchFunc(L, "send_lua_update", func(l *lua.State) int {
		botID, _ := l.ToString(1)
		var code string
		var bndl scenario.AIBundle
		if l.IsString(2) {
			code, _ = l.ToString(2)
		} else if l.IsTable(2) {
			bndl = luaValueToBundle(l, 2)
			code = bndl.Main
		}
		for _, a := range o.assignments {
			if botID != "all" && a.BotID != botID {
				continue
			}
			if a.NodeAddress == "local" {
				if v, ok := o.localBots.Load(a.BotID); ok {
					if bb, ok2 := v.(*bot.Bot); ok2 {
						if !bndl.IsEmpty() {
							_ = bb.LoadAIBundle(bndl)
						} else {
							_ = bb.LoadLuaScript(code)
						}
						continue
					}
				}
				if !bndl.IsEmpty() {
					fmt.Printf("[Orchestrator scenario] (local) bundle update %s (main len=%d)\n", a.BotID, len(bndl.Main))
				} else {
					fmt.Printf("[Orchestrator scenario] (local) would update %s with code prefix: %.40s\n", a.BotID, code)
				}
				continue
			}
			if !bndl.IsEmpty() {
				req := map[string]interface{}{"bot_id": a.BotID, "ai_bundle": bndl}
				_ = o.sendToNode(a.NodeAddress, "/ai", req)
			} else {
				req := map[string]string{"bot_id": a.BotID, "lua_code": code}
				_ = o.sendToNode(a.NodeAddress, "/lua", req)
			}
		}
		return 0
	})

	// orch.send_ai_update(botID|"all", bundleTable) -- explicit full bundle/phase update
	o.setOrchFunc(L, "send_ai_update", func(l *lua.State) int {
		botID, _ := l.ToString(1)
		var bndl scenario.AIBundle
		if l.IsTable(2) {
			bndl = luaValueToBundle(l, 2)
		} else if l.IsString(2) {
			bndl = scenario.AIBundle{Main: func() string { s, _ := l.ToString(2); return s }(), TickFunc: "on_tick"}
		}
		for _, a := range o.assignments {
			if botID != "all" && a.BotID != botID {
				continue
			}
			if a.NodeAddress == "local" {
				if v, ok := o.localBots.Load(a.BotID); ok {
					if bb, ok2 := v.(*bot.Bot); ok2 {
						_ = bb.LoadAIBundle(bndl)
						continue
					}
				}
				fmt.Printf("[Orchestrator scenario] (local) ai bundle update for %s mainLen=%d dataKeys=%d\n", a.BotID, len(bndl.Main), len(bndl.Data))
				continue
			}
			req := map[string]interface{}{
				"bot_id":    a.BotID,
				"ai_bundle": bndl,
			}
			_ = o.sendToNode(a.NodeAddress, "/ai", req)
		}
		return 0
	})

	// orch.log(msg)
	o.setOrchFunc(L, "log", func(l *lua.State) int {
		msg, _ := l.ToString(1)
		fmt.Printf("[scenario] %s\n", msg)
		return 0
	})

	// orch.sleep(ms)
	o.setOrchFunc(L, "sleep", func(l *lua.State) int {
		ms, _ := l.ToInteger(1)
		time.Sleep(time.Duration(ms) * time.Millisecond)
		return 0
	})

	// Finish the table and set as global "orch"
	L.SetGlobal("orch")
}

// setOrchFunc is the helper modeled after luaengine.setFunc: pushes Go func as closure on the table at -2.
func (o *Orchestrator) setOrchFunc(L *lua.State, name string, fn lua.Function) {
	L.PushGoFunction(fn)
	L.SetField(-2, name)
}

// RunScenario loads and executes a Lua scenario script.
// Scripts may imperatively call orch.prepare_accounts(), orch.launch_group(...), orch.sleep etc.
// or return a plan table (future richer handling).
func (o *Orchestrator) RunScenario(path string) error {
	L := lua.NewState()
	lua.OpenLibraries(L)
	o.registerScenarioFuncs(L)
	o.lua = L

	if err := lua.DoFile(L, path); err != nil {
		return fmt.Errorf("scenario lua error in %s: %w", path, err)
	}

	// If script left a table result, note it (basic).
	if L.IsTable(-1) {
		L.Field(-1, "groups")
		if L.IsTable(-1) {
			fmt.Println("[Orchestrator] Scenario script returned a groups table (side-effect launches via orch.* preferred).")
		}
		L.Pop(1)
	}

	fmt.Printf("[Orchestrator] RunScenario(%s) completed (basic host).\n", path)
	return nil
}

// luaValueToBundle converts a lua value (string or table) at idx into scenario.AIBundle.
// Used by scenario launch paths (recreated from migration plan expectations).
func luaValueToBundle(l *lua.State, idx int) scenario.AIBundle {
	abs := l.AbsIndex(idx)
	if l.IsString(abs) {
		s, _ := l.ToString(abs)
		return scenario.AIBundle{Main: s, TickFunc: "on_tick"}
	}
	if !l.IsTable(abs) {
		return scenario.AIBundle{}
	}

	b := scenario.AIBundle{
		Helpers: make(map[string]string),
		Data:    make(map[string]any),
	}

	// Iterate table using absolute index
	l.PushNil()
	for l.Next(abs) {
		key, _ := l.ToString(-2)
		switch key {
		case "main", "Main":
			b.Main, _ = l.ToString(-1)
		case "tick_func", "TickFunc", "tickFunc":
			b.TickFunc, _ = l.ToString(-1)
		case "helpers", "Helpers":
			if l.IsTable(-1) {
				l.PushNil()
				for l.Next(-2) {
					hk, _ := l.ToString(-2)
					hv, _ := l.ToString(-1)
					if hk != "" {
						b.Helpers[hk] = hv
					}
					l.Pop(1)
				}
			}
		case "data", "Data":
			if l.IsTable(-1) {
				l.PushNil()
				for l.Next(-2) {
					dk, _ := l.ToString(-2)
					if l.IsNumber(-1) {
						if n, ok := l.ToNumber(-1); ok {
							if n == float64(int64(n)) {
								b.Data[dk] = int64(n)
							} else {
								b.Data[dk] = n
							}
						}
					} else if l.IsString(-1) {
						s, _ := l.ToString(-1)
						b.Data[dk] = s
					} else if l.IsBoolean(-1) {
						bl := l.ToBoolean(-1)
						b.Data[dk] = bl
					}
					l.Pop(1)
				}
			}
		}
		l.Pop(1)
	}
	if b.TickFunc == "" {
		b.TickFunc = "on_tick"
	}
	return b
}
