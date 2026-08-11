# AzerothGhost live-stack e2e

Protocol-level bots and waiters for **AzerothCore** 3.3.5a (auth + world + MySQL).
The client is pure WotLK 3.3.5a; the world may be standalone AC or AC behind a
gateway such as ToCloud9.

## Goal

Make covering **any** WotLK 3.3.5a scenario easy from **any Go module**: import
`e2e/e2eharness`, login bots of any race/class, GM-setup the world, drive
protocol (cast, attack, quest, aura, death, mount, guild…), assert on packets /
object cache / DB.

Downstream projects should **import the harness** and write tests in *their*
module. This repo publishes **example** tests under `e2e/examples/`. Private
live regressions live under `e2e/local/` (gitignored).

## Authoring docs (external consumers)

| Doc | Audience |
|-----|----------|
| **[EXAMPLES.md](./EXAMPLES.md)** | Import harness into your project: setup, APIs, skeletons, footguns |
| **[LLM_GUIDE.md](./LLM_GUIDE.md)** | Compact rules for LLM context when generating consumer tests |
| **[examples/](./examples/)** | Runnable example tests (copy patterns into your module) |

```go
import "github.com/walkline/AzerothGhost/e2e/e2eharness"
```

Prefer `ScenarioBot` methods over raw `Session` / package helpers for combat and
quest scenarios.

## Prerequisites (AzerothCore)

- AzerothCore authserver / realm list reachable (default `127.0.0.1:3724`)
- MySQL with `acore_auth` + `acore_characters` (+ `acore_world` when needed)
- Worldserver accepting client connections (direct AC **or** via a gateway)
- Accounts are created by the harness (GM level 3) with password `test`

## How to run

Unit helpers (no stack):

```bash
go test ./client ./e2e/e2eharness -count=1
# or
make test-client
```

Published example suite (live stack):

```bash
go test -tags=e2e ./e2e/examples -count=1 -v -timeout 30m -parallel 2
# or
make test-e2e
# or
make test-e2e-examples
```

Private local regressions (gitignored `e2e/local/`):

```bash
go test -tags=e2e ./e2e/local -count=1 -v -timeout 30m -parallel 2
# or
make test-e2e-local

go test -tags=e2e ./e2e/local -run TestAC_27095 -count=1 -v -timeout 15m
go test -tags=e2e ./e2e/local -run 'TestGuild' -count=1 -v -timeout 20m
```

## Env overrides

| Variable        | Default                                                        |
|-----------------|----------------------------------------------------------------|
| `E2E_AUTH_ADDR` | `127.0.0.1:3724`                                               |
| `E2E_AUTH_DSN`  | `acore:acore@tcp(127.0.0.1:3306)/acore_auth`                   |
| `E2E_CHAR_DSN`  | `acore:acore@tcp(127.0.0.1:3306)/acore_characters`             |
| `E2E_WORLD_DSN` | `acore:acore@tcp(127.0.0.1:3306)/acore_world`                  |

## Layout

| Path | Role |
|------|------|
| `e2e/e2eharness` | **Library** — import from other projects |
| `e2e/EXAMPLES.md` / `LLM_GUIDE.md` | Consumer authoring docs |
| `e2e/examples/` | **Tracked** runnable example tests |
| `e2e/local/` | **Gitignored** private live AC suite |

### Scenario API (preferred for new tests)

Prefer **methods on `ScenarioBot`** so you never choose `bot.World` vs `bot.Session`:

```go
bot := e2eharness.NewSolo(t, e2eharness.ScenarioOpts{
    Prefix: "MyTest",
    Race:   e2eharness.RaceOrc,      // optional (default Human)
    Class:  e2eharness.ClassShaman,  // optional (default Warrior)
    Level:  80,
    LearnAllClass: true,
    // CombatReady: true,  // after setup: .gm off + god (for pulls)
    // StartPad: &e2eharness.PadStormwindOutskirts,
})
bot.Teleport(t, x, y, z, mapID)
bot.TeleNamed(t, "Freya")           // .tele + wait transfer
bot.GoCreatureID(t, 32906)          // .go creature id (melee range after tele)
bot.CombatReady(t)                  // .gm off + god — never .gm on mid-fight
bot.Engage(t, bossGUID, 15*time.Second)
bot.DamageKill(t, addGUIDs, 10_000_000, 10*time.Second) // no GM mode toggle
bot.AddQuest(t, questID)
bot.ApplyAura(t, spellID)
bot.CastMust(t, spellID, targetGUID, 10*time.Second)
bot.AssertQuestStatus(t, questID, e2eharness.QuestStatusFailed)
```

