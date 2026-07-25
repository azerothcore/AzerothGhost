package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/walkline/AzerothGhost/config"
	"github.com/walkline/AzerothGhost/orchestrator"
)

// runScenario implements `azghost scenario run <file.lua>` (and future subcmds).
// Enhanced with profile/config support for auth server and data dir.
func runScenario(args []string, cliCfg config.CLIConfig) {
	// Skip leading global flags (e.g. --profile ...) so that "run <file>" can be found
	// even when globals appear before the "scenario" verb.
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		if !strings.Contains(args[0], "=") && len(args) > 1 && !strings.HasPrefix(args[1], "-") {
			args = args[2:] // skip --key value
		} else {
			args = args[1:]
		}
	}
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Println("Usage: azghost scenario run <path/to/scenario.lua>")
		fmt.Println("Example: azghost scenario run scripts/test_basic.lua")
		fmt.Println("         azghost --profile local-ac --nodes \"10.0.0.5:8888,10.0.0.6:8888\" scenario run scripts/siege.lua")
		fmt.Println("Orgrimmar Siege (large PvP): azghost --profile local-ac --alliance-bots 30 --horde-bots 30 scenario run scenarios/orgrimmar_siege.lua")
		return
	}
	sub := args[0]
	switch sub {
	case "run":
		if len(args) < 2 {
			fmt.Println("Usage: azghost scenario run <file.lua>")
			return
		}
		file := args[1]
		if file == "--help" || file == "-h" {
			fmt.Println("Usage: azghost scenario run <file.lua>")
			return
		}

		cfg := orchestrator.DefaultConfig()
		// Apply from loaded profile / config
		if cliCfg.AuthServer != "" {
			cfg.AuthServerAddr = cliCfg.AuthServer
		}
		if cliCfg.DataDir != "" {
			cfg.DataDir = cliCfg.DataDir
		}
		if cliCfg.Nodes != "" {
			cfg.NodeAddresses = strings.Split(cliCfg.Nodes, ",")
		}
		if cliCfg.NumBots > 0 {
			cfg.NumBots = cliCfg.NumBots
		}
		if cliCfg.AccountPrefix != "" {
			cfg.AccountPrefix = cliCfg.AccountPrefix
		}
		if cliCfg.AccountPassword != "" {
			cfg.AccountPassword = cliCfg.AccountPassword
		}
		if cliCfg.DeleteExistingChars {
			cfg.DeleteExistingCharacters = true
		}
		if cliCfg.LogDecisionsToChat {
			cfg.LogDecisionsToChat = true
		}
		if d := os.Getenv("AZGHOST_DATA_DIR"); d != "" {
			cfg.DataDir = d
		}
		// DB etc can come from profile too
		if cliCfg.DBDSN != "" {
			cfg.AuthDBDSN = cliCfg.DBDSN
		}

		fmt.Printf("[scenario] using auth=%s dataDir=%s\n", cfg.AuthServerAddr, cfg.DataDir)

		orch, err := orchestrator.NewOrchestrator(cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create orchestrator for scenario: %v\n", err)
			os.Exit(1)
		}
		defer orch.Close()

		host := orchestrator.NewScenarioHost(orch)
		if err := host.RunFile(file); err != nil {
			fmt.Fprintf(os.Stderr, "scenario error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Scenario completed successfully.")
	default:
		fmt.Printf("Unknown scenario subcommand: %s\n", sub)
		fmt.Println("Supported: run <file.lua>")
	}
}
