-- scripts/ai/core/action.lua
-- Base Action: Execute(ctx) , isUseful(ctx), isPossible(ctx).
-- Concrete actions are usually registered as simple functions in engine, or tables.

local M = {}
M.Action = {}
M.Action.__index = M.Action

function M.Action:new(o)
  o = o or {}
  setmetatable(o, M.Action)
  return o
end

function M.Action:Execute(ctx)
  return false
end

function M.Action:isUseful(ctx)
  return true
end

function M.Action:isPossible(ctx)
  return true
end

return M
