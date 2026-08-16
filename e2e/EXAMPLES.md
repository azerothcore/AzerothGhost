# Using AzerothGhost e2e harness from another project

Import **`github.com/azerothcore/AzerothGhost/e2e/e2eharness`** to drive protocol-level
bots against a live **AzerothCore** 3.3.5a stack (auth + world + MySQL). The
client speaks pure WotLK 3.3.5a; the world may be standalone AzerothCore or
AzerothCore fronted by a gateway such as **ToCloud9**.

This guide is for **downstream authors and LLMs**: your tests live in *your*
module. You do not need to add files under the AzerothGhost repo.

Companion: [`LLM_GUIDE.md`](./LLM_GUIDE.md) (compact rules for LLM context).

---

## What you import

| Import path | Role |
|-------------|------|
| `github.com/azerothcore/AzerothGhost/e2e/e2eharness` | Login bots, ScenarioBot, waiters, combat/quest/guild helpers |
| `github.com/go-sql-driver/mysql` | Blank-import in tests that open MySQL via the harness |

Harness package has **no** `//go:build e2e` tag — it is always importable.
Use a build tag only in *your* tests if you want to skip live tests by default.

```bash
# In your module
go get github.com/azerothcore/AzerothGhost@latest
# Or pin a commit / use a replace for a local checkout:
# replace github.com/azerothcore/AzerothGhost => ../AzerothGhost
```

```go
import (
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/azerothcore/AzerothGhost/e2e/e2eharness"
)
```

Prefer **methods on `e2eharness.ScenarioBot`** so callers never choose
`bot.World` vs `bot.Session` by hand.

---

## Target stack (AzerothCore first)

**Required**

- AzerothCore **3.3.5a** authserver + worldserver (or equivalent protocol endpoint)
- MySQL with `acore_auth` + `acore_characters` (and `acore_world` when you need spawn coords / tele names from DB tooling)

**Optional**

- A game gateway (e.g. ToCloud9) in front of world: set `E2E_AUTH_ADDR` / realm
  address to whatever clients use to reach the realm list and world entrypoint

The harness defaults assume a local AC-style layout:

| Variable | Default | Meaning |
|----------|---------|---------|
| `E2E_AUTH_ADDR` | `127.0.0.1:3724` | Auth / realm list address the bot connects to |
| `E2E_AUTH_DSN` | `acore:acore@tcp(127.0.0.1:3306)/acore_auth` | Auth DB |
| `E2E_CHAR_DSN` | `acore:acore@tcp(127.0.0.1:3306)/acore_characters` | Characters DB |
| `E2E_WORLD_DSN` | `acore:acore@tcp(127.0.0.1:3306)/acore_world` | World DB (optional for many tests) |

Override these to point at **your** AC (or gateway) environment. Accounts are
created by the harness at GM level 3 with password `test` (`DefaultPassword`).

---

## Minimal test in your project

```go
// Optional: gate live tests so normal `go test ./...` stays offline-friendly.
//go:build e2e

package myservice_test

import (
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/azerothcore/AzerothGhost/e2e/e2eharness"
)

func TestMyFeature_SomethingBlizzlike(t *testing.T) {
	t.Parallel()
	bot := e2eharness.NewSolo(t, e2eharness.ScenarioOpts{
		Prefix:        "MyFeat", // short unique account prefix
		Race:          e2eharness.RaceHuman,
		Class:         e2eharness.ClassWarrior,
		Level:         80,
		LearnAllClass: true,
	})

	bot.TeleportPad(t, e2eharness.PadStormwindOutskirts)
	// Setup with GM mode (default on) → CombatReady before real pulls.
	// Drive protocol → assert packets / object cache / DB after Save.
	t.Logf("PASS MyFeature …")
}
```

Run against your stack:

```bash
export E2E_AUTH_ADDR=127.0.0.1:3724   # or your gateway/auth host:port
go test -tags=e2e ./... -count=1 -v -timeout 30m
```

