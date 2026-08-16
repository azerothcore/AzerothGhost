package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/azerothcore/AzerothGhost/bot"
	"github.com/azerothcore/AzerothGhost/config"
	"github.com/azerothcore/AzerothGhost/orchestrator"
	"github.com/azerothcore/AzerothGhost/server"
)

// Version is set at build time via -ldflags or defaults here for skeleton.
var Version = "0.0.1-skeleton"

func main() {
	// Support subcommand-style: azghost [global-flags] <mode> [--mode-flags]...
	// Global flags like --profile / --config can appear before or after the mode verb.
	runMode := "cli"
	verbIdx := -1
	for i := 1; i < len(os.Args); i++ {
		a := os.Args[i]
		if strings.HasPrefix(a, "-") {
			if !strings.Contains(a, "=") && i+1 < len(os.Args) && !strings.HasPrefix(os.Args[i+1], "-") {
				i++ // skip value
			}
			continue
		}
		switch a {
		case "cli", "node", "server", "orchestrator", "scenario":
			runMode = a
			verbIdx = i
			goto foundVerb
		}
	}
foundVerb:
	if verbIdx > 0 {
		// Remove only the verb token, keep all flags (before and after) for flag.Parse + Visit.
		newArgs := make([]string, 0, len(os.Args)-1)
		newArgs = append(newArgs, os.Args[0])
		newArgs = append(newArgs, os.Args[1:verbIdx]...)
		newArgs = append(newArgs, os.Args[verbIdx+1:]...)
		os.Args = newArgs
	}

	// CLI flags (common + mode-specific; full wiring in later PRs)
	mode := flag.String("mode", "", "Run mode: 'cli' for single bot, 'node' for HTTP API node server, 'orchestrator' for test controller, 'scenario' for Lua scenario runner")
	username := flag.String("username", "admin", "Account username")
	password := flag.String("password", "admin", "Account password")
	authServer := flag.String("auth-server", "127.0.0.1:3724", "Auth server address (host:port)")
	charName := flag.String("char-name", "Loadtst", "Character name")
	realmIndex := flag.Int("realm-index", 0, "Realm index (0-based)")
	listenAddr := flag.String("listen", ":8888", "HTTP server listen address (node mode)")
	race := flag.Int("race", 1, "Character race for creation (default: 1=Human)")
	class := flag.Int("class", 1, "Character class for creation (default: 1=Warrior)")
	botMode := flag.String("bot-mode", "grind", "Bot behavior mode: grind, hogger, dungeon, idle, lua")
	bots := flag.Int("bots", 0, "Alias for --num-bots")
	dungeonName := flag.String("dungeon", "", "Dungeon name for dungeon mode")
	dataDir := flag.String("data-dir", "", "Path to data directory root containing mmaps/, maps/, vmaps/ for embedded pathfinding")
	pathfindingAddr := flag.String("pathfinding-addr", "", "Address of external pathfinding gRPC service (optional)")
	luaScript := flag.String("lua-script", "", "Path to Lua script file")
	deleteExistingChars := flag.Bool("delete-existing-chars", false, "Delete all existing characters on the account before creating the target one")
	spawnRateLimit := flag.Int("spawn-rate-limit", 50, "Max bots to spawn per spawn-rate-interval (orchestrator)")
	spawnRateInterval := flag.Duration("spawn-rate-interval", 2*time.Second, "Interval for spawn rate limit (orchestrator)")
	logDecisionsToChat := flag.Bool("log-decisions-to-chat", true, "Bots will /say their major AI decisions (throttled). Disable for scale.")
	disableTargetCache := flag.Bool("disable-target-cache", false, "Disable the short-lived target cache in findBestTarget")

	// Validation tooling (OFF by default - zero perf impact on regular runs)
	validationMode := flag.Bool("validation-mode", false, "Enable validation instrumentation (structured logs, ring buffers). Use only for E2E quality runs.")
	validationLog := flag.String("validation-log", "", "Path to write structured validation JSONL (only when --validation-mode).")
	tracePackets := flag.Bool("trace-packets", false, "Trace key packets (SPELL/AURA/ATTACK) when --validation-mode (expensive).")
	enableDetailedAuras := flag.Bool("enable-detailed-auras", false, "Keep full aura metadata (duration/stacks) - only with validation-mode.")

	// Orchestrator flags
	numBots := flag.Int("num-bots", 1, "Number of bots to create (orchestrator mode)")
	nodes := flag.String("nodes", "", "Comma-separated list of node addresses (orchestrator mode)")
	accountPrefix := flag.String("account-prefix", "loadbot", "Account name prefix (orchestrator mode)")
	accountPassword := flag.String("account-password", "loadbot", "Account password (orchestrator mode)")
	dbDSN := flag.String("db-dsn", "acore:acore@tcp(127.0.0.1:3306)/acore_auth", "Auth database DSN (orchestrator mode)")
	duration := flag.Duration("duration", 0, "Run duration for cli/orchestrator (0 = until Ctrl+C; orchestrator default applied if unset there)")

	// Ergonomic config flags
	profile := flag.String("profile", "", "Named profile to load (e.g. --profile local-ac). Looks in ~/.config/azghost/profiles/ and .azghost/profiles/")
	cfgPath := flag.String("config", "", "Path to config file (YAML). Overrides auto-discovery.")

	// Version / help are handled by flag automatically for -h/--help
	versionFlag := flag.Bool("version", false, "Print version and exit")

	flag.Parse()

	if *versionFlag {
		fmt.Printf("azghost version %s\n", Version)
		os.Exit(0)
	}

	// --mode flag overrides subcommand if set
	if *mode != "" {
		runMode = *mode
	}

	// --bots N is a convenient alias for --num-bots N
	if *bots > 0 {
		*numBots = *bots
	}

	// Load configuration (profile + config file + env + defaults).
	// CLI flags (explicitly passed) are applied on top (highest precedence).
	cliCfg, err := config.Load(*profile, *cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config load error: %v\n", err)
		os.Exit(1)
	}

	// Only apply flags that were explicitly set on the command line (use Visit).
	// This prevents default flag values from clobbering profile/env values.
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "username":
			cliCfg.Username = *username
		case "password":
			cliCfg.Password = *password
		case "auth-server":
			cliCfg.AuthServer = *authServer
		case "char-name":
			cliCfg.CharName = *charName
		case "realm-index":
			cliCfg.RealmIndex = *realmIndex
		case "race":
			cliCfg.Race = *race
		case "class":
			cliCfg.Class = *class
		case "bot-mode":
			cliCfg.BotMode = *botMode
		case "dungeon":
			cliCfg.DungeonName = *dungeonName
		case "data-dir":
			cliCfg.DataDir = *dataDir
		case "pathfinding-addr":
			cliCfg.PathfindingAddress = *pathfindingAddr
		case "lua-script":
			cliCfg.LuaScript = *luaScript
			// Passing a script always selects Lua AI (profile default is often "grind").
			cliCfg.BotMode = "lua"
		case "delete-existing-chars":
			cliCfg.DeleteExistingChars = *deleteExistingChars
		case "log-decisions-to-chat":
			cliCfg.LogDecisionsToChat = *logDecisionsToChat
		case "disable-target-cache":
			cliCfg.DisableTargetCache = *disableTargetCache
		case "validation-mode":
			cliCfg.ValidationMode = *validationMode
		case "validation-log":
			cliCfg.ValidationLogPath = *validationLog
		case "trace-packets":
			cliCfg.EnablePacketTrace = *tracePackets
		case "enable-detailed-auras":
			cliCfg.EnableDetailedAuras = *enableDetailedAuras
		case "listen":
			cliCfg.Listen = *listenAddr
		case "num-bots":
			cliCfg.NumBots = *numBots
		case "nodes":
			cliCfg.Nodes = *nodes
		case "account-prefix":
			cliCfg.AccountPrefix = *accountPrefix
		case "account-password":
			cliCfg.AccountPassword = *accountPassword
		case "db-dsn":
			cliCfg.DBDSN = *dbDSN
		case "duration":
			cliCfg.Duration = *duration
		case "spawn-rate-limit":
			cliCfg.SpawnRateLimit = *spawnRateLimit
		case "spawn-rate-interval":
			cliCfg.SpawnRateInterval = *spawnRateInterval
		}
	})

	// If profile was loaded, surface it
	if cliCfg.Profile != "" {
		fmt.Printf("[config] using profile: %s\n", cliCfg.Profile)
	}
	if cliCfg.ConfigFile != "" {
		fmt.Printf("[config] using config file: %s\n", cliCfg.ConfigFile)
	}

	switch runMode {
	case "cli":
		runCLI(cliCfg)
	case "node", "server":
		runNode(cliCfg)
	case "orchestrator":
		runOrchestrator(cliCfg)
	case "scenario":
		runScenario(os.Args[1:], cliCfg) // pass remaining args + loaded config for E2E (auth/data_dir from profile)
	default:
		fmt.Fprintf(os.Stderr, "Unknown mode: %s\n", runMode)
		flag.Usage()
		os.Exit(1)
	}
}

