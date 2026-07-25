-- ai/examples/advanced_warrior_grind.lua
-- Example of using the class libraries foundation for a warrior (works for any class now).
-- Loads ai/init (which auto registers class strats based on bot.get_class()),
-- enables sensible defaults, then drives via ai:Tick() in on_tick.
-- use ai.detect_spec() for heuristic spec.
--
-- Usage (local): azghost --profile local-ac cli --char-name MyWarrior --lua-script scripts/ai/examples/advanced_warrior_grind.lua
-- Or embed in AIBundle main for orchestrator/scenarios.
-- For other classes use analogous or load and enable("retribution") etc.
--
-- Smoke harness (for review): see /tmp/smoke_ai.lua equiv or run with mocked bot to verify load+register+Tick for all 10 + generics + detect + bb. (Verified post-fix.)

local ai = dofile("scripts/ai/init.lua")

-- enable core + class (init already wires warrior if class==1, but explicit for clarity)
ai:enable("survive")
ai:enable("grind")
ai:enable("loot")
ai:enable("melee")
ai:enable("generic_warrior")
ai:enable("arms")  -- or "fury" / "prot" ; user can switch dynamically
-- local spec = ai.detect_spec and ai.detect_spec() or nil; if spec then ai:enable(spec) end

-- Example: scenario data can drive spec e.g. if (scenario_data or {}).spec == "fury" then ai:enable("fury"); ai:disable("arms") end
-- Blackboard e.g. ai:set_blackboard("leader_guid", someGuid) for follow strat.

function on_tick()
  if not bot.is_alive() then
    if bot.send_guild_command then bot.send_guild_command(".revive") else bot.send_command(".revive") end
    return
  end
  ai:Tick()
end

-- Advanced users: ai:disable("arms"); ai:enable("fury") mid-run etc.
