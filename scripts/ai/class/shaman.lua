-- scripts/ai/class/shaman.lua
-- Shaman: resto/ele/enh .
-- Shocks, lb, totems, heals, stormstrike, maelstrom implied by prio.
-- Uses power_type for ele vs enh somewhat. (guards per prior patterns)

local strategy_mod = dofile("scripts/ai/core/strategy.lua")
local Strategy = strategy_mod.Strategy
local utils = dofile("scripts/ai/core/utils.lua")

local M = {}

local data_ok, data = pcall(dofile, "scripts/ai/data/shaman_spells.lua")
local SPELLS = (data_ok and data.SPELLS) or {
  LIGHTNING_BOLT = 403, CHAIN_LIGHTNING = 421, EARTH_SHOCK = 8042, FLAME_SHOCK = 8050,
  HEALING_WAVE = 331, CHAIN_HEAL = 1064, STORMSTRIKE = 17364,
  STRENGTH_OF_EARTH_TOTEM = 8071, HEALING_STREAM_TOTEM = 5394,
}

local ShamanGeneric = Strategy:new({name = "shaman_generic"})

function ShamanGeneric:getName() return "shaman_generic" end
function ShamanGeneric:getType() return {"combat", "dps", "healer", "ranged", "melee", "shaman"} end

function ShamanGeneric:getDefaultActions()
  return {
    {name = "shaman_lightning_bolt", relevance = 10},
    {name = "shaman_earth_shock", relevance = 8},
    {name = "shaman_healing_wave", relevance = 6},
    {name = "shaman_totem", relevance = 1},
  }
end

function ShamanGeneric:getTriggers()
  return {
    {
      name = "flame_shock_missing",
      IsActive = function(ctx)
        local t = bot.get_target() or 0
        if t == 0 then return false end
        return not bot.has_aura_on(t, SPELLS.FLAME_SHOCK)
      end,
      getHandlers = function() return {{name="shaman_flame_shock", relevance=12}} end,
    },
  }
end

-- Elemental
local Elemental = Strategy:new({name = "elemental"})

function Elemental:getName() return "elemental" end
function Elemental:getType() return {"combat", "dps", "ranged", "ele"} end

function Elemental:getDefaultActions()
  return {
    {name = "shaman_lightning_bolt", relevance = 16},
    {name = "shaman_chain_lightning", relevance = 13},
    {name = "shaman_lava_burst", relevance = 15},
  }
end

function Elemental:getTriggers()
  return {
    {
      name = "shock_prio",
      IsActive = function(ctx)
        local t = bot.get_target() or 0
        return t ~= 0 and bot.is_spell_ready(SPELLS.EARTH_SHOCK)
      end,
      getHandlers = function() return {{name="shaman_earth_shock", relevance=14}} end,
    },
  }
end

-- Enhancement
local Enhancement = Strategy:new({name = "enhancement"})

function Enhancement:getName() return "enhancement" end
function Enhancement:getType() return {"combat", "dps", "melee", "enh"} end

function Enhancement:getDefaultActions()
  return {
    {name = "shaman_stormstrike", relevance = 18},
    {name = "shaman_earth_shock", relevance = 11},
    {name = "shaman_lightning_bolt", relevance = 7},
  }
end

-- Resto
local RestoShaman = Strategy:new({name = "resto_shaman"})

function RestoShaman:getName() return "resto_shaman" end
function RestoShaman:getType() return {"combat", "healer", "shaman"} end

function RestoShaman:getDefaultActions()
  return {
    {name = "shaman_chain_heal", relevance = 15},
    {name = "shaman_healing_wave", relevance = 13},
  }
end

function RestoShaman:getTriggers()
  return {
    {
      name = "low_group_heal",
      IsActive = function(ctx)
        local hp = ctx:get_value("health_pct") or 100
        return hp < 65
      end,
      getHandlers = function() return {{name="shaman_healing_wave", relevance=20}} end,
    },
  }
end

function M.register(ctx)
  if not ctx then return end
  ctx:register_strategy("shaman_generic", ShamanGeneric)
  ctx:register_strategy("elemental", Elemental)
  ctx:register_strategy("enhancement", Enhancement)
  ctx:register_strategy("resto_shaman", RestoShaman)

  ctx:register_action("shaman_lightning_bolt", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.can_cast(SPELLS.LIGHTNING_BOLT, t) then
      utils.log_decision("shaman: lightning bolt")
      return bot.cast_spell(SPELLS.LIGHTNING_BOLT, t)
    end
    return false
  end)

  ctx:register_action("shaman_earth_shock", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.can_cast(SPELLS.EARTH_SHOCK, t) then
      utils.log_decision("shaman: earth shock")
      return bot.cast_spell(SPELLS.EARTH_SHOCK, t)
    end
    return false
  end)

  ctx:register_action("shaman_flame_shock", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.can_cast(SPELLS.FLAME_SHOCK, t) then
      utils.log_decision("shaman: flame shock")
      return bot.cast_spell(SPELLS.FLAME_SHOCK, t)
    end
    return false
  end)

  ctx:register_action("shaman_chain_lightning", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.is_spell_ready(SPELLS.CHAIN_LIGHTNING) then
      utils.log_decision("shaman(ele): chain lightning")
      return bot.cast_spell(SPELLS.CHAIN_LIGHTNING, t)
    end
    return false
  end)

  ctx:register_action("shaman_lava_burst", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.is_spell_ready(SPELLS.LAVA_BURST or SPELLS.FLAME_SHOCK) then
      utils.log_decision("shaman(ele): lava burst")
      return bot.cast_spell(SPELLS.LAVA_BURST or SPELLS.FLAME_SHOCK, t)
    end
    return false
  end)

  ctx:register_action("shaman_stormstrike", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.is_spell_ready(SPELLS.STORMSTRIKE) then
      utils.log_decision("shaman(enh): stormstrike")
      return bot.cast_spell(SPELLS.STORMSTRIKE, t)
    end
    return false
  end)

  ctx:register_action("shaman_healing_wave", function(ctx2)
    if bot.is_spell_ready(SPELLS.HEALING_WAVE) then
      utils.log_decision("shaman: healing wave")
      return bot.cast_spell(SPELLS.HEALING_WAVE, 0)
    end
    return false
  end)

  ctx:register_action("shaman_chain_heal", function(ctx2)
    if bot.is_spell_ready(SPELLS.CHAIN_HEAL) then
      utils.log_decision("shaman(resto): chain heal")
      return bot.cast_spell(SPELLS.CHAIN_HEAL, 0)
    end
    return false
  end)

  ctx:register_action("shaman_totem", function(ctx2)
    if bot.is_spell_ready(SPELLS.STRENGTH_OF_EARTH_TOTEM) then
      utils.log_decision("shaman: strength totem")
      return bot.cast_spell(SPELLS.STRENGTH_OF_EARTH_TOTEM, 0)
    end
    return false
  end)

  utils.log_decision("shaman class registered (resto/ele/enh + generic)")
end

M.SPELLS = SPELLS
M.ShamanGeneric = ShamanGeneric
M.Elemental = Elemental
M.Enhancement = Enhancement
M.RestoShaman = RestoShaman

return M