Package name, directory layout, and test names are **yours**. Harness only
needs a `*testing.T` and a reachable stack.

### `ScenarioOpts` knobs

| Field | Meaning |
|-------|---------|
| `Prefix` | Unique account/name seed |
| `Race` / `Class` / `Level` / `LearnAllClass` | Homogeneous defaults |
| `Bots` | Heterogeneous multi-bot (`BotSpec` per role) |
| `Count` | Homogeneous bot count when `Bots` empty (default 1) |
| `SkipGM` | Leave GM **mode** off at login (default enables `.gm on` for setup) |
| `CombatReady` | After login: `.gm off` + god |
| `CombatReadyFull` | After login: `.gm off` + god + power |
| `StartPad` | Teleport every bot to a `Position3` after setup |

Default login enables **GM mode** so `.go` / `.learn` / `.quest` work. Call
`bot.CombatReady(t)` before real combat.

---

## Multi-bot

```go
bots := e2eharness.NewScenario(t, e2eharness.ScenarioOpts{
	Prefix: "PvP",
	Bots: []e2eharness.BotSpec{
		{Role: "shaman", Race: e2eharness.RaceTauren, Class: e2eharness.ClassShaman, Level: 80, LearnAllClass: true},
		{Role: "warlock", Race: e2eharness.RaceHuman, Class: e2eharness.ClassWarlock, Level: 80, LearnAllClass: true},
	},
})
shaman := e2eharness.ByRole(t, bots, "shaman")
warlock := e2eharness.ByRole(t, bots, "warlock")

pad := e2eharness.PadStormwindOutskirts
e2eharness.TeleportAll(t, bots, pad.X, pad.Y, pad.Z, pad.Map)
e2eharness.EnableHostilePvP(t, shaman, warlock) // .pvp on + .gm off both
```

Login is parallel (cap `MaxParallelLogins`). Sessions/DBs use `t.Cleanup`.

Homogeneous multi-bot:

```go
bots := e2eharness.NewScenario(t, e2eharness.ScenarioOpts{
	Prefix: "Squad",
	Count:  3,
	Race:   e2eharness.RaceOrc,
	Class:  e2eharness.ClassWarrior,
	Level:  80,
})
```

---

## Combat lifecycle

1. Login/setup (GM mode on by default)
2. Place: `TeleNamed` / `GoCreatureID` / `Teleport` / `TeleportPad`
3. `bot.CombatReady(t)` or `CombatReadyFull` — **GM mode off** + god (+ power)
4. `bot.Engage(t, guid, timeout)` — face + swing; may nudge with `.damage 1`
5. `bot.Damage` / `bot.DamageKill` — `.damage` **without** enabling GM mode

```go
bot.TeleNamed(t, "Freya")
bot.GoCreatureID(t, 32906) // named tele is often short of melee
bot.CombatReady(t)

boss := bot.WaitUnitAny(t, 30*time.Second, 32906, 33360)
bot.Engage(t, boss, 15*time.Second)
bot.DamageKill(t, addGUIDs, 10_000_000, 10*time.Second)
```

### Footgun: GM mode mid-fight

| Do | Do not |
|----|--------|
| `CombatReady` before pull | Leave `.gm on` during pull (weak/no aggro) |
| `Damage` / `DamageKill` with mode off | `.gm on` mid-fight only to use `.damage` (boss evade/reset) |
| Account GM still allows `.damage` / `.go` with mode **off** | Hand-roll `.gm off` + cheats instead of `CombatReady` |

Crash probe after a risky cast:

```go
e2eharness.ProbeWorldAlive(t, probeBot, 27061) // issue# → ConfirmedBugf if dead
bot.AssertWorldAlive(t)
```

---

## Teleport, creatures, pads

