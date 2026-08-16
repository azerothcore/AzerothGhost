# LLM guide: AzerothGhost e2eharness (import from your project)

Token-efficient rules for generating live-stack tests that **import** the harness.
Full recipes: [`EXAMPLES.md`](./EXAMPLES.md).

**Audience:** another Go module’s tests (or an LLM writing them). Not “edit AzerothGhost/e2e”.

**Target:** AzerothCore 3.3.5a (auth + world + MySQL). Optional gateway (e.g. ToCloud9) in front — set `E2E_AUTH_ADDR` to the client entrypoint.

---

## Dependency

```text
import "github.com/azerothcore/AzerothGhost/e2e/e2eharness"
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
# packages parallel; keep tests serial within package (pad isolation)
go test -tags=e2e ./... -count=1 -v -timeout 30m -parallel 1
```

Optional: `E2E_ALLOW_SOFT_PASS=1` only for local SoftPass debug (fail-closed by default).

### WorldClient log verbosity (`E2E_WORLD_LOG`)

Harness defaults to **Info**: session phases, auth/login, short action GM (`.go`, `.npc`, `.summon`, `.learn`), trade/summon/vehicle protocol, one learn summary after `.learn all`, self `SPELL_FAILURE` with reason name.

| Value | Use |
|-------|-----|
| *(unset)* / `info` | Default e2e |
| `debug` | `SMSG_CAST_FAILED` (+ reason name), character enum, selection, swings, damage, prep GM (`.gm on/off`, `.combatstop`), repeat INITIAL_SPELLS |
| `trace` | Per-id `SMSG_LEARNED_SPELL` + opcode hex if `TraceLogOpcodes` |
| `warn` / `error` / `silent` | Quieter CI |

```bash
# Combat / engage flake triage
E2E_WORLD_LOG=debug go test -tags=e2e -v ./suites/combat/threat/ -run Engage
```

Army bots default WorldClient to **Warn** (see `bot/bot.go`); do not point load nodes at `E2E_WORLD_LOG=info`.

---

## Always-use APIs

**Fixture:** `NewSolo` · `NewScenario` + `ByRole` · opts `Prefix,Race,Class,Level,LearnAllClass,CombatReady,CombatReadyFull,StartPad,SkipGM`

**Place:** **`PackagePad(t)`** (sticky per suite folder) · `Teleport` · `TeleportPad` · `TeleportAll` / **`TeleportAllPad`** · `TeleNamed` · `GoCreatureID` · `GoCreatureGUID` · `WaitUnit` · `WaitUnitAny` · `FindUnit` · `Pos` · `DistFrom` · **`AssertNear`** / **`AssertNearPad`** / **`AssertMoved`** · `Distance3D` · maps `MapEasternKingdoms|Kalimdor|Outland|Northrend|Ulduar`  
Legacy: `PadStormwindOutskirts` (= AbandonHouse) — prefer `PackagePad`

**Combat:** `CombatReady` / `CombatReadyFull` (end with `FlushWorld`) · `Engage` (no FlushWorld — bosses evade) · `Damage` · `DamageKill` (never toggle `.gm on`) · `Attack` · `UnitInCombat` · `WaitUnitCombat` · `WaitUnitDead` · `UnitHP` · `UnitTarget` · `WaitUnitTarget` · `AssertUnitTarget`

**Cast:** `Cast` · `CastMust` · `TryCast` · `CastOrGM` · `CastRetries` · `CastAtPosition` · `CastSelfGM` · `Learn` · `LearnAll` · `Face` · `SpellFailReasonName` · `DefaultCastTimeout` · `PlayerPower` · `IsChanneling` / `ChannelSpell` · `WaitChanneling` / `WaitNotChanneling` · `CancelCast` · `CancelAura`

**Aura:** self `ApplyAura` · `HasAura` · `AuraStacks` · `AssertAuraRemains` · `AssertAuraConsumed` · unit `WaitUnitAura` · `UnitHasAura` · `UnitAuraStacks` · package `AssertUnitAuraStable(t, bot.World, …)`

