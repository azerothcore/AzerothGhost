# LLM guide: AzerothGhost e2eharness (import from your project)

Token-efficient rules for generating live-stack tests that **import** the harness.
Full recipes: [`EXAMPLES.md`](./EXAMPLES.md).

**Audience:** another Go module’s tests (or an LLM writing them). Not “edit AzerothGhost/e2e”.

**Target:** AzerothCore 3.3.5a (auth + world + MySQL). Optional gateway (e.g. ToCloud9) in front — set `E2E_AUTH_ADDR` to the client entrypoint.

---

## Dependency

```text
import "github.com/walkline/AzerothGhost/e2e/e2eharness"
_ "github.com/go-sql-driver/mysql"
```

- Harness has **no** build tag (always importable).
- Optional in *your* tests: `//go:build e2e` so offline `go test` skips live cases.
- Package name: **yours** (`myservice_test`, etc.) — not required to use AzerothGhost paths.
- Runnable patterns in this repo: `e2e/examples/` (`package examples_test`).
- Private local suite: `e2e/local/` (gitignored).
- Accounts: harness-created, password `test`, GM level 3.

Env (point at **your** AC):

| Var | Default |
|-----|---------|
| `E2E_AUTH_ADDR` | `127.0.0.1:3724` |
| `E2E_AUTH_DSN` | `…/acore_auth` |
| `E2E_CHAR_DSN` | `…/acore_characters` |
| `E2E_WORLD_DSN` | `…/acore_world` |

```bash
go test -tags=e2e ./... -count=1 -v -timeout 30m
```

---

## Always-use APIs

**Fixture:** `NewSolo` · `NewScenario` + `ByRole` · opts `Prefix,Race,Class,Level,LearnAllClass,CombatReady,CombatReadyFull,StartPad,SkipGM`

**Place:** `Teleport` · `TeleportPad` · `TeleNamed` · `GoCreatureID` · `GoCreatureGUID` · `WaitUnit` · `WaitUnitAny` · `FindUnit` · `Pos` · `PadStormwindOutskirts` · maps `MapEasternKingdoms|Outland|Northrend|Ulduar`

**Combat:** `CombatReady` / `CombatReadyFull` · `Engage` · `Damage` · `DamageKill` (never toggle `.gm on`) · `Attack` · `UnitInCombat` · `WaitUnitCombat` · `WaitUnitDead` · `UnitHP`

**Cast:** `Cast` · `CastMust` · `TryCast` · `CastOrGM` · `CastRetries` · `CastAtPosition` · `CastSelfGM` · `Learn` · `LearnAll` · `Face` · `SpellFailReasonName` · `DefaultCastTimeout`

**Aura:** self `ApplyAura` · `HasAura` · `AssertAuraRemains` · `AssertAuraConsumed` · unit `WaitUnitAura` · `UnitHasAura` · package `AssertUnitAuraStable(t, bot.World, …)`

**Quest/death/items:** `AddQuest` · `Save` · `QuestStatus` · `QuestStatusAfterSave` · `AssertQuestStatus` · `DieAndRepop` · `AddItem` · `EquipEntry` · `SetSkill`

**Observe:** `Spawn` · `UnitsByEntry` · `WaitNewUnits` · package `NewSpawnSetTracker` · `LivingByEntries` · `AssertIntervalNotAccelerated` · `ObserveUnitTargets`

**Misc:** `TeleportAll` · `EnableHostilePvP` · `Relog` · `GM` · `ProbeWorldAlive` / `AssertWorldAlive`

**Assert severity:** `Preconditionf` · `ConfirmedBugf(t, issue, …)` · `HarnessFailf` · `SoftWarnf`

---

## Never-do

- Do **not** write tests only inside AzerothGhost unless contributing to that repo
- Do **not** `.gm on` mid-fight for `.damage` → evade; use `Damage`/`DamageKill`
- Do **not** pull with GM mode still on → `CombatReady` first
- Do **not** bare `.tele` when melee matters → `GoCreatureID`
- Do **not** fixed long sleeps instead of waiters
- Do **not** invent harness APIs; use package helpers with `bot.World` if no method
- Do **not** `.npc add` when bug is spell-summon path
- Do **not** re-arm waiters during Wait
- Do **not** assume ToCloud9 is required — AC world/auth is enough if `E2E_AUTH_ADDR` reaches clients’ auth/realm entry

---

## Severity

| Situation | Helper |
|-----------|--------|
| Setup blocked evaluation | `Preconditionf` |
| Wrong core behaviour for tracked issue | `ConfirmedBugf(t, N, …)` |
| Infra/timeout/SQL/cache | `HarnessFailf` |
| Soft note | `SoftWarnf` |

---

## Golden template

```go
//go:build e2e

package myservice_test

import (
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/walkline/AzerothGhost/e2e/e2eharness"
)

// Issue: https://github.com/azerothcore/azerothcore-wotlk/issues/NNNNN  (optional)
func TestMyFeature_ShortName(t *testing.T) {
	t.Parallel()
	bot := e2eharness.NewSolo(t, e2eharness.ScenarioOpts{
		Prefix:        "Short",
		Race:          e2eharness.RaceHuman,
		Class:         e2eharness.ClassWarrior,
		Level:         80,
		LearnAllClass: true,
	})
	bot.TeleportPad(t, e2eharness.PadStormwindOutskirts)
	// GM mode on by default for setup. bot.CombatReady(t) before pulls.
	// Drive: Cast/Engage/ApplyAura/DieAndRepop/Relog.
	// Setup fail → Preconditionf; wrong core → ConfirmedBugf(t, N, …).
	t.Logf("PASS …")
}
```

Multi-bot: `NewScenario` + `BotSpec{Role}` + `ByRole` + `EnableHostilePvP` if cross-faction.

---

## Checklist

1. `go get` / replace AzerothGhost; import `e2e/e2eharness` + mysql driver  
2. `E2E_*` → your AzerothCore (or gateway)  
3. `NewSolo` / `NewScenario`  
4. Place → setup → `CombatReady` if fight → drive → assert  
5. Severity-tagged fatals for core bugs  
6. Protocol/cache first; DB after `Save`  
7. Guild charter/bank: Session helpers (`LoginAllianceBots`, bank waiters)  

---

## ScenarioBot vs Session

| ScenarioBot | Session / guild helpers |
|-------------|-------------------------|
| Combat, quest, aura, death, relog | Charter, bank, petition waiters |
| Default for new feature tests | When testing guild protocol only |

---

## Handy constants (harness)

`SpellCharge`, `SpellBlendingInAura`, `SpellGroundingTotem`, `SpellGroundingTotemEffect`,
`SpellRainOfFire`, `SpellRaiseDead`, `SpellMountSwiftGryphon`, `QuestRethbanGauntlet`,
`CreatureKologarn`, `CreatureGroundingTotem`, `CreatureTargetDummy`,
`RaceHuman`/`RaceOrc`/`RaceTauren`, `ClassWarrior`/`ClassRogue`/`ClassShaman`/`ClassWarlock`/`ClassDeathKnight`.
