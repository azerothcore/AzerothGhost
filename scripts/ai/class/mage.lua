-- scripts/ai/class/mage.lua
-- Mage class lib (fire/frost emphasis, hot streak proc trigger).
-- Triggers for hot streak, missing armor, nova range kiting, low mana evo.
-- Uses bot.has_aura_on (for hot streak 48108), bot.can_cast, etc.

local strategy_mod = dofile("scripts/ai/core/strategy.lua")
local Strategy = strategy_mod.Strategy
local utils = dofile("scripts/ai/core/utils.lua")

local M = {}

local data_ok, data = pcall(dofile, "scripts/ai/data/mage_spells.lua")
local SPELLS = (data_ok and data.SPELLS) or {
  FIREBALL = 133, FROSTBOLT = 116, FROST_NOVA = 122,
  SCORCH = 2948, PYROBLAST = 11366, FLAMESTRIKE = 2120,
  LIVING_BOMB = 44457, EVOCATION = 12051, ARCANE_BLAST = 42897,
  HOT_STREAK = 48108,
  POLYMORPH = 118,
}

local MageGeneric = Strategy:new({name = "mage_generic"})

function MageGeneric:getName() return "mage_generic" end
function MageGeneric:getType() return {"combat", "dps", "ranged", "mage"} end

function MageGeneric:getDefaultActions()
  return {
    {name = "mage_fireball", relevance = 10},
    {name = "mage_frostbolt", relevance = 9},
    {name = "mage_scorch", relevance = 7},
    {name = "mage_polymorph_cc", relevance = 6}, -- light dungeon/raid cc
  }
end

function MageGeneric:getTriggers()
  return {
    {
      name = "hot_streak_proc",
      IsActive = function(ctx)
        return bot.has_aura_on(0, SPELLS.HOT_STREAK)
      end,
      getHandlers = function() return {{name="mage_pyroblast", relevance=22}} end,
    },
    {
      name = "low_mana_evo",
      IsActive = function(ctx)
        local pp = ctx:get_value("power_pct") or 100
        return pp < 20 and bot.is_spell_ready(SPELLS.EVOCATION)
      end,
      getHandlers = function() return {{name="mage_evocation", relevance=30}} end,
    },
    -- dungeon/raid light cc + pull hint via blackboard or enemy count
    {
      name = "need_cc",
      IsActive = function(ctx)
        local cnt = ctx:get_value("enemy_count") or 0
        local bb = (ctx.get_blackboard and ctx:get_blackboard("need_cc")) or false
        return (cnt > 1 or bb) and bot.is_spell_ready(SPELLS.POLYMORPH)
      end,
      getHandlers = function() return {{name="mage_polymorph_cc", relevance=15}} end,
    },
  }
end

-- Fire spec
local FireStrategy = Strategy:new({name = "fire"})

function FireStrategy:getName() return "fire" end
function FireStrategy:getType() return {"combat", "dps", "ranged", "fire"} end

function FireStrategy:getDefaultActions()
  return {
    {name = "mage_pyroblast", relevance = 15},
    {name = "mage_living_bomb", relevance = 11},
    {name = "mage_flamestrike", relevance = 8},
  }
end

-- Frost spec
local FrostStrategy = Strategy:new({name = "frost"})

function FrostStrategy:getName() return "frost" end
function FrostStrategy:getType() return {"combat", "dps", "ranged", "frost", "kite"} end

function FrostStrategy:getDefaultActions()
  return {
    {name = "mage_frostbolt", relevance = 12},
    {name = "mage_frost_nova", relevance = 5},
  }
end

function FrostStrategy:getTriggers()
  return {
    {
      name = "enemy_in_nova",
      IsActive = function(ctx)
        local d = ctx:get_value("distance_to_target") or 999
        return d < 10 and bot.is_spell_ready(SPELLS.FROST_NOVA)
      end,
      getHandlers = function() return {{name="mage_frost_nova", relevance=18}} end,
    },
  }
