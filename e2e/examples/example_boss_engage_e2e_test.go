//go:build e2e

package examples_test

import (
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/walkline/AzerothGhost/e2e/e2eharness"
)

// Example: tele to a named boss pad, enter melee, pull, observe Allies waves.
// Pattern from AC #27095 (Freya Allies of Nature spawn timing).
//
// This is a long raid-style scenario (several minutes). Not parallel.
//
//	go test -tags=e2e ./e2e/examples -run TestExample_BossEngageAndWaves -count=1 -v -timeout 15m
func TestExample_BossEngageAndWaves(t *testing.T) {
	bot := e2eharness.NewSolo(t, e2eharness.ScenarioOpts{
		Prefix: "ExBoss",
		Level:  80,
	})

	const (
		npcFreya10          = uint32(32906)
		npcFreya25          = uint32(33360)
		npcStormLasher      = uint32(32919)
		npcWaterSpirit      = uint32(33202)
		npcSnaplasher       = uint32(32916)
		npcConservator      = uint32(33203)
		npcDetonatingLasher = uint32(32918)
	)
	allyEntries := []uint32{
		npcStormLasher, npcWaterSpirit, npcSnaplasher,
		npcConservator, npcDetonatingLasher,
	}

	// Named tele often lands short of melee — go onto the creature after.
	bot.TeleNamed(t, "Freya")
	bot.GoCreatureID(t, npcFreya10)
	bot.CombatReady(t) // .gm off + god; never .gm on mid-fight

	boss := bot.WaitUnitAny(t, 30*time.Second, npcFreya10, npcFreya25)
	bot.Engage(t, boss, 15*time.Second)

	// Group multi-entry packs (Trio) under one kind label.
	tr := e2eharness.NewSpawnSetTracker(allyEntries, 3*time.Second)
	tr.KindOf = func(entry uint32) string {
		switch entry {
		case npcStormLasher, npcWaterSpirit, npcSnaplasher:
			return "Trio"
		case npcConservator:
			return "Conservator"
		case npcDetonatingLasher:
			return "Lashers"
		default:
			return "Unknown"
		}
	}
	sets := tr.WaitSets(t, bot.World, 2, 4*time.Minute)
	t.Logf("wave1=%s (%d) wave2=%s (%d) gap=%s",
		sets[0].Kind, len(sets[0].Guids),
		sets[1].Kind, len(sets[1].Guids),
		sets[1].SpawnT.Sub(sets[0].SpawnT).Round(time.Millisecond))

	// Prefer killing a non-Lasher older set (Lashers explode and can wipe the field).
	older, newer := sets[0], sets[1]
	if older.Kind == "Lashers" && len(sets) < 3 {
		sets = tr.WaitSets(t, bot.World, 3, 4*time.Minute)
		older, newer = sets[1], sets[2]
	}
	if older.Kind == "Lashers" {
		e2eharness.Preconditionf(t, "need a non-Lasher older set to damage-kill safely")
	}

	time.Sleep(2 * time.Second)
	var killGUIDs []uint64
	for _, g := range older.Guids {
		if hp, _ := bot.UnitHP(g); hp > 0 {
			killGUIDs = append(killGUIDs, g)
		}
	}
	if len(killGUIDs) == 0 {
		e2eharness.Preconditionf(t, "older set already dead")
	}

	// Account GM can .damage with mode off — do not toggle .gm on.
	bot.DamageKill(t, killGUIDs, 10_000_000, 10*time.Second)
	killT := time.Now()

	known := tr.Known()
	for _, s := range bot.UnitsByEntry(120, allyEntries...) {
		known[s.GUID] = struct{}{}
	}
	fresh := bot.WaitNewUnits(t, known, allyEntries, 90*time.Second)
	now := time.Now()
	fromKill := now.Sub(killT)
	fromNewer := now.Sub(newer.SpawnT)
	t.Logf("next wave after kill: +%d units Δkill=%s Δnewer=%s",
		len(fresh), fromKill.Round(time.Millisecond), fromNewer.Round(time.Millisecond))

	// Expect ~60s schedule, not ~5s acceleration from killing an *older* set.
	e2eharness.AssertIntervalNotAccelerated(t, 27095, fromKill, fromNewer, e2eharness.IntervalBugOpts{
		MaxFromEvent:    20 * time.Second,
		MaxFromBaseline: 45 * time.Second,
	})
	t.Logf("PASS boss engage + wave timer after killing older set")
}
