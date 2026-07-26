-- scripts/ai/core/engine.lua
-- The AI Engine. Provides Tick() that implements relevance-based action selection
-- modeled on playerbots: ProcessTriggers + DoNextAction + Multiply relevance + Execute first viable.
--
-- Usage:
--   local Engine = dofile("scripts/ai/core/engine.lua").Engine
--   local ai = Engine:new()
--   ai:register_strategy(...)
--   ai:enable("survive")
--   ...
--   function on_tick() ai:Tick() end

local strategy_mod = dofile("scripts/ai/core/strategy.lua")
local values = dofile("scripts/ai/core/values.lua")
local utils = dofile("scripts/ai/core/utils.lua")

local M = {}
M.Engine = {}
M.Engine.__index = M.Engine

function M.Engine:new()
  local o = {
    registered_strategies = {},
    active_strategies = {},
    registered_actions = {},
    blackboard = {},  -- simple shared state for strats
    _tick = 0,
  }
  setmetatable(o, M.Engine)
  return o
end

function M.Engine:register_strategy(name, strat)
  if not strat then return end
  self.registered_strategies[name] = strat
end

function M.Engine:enable(name)
  local strat = self.registered_strategies[name]
  if strat then
    self.active_strategies[name] = strat
    -- allow strats to init their state on enable
    if strat.onEnable then strat:onEnable(self) end
  end
end

function M.Engine:disable(name)
  self.active_strategies[name] = nil
end

function M.Engine:register_action(name, action)
  -- action can be function(ctx) or table with Execute/isUseful/isPossible
  self.registered_actions[name] = action
end

function M.Engine:get_value(name)
  return values.get_value(name, self)
end

-- blackboard for simple sharing (e.g. scenario hints, follow leader, group coord)
function M.Engine:set_blackboard(key, val)
  self.blackboard[key] = val
end
function M.Engine:get_blackboard(key)
  return self.blackboard[key]
end


function M.Engine:Tick()
  self._tick = self._tick + 1
  if not bot then return end

  -- Summon / near-teleport / worldport: Go already interrupted movement+attack.
  -- Drop sticky target/rest state so strategies re-acquire at the new position.
  if bot.consume_teleport and bot.consume_teleport() then
    if bot.stop_moving then pcall(bot.stop_moving) end
    if bot.stop_attack then pcall(bot.stop_attack) end
    if bot.set_target then pcall(bot.set_target, 0) end
    if self.set_blackboard then
      self:set_blackboard("teleported", true)
      self:set_blackboard("current_target", nil)
      self:set_blackboard("rest_until", nil)
    end
    return
  end

  values.update_cache(self._tick, self)

  -- Expose power on ctx for strategies that only check is_spell_ready.
  -- (Cached each tick; cheap.)
  if bot.get_power then
    local cur = bot.get_power()
    self._power = tonumber(cur) or 0
  end

  local candidates = {}

  -- 1. Collect from active strategies' default actions (always)
  for _, strat in pairs(self.active_strategies) do
    local defs = {}
    local ok, res = pcall(function() if strat.getDefaultActions then return strat:getDefaultActions() end end)
    if ok and res then defs = res or {} end
    if type(defs) ~= "table" then defs = {} end
    for _, da in ipairs(defs) do
      if type(da) == "table" then
        local src = "unknown"
        local okn, nm = pcall(function() return strat.getName and strat:getName() end)
        if okn and nm then src = nm end
        table.insert(candidates, {
          name = da.name,
          relevance = tonumber(da.relevance) or 0,
          source = src
        })
      end
    end
  end

  -- 2. Collect from triggers (always; removed %2 throttle per review to ensure high-relevance conditional mechanics like execute/rend/no_pet/shadowform/low_health never skipped)
  for _, strat in pairs(self.active_strategies) do
    local trigs = {}
    local ok, res = pcall(function() if strat.getTriggers then return strat:getTriggers() end end)
    if ok and res then trigs = res or {} end
    if type(trigs) ~= "table" then trigs = {} end
    for _, t in ipairs(trigs) do
      if type(t) == "table" then
        local active = false
        if t.IsActive then
          local ok, res = pcall(t.IsActive, self)
          active = ok and res
        end
        if active and t.getHandlers then
          local ok, handlers = pcall(t.getHandlers, t)
          if ok and handlers then
            if type(handlers) == "table" then
              for _, h in ipairs(handlers) do
                if type(h) == "table" or type(h) == "string" then
                  table.insert(candidates, {
                    name = (type(h)=="table" and h.name) or h,
                    relevance = tonumber( (type(h)=="table" and h.relevance) or 5 ) or 5,
                    source = t.name or "trigger"
                  })
                end
              end
            end
          end
        end
      end
    end
  end


  -- 3. Sort by relevance desc (highest priority first)
  table.sort(candidates, function(a, b)
    local ra = tonumber(a.relevance) or 0
    local rb = tonumber(b.relevance) or 0
    if ra ~= rb then return ra > rb end
    -- tie-break for determinism (name, then source); tostring to avoid compare errors on non-strings
    local na = tostring(a.name or "")
    local nb = tostring(b.name or "")
    if na ~= nb then return na < nb end
    return tostring(a.source or "") < tostring(b.source or "")
  end)

  -- 4. Execute the first viable action (isPossible + isUseful)
  for _, cand in ipairs(candidates) do
    local act = self.registered_actions[cand.name]
    if act then
      local possible = true
      local useful = true
      if type(act) == "table" then
        if act.isPossible then
          local ok, res = pcall(act.isPossible, act, self)
          possible = ok and res
        end
        if act.isUseful then
          local ok, res = pcall(act.isUseful, act, self)
          useful = ok and res
        end
        if possible and useful and act.Execute then
          local ok, res = pcall(act.Execute, act, self)
          if ok and res ~= false then
            return  -- executed one
          end
        end
      else
        -- plain function(ctx)
        local ok, res = pcall(act, self)
        if ok and res ~= false then
          return
        end
      end
    end
  end
  -- no action taken this tick
end

return M
