//go:build e2e

package examples_test

import (
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/azerothcore/AzerothGhost/e2e/e2eharness"
)

// Example: player aura must survive mounting.
// Pattern from AC #26130 (Blending In cloak aura + flying mount).
//
//	go test -tags=e2e ./e2e/examples -run TestExample_AuraSurvivesMount -count=1 -v -timeout 10m
func TestExample_AuraSurvivesMount(t *testing.T) {
	t.Parallel()
	bot := e2eharness.NewSolo(t, e2eharness.ScenarioOpts{
		Prefix: "ExAura",
		Race:   e2eharness.RaceOrc,
		Class:  e2eharness.ClassWarrior,
		Level:  78,
	})

	// Northrend pad near the Blending In quest area (any safe outdoor works for mount).
	bot.Teleport(t, 3758.2554, 3689.5754, 47.241505, e2eharness.MapNorthrend)

	bot.ApplyAura(t, e2eharness.SpellBlendingInAura)
	bot.Learn(t, e2eharness.SpellMountSwiftGryphon)
	_ = bot.CastOrGM(t, e2eharness.SpellMountSwiftGryphon, 0, 10*time.Second)

	// issue=26130 → ConfirmedBugf if aura is stripped; use 0 + Preconditionf path via helper
	// only when tracking that issue (AssertAuraRemains takes issue id).
	bot.AssertAuraRemains(t, e2eharness.SpellBlendingInAura, 800*time.Millisecond, 26130)
	t.Logf("PASS aura %d survived mount", e2eharness.SpellBlendingInAura)
}