end

function M.register(ctx)
  if not ctx then return end
  ctx:register_strategy("mage_generic", MageGeneric)
  ctx:register_strategy("fire", FireStrategy)
  ctx:register_strategy("frost", FrostStrategy)

  ctx:register_action("mage_fireball", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.can_cast(SPELLS.FIREBALL, t) then
      utils.log_decision("mage: fireball")
      return bot.cast_spell(SPELLS.FIREBALL, t)
    end
    return false
  end)

  ctx:register_action("mage_frostbolt", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.can_cast(SPELLS.FROSTBOLT, t) then
      utils.log_decision("mage: frostbolt")
      return bot.cast_spell(SPELLS.FROSTBOLT, t)
    end
    return false
  end)

  ctx:register_action("mage_pyroblast", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.can_cast(SPELLS.PYROBLAST, t) then
      utils.log_decision("mage(fire): pyroblast (hot streak)")
      return bot.cast_spell(SPELLS.PYROBLAST, t)
    end
    return false
  end)

  ctx:register_action("mage_scorch", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.is_spell_ready(SPELLS.SCORCH) then
      utils.log_decision("mage: scorch")
      return bot.cast_spell(SPELLS.SCORCH, t)
    end
    return false
  end)

  ctx:register_action("mage_living_bomb", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.is_spell_ready(SPELLS.LIVING_BOMB) then
      utils.log_decision("mage(fire): living bomb")
      return bot.cast_spell(SPELLS.LIVING_BOMB, t)
    end
    return false
  end)

  ctx:register_action("mage_frost_nova", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.can_cast(SPELLS.FROST_NOVA, t) then
      utils.log_decision("mage(frost): frost nova (kite)")
      local res = bot.cast_spell(SPELLS.FROST_NOVA, t)
      -- move back a bit for kite
      if bot.get_unit then
        local u = bot.get_unit(t)
        if u then
          local ux, uy, uz = u.x or 0, u.y or 0, u.z or 0
          bot.move_to(ux - 8, uy, uz)
        end -- simple back
      end
      return res
    end
    return false
  end)

  ctx:register_action("mage_evocation", function(ctx2)
    if bot.is_spell_ready(SPELLS.EVOCATION) then
      utils.log_decision("mage: evocation")
      return bot.cast_spell(SPELLS.EVOCATION, 0)
    end
    return false
  end)

  -- light cc for dungeon/raid (poly non-elite or add)
  ctx:register_action("mage_polymorph_cc", function(ctx2)
    -- select secondary/non-primary add when need_cc (skip current target)
    local cur = bot.get_target and bot.get_target() or 0
    if bot.get_nearby_units then
      local units = bot.get_nearby_units(30) or {}
      for _, u in ipairs(units) do
        if u and u.is_alive and not u.is_player and u.guid and u.guid ~= cur then
          if bot.can_cast and bot.can_cast(SPELLS.POLYMORPH, u.guid) then
            utils.log_decision("mage: polymorph cc (add)")
            return bot.cast_spell(SPELLS.POLYMORPH, u.guid)
          end
        end
      end
    end
    -- fallback to cur if no add
    if cur ~= 0 and bot.can_cast(SPELLS.POLYMORPH, cur) then
      utils.log_decision("mage: polymorph cc")
      return bot.cast_spell(SPELLS.POLYMORPH, cur)
    end
    return false
  end)

  ctx:register_action("mage_flamestrike", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.is_spell_ready(SPELLS.FLAMESTRIKE) then
      utils.log_decision("mage: flamestrike")
      return bot.cast_spell(SPELLS.FLAMESTRIKE, t)
    end
    return false
  end)

  utils.log_decision("mage class registered (fire/frost + generic)")
end

M.SPELLS = SPELLS
M.MageGeneric = MageGeneric
M.FireStrategy = FireStrategy
M.FrostStrategy = FrostStrategy

return M
