-- scripts/ai/class/warlock.lua
-- Warlock: affliction/destruction/demonology.
-- Dots (corr/immol), shadowbolt/incin, pet, life tap, fear, meta for demo. (t~=0 + defensive per prior).

local strategy_mod = dofile("scripts/ai/core/strategy.lua")
local Strategy = strategy_mod.Strategy
local utils = dofile("scripts/ai/core/utils.lua")

local M = {}

local data_ok, data = pcall(dofile, "scripts/ai/data/warlock_spells.lua")
local SPELLS = (data_ok and data.SPELLS) or {
  SHADOW_BOLT = 686, CORRUPTION = 172, IMMOLATE = 348, CONFLAGRATE = 17962,
  INCINERATE = 29722, LIFE_TAP = 1454, FEAR = 5782, SUMMON_IMP = 688,
  METAMORPHOSIS = 47241,
}

local WarlockGeneric = Strategy:new({name = "warlock_generic"})

function WarlockGeneric:getName() return "warlock_generic" end
function WarlockGeneric:getType() return {"combat", "dps", "ranged", "warlock"} end

function WarlockGeneric:getDefaultActions()
  return {
    {name = "warlock_shadow_bolt", relevance = 10},
    {name = "warlock_corruption", relevance = 9},
    {name = "warlock_immolate", relevance = 8},
    {name = "warlock_life_tap", relevance = 4},
    {name = "warlock_fear", relevance = 1},
  }
end

function WarlockGeneric:getTriggers()
  return {
    {
      name = "corr_missing",
      IsActive = function(ctx)
        local t = bot.get_target() or 0
        if t == 0 then return false end
        return not bot.has_aura_on(t, SPELLS.CORRUPTION)
      end,
      getHandlers = function() return {{name="warlock_corruption", relevance=14}} end,
    },
    {
      name = "low_mana_tap",
      IsActive = function(ctx)
        local pp = ctx:get_value("power_pct") or 100
        return pp < 25 and bot.is_spell_ready(SPELLS.LIFE_TAP)
      end,
      getHandlers = function() return {{name="warlock_life_tap", relevance=18}} end,
    },
  }
end

-- Affliction
local Affliction = Strategy:new({name = "affliction"})

function Affliction:getName() return "affliction" end
function Affliction:getType() return {"combat", "dps", "ranged", "aff"} end

function Affliction:getDefaultActions()
  return {
    {name = "warlock_corruption", relevance = 15},
    {name = "warlock_unstable", relevance = 13},
    {name = "warlock_shadow_bolt", relevance = 11},
  }
end

-- Destruction
local Destruction = Strategy:new({name = "destruction"})

function Destruction:getName() return "destruction" end
function Destruction:getType() return {"combat", "dps", "ranged", "destro"} end

function Destruction:getDefaultActions()
  return {
    {name = "warlock_immolate", relevance = 16},
    {name = "warlock_incinerate", relevance = 14},
    {name = "warlock_conflagrate", relevance = 17},
  }
end

function Destruction:getTriggers()
  return {
    {
      name = "immolate_up_for_conf",
      IsActive = function(ctx)
        local t = bot.get_target() or 0
        if t == 0 then return false end
        return bot.has_aura_on(t, SPELLS.IMMOLATE) and bot.is_spell_ready(SPELLS.CONFLAGRATE)
      end,
      getHandlers = function() return {{name="warlock_conflagrate", relevance=20}} end,
    },
  }
end

-- Demonology
local Demonology = Strategy:new({name = "demonology"})

function Demonology:getName() return "demonology" end
function Demonology:getType() return {"combat", "dps", "pet", "demo"} end

function Demonology:getDefaultActions()
  return {
    {name = "warlock_summon", relevance = 22},
    {name = "warlock_meta", relevance = 10},
    {name = "pet_attack", relevance = 7}, -- use generic pet too
  }
end

