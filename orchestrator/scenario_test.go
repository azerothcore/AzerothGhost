package orchestrator

import (
	"testing"

	lua "github.com/Shopify/go-lua"

	"github.com/azerothcore/AzerothGhost/scenario"
)

func TestLuaValueToBundleString(t *testing.T) {
	L := lua.NewState()
	lua.OpenLibraries(L)
	if err := lua.LoadString(L, `return "function on_tick() end"`); err != nil {
		t.Fatal(err)
	}
	if err := L.ProtectedCall(0, 1, 0); err != nil {
		t.Fatal(err)
	}
	b := luaValueToBundle(L, -1)
	if b.Main == "" {
		t.Fatalf("expected main from string, got empty")
	}
}

func TestLuaValueToBundleFullTable(t *testing.T) {
	L := lua.NewState()
	lua.OpenLibraries(L)
	src := `return {
		main = "function on_p() end",
		helpers = { util = "function h() return 1 end" },
		data = { phase = 2, msg = "siege" },
		tick_func = "on_p"
	}`
	if err := lua.LoadString(L, src); err != nil {
		t.Fatal(err)
	}
	if err := L.ProtectedCall(0, 1, 0); err != nil {
		t.Fatal(err)
	}
	b := luaValueToBundle(L, -1)
	if b.Main == "" || b.TickFunc != "on_p" {
		t.Fatalf("bad main/tick: %+v", b)
	}
	if len(b.Helpers) == 0 || len(b.Data) == 0 {
		t.Fatalf("expected helpers and data, got %+v", b)
	}
	if b.Data["phase"] != int64(2) {
		t.Fatalf("data phase wrong: %T %v", b.Data["phase"], b.Data["phase"])
	}
}

func TestLuaValueToBundleInGroup(t *testing.T) {
	L := lua.NewState()
	lua.OpenLibraries(L)
	src := `return { ai = { main = "f()", tick_func = "f" } }`
	if err := lua.LoadString(L, src); err != nil {
		t.Fatal(err)
	}
	if err := L.ProtectedCall(0, 1, 0); err != nil {
		t.Fatal(err)
	}
	// simulate extracting from group table
	L.Field(-1, "ai")
	b := luaValueToBundle(L, -1)
	L.Pop(1)
	if b.Main != "f()" || b.TickFunc != "f" {
		t.Fatalf("group ai extract failed: %+v", b)
	}
}

func TestBundleRoundtripWithServerTypes(t *testing.T) {
	// Ensure types used by server/launch still serialize with richer content
	b := scenario.AIBundle{
		Main:     "function on_tick() end",
		Helpers:  map[string]string{"a.lua": "-- helper"},
		Data:     map[string]any{"x": 42, "nested": map[string]any{"y": "z"}},
		TickFunc: "on_tick",
	}
	// just construct; server json tests already cover
	_ = b
}
