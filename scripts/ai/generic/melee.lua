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
  -- Below class ability defaults (MS/rend/execute ~11–25) so rotation casts try first;
  -- still above wander so we keep auto-attack and sticky chase when spells are not ready.
  return {
    {name = "engage_melee", relevance = 7},
  }
end

-- actions implemented in init

M.MeleeStrategy = MeleeStrategy
return M