func runCLI(c config.CLIConfig) {
	fmt.Println("=== AzerothGhost CLI ===")
	fmt.Printf("  user=%s@%s char=%s mode=%s dataDir=%s\n",
		c.Username, c.AuthServer, c.CharName, c.BotMode, c.DataDir)
	if c.Profile != "" {
		fmt.Printf("  (profile: %s)\n", c.Profile)
	}

	botCfg := bot.Config{
		Username:                 c.Username,
		Password:                 c.Password,
		AuthServer:               c.AuthServer,
		CharacterName:            c.CharName,
		RealmIndex:               c.RealmIndex,
		Race:                     uint8(c.Race),
		Class:                    uint8(c.Class),
		Mode:                     c.BotMode,
		DungeonName:              c.DungeonName,
		DataDir:                  c.DataDir,
		PathfindingAddress:       c.PathfindingAddress,
		LuaScript:                c.LuaScript,
		LuaCode:                  c.LuaCode,
		AITickMs:                 200,
		DeleteExistingCharacters: c.DeleteExistingChars,
		LogDecisionsToChat:       c.LogDecisionsToChat,
		DisableTargetCache:       c.DisableTargetCache,
		ValidationMode:           c.ValidationMode,
		ValidationLogPath:        c.ValidationLogPath,
		EnablePacketTrace:        c.EnablePacketTrace,
		EnableDetailedAuras:      c.EnableDetailedAuras,
	}

	b := bot.NewBot("cli-1", botCfg)

	// Handle SIGINT/SIGTERM
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down bot...")
		b.Stop()
	}()

	// Optional wall-clock limit for CLI (validation / smoke runs).
	if c.Duration > 0 {
		fmt.Printf("  duration=%v\n", c.Duration)
		go func() {
			time.Sleep(c.Duration)
			fmt.Println("\nCLI duration reached, stopping bot...")
			b.Stop()
		}()
	}

	result := b.Run()

	fmt.Printf("\n=== Bot Result ===\n")
	fmt.Printf("Status: %s\n", result.Status)
	fmt.Printf("Level:  %d\n", result.Level)
	fmt.Printf("Kills:  %d\n", result.Kills)
	fmt.Printf("Deaths: %d\n", result.Deaths)
	if result.Error != "" {
		fmt.Printf("Error:  %s\n", result.Error)
	}

	fmt.Printf("\n=== Events (last 20) ===\n")
	events := b.Events()
	start := 0
	if len(events) > 20 {
		start = len(events) - 20
	}
	for _, e := range events[start:] {
		fmt.Printf("[%s] %s: %s\n", e.Time.Format("15:04:05"), e.Type, e.Message)
	}

	if result.Status == bot.BotStatusError {
		os.Exit(1)
	}
}

