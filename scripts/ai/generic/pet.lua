-- scripts/ai/generic/pet.lua
-- Generic pet management strategy.
-- Focus: call/revive/mend, modes (attack/follow/defensive), pet hp awareness.
-- Used by hunter (bm), warlock (demo) etc. Register + actions wired in init.lua .
-- Follows exact pattern of other generics (survive etc); uses Trigger:new for shape consistency.

local strategy_mod = dofile("scripts/ai/core/strategy.lua")
local Strategy = strategy_mod.Strategy
local trigger_mod = dofile("scripts/ai/core/trigger.lua")

local M = {}

local PetStrategy = Strategy:new({name = "pet_management"})

function PetStrategy:getName() return "pet_management" end
function PetStrategy:getType() return {"generic", "pet", "support"} end

function PetStrategy:getDefaultActions()
  return {
    {name = "pet_attack", relevance = 8},
    {name = "pet_mend", relevance = 7},
    {name = "pet_call_or_revive", relevance = 25},
  }
end

function PetStrategy:getTriggers()
  return {
    trigger_mod.Trigger:new({
      name = "no_pet",
      IsActive = function(ctx)
        local exists = ctx:get_value("pet_exists")
        local hp = ctx:get_value("pet_health_pct") or 100
        return not exists or hp <= 0
      end,
      getHandlers = function() return {{name="pet_call_or_revive", relevance=30}} end,
    }),
    trigger_mod.Trigger:new({
      name = "pet_low_health",
      IsActive = function(ctx)
        local hp = ctx:get_value("pet_health_pct") or 100
        return hp > 0 and hp < 50
      end,
      getHandlers = function() return {{name="pet_mend", relevance=18}} end,
    }),
  }
end

M.PetStrategy = PetStrategy
return M
