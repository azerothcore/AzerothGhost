-- advanced_mage_hunter.lua
-- Demonstrates full power: mage fireball/pyro, hunter with pet.
-- Load via --lua-script or embed in AIBundle for orchestrator/scenarios.

local ai = dofile("scripts/ai/init.lua")
ai:enable_default_strategies()
-- class specific auto via init based on bot.get_class(), or:
-- if bot.get_class() == 3 then ai:enable("beast_mastery"); ... end

function on_tick()
  if not bot.is_alive() then
    if bot.send_guild_command then bot.send_guild_command(".revive") else bot.send_command(".revive") end
    return
  end
  ai:Tick()
  -- example decision log for E2E: "mage bot casts fireball"
  -- (actual cast decision logged inside class action/trigger)
end
