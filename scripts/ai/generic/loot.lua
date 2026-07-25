-- scripts/ai/generic/loot.lua
-- Generic loot: when not in combat and no (live) target, loot nearby if possible.
-- Improves on basic grind by using explicit relevance.

local strategy_mod = dofile("scripts/ai/core/strategy.lua")
local Strategy = strategy_mod.Strategy
local trigger_mod = dofile("scripts/ai/core/trigger.lua")

local M = {}

local LootStrategy = Strategy:new({name = "loot"})

function LootStrategy:getName()
  return "loot"
end

function LootStrategy:getType()
  return {"generic", "noncombat", "loot"}
end

function LootStrategy:getDefaultActions()
  return {
    {name = "loot_nearby", relevance = 15},
  }
end

function LootStrategy:getTriggers()
  return {
    trigger_mod.Trigger:new({
      name = "can_loot",
      IsActive = function(ctx)
        if ctx:get_value("in_combat") then return false end
        local tg = bot and bot.get_target and bot.get_target() or 0
        if tg == 0 then return true end
        local u = bot and bot.get_unit and bot.get_unit(tg) or nil
        return not (u and u.is_alive)
      end,
      getHandlers = function()
        return {{name = "loot_nearby", relevance = 20}}
      end,
    }),
  }
end

M.LootStrategy = LootStrategy
return M