func runNode(c config.CLIConfig) {
	fmt.Println("=== AzerothGhost Node ===")
	srv := server.NewServerWithDefaults(c.DataDir, c.PathfindingAddress)

	// Handle SIGINT/SIGTERM for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nNode server shutting down...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := srv.Stop(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "server shutdown error: %v\n", err)
		}
	}()

	fmt.Printf("Starting node HTTP server on %s (dataDir=%q, pathfinding=%q)\n", c.Listen, c.DataDir, c.PathfindingAddress)
	if err := srv.Start(c.Listen); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "Node server error: %v\n", err)
	}
}

func runOrchestrator(c config.CLIConfig) {
	fmt.Println("=== AzerothGhost Orchestrator ===")
	var nodeAddrs []string
	if c.Nodes != "" {
		nodeAddrs = strings.Split(c.Nodes, ",")
	}

	orchCfg := orchestrator.Config{
		AuthDBDSN:                c.DBDSN,
		AuthServerAddr:           c.AuthServer,
		NodeAddresses:            nodeAddrs,
		AccountPrefix:            c.AccountPrefix,
		AccountPassword:          c.AccountPassword,
		NumBots:                  c.NumBots,
		DefaultRace:              uint8(c.Race),
		DefaultClass:             uint8(c.Class),
		DefaultMode:              c.BotMode,
		DungeonName:              c.DungeonName,
		DataDir:                  c.DataDir,
		PathfindingAddress:       c.PathfindingAddress,
		LuaScript:                c.LuaScript,
		LuaCode:                  c.LuaCode,
		DeleteExistingCharacters: c.DeleteExistingChars || true,
		SpawnRateLimit:           c.SpawnRateLimit,
		SpawnRateInterval:        c.SpawnRateInterval,
		LogDecisionsToChat:       c.LogDecisionsToChat,
		DisableTargetCache:       c.DisableTargetCache,
		ValidationMode:           c.ValidationMode,
		ValidationLogPath:        c.ValidationLogPath,
		EnablePacketTrace:        c.EnablePacketTrace,
		EnableDetailedAuras:      c.EnableDetailedAuras,
	}
	fmt.Printf("[main] orchestrator cfg: spawnRateLimit=%d spawnRateInterval=%v numBots=%d nodes=%v\n", c.SpawnRateLimit, c.SpawnRateInterval, c.NumBots, nodeAddrs)

	orch, err := orchestrator.NewOrchestrator(orchCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create orchestrator: %v\n", err)
		os.Exit(1)
	}
	defer orch.Close()

	fmt.Println("Preparing accounts (DB optional)...")
	assignments, err := orch.PrepareAccounts()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to prepare accounts: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Prepared %d bot accounts\n", len(assignments))
	for _, a := range assignments {
		fmt.Printf("  %s -> %s@%s (char: %s r%d/c%d)\n", a.BotID, a.AccountName, a.NodeAddress, a.CharacterName, a.Race, a.Class)
	}

	// Remote nodes path
	if len(nodeAddrs) > 0 {
		fmt.Println("Launching bots on remote nodes (rate-limited)...")
		if err := orch.LaunchBots(assignments); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to launch bots: %v\n", err)
			os.Exit(1)
		}
	}

	// Local execution path (no nodes or explicit "local")
	var localBots []*bot.Bot
	if len(nodeAddrs) == 0 || (len(nodeAddrs) == 1 && nodeAddrs[0] == "local") {
		fmt.Println("Running bots locally (via rate-limited launch using bot pkg)...")
		localBots, _ = orch.LaunchLocal(assignments)
	}

	// Wait for duration or signal (default 5m if --duration not set)
	dur := c.Duration
	if dur <= 0 {
		dur = 5 * time.Minute
	}
	fmt.Printf("Test running for %v (Ctrl+C to stop early)...\n", dur)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case <-sigCh:
		fmt.Println("\nStopping test early...")
	case <-time.After(dur):
		fmt.Println("Test duration reached.")
	}

	for _, b := range localBots {
		b.Stop()
	}
	time.Sleep(1 * time.Second)

	fmt.Println("\n=== Orchestrator done ===")
	if len(localBots) > 0 {
		for _, b := range localBots {
			st := b.Status()
			fmt.Printf("  bot=%s status=%s level=%d kills=%d deaths=%d err=%s\n", st.ID, st.Status, st.Level, st.Kills, st.Deaths, st.Error)
		}
	}
}

// runScenario is defined in scenario.go
