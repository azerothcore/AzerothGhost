package bot

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/azerothcore/AzerothGhost/ai/luaengine"
)

// TestAdvancedAIGrindPipeline validates the production advanced AI path:
// load_for_bot / enable_default_strategies, single-spec warrior, survive,
// rest OOC, rend when in melee with a live target.
func TestAdvancedAIGrindPipeline(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	oldWD, _ := os.Getwd()
	_ = os.Chdir(filepath.Join(filepath.Dir(thisFile), ".."))
	defer os.Chdir(oldWD)

	mock := &aiLogicMock{
		class:    1,
		alive:    true,
		hpCur:    90,
		hpMax:    100,
		inCombat: true,
		target:   555,
		auras:    map[uint64]map[uint32]bool{},
		// Lowbie arms-like: no MS/BT/slam — force rend/shout/engage path.
		spellReady: map[uint32]bool{
			6673:  true, // battle shout
			772:   true, // rend
			78:    true, // HS
			5308:  true, // execute
			34428: true, // VR
			100:   true, // charge
		},
	}

	e := luaengine.NewEngine(mock)
	if err := e.DoString(`
local boot = dofile("scripts/ai/init.lua")
ai = boot.load_for_bot()
assert(ai, "load_for_bot nil")
assert(ai.active_strategies["survive"], "survive not enabled")
assert(ai.active_strategies["rest"], "rest not enabled")
assert(ai.active_strategies["grind"], "grind not enabled")
assert(ai.active_strategies["melee"], "melee not enabled")
assert(ai.active_strategies["generic_warrior"], "generic_warrior not enabled")
assert(ai.active_strategies["arms"], "arms not enabled")
assert(not ai.active_strategies["fury"], "fury must NOT be enabled by default")
assert(not ai.active_strategies["prot"], "prot must NOT be enabled by default")
`); err != nil {
		t.Fatalf("load: %v", err)
	}

	// --- Rend: target without rend aura, spells gated ---
	mock.casts = nil
	mock.logs = nil
	mock.auras[555] = map[uint32]bool{} // no rend
	// Self already has shout so shout doesn't steal every tick
	mock.auras[123456] = map[uint32]bool{6673: true}
	mock.auras[0] = map[uint32]bool{6673: true}

	if err := e.DoString(`for i=1,8 do ai:Tick() end`); err != nil {
		t.Fatalf("tick: %v", err)
	}

	rend := false
	for _, c := range mock.casts {
		if c == "772@555" {
			rend = true
			break
		}
	}
	for _, l := range mock.logs {
		if contains(l, "rend") {
			rend = true
			break
		}
	}
	if !rend {
		t.Fatalf("expected rend cast with live target; casts=%v logs=%v", mock.casts, firstN(mock.logs, 12))
	}
	t.Log("✓ arms rend fired with single primary spec")

	// --- Death -> revive ---
	mock.alive = false
	mock.commands = nil
	if err := e.DoString(`ai:Tick()`); err != nil {
		t.Fatalf("dead tick: %v", err)
	}
	gotRevive := false
	for _, c := range mock.commands {
		if contains(c, ".revive") {
			gotRevive = true
			break
		}
	}
	if !gotRevive {
		t.Fatalf("expected .revive when dead; commands=%v", mock.commands)
	}
	t.Log("✓ survive revive on death")

	// --- Low HP OOC rest (must log + not pull) ---
	mock.alive = true
	mock.inCombat = false
	mock.hpCur = 20
	mock.target = 0
	mock.logs = nil
	mock.casts = nil
	mock.moves = 0
	if err := e.DoString(`for i=1,3 do ai:Tick() end`); err != nil {
		t.Fatalf("lowhp: %v", err)
	}
	low := false
	for _, l := range mock.logs {
		if contains(l, "low health") || contains(l, "rest:") {
			low = true
			break
		}
	}
	if !low {
		t.Fatalf("expected rest/survive low-HP decision OOC; logs=%v", firstN(mock.logs, 10))
	}
	t.Log("✓ OOC low HP rest/survive")
}