| API | What it does |
|-----|----------------|
| `bot.Teleport(t, x, y, z, mapID)` | `.go xyz` + wait teleport |
| `bot.TeleportPad(t, pad)` | Same for `Position3` |
| `bot.TeleNamed(t, "Freya")` | `.tele <name>` + wait transfer |
| `bot.GoCreatureID(t, entry)` | `.go creature id N` |
| `bot.GoCreatureGUID(t, spawn)` | `.go creature N` |
| `bot.WaitUnit` / `WaitUnitAny` / `FindUnit` | Object-cache unit lookup |
| `bot.Pos()` | Current position |

```go
bot.TeleportPad(t, e2eharness.PadStormwindOutskirts)
```

Maps: `MapEasternKingdoms` (0), `MapOutland` (530), `MapNorthrend` (571),
`MapUlduar` (603).

Near-teleport **clears** the client object cache — re-`WaitUnit` after tele.

---

## Cast APIs

| Method | Behavior |
|--------|----------|
| `Cast` | Client cast → `SpellCastResult` |
| `CastMust` | Fatals unless `SMSG_SPELL_GO` |
| `TryCast` | Timeout returns error (hang detection) |
| `CastOrGM` | Client path; on fail → `.cast` / `.cast self` |
| `CastRetries` | Up to *n* client attempts |
| `CastAtPosition` | Ground-targeted |
| `CastSelfGM` | Force `.cast self N` |
| `Learn` / `LearnAll` / `Face` | Setup helpers |

`e2eharness.DefaultCastTimeout` is 10s when timeout ≤ 0 for some helpers.

Waiters are **Arm → Send → Wait**. Do not re-arm while a Wait is outstanding.

```go
res := bot.Cast(t, e2eharness.SpellGroundingTotem, 0, 10*time.Second)
if !res.Success {
	e2eharness.Preconditionf(t, "cast failed reason=%d (%s)",
		res.FailReason, e2eharness.SpellFailReasonName(res.FailReason))
}
```

---

## Auras

| Method | Use |
|--------|-----|
| `ApplyAura` / `HasAura` | Self aura apply / snapshot |
| `AssertAuraRemains(t, spell, after, issue)` | Must **not** strip → `ConfirmedBugf` if gone |
| `AssertAuraConsumed(t, spell, timeout, issue)` | Must consume → `ConfirmedBugf` if present |
| `WaitUnitAura` / `UnitHasAura` | Other units |
| `AssertUnitAuraStable(t, bot.World, guid, spell, window, issue)` | Package-level; debounce flicker |

```go
bot.ApplyAura(t, e2eharness.SpellBlendingInAura)
_ = bot.CastOrGM(t, e2eharness.SpellMountSwiftGryphon, 0, 10*time.Second)
bot.AssertAuraRemains(t, e2eharness.SpellBlendingInAura, 800*time.Millisecond, 26130)
```

---

## Death, quests, items

```go
bot.AddQuest(t, e2eharness.QuestRethbanGauntlet)
st, ok := bot.QuestStatusAfterSave(t, e2eharness.QuestRethbanGauntlet)
// ...
bot.DieAndRepop(t)
bot.Save(t)
bot.AssertQuestStatus(t, questID, e2eharness.QuestStatusFailed)
```

Quest status constants: `QuestStatusIncomplete` (3), `QuestStatusFailed` (5),
`QuestStatusComplete` (1). DB asserts only after `.save`.

Items: `AddItem`, `AddItemWait`, `EquipEntry`, `SetSkill`, `GiveTotems`.

---

## Waves / object-cache observation

```go
tr := e2eharness.NewSpawnSetTracker([]uint32{32919, 33202, 32916, 33203, 32918}, 3*time.Second)
tr.KindOf = func(entry uint32) string { /* map entries → kind label */ return "Trio" }
sets := tr.WaitSets(t, bot.World, 2, 4*time.Minute)

known := tr.Known()
fresh := bot.WaitNewUnits(t, known, allyEntries, 90*time.Second)

e2eharness.AssertIntervalNotAccelerated(t, 27095, fromKill, fromBaseline, e2eharness.IntervalBugOpts{})
```

Also package-level: `UnitsByEntry`, `LivingByEntries`, `CountLivingWithRetry`,
`ObserveUnitTargets`, `AssertNoIdleTargeters`, `SampleUntil`.