**Heterogeneous multi-bot** (different race/class per role):

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
```

### New-scenario checklist

1. **Fixture** — `NewSolo` / `NewScenario` (+ `BotSpec` when roles differ)
2. **Place** — `Teleport` / `TeleNamed` / `GoCreatureID` / `TeleportPad`
3. **Combat prep** — `CombatReady` / `CombatReadyFull` (not raw `.gm off` + cheats)
4. **Drive** — `Engage`, `Damage`/`DamageKill`, `Cast` / `CastOrGM` / `CastAtPosition`
5. **Assert** — protocol first; use `Preconditionf` / `ConfirmedBugf` / `HarnessFailf`

### Patterns

| Intent | API |
|--------|-----|
| Unit cast | `bot.Cast` / `bot.CastMust` / `bot.CastOrGM` |
| Ground AoE | `bot.CastAtPosition` |
| Spawn + GUID | `bot.Spawn(t, entry, timeout)` / `bot.WaitUnit` / `bot.WaitUnitAny` |
| Boss pull | `bot.CombatReady` → `bot.Engage` |
| GM damage kill | `bot.Damage` / `bot.DamageKill` (**never** toggles `.gm on`) |
| Named tele | `bot.TeleNamed` / `bot.GoCreatureID` |
| Death | `bot.DieAndRepop` or `Die` → `WaitDead` → `ReleaseSpirit` |
| Self aura | `bot.ApplyAura` / `AssertAuraRemains` / `AssertAuraConsumed` |
| Unit aura | `WaitUnitAura` / `AssertUnitAuraStable` |
| Waves / new units | `SpawnSetTracker` / `WaitNewUnits` / `UnitsByEntry` |
| Relog | `bot.Relog` |
| Quest status | `bot.QuestStatusAfterSave` / `bot.AssertQuestStatus` |
| AC issue assert | `Preconditionf` / `ConfirmedBugf(issue, …)` / `HarnessFailf` |
| Fail reason text | `SpellFailReasonName(code)` |

Package-level helpers still exist (`TeleportXYZ`, `CastAndWait`, …) for guild tests and low-level use.
`WaitNearbyUnitEntry` is a deprecated alias of `WaitNearbyUnitByEntry` — prefer `bot.WaitUnit`.

### Escape hatches (when to leave ScenarioBot methods)

| Stay on `bot.*` | Drop to lower layer |
|-----------------|---------------------|
| Teleport, cast, learn, die, aura, spawn, quest, combat | Custom opcodes not wrapped yet |
| Multi-bot roles via `BotSpec` | Guild charter/bank waiters (`ArmAllWaiters`, petition helpers) |
| `bot.GM(".…")` for rare commands | Offline account + raw SQL (e.g. multi-realm `account_access`) |
| | Direct `bot.World` for object-cache dumps / debug |

Guild charter/bank remain on the `Session` + package-helper style (shared bank fixture). New combat/quest scenarios should use `ScenarioBot`.

### Footguns

| Mistake | What happens | Do this instead |
|---------|--------------|-----------------|
| Leave `.gm on` during a pull | NPCs may not aggro | `CombatReady` (gm off + god) |
| `.gm on` mid-fight to use `.damage` | Boss evade/reset | Account GM still runs `.damage` without mode |
| Bare `.tele Boss` only | Often short of melee | `TeleNamed` + `GoCreatureID` |
| Sleep instead of waiters | Flakes | `WaitUnit`, teleport waiters, cast waiters |
| `t.Fatalf("CONFIRMED BUG…")` ad-hoc | Hard to filter in CI | `ConfirmedBugf` / `Preconditionf` |

### Waiter contract

Packet waiters use **Arm → Send → Wait**. Re-arming recreates channels; never
Arm while a Wait is outstanding or the packet can be lost. Full channels log a
WARNING drop line under the bot account.

Login and teleports prefer protocol signals over fixed sleeps:

- world auth → `WaitForSessionPhase(PhaseAuthed)`
- enter world → `WaitForLogin`
- `.go …` → `WaitForTeleportAfter`
- multi-bot login is **parallel** (cap `MaxParallelLogins`, default 5)

### GM commands & race language

GM lines are sent as `CHAT_MSG_SAY` in the character’s **native language**
(Common for Alliance, Orcish for Horde). `LANG_UNIVERSAL` is rejected by AC as
a cheat; Horde cannot speak Common, so wrong language silently drops all
`.gm` / `.go` / `.learn` commands.

## AC issue scenarios (`TestAC_*`)

Each test asserts **blizzlike / fixed** behaviour from an AzerothCore PR.
On an unfixed core the test fails with `CONFIRMED BUG` — that failure documents
the issue. On a fixed core it is a regression guard.

| Test | AC issue / PR | What it asserts |
|------|---------------|-----------------|
| `TestAC_26549_*` | [#26549](https://github.com/azerothcore/azerothcore-wotlk/issues/26549) / [#26989](https://github.com/azerothcore/azerothcore-wotlk/pull/26989) | Quest 1699 (STAY_ALIVE) fails on death |
| `TestAC_27088_*` | [#27088](https://github.com/azerothcore/azerothcore-wotlk/pull/27088) | `.account set gmlevel` only touches the requested realm |
| `TestAC_26130_*` | [#26130](https://github.com/azerothcore/azerothcore-wotlk/issues/26130) / [#27021](https://github.com/azerothcore/azerothcore-wotlk/pull/27021) | Blending In aura survives mount |
| `TestAC_26774_*` | [#26774](https://github.com/azerothcore/azerothcore-wotlk/issues/26774) / [#26792](https://github.com/azerothcore/azerothcore-wotlk/pull/26792) | Engineering dummy *spell summons* use item rank levels |
| `TestAC_26266_*` | [#26266](https://github.com/azerothcore/azerothcore-wotlk/issues/26266) / [#26758](https://github.com/azerothcore/azerothcore-wotlk/pull/26758) | Charge on Kologarn stays on bridge |
| `TestAC_27061_*` | [#27061](https://github.com/azerothcore/azerothcore-wotlk/pull/27061) | Raise Dead near player corpse does not crash |
| `TestAC_26584_*` | [#26584](https://github.com/azerothcore/azerothcore-wotlk/issues/26584) / [#26809](https://github.com/azerothcore/azerothcore-wotlk/pull/26809) | Grounding Totem Effect not consumed by hostile AoE |

Tests assert **fixed/blizzlike** behaviour. On a buggy core they fail with
`CONFIRMED BUG` (that is the point). They only go green after the matching PR
is present on the worldserver under test — or if that bug never existed on the
revision (e.g. crash sites introduced later).

### More issues (repro-steps first) — `ac_issues_repro_e2e_test.go`

Chosen for **clear steps to reproduce**, not harness convenience:

| Test | Issue | Steps (from issue) |
|------|-------|--------------------|
| `TestAC_25793_*` | [#25793](https://github.com/azerothcore/azerothcore-wotlk/issues/25793) | `.gm vis off` → relog → flag sticks |
| `TestAC_25985_*` | [#25985](https://github.com/azerothcore/azerothcore-wotlk/issues/25985) | Horde, `.quest add 12448`, `.go c i 27749`, orcs must damage Scourge |
| `TestAC_27099_*` | [#27099](https://github.com/azerothcore/azerothcore-wotlk/issues/27099) | Remorseless aura → Mutilate → aura consumed |

```bash
go test -tags=e2e ./e2e -run 'TestAC_25793|TestAC_25985|TestAC_27099' -count=1 -v -timeout 20m
```

## Notes

- Prefer protocol (`SMSG_SPELL_GO`, `SMSG_AURA_UPDATE`, `SMSG_ITEM_PUSH_RESULT`) over
  fixed sleeps. DB is for quest status after `.save`, or identity after item push.
- Charter tests call `t.Parallel()`; bank tests share one leader session.
- Do not run multiple e2e packages against one realm without isolation.