**Quest/death/items/money:** `AddQuest` · `Save` · `QuestStatus` · `QuestStatusAfterSave` · `AssertQuestStatus` · `DieAndRepop` · `Die` · `WaitDead` · `ReleaseSpirit` · `ReclaimCorpse` · `WaitAlive` · `AddItem` · `EquipEntry` · `SetSkill` · `PlayerMoney` · `WaitPlayerMoney` · `MoneyAfterSave` · `ModMoney` / `SetMoney` · **`AssertMoneyAtLeast`** / `AssertMoneyEqual` · `InventoryCount` · **`AssertInventoryAtLeast`** (CharDB after Save)

**Group (multi-bot):** `FormParty(t, leader, mates…)` / **`FormPartyAtPad(t, pad, leader, mates…)`** / `FormPartyWith` · `Invite` · `WaitGroupInvite` · `AcceptGroup` · `DeclineGroup` · `LeaveGroup` / `DisbandGroup` · `DisbandParty` · **`WaitGroupList`** · `WaitNotInGroup` · **`WaitIsGroupLeader`** · `InGroup` · `IsGroupLeader` · `GroupMembers` · `GroupState` · `SetLeader` · `Uninvite` · `SetLootMethod` (client `LootMethod*` constants). Prefer `WaitGroupList` / `WaitIsGroupLeader` over sleep loops after invite/accept/leader transfer.

**Trade (multi-bot):** `OpenTrade(t, initiator, target)` · `InitiateTrade` · `AcceptTradeWindow` · **`WaitTradeOpen`** · `SetTradeItem(slot,bag,invSlot)` · `SetTradeGold` · `AcceptTrade` · `CancelTrade` · `CompleteTrade` · `WaitTradeComplete` · `WaitTradeCancelled` · `WaitTradeStatus` · `TradeOpen` (cache). `OpenTrade`/`CompleteTrade` arm packet waiters — do not add fixed sleeps around them.

**Loot / rolls:** after kill → **`WaitUnitLootable`** then `OpenLoot` · `LootRelease` · `LootTakeItem` · `WaitLootStartRoll` · `RollNeed`/`RollGreed`/`RollPass` · `WaitLootRollWon` · `WaitLootAllPassed` · `MasterLootGive`

**Pets:** `WaitPlayerPet` · `PlayerPetGUID` · `DismissPet` · `WaitNoPlayerPet` · `AssertNoPlayerPet` · `PetAttack`

**Observe:** `Spawn` · `UnitsByEntry` · `WaitNewUnits` · package `NewSpawnSetTracker` · `LivingByEntries` · `AssertIntervalNotAccelerated` · `ObserveUnitTargets`

**Session lifecycle:** **`WaitInWorld`** (PhaseInWorld after Relog / far tele) · `Relog` (graceful logout+reenter, waits InWorld) · **`HardDisconnect` / `CloseHard`** (socket close, **no** logout — probe with **another** bot via `ProbeWorldAlive` / `AssertWorldAlive`) · `GM` · **`FlushWorld`** (`.gps` → `SMSG_MESSAGECHAT`: same-session MustGM before this call is applied) · `TeleportAll` / `TeleportAllPad` · `EnableHostilePvP` (`CMSG_TOGGLE_PVP` + `WaitSelfPvP`) · **`WaitSelfPvP` / `WaitUnitPvP`**

**Vehicles:** `SpellClick` · **`EnterVehicle(t, guid)`** / **`ExitVehicle`** · **`IsOnVehicle`** / `VehicleGUID` / `WaitOnVehicle` / `WaitNotOnVehicle` · `EnterPlayerVehicle` · fixture **`CreatureStormwindSteed`** (33217) via `Spawn` (cleanup); avoids UNINTERACTIBLE mounts like Mechano-hog

**Instance summon / reset:** **`LeaderResetInstances`** · **3-role ritual:** `ArmSummonRequest` on far → **`RitualSummon(t, initiator, helper, far)`** (portal GO + helper click) → far **`AcceptSummon` / `DeclineSummon`** · `GameObjectUse` · `WaitGameObject` · meeting-stone portal GO **179944** (req 2: initiator+helper)

**Assert severity:** `Preconditionf` · `ConfirmedBugf(t, issue, …)` · `HarnessFailf` · `SoftWarnf` · **`Assertf`/`AssertBugf`** (post-drive product oracle). **`SoftPass` is disabled by default** (fails); only `E2E_ALLOW_SOFT_PASS=1` allows it — prefer Preconditionf. Grep failures: `E2E_FAIL` or `--- FAIL` (routine `SMSG_CAST_FAILED` is Debug-only under default `E2E_WORLD_LOG=info`).

