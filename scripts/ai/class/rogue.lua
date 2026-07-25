-- scripts/ai/class/rogue.lua
-- Rogue class: assassination/combat/subtlety.
-- Key: slice dice, rupture, evis, backstab/hemorrhage, stealth opener, kick interrupt.
-- Uses has_aura_on for slice, can_cast, get_stance (energy via power).

local strategy_mod = dofile("scripts/ai/core/strategy.lua")
local Strategy = strategy_mod.Strategy
local utils = dofile("scripts/ai/core/utils.lua")

local M = {}

local data_ok, data = pcall(dofile, "scripts/ai/data/rogue_spells.lua")
local SPELLS = (data_ok and data.SPELLS) or {
  SINISTER_STRIKE = 1752, EVISCERATE = 2098, BACKSTAB = 53, SLICE_AND_DICE = 5171,
  RUPTURE = 1943, KIDNEY_SHOT = 408, SPRINT = 2983, STEALTH = 1784,
  MUTILATE = 1329, HEMORRHAGE = 16511, ENVENOM = 32645,
}

local RogueGeneric = Strategy:new({name = "rogue_generic"})

function RogueGeneric:getName() return "rogue_generic" end
function RogueGeneric:getType() return {"combat", "dps", "melee", "rogue"} end

function RogueGeneric:getDefaultActions()
  return {
    {name = "rogue_sinister", relevance = 10},
    {name = "rogue_eviscerate", relevance = 8},
    {name = "rogue_slice", relevance = 7},
    {name = "rogue_auto", relevance = 0.5},
  }
end

function RogueGeneric:getTriggers()
  return {
    {
      name = "slice_missing",
      IsActive = function(ctx)
        return not bot.has_aura_on(0, SPELLS.SLICE_AND_DICE)
      end,
      getHandlers = function() return {{name="rogue_slice", relevance=15}} end,
    },
    {
      name = "rupture_missing",
      IsActive = function(ctx)
        local t = bot.get_target() or 0
        if t == 0 then return false end
        return not bot.has_aura_on(t, SPELLS.RUPTURE)
      end,
      getHandlers = function() return {{name="rogue_rupture", relevance=14}} end,
    },
  }
end

-- Assassination
local Assassination = Strategy:new({name = "assassination"})

function Assassination:getName() return "assassination" end
function Assassination:getType() return {"combat", "dps", "melee", "assass"} end

function Assassination:getDefaultActions()
  return {
    {name = "rogue_mutilate", relevance = 16},
    {name = "rogue_eviscerate", relevance = 13},
    {name = "rogue_rupture", relevance = 11},
  }
end

-- Combat
local Combat = Strategy:new({name = "combat"})

function Combat:getName() return "combat" end
function Combat:getType() return {"combat", "dps", "melee", "combat_rogue"} end

function Combat:getDefaultActions()
  return {
    {name = "rogue_sinister", relevance = 15},
    {name = "rogue_kidney", relevance = 9},
  }
end

function Combat:getTriggers()
  return {
    {
      name = "behind_target",
      IsActive = function(ctx)
        local t = bot.get_target() or 0
        return t ~= 0 and bot.is_behind_target and bot.is_behind_target(t) and bot.is_spell_ready(SPELLS.BACKSTAB)
      end,
      getHandlers = function() return {{name="rogue_backstab", relevance=17}} end,
    },
  }
end

-- Subtlety
local Subtlety = Strategy:new({name = "subtlety"})

function Subtlety:getName() return "subtlety" end
function Subtlety:getType() return {"combat", "dps", "melee", "subtlety"} end

function Subtlety:getDefaultActions()
  return {
    {name = "rogue_stealth", relevance = 5},
    {name = "rogue_hemorrhage", relevance = 12},
  }
end

function M.register(ctx)
  if not ctx then return end
  ctx:register_strategy("rogue_generic", RogueGeneric)
  ctx:register_strategy("assassination", Assassination)
  ctx:register_strategy("combat", Combat)
  ctx:register_strategy("subtlety", Subtlety)

  ctx:register_action("rogue_sinister", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.is_spell_ready(SPELLS.SINISTER_STRIKE) then
      utils.log_decision("rogue: sinister strike")
      return bot.cast_spell(SPELLS.SINISTER_STRIKE, t)
    end
    return false
  end)

  ctx:register_action("rogue_eviscerate", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.can_cast(SPELLS.EVISCERATE, t) then
      utils.log_decision("rogue: eviscerate")
      return bot.cast_spell(SPELLS.EVISCERATE, t)
    end
    return false
  end)

  ctx:register_action("rogue_slice", function(ctx2)
    if bot.is_spell_ready(SPELLS.SLICE_AND_DICE) then
      utils.log_decision("rogue: slice and dice")
      return bot.cast_spell(SPELLS.SLICE_AND_DICE, 0)
    end
    return false
  end)

  ctx:register_action("rogue_rupture", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.can_cast(SPELLS.RUPTURE, t) then
      utils.log_decision("rogue: rupture")
      return bot.cast_spell(SPELLS.RUPTURE, t)
    end
    return false
  end)

  ctx:register_action("rogue_mutilate", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.is_spell_ready(SPELLS.MUTILATE or SPELLS.SINISTER_STRIKE) then
      utils.log_decision("rogue(ass): mutilate")
      return bot.cast_spell(SPELLS.MUTILATE or SPELLS.SINISTER_STRIKE, t)
    end
    return false
  end)

  ctx:register_action("rogue_backstab", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.is_spell_ready(SPELLS.BACKSTAB) then
      utils.log_decision("rogue: backstab")
      return bot.cast_spell(SPELLS.BACKSTAB, t)
    end
    return false
  end)

  ctx:register_action("rogue_kidney", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.is_spell_ready(SPELLS.KIDNEY_SHOT) then
      utils.log_decision("rogue: kidney shot")
      return bot.cast_spell(SPELLS.KIDNEY_SHOT, t)
    end
    return false
  end)

  ctx:register_action("rogue_hemorrhage", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.is_spell_ready(SPELLS.HEMORRHAGE or SPELLS.SINISTER_STRIKE) then
      utils.log_decision("rogue(sub): hemorrhage")
      return bot.cast_spell(SPELLS.HEMORRHAGE or SPELLS.SINISTER_STRIKE, t)
    end
    return false
  end)

  ctx:register_action("rogue_stealth", function(ctx2)
    if bot.is_spell_ready(SPELLS.STEALTH) and not bot.has_aura_on(0, SPELLS.STEALTH) then
      utils.log_decision("rogue: stealth")
      return bot.cast_spell(SPELLS.STEALTH, 0)
    end
    return false
  end)

  ctx:register_action("rogue_auto", function(ctx2)
    local t = bot.get_target() or 0
    if t == 0 then return false end
    if bot.set_target then pcall(function() bot.set_target(t) end) end
    -- Set target before attacking for visibility to external players
    bot.attack(t)
    return true
  end)

  utils.log_decision("rogue class registered (assass/combat/subtlety + generic)")
end

M.SPELLS = SPELLS
M.RogueGeneric = RogueGeneric
M.Assassination = Assassination
M.Combat = Combat
M.Subtlety = Subtlety

return M
