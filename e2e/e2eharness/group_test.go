package e2eharness

import (
	"testing"

	"github.com/azerothcore/AzerothGhost/client"
)

func TestMemberNamesJoin(t *testing.T) {
	// pure helper sanity (no live stack)
	a := &ScenarioBot{Session: &Session{Name: "Alpha"}}
	b := &ScenarioBot{Session: &Session{Name: "Beta"}}
	got := memberNames([]*ScenarioBot{a, b, nil})
	if got != "Alpha,Beta" {
		t.Fatalf("got %q", got)
	}
}

func TestLootMethodConstantsMatchClient(t *testing.T) {
	// Document expected loot method enum for authors (master loot = 2).
	if client.LootMethodMasterLoot != 2 {
		t.Fatalf("master loot=%d", client.LootMethodMasterLoot)
	}
	if client.LootMethodGroupLoot != 3 {
		t.Fatalf("group loot=%d", client.LootMethodGroupLoot)
	}
	if client.LootMethodNeedBeforeGreed != 4 {
		t.Fatalf("nbg=%d", client.LootMethodNeedBeforeGreed)
	}
}

func TestDistance3DBasics(t *testing.T) {
	if d := Distance3D(0, 0, 0, 3, 4, 0); d < 4.99 || d > 5.01 {
		t.Fatalf("3-4-5 distance got %v", d)
	}
	if d := Distance3D(1, 1, 1, 1, 1, 1); d != 0 {
		t.Fatalf("same point dist=%v", d)
	}
}

func TestPadStormwindOutskirtsMap(t *testing.T) {
	if PadStormwindOutskirts.Map != MapEasternKingdoms {
		t.Fatalf("pad map=%d", PadStormwindOutskirts.Map)
	}
	if DefaultNearPadDist <= 0 {
		t.Fatalf("DefaultNearPadDist=%v", DefaultNearPadDist)
	}
}