**Hooks (race-safe multi-subscriber):** prefer `AddPacketHook` / `AddTradeStatusHook` / `AddLoot*Hook` / `AddGroup*Hook` / `AddSpellCastResultHook` with cancel; avoid assigning `On*` fields.

**R1 helpers:** `DieMust` · `TryOpenLoot` · `SpawnKillLootable` · `ArmLootStartRoll` · `ArmLootRollOutcome` (Arm → Roll* → Wait) · `ArmGroupInvite` (Arm → Invite → Wait) · `WaitNear` · `DamageToFraction` · `HardDisconnectAndProbe` · `WaitLootMethod` · `TryWaitChanneling` · `CancelCastWhenChanneling` · `meta.Begin` (AC: no Parallel if serial)

**Group loot fixtures:** use `CreatureGroupLootFixture` (15209) + `LootThresholdUncommon` (2). AC rejects thresh &lt; Uncommon; outdoor critters (3098) only drop greys/whites → no `SMSG_LOOT_START_ROLL`. Avoid MULTI_DROP / quest-conditioned “100%” items.

**NPC / GO spawn cleanup:** `.npc add` and `.gobject add` are **persistent DB spawns**. Always use `SpawnKillLootable` / `Spawn` / `SpawnGameObject` (they register cleanup) or `DespawnCreatureSpawn` / `DespawnGameObjectSpawn` by **DB spawn id**. Never bare `.npc add` / `.gobject add` without cleanup — pad litter (Crimson Templar 15209, Gift of the Observer GO 194821) is a real failure mode. **Do not use `.npc add temp` for loot** (`TEMPSUMMON_CORPSE_DESPAWN` removes the corpse instantly; temp still lives ~120s — `Spawn` cleans on test end).

**Pets / Risen Ghoul / guardians:** `NewSolo`/`NewScenario` register `CleanupOwnedSummons` on cleanup (dismiss pet + despawn SUMMONEDBY/CREATEDBY units: Risen Ghoul 26125, imps, totems…). After Raise Dead / heavy summons, also call `bot.CleanupOwnedSummons(t)` while still InWorld.

**Pad isolation:** `go test ./... -parallel 1` → packages still parallel, tests **serial within package**. Use **`e2eharness.PackagePad(t)`** (sticky per suite folder for process life). 27 IsolationPads (EK towers/Elwynn/Burning Steppes; Outland islands; Kalimdor mountains/Mulgore/Stonetalon/Ashenvale/Felwood/Hyjal). Preferred combat/social suites in `PreferredPackagePads`. Never park every suite on one SW cell.

**Spawn cleanup path:** persistent adds → SQL DELETE + soft live despawn (`DespawnCreatureSpawn` / `DespawnGameObjectSpawn`). Safe after session close for SQL half; never bare add.

---

## Waiters over sleeps (required for new code)

Pattern: **Arm → Send → Wait** on a real signal (packet / object cache / phase / TradeOpen / GroupState / HP).

| After… | Wait on |
|--------|---------|
| Relog / far `.tele` / map transfer | **`WaitInWorld`** (and tele helpers use `WaitForTeleportAfter`) |
| HardDisconnect | different bot: `ProbeWorldAlive` / `AssertWorldAlive` |
| Group invite/accept / SetLeader | `WaitGroupList` / `WaitIsGroupLeader` / `WaitNotInGroup` |
| Trade initiate/accept | `OpenTrade` / `WaitTradeOpen` / `WaitTradeStatus` / `CompleteTrade` |
| Kill → loot | `WaitUnitDead` → **`WaitUnitLootable`** → `OpenLoot` |
| Cast / aura / death | `Cast`/`WaitSpell*` · `WaitAura` · `WaitDead` / `WaitAlive` |
| Nearby NPC after tele | `WaitUnit` / `WaitUnitGUID` (object cache) |

**Banned in new tests:** `time.Sleep` / `WaitMS` / “settle” delays after an action when a condition exists. Short poll intervals (20–50ms) **inside** harness waiters are fine. If you truly need wall-clock (e.g. aura duration), assert with `AssertAuraRemains` / deadline waiters — not blind sleeps after setup.

