-- scripts/ai/generic/rest.lua
-- Rest strategy: eat/drink or pause when low resources and out of combat.
-- Simple impl: stop moving + log (real eat would use items/inv + commands).

local strategy_mod = dofile("scripts/ai/core/strategy.lua")
local Strategy = strategy_mod.Strategy

local M = {}

local RestStrategy = Strategy:new({name = "rest"})

function RestStrategy:getName()
  return "rest"
end

function RestStrategy:getType()
  return {"generic", "noncombat", "rest", "rpg"}
end

function RestStrategy:getDefaultActions()
  -- Above grind select (25) so low-HP OOC pauses win over new pulls.
  return {
    {name = "rest_if_low", relevance = 32},
  }
end

M.RestStrategy = RestStrategy
return M