---

## Relog

```go
bot.GM(t, ".gm visible off")
bot.Save(t)
bot.Relog(t) // same account/char; Session rebound in place
// query bot.CharDB for persisted flags
```

---

## Assert severity (AC-issue style)

| Helper | When |
|--------|------|
| `Preconditionf` | Setup never reached a state where the bug can be judged |
| `ConfirmedBugf(t, issue, …)` | Core behaviour is wrong for that AC issue/PR |
| `HarnessFailf` | Infra: timeout, SQL, empty cache, send error |
| `SoftWarnf` | Soft deviation; test may still pass |

Downstream projects can reuse this vocabulary for any core bug tracker, not only
AzerothCore issue numbers.

---

## Copy-paste skeletons

### a) Quest fails on death

```go
//go:build e2e

package myservice_test

import (
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/azerothcore/AzerothGhost/e2e/e2eharness"
)

func TestQuest_StayAliveFailsOnDeath(t *testing.T) {
	t.Parallel()
	bot := e2eharness.NewSolo(t, e2eharness.ScenarioOpts{
		Prefix: "QAlive", Class: e2eharness.ClassWarrior, Level: 30,
	})
	bot.AddQuest(t, e2eharness.QuestRethbanGauntlet)
	bot.Teleport(t, -9222.58, -2147.87, 63.814, e2eharness.MapEasternKingdoms)

	st, ok := bot.QuestStatusAfterSave(t, e2eharness.QuestRethbanGauntlet)
	if !ok || st != e2eharness.QuestStatusIncomplete {
		e2eharness.Preconditionf(t, "quest incomplete, got ok=%v st=%d", ok, st)
	}
	bot.DieAndRepop(t)
	bot.Save(t)
	st, ok = bot.QuestStatus(t, e2eharness.QuestRethbanGauntlet)
	if !ok || st != e2eharness.QuestStatusFailed {
		e2eharness.ConfirmedBugf(t, 26549, "quest status=%d after death, want FAILED", st)
	}
}
```

### b) Boss engage + kill adds

```go
func TestBoss_EngageAndKillAdds(t *testing.T) {
	bot := e2eharness.NewSolo(t, e2eharness.ScenarioOpts{Prefix: "Boss", Level: 80})
	bot.TeleNamed(t, "Freya")
	bot.GoCreatureID(t, 32906)
	bot.CombatReady(t)
	boss := bot.WaitUnitAny(t, 30*time.Second, 32906, 33360)
	bot.Engage(t, boss, 15*time.Second)
	// wait for adds via WaitUnit / SpawnSetTracker, then:
	// bot.DamageKill(t, guids, 10_000_000, 10*time.Second)
}
```

### c) Aura survives action

```go
func TestAura_SurvivesMount(t *testing.T) {
	t.Parallel()
	bot := e2eharness.NewSolo(t, e2eharness.ScenarioOpts{Prefix: "Aura", Level: 80})
	bot.Teleport(t, 3758.25, 3689.57, 47.24, e2eharness.MapNorthrend)
	bot.ApplyAura(t, e2eharness.SpellBlendingInAura)
	bot.Learn(t, e2eharness.SpellMountSwiftGryphon)
	_ = bot.CastOrGM(t, e2eharness.SpellMountSwiftGryphon, 0, 10*time.Second)
	bot.AssertAuraRemains(t, e2eharness.SpellBlendingInAura, 800*time.Millisecond, 26130)
}
```

### d) Multi-bot hostile cast

