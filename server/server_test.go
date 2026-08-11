package server

import (
	"encoding/json"
	"testing"

	"github.com/walkline/AzerothGhost/scenario"
)

func TestServerNavigationDefaultsOverrideLaunchRequestPaths(t *testing.T) {
	s := NewServerWithDefaults("/data", "pathfinding:9090")

	if got := s.effectiveDataDir("/home/walkline/dev/wow/Data"); got != "/data" {
		t.Fatalf("effective data dir = %q, want /data", got)
	}
	if got := s.effectivePathfindingAddress("host-pathfinding:9090"); got != "pathfinding:9090" {
		t.Fatalf("effective pathfinding address = %q, want pathfinding:9090", got)
	}
}

func TestServerNavigationDefaultsFallBackToLaunchRequest(t *testing.T) {
	s := NewServer()

	if got := s.effectiveDataDir("/home/walkline/dev/wow/Data"); got != "/home/walkline/dev/wow/Data" {
		t.Fatalf("effective data dir = %q, want request path", got)
	}
	if got := s.effectivePathfindingAddress("host-pathfinding:9090"); got != "host-pathfinding:9090" {
		t.Fatalf("effective pathfinding address = %q, want request address", got)
	}
}

func TestLaunchRequestJSONWithAIPayloads(t *testing.T) {
	req := LaunchRequest{
		Username:      "test",
		Password:      "pw",
		AuthServer:    "127.0.0.1:3724",
		CharacterName: "Bot1",
		LuaCode:       `function on_tick() bot.send_chat("hello from lua_code") end`,
		AIBundle: scenario.AIBundle{
			Main:     `function on_tick() bot.send_chat("from bundle main") end`,
			Helpers:  map[string]string{"util.lua": "function helper() return 42 end"},
			Data:     map[string]any{"phase": 2, "msg": "siege"},
			TickFunc: "on_tick",
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded LaunchRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.LuaCode == "" || decoded.AIBundle.Main == "" {
		t.Fatalf("expected LuaCode and AIBundle.Main to roundtrip, got LuaCode=%q Main=%q", decoded.LuaCode, decoded.AIBundle.Main)
	}
	if decoded.AIBundle.TickFunc != "on_tick" {
		t.Fatalf("expected TickFunc roundtrip")
	}
	if len(decoded.AIBundle.Data) == 0 {
		t.Fatalf("expected Data map roundtrip")
	}
}

func TestNodeLaunchRequestJSONAndMapping(t *testing.T) {
	// Verify NodeLaunchRequest (used by LaunchFromOrchestrator) carries AI fields.
	nlr := NodeLaunchRequest{
		BotID:         "b1",
		Username:      "u",
		Password:      "p",
		AuthServer:    "a:1",
		CharacterName: "C",
		LuaCode:       "function on_tick() end",
		AIBundle:      scenario.AIBundle{Main: "-- ai here"},
	}

	b, _ := json.Marshal(nlr)
	var out NodeLaunchRequest
	json.Unmarshal(b, &out)

	if out.LuaCode != "function on_tick() end" || out.AIBundle.Main != "-- ai here" {
		t.Fatalf("NodeLaunchRequest AI fields did not roundtrip: %+v", out)
	}
}
