-- scripts/ai/class/priest.lua
-- Priest: shadow/holy/disc . Shadow dots + mf, holy heals, disc shield/penance.
-- Heuristics: shadowform aura for spec detect.

local strategy_mod = dofile("scripts/ai/core/strategy.lua")
local Strategy = strategy_mod.Strategy
local utils = dofile("scripts/ai/core/utils.lua")

local M = {}

local data_ok, data = pcall(dofile, "scripts/ai/data/priest_spells.lua")
local SPELLS = (data_ok and data.SPELLS) or {
  SMITE = 585, HEAL = 2050, FLASH_HEAL = 2061, RENEW = 139, POWER_WORD_SHIELD = 17,
  SHADOW_WORD_PAIN = 589, MIND_BLAST = 8092, MIND_FLAY = 15407, SHADOWFORM = 15473,
  PENANCE = 47540,
}

local PriestGeneric = Strategy:new({name = "priest_generic"})

function PriestGeneric:getName() return "priest_generic" end
function PriestGeneric:getType() return {"combat", "healer", "dps", "ranged", "priest"} end

function PriestGeneric:getDefaultActions()
  return {
    {name = "priest_smite", relevance = 9},
    {name = "priest_renew", relevance = 8},
    {name = "priest_shield", relevance = 7},
  }
end

function PriestGeneric:getTriggers()
  return {
    {
      name = "swp_missing",
      IsActive = function(ctx)
        local t = bot.get_target() or 0
        if t == 0 then return false end
        return not bot.has_aura_on(t, SPELLS.SHADOW_WORD_PAIN)
      end,
      getHandlers = function() return {{name="priest_swp", relevance=13}} end,
    },
    {
      name = "low_health_heal",
      IsActive = function(ctx)
        local hp = ctx:get_value("health_pct") or 100
        return hp < 50 and bot.is_spell_ready(SPELLS.FLASH_HEAL)
      end,
      getHandlers = function() return {{name="priest_flash_heal", relevance=20}} end,
    },
  }
end

-- Shadow
local Shadow = Strategy:new({name = "shadow"})

function Shadow:getName() return "shadow" end
function Shadow:getType() return {"combat", "dps", "ranged", "shadow"} end

function Shadow:getDefaultActions()
  return {
    {name = "priest_mind_flay", relevance = 15},
    {name = "priest_mind_blast", relevance = 14},
    {name = "priest_swp", relevance = 12},
  }
end

function Shadow:getTriggers()
  return {
    {
      name = "enter_shadowform",
      IsActive = function(ctx)
        return not bot.has_aura_on(0, SPELLS.SHADOWFORM) and bot.is_spell_ready(SPELLS.SHADOWFORM)
      end,
      getHandlers = function() return {{name="priest_shadowform", relevance=25}} end,
    },
  }
end

-- Holy
local HolyPriest = Strategy:new({name = "holy_priest"})

function HolyPriest:getName() return "holy_priest" end
function HolyPriest:getType() return {"combat", "healer", "priest"} end

function HolyPriest:getDefaultActions()
  return {
    {name = "priest_flash_heal", relevance = 16},
    {name = "priest_renew", relevance = 11},
  }
end

-- Discipline
local Discipline = Strategy:new({name = "discipline"})

function Discipline:getName() return "discipline" end
function Discipline:getType() return {"combat", "healer", "disc"} end

function Discipline:getDefaultActions()
  return {
    {name = "priest_shield", relevance = 17},
    {name = "priest_penance", relevance = 15},
  }
end

function M.register(ctx)
  if not ctx then return end
  ctx:register_strategy("priest_generic", PriestGeneric)
  ctx:register_strategy("shadow", Shadow)
  ctx:register_strategy("holy_priest", HolyPriest)
  ctx:register_strategy("discipline", Discipline)

  ctx:register_action("priest_smite", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.can_cast(SPELLS.SMITE, t) then
      utils.log_decision("priest: smite")
      return bot.cast_spell(SPELLS.SMITE, t)
    end
    return false
  end)

  ctx:register_action("priest_swp", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.can_cast(SPELLS.SHADOW_WORD_PAIN, t) then
      utils.log_decision("priest: swp")
      return bot.cast_spell(SPELLS.SHADOW_WORD_PAIN, t)
    end
    return false
  end)

  ctx:register_action("priest_mind_flay", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.can_cast(SPELLS.MIND_FLAY, t) then
      utils.log_decision("priest(shadow): mind flay")
      return bot.cast_spell(SPELLS.MIND_FLAY, t)
    end
    return false
  end)

  ctx:register_action("priest_mind_blast", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.is_spell_ready(SPELLS.MIND_BLAST) then
      utils.log_decision("priest(shadow): mind blast")
      return bot.cast_spell(SPELLS.MIND_BLAST, t)
    end
    return false
  end)

  ctx:register_action("priest_flash_heal", function(ctx2)
    local t = bot.get_target() or 0
    if bot.is_spell_ready(SPELLS.FLASH_HEAL) then
      utils.log_decision("priest: flash heal")
      local u = (t ~= 0 and bot.get_unit) and bot.get_unit(t) or nil
      local tgt = (u and u.is_alive) and t or 0
      return bot.cast_spell(SPELLS.FLASH_HEAL, tgt)
    end
    return false
  end)

  ctx:register_action("priest_renew", function(ctx2)
    if bot.is_spell_ready(SPELLS.RENEW) then
      utils.log_decision("priest: renew")
      return bot.cast_spell(SPELLS.RENEW, 0)
    end
    return false
  end)

  ctx:register_action("priest_shield", function(ctx2)
    if bot.is_spell_ready(SPELLS.POWER_WORD_SHIELD) then
      utils.log_decision("priest: pw shield")
      return bot.cast_spell(SPELLS.POWER_WORD_SHIELD, 0)
    end
    return false
  end)

  ctx:register_action("priest_penance", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.is_spell_ready(SPELLS.PENANCE) then
      utils.log_decision("priest(disc): penance")
      return bot.cast_spell(SPELLS.PENANCE, t)
    end
    return false
  end)

  ctx:register_action("priest_shadowform", function(ctx2)
    if bot.is_spell_ready(SPELLS.SHADOWFORM) then
      utils.log_decision("priest(shadow): shadowform")
      return bot.cast_spell(SPELLS.SHADOWFORM, 0)
    end
    return false
  end)

  utils.log_decision("priest class registered (shadow/holy/disc + generic)")
end

M.SPELLS = SPELLS
M.PriestGeneric = PriestGeneric
M.Shadow = Shadow
M.HolyPriest = HolyPriest
M.Discipline = Discipline

return M
