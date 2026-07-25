-- scripts/ai/generic/rpg.lua
-- Simple RPG behaviors: occasional emotes, wander when idle, use blackboard hints.
-- Complements follow/rest/buff for "simple rpg" mode.

local strategy_mod = dofile("scripts/ai/core/strategy.lua")
local Strategy = strategy_mod.Strategy

local M = {}

local RpgStrategy = Strategy:new({name = "rpg"})

function RpgStrategy:getName()
  return "rpg"
end

function RpgStrategy:getType()
  return {"generic", "noncombat", "rpg"}
end

function RpgStrategy:getDefaultActions()
  return {
    {name = "rpg_idle_emote", relevance = 1},
  }
end

M.RpgStrategy = RpgStrategy
return M
