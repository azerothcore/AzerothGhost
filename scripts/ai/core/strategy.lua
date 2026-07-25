-- scripts/ai/core/strategy.lua
-- Base Strategy modeled on playerbots Strategy (getName, getType, initTriggers, getDefaultActions).
-- Used by engine to collect default actions and triggers.

local M = {}
M.Strategy = {}
M.Strategy.__index = M.Strategy

function M.Strategy:new(o)
  o = o or {}
  setmetatable(o, M.Strategy)
  o.triggers = o.triggers or {}
  o._inited = false
  return o
end

function M.Strategy:getName()
  return self.name or "base"
end

function M.Strategy:getType()
  return self.types or {"generic"}
end

function M.Strategy:initTriggers(triggerList)
  -- subclasses append their trigger tables here; triggerList may be ignored if using getTriggers
end

function M.Strategy:getDefaultActions()
  -- return list of {name = "action_name", relevance = 10.0 }
  return {}
end

function M.Strategy:getTriggers()
  -- return list of trigger tables {name=..., IsActive=function(ctx), getHandlers=function() return {{name, relevance}} end }
  return self.triggers or {}
end

return M
