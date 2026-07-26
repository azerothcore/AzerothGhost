-- scripts/ai/generic/grind.lua
-- Improved generic grind target selection using values (dist, level via unit, alive).
-- Better than basic grind.lua filter: prefers closer + reasonable level + alive.
-- Sets target and engages at appropriate distance.

local strategy_mod = dofile("scripts/ai/core/strategy.lua")
local Strategy = strategy_mod.Strategy
local trigger_mod = dofile("scripts/ai/core/trigger.lua")

local M = {}

local GrindStrategy = Strategy:new({name = "grind"})

function GrindStrategy:getName()
  return "grind"
end

function GrindStrategy:getType()
  return {"generic", "grind"}
end

function GrindStrategy:getDefaultActions()
  return {
    {name = "select_grind_target", relevance = 25},
    -- Very low: only when no target / not resting / not casting
    {name = "wander_idle", relevance = 2},
  }
end

function GrindStrategy:getTriggers()
  return {
    trigger_mod.Trigger:new({
      name = "no_target",
      IsActive = function(ctx)
        local tg = bot and bot.get_target and bot.get_target() or 0
        if tg == 0 then return true end
        local u = bot.get_unit and bot.get_unit(tg) or nil
        return not (u and u.is_alive and (u.health or 0) > 0)
      end,
      getHandlers = function()
        return {{name = "select_grind_target", relevance = 30}}
      end,
    }),
  }
end

M.GrindStrategy = GrindStrategy
return M
