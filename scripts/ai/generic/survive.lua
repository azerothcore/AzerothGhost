-- scripts/ai/generic/survive.lua
-- Generic survive strategy: death recovery + low health reaction.
-- High relevance so it takes precedence. Uses only existing bot.* 

local strategy_mod = dofile("scripts/ai/core/strategy.lua")
local Strategy = strategy_mod.Strategy
local trigger_mod = dofile("scripts/ai/core/trigger.lua")

local M = {}

local SurviveStrategy = Strategy:new({name = "survive"})

function SurviveStrategy:getName()
  return "survive"
end

function SurviveStrategy:getType()
  return {"generic", "survive"}
end

function SurviveStrategy:getDefaultActions()
  return {
    {name = "survive_check_alive", relevance = 100},
    {name = "survive_low_health", relevance = 80},
  }
end

function SurviveStrategy:getTriggers()
  return {
    trigger_mod.Trigger:new({
      name = "dead",
      IsActive = function(ctx)
        return not ctx:get_value("is_alive")
      end,
      getHandlers = function()
        return {{name = "survive_check_alive", relevance = 100}}
      end,
    }),
    trigger_mod.Trigger:new({
      name = "low_health",
      IsActive = function(ctx)
        local hp = ctx:get_value("health_pct") or 100
        return hp < 25
      end,
      getHandlers = function()
        return {{name = "survive_low_health", relevance = 75}}
      end,
    }),
  }
end

-- actions registered by init.lua using the names above

M.SurviveStrategy = SurviveStrategy
return M
