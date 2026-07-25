# AzerothGhost Lua AI Core Framework

This directory contains the Lua-native Strategy + Trigger + Action + Value AI framework
inspired by playerbots but implemented 100% in idiomatic Lua for extensibility via AIBundles.

Note on style: ai/ files (core/generic) use 2-space indentation to match the library-style
in scripts/lib/behaviors.lua (and setup.lua when present). User on_tick examples (grind.lua, hogger.lua)
use 4 spaces.

## Usage (from on_tick or bundle main)

```lua
local ai = dofile("scripts/ai/init.lua")
ai:enable_default_strategies()

function on_tick()
  if not bot.is_alive() then
    if bot.send_guild_command then bot.send_guild_command(".revive") else bot.send_command(".revive") end
    return
  end
  ai:Tick()
end
```

## Structure

- core/: base classes and the engine (Tick loop modeled on playerbots ProcessTriggers + relevance selection)
- generic/: basic strategies (survive, melee, ranged, loot, grind) that work for any class using existing bot.* API
- class/: per-class libs with per-spec strategies (ret/prot/holy, assass/combat/sub etc), triggers for key mechanics, spec detection heuristics.
- data/: shared spell ids and data + class _spells.lua tables
- init.lua: loader that wires generics + registers class libs based on bot.get_class() (1,3,8)

## Engine Tick Overview

`ai:Tick()`:
1. Update cached values (health_pct, distance_to_target, etc.)
2. Collect candidate NextActions from:
   - active strategies' getDefaultActions() (relevance pairs)
   - active triggers' getHandlers() when IsActive(ctx)
3. Sort candidates by relevance descending
4. For highest first: lookup registered action, test isPossible+isUseful, Execute if so. Stop after first success.

This provides composable, relevance-based decision making better than simple if-chains in grind.lua.

Strategies can be enabled/disabled dynamically:
ai:enable("survive")
ai:disable("grind")

## Defaults
Advanced Lua AI (this framework) is the recommended power-user path for class bots, hunters with pets, spec-aware rotations.
Use via `ai = dofile("scripts/ai/init.lua"); ai:enable_default_strategies(); ... ai:Tick()`
in your on_tick or in AIBundle main (see siege.lua, orchestrator examples).
See ../docs/ADVANCED_LUA_AI_DESIGN.md for user extension + "how to write custom strategy".

## Notes

- Class foundation: ai/init auto-loads class/ based on bot.get_class() (all 10); note data/*_spells.lua must `return M`. Strategies like "arms", "beast_mastery", "retribution" etc. available.
- Uses bot.* : has_aura_on, can_cast, get_pet_guid, pet_attack, is_behind_target + prior (get_stance API available but not exercised).
- Triggers for key: rend missing, serpent missing, hot streak (48108), pet, execute, stances.
- Backward compatible: old scripts unaffected. Opt-in advanced via dofile + enable + Tick().
- Improve behaviors.lua or use new (examples show both).

## Remaining Classes + Polish

- All 10 classes: paladin (ret/prot/holy), rogue(assass/combat/subtlety), priest(shadow/holy/disc), deathknight(blood/frost/unholy), shaman(resto/ele/enh), warlock(aff/destro/demo), druid(balance/feral/resto). (Data modules return M; see fixes.)
- More generics: follow, buff (noncombat), rest, rpg (simple). Enable via ai:enable("follow") etc explicitly, or use ai:enable_rpg_mode() to enable all 4 together.
- Spec detection: ai.detect_spec() (dynamic); uses is_spell_ready(high spells), has_aura_on(stance/form), power_type + pcalls.
- Performance: value caching per-engine (ctx._value_cache), trigger collection always (throttle removed for correctness of high-rel mechanics).
- Threat: improved in values using has_aggro_on/get_threat (now cached).
- Blackboard: engine.blackboard + set/get_blackboard for scenario hints (e.g. leader_guid for follow). New generics use it.
- Updated init for cls 2,4-7,9,11; data/*_spells; examples can use ai:enable_default_strategies() + ai.detect_spec().

See top of source files for more API comments.
