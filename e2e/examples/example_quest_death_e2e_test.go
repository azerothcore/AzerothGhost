//go:build e2e

package examples_test

import (
	"testing"

	_ "github.com/go-sql-driver/mysql"

	"github.com/azerothcore/AzerothGhost/e2e/e2eharness"
)

// Example: STAY_ALIVE quest fails when the player dies.
// Pattern from AC #26549 (Rethban Gauntlet / quest 1699).
//
//	go test -tags=e2e ./e2e/examples -run TestExample_QuestFailsOnDeath -count=1 -v -timeout 10m
func TestExample_QuestFailsOnDeath(t *testing.T) {
	t.Parallel()
	bot := e2eharness.NewSolo(t, e2eharness.ScenarioOpts{
		Prefix: "ExQst",
		Class:  e2eharness.ClassWarrior,
		Level:  30,
	})

	bot.AddQuest(t, e2eharness.QuestRethbanGauntlet)
	bot.Teleport(t, -9222.58, -2147.87, 63.814, e2eharness.MapEasternKingdoms)

	st, ok := bot.QuestStatusAfterSave(t, e2eharness.QuestRethbanGauntlet)
	if !ok || st != e2eharness.QuestStatusIncomplete {
		e2eharness.Preconditionf(t, "quest should be INCOMPLETE, got ok=%v status=%d (%s)",
			ok, st, e2eharness.QuestStatusName(st))
	}

	bot.DieAndRepop(t)
	bot.Save(t)

	st, ok = bot.QuestStatus(t, e2eharness.QuestRethbanGauntlet)
	if !ok {
		e2eharness.HarnessFailf(t, "quest row missing after death")
	}
	if st != e2eharness.QuestStatusFailed {
		// Use ConfirmedBugf when tracking an AC issue number; Preconditionf/HarnessFailf otherwise.
		e2eharness.ConfirmedBugf(t, 26549, "quest status=%d (%s) after death+repop, want FAILED(5)",
			st, e2eharness.QuestStatusName(st))
	}
	t.Logf("PASS quest failed on death (status=%s)", e2eharness.QuestStatusName(st))
}
