-- scripts/ai/generic/melee.lua
-- Basic melee engagement: face/move close + auto attack when in range.
-- Relevance lower; lets survive/grind decide first.

local strategy_mod = dofile("scripts/ai/core/strategy.lua")
local Strategy = strategy_mod.Strategy

local M = {}

local MeleeStrategy = Strategy:new({name = "melee"})

function MeleeStrategy:getName()
  return "melee"
end

function MeleeStrategy:getType()
  return {"generic", "combat", "melee"}
end

function MeleeStrategy:getDefaultActions()
  return {
    {name = "engage_melee", relevance = 10},
  }
end

-- actions implemented in init

M.MeleeStrategy = MeleeStrategy
return M
