-- scripts/ai/generic/buff.lua
-- Noncombat buffing strategy: self buffs when out of combat (e.g. via class or generic like fort/motw if known).
-- Relevance low, lets class specific handle main buffs.

local strategy_mod = dofile("scripts/ai/core/strategy.lua")
local Strategy = strategy_mod.Strategy

local M = {}

local BuffStrategy = Strategy:new({name = "buff"})

function BuffStrategy:getName()
  return "buff"
end

function BuffStrategy:getType()
  return {"generic", "noncombat", "buff"}
end

function BuffStrategy:getDefaultActions()
  return {
    {name = "generic_buff_self", relevance = 3},
  }
end

M.BuffStrategy = BuffStrategy
return M
