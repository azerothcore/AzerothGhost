-- scripts/ai/core/trigger.lua
-- Base for Triggers: name + IsActive(ctx) -> bool + getHandlers() -> { {name=.., relevance=..} , ... }
-- Triggers are evaluated each Tick from active strategies; when active they contribute high-relevance actions.
-- Note: engine accepts raw table shapes (duck-typed) or Trigger:new() instances; both used by generics for simplicity.

local M = {}
M.Trigger = {}
M.Trigger.__index = M.Trigger

function M.Trigger:new(o)
  o = o or {}
  setmetatable(o, M.Trigger)
  return o
end

function M.Trigger:getName()
  return self.name or "base_trigger"
end

function M.Trigger:IsActive(ctx)
  return false
end

function M.Trigger:getHandlers()
  return {}
end

return M
