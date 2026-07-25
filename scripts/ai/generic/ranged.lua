-- scripts/ai/generic/ranged.lua
-- Basic ranged engagement skeleton. Falls back to same as melee basics
-- (auto + move). Class libs will register better cast priorities.

local strategy_mod = dofile("scripts/ai/core/strategy.lua")
local Strategy = strategy_mod.Strategy

local M = {}

local RangedStrategy = Strategy:new({name = "ranged"})

function RangedStrategy:getName()
  return "ranged"
end

function RangedStrategy:getType()
  return {"generic", "combat", "ranged"}
end

function RangedStrategy:getDefaultActions()
  return {
    {name = "engage_ranged", relevance = 9},
  }
end

M.RangedStrategy = RangedStrategy
return M