```go
func TestPvP_HostileAoE(t *testing.T) {
	t.Parallel()
	bots := e2eharness.NewScenario(t, e2eharness.ScenarioOpts{
		Prefix: "AoE",
		Bots: []e2eharness.BotSpec{
			{Role: "shaman", Race: e2eharness.RaceTauren, Class: e2eharness.ClassShaman, Level: 80, LearnAllClass: true},
			{Role: "warlock", Race: e2eharness.RaceHuman, Class: e2eharness.ClassWarlock, Level: 80, LearnAllClass: true},
		},
	})
	shaman, warlock := e2eharness.ByRole(t, bots, "shaman"), e2eharness.ByRole(t, bots, "warlock")
	shaman.Teleport(t, -13200, 220, 32, e2eharness.MapEasternKingdoms)
	warlock.Teleport(t, -13200, 225, 32, e2eharness.MapEasternKingdoms)
	shaman.CombatReadyFull(t)
	warlock.CombatReadyFull(t)
	e2eharness.EnableHostilePvP(t, shaman, warlock)
	// cast totems / AoE and assert auras on units
}
```

### e) Relog + DB flag

```go
func TestGM_VisibilitySurvivesRelog(t *testing.T) {
	t.Parallel()
	bot := e2eharness.NewSolo(t, e2eharness.ScenarioOpts{Prefix: "GmVis", Level: 10})
	bot.GM(t, ".gm visible off")
	bot.Save(t)
	guid := bot.GUID
	bot.Relog(t)
	time.Sleep(500 * time.Millisecond)
	var flags uint16
	err := bot.CharDB.QueryRow(`SELECT extra_flags FROM characters WHERE guid=?`, bot.GUID).Scan(&flags)
	if err != nil {
		_ = bot.CharDB.QueryRow(`SELECT extra_flags FROM characters WHERE guid=?`, guid).Scan(&flags)
	}
	// assert bit 0x10 (PLAYER_EXTRA_GM_INVISIBLE) as needed
}
```

---

## Guild / Session path (optional)

Charter and guild bank helpers are **Session**-centric (`LoginAllianceBots`,
`BuyGuildCharter`, `DepositItemToBank`, money waiters). Use them when testing
guild protocol; use **ScenarioBot** for combat/quest/aura/death/relog.

You can mix both in one module: ScenarioBot for character scenarios, Session
helpers for guild fixtures.

---

## Footguns

| Mistake | Effect | Prefer |
|---------|--------|--------|
| Leave `.gm on` on pull | Weak/no aggro | `CombatReady` |
| `.gm on` mid-fight for damage | Boss evade | `Damage` / `DamageKill` |
| Named tele only | Short of melee | `GoCreatureID` after tele |
| Long sleeps | Flakes | Waiters / object-cache polls |
| Assert before `.save` | Stale quest DB | `Save` / `QuestStatusAfterSave` |
| Wrong race language for GM | Silent command drop | Set `Race` on bot; harness uses racial language |
| Re-arm waiters during Wait | Lost packets | Arm → Send → Wait only |

---

## Anti-patterns

- Inventing wrappers that hide GM mode / map state without logs  
- Sleeping as the primary sync mechanism  
- Using `.npc add` when the bug is on a **spell summon** path  
- Calling package `CastAndWait(session)` when `bot.Cast` exists  
- Hardcoding another project’s internal paths (`e2e_test` under AzerothGhost)

---

## Checklist (downstream test)

1. Depend on `github.com/azerothcore/AzerothGhost` and import `e2e/e2eharness`  
2. Blank-import MySQL driver  
3. Point `E2E_*` env at **your** AzerothCore (or gateway)  
4. `NewSolo` / `NewScenario` — not ad-hoc low-level login for simple cases  
5. Place → setup → `CombatReady` if pull → drive → assert  
6. Severity-tagged fatals when tracking core bugs  
7. Protocol / object cache first; DB after `Save`  

---

## This repository’s layout

| Path | Role |
|------|------|
| `e2e/e2eharness` | Library to import |
| `e2e/examples/` | **Tracked** runnable example tests (copy patterns) |
| `e2e/local/` | **Gitignored** private live AC suite |

```bash
go test -tags=e2e ./e2e/examples -count=1 -v -timeout 30m
go test -tags=e2e ./e2e/local -run TestAC_27095 -count=1 -v -timeout 15m   # if present
```

See [`examples/README.md`](./examples/README.md) and [`README.md`](./README.md).