---

## Never-do

- Do **not** write tests only inside AzerothGhost unless contributing to that repo
- Do **not** `.gm on` mid-fight for `.damage` → evade; use `Damage`/`DamageKill`
- Do **not** pull with GM mode still on → `CombatReady` first
- Do **not** bare `.tele` when melee matters → `GoCreatureID`
- Do **not** fixed long sleeps / **`WaitMS`** / blind `time.Sleep` after actions — use waiters (see above)
- Do **not** invent harness APIs; use package helpers with `bot.World` if no method
- Do **not** `.npc add` when bug is spell-summon path; do **not** bare `.npc add`/`.gobject add` without cleanup
- Do **not** re-arm waiters during Wait
- Do **not** share one world cell across packages — use `PackagePad(t)`
- Do **not** use `SoftPass` to greenwash unjudgeable setup (fail-closed unless `E2E_ALLOW_SOFT_PASS=1`)
- Do **not** assume ToCloud9 is required — AC world/auth is enough if `E2E_AUTH_ADDR` reaches clients’ auth/realm entry

---

## Severity

| Situation | Helper |
|-----------|--------|
| Setup blocked evaluation | `Preconditionf` |
| Wrong core behaviour for tracked issue | `ConfirmedBugf(t, N, …)` |
| Infra/timeout/SQL/cache | `HarnessFailf` |
| Post-drive product oracle | `Assertf` / `AssertBugf` |
| Soft note | `SoftWarnf` |
| SoftPass | **Fails** unless `E2E_ALLOW_SOFT_PASS=1` — avoid in new tests |

CI log triage: grep `E2E_FAIL` (harness fatals) or go’s `--- FAIL`; do not treat Info-level WorldClient lines as failures (`SMSG_CAST_FAILED` is Debug).

---

## Golden template

```go
//go:build e2e

package myservice_test

import (
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/azerothcore/AzerothGhost/e2e/e2eharness"
)

// Issue: https://github.com/azerothcore/azerothcore-wotlk/issues/NNNNN  (optional)
func TestMyFeature_ShortName(t *testing.T) {
	t.Parallel() // omit or use meta.Begin(serial) if the scenario needs exclusive realm
	bot := e2eharness.NewSolo(t, e2eharness.ScenarioOpts{
		Prefix:        "Short",
		Race:          e2eharness.RaceHuman,
		Class:         e2eharness.ClassWarrior,
		Level:         80,
		LearnAllClass: true,
	})
	pad := e2eharness.PackagePad(t) // sticky isolation pad for this suite folder
	bot.TeleportPad(t, pad)
	// GM mode on by default for setup. bot.CombatReady(t) before pulls.
	// Drive: Cast/Engage/ApplyAura/DieAndRepop/Relog.
	// Setup fail → Preconditionf; wrong core → ConfirmedBugf(t, N, …). Never SoftPass in new tests.
	t.Logf("PASS …")
}
```

Multi-bot: `NewScenario` + `BotSpec{Role}` + `ByRole` + `EnableHostilePvP` if cross-faction.  
Party: prefer `FormPartyAtPad(t, PackagePad(t), leader, mate)` (or `TeleportAllPad` + `FormParty`); cleanup with `DisbandParty` or `LeaveGroup`.  
Hard drop / crash probe: `victim.HardDisconnect(t)` then `ProbeWorldAlive(t, probe, issue)` — never reuse victim session.  
After Relog / far tele: `bot.WaitInWorld(t, 0)` if you left the helper path; `Relog`/`TeleNamed` already wait.

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

`SpellCharge`, `SpellTaunt`, `SpellDismissPet`, `SpellBlendingInAura`, `SpellGroundingTotem`, `SpellGroundingTotemEffect`,
`SpellRainOfFire`, `SpellRaiseDead`, `SpellMountSwiftGryphon`, `QuestRethbanGauntlet`,
`CreatureKologarn`, `CreatureGroundingTotem`, `CreatureTargetDummy`,
`LootMethodFreeForAll`/`GroupLoot`/`MasterLoot`/`NeedBeforeGreed` (client package),
`RaceHuman`/`RaceOrc`/`RaceTauren`, `ClassWarrior`/`ClassRogue`/`ClassShaman`/`ClassWarlock`/`ClassDeathKnight`.