function Demonology:getTriggers()
  return {
    {
      name = "no_pet_warlock",
      IsActive = function(ctx)
        local pg = bot.get_pet_guid and bot.get_pet_guid() or 0
        return pg == 0
      end,
      getHandlers = function() return {{name="warlock_summon", relevance=30}} end,
    },
    {
      name = "pet_low_warlock",
      IsActive = function(ctx)
        local php = ctx.get_value and ctx:get_value("pet_health_pct") or 100
        return php > 0 and php < 40
      end,
      getHandlers = function() return {{name="pet_mend", relevance=15}} end,
    },
  }
end

function M.register(ctx)
  if not ctx then return end
  ctx:register_strategy("warlock_generic", WarlockGeneric)
  ctx:register_strategy("affliction", Affliction)
  ctx:register_strategy("destruction", Destruction)
  ctx:register_strategy("demonology", Demonology)

  ctx:register_action("warlock_shadow_bolt", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.can_cast(SPELLS.SHADOW_BOLT, t) then
      utils.log_decision("warlock: shadow bolt")
      return bot.cast_spell(SPELLS.SHADOW_BOLT, t)
    end
    return false
  end)

  ctx:register_action("warlock_corruption", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.can_cast(SPELLS.CORRUPTION, t) then
      utils.log_decision("warlock: corruption")
      return bot.cast_spell(SPELLS.CORRUPTION, t)
    end
    return false
  end)

  ctx:register_action("warlock_immolate", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.can_cast(SPELLS.IMMOLATE, t) then
      utils.log_decision("warlock: immolate")
      return bot.cast_spell(SPELLS.IMMOLATE, t)
    end
    return false
  end)

  ctx:register_action("warlock_incinerate", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.is_spell_ready(SPELLS.INCINERATE or SPELLS.SHADOW_BOLT) then
      utils.log_decision("warlock(destro): incinerate")
      return bot.cast_spell(SPELLS.INCINERATE or SPELLS.SHADOW_BOLT, t)
    end
    return false
  end)

  ctx:register_action("warlock_conflagrate", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.is_spell_ready(SPELLS.CONFLAGRATE) then
      utils.log_decision("warlock(destro): conflagrate")
      return bot.cast_spell(SPELLS.CONFLAGRATE, t)
    end
    return false
  end)

  ctx:register_action("warlock_life_tap", function(ctx2)
    if bot.is_spell_ready(SPELLS.LIFE_TAP) then
      utils.log_decision("warlock: life tap")
      return bot.cast_spell(SPELLS.LIFE_TAP, 0)
    end
    return false
  end)

  ctx:register_action("warlock_summon", function(ctx2)
    -- guard: prevent spam when pet exists (unconditional default issue)
    local exists = (ctx2 and ctx2.get_value and ctx2:get_value("pet_exists")) or (bot.get_pet_guid and (bot.get_pet_guid() or 0) ~= 0)
    if exists then return false end
    if bot.is_spell_ready(SPELLS.SUMMON_IMP) then
      utils.log_decision("warlock: summon imp")
      return bot.cast_spell(SPELLS.SUMMON_IMP, 0)
    end
    return false
  end)

  ctx:register_action("warlock_meta", function(ctx2)
    if bot.is_spell_ready(SPELLS.METAMORPHOSIS) then
      utils.log_decision("warlock(demo): metamorphosis")
      return bot.cast_spell(SPELLS.METAMORPHOSIS, 0)
    end
    return false
  end)

  ctx:register_action("warlock_fear", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.is_spell_ready(SPELLS.FEAR) then
      utils.log_decision("warlock: fear")
      return bot.cast_spell(SPELLS.FEAR, t)
    end
    return false
  end)

  utils.log_decision("warlock class registered (aff/destro/demo + generic)")
end

M.SPELLS = SPELLS
M.WarlockGeneric = WarlockGeneric
M.Affliction = Affliction
M.Destruction = Destruction
M.Demonology = Demonology

return M
