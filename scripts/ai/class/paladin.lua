-- scripts/ai/class/paladin.lua
-- Full class library for Paladin (holy/prot/ret + generic).
-- Researched rotations: seals, judgements, crusader/divine storm, consec, avengers for prot, holy shock/heals.
-- Uses APIs: bot.has_aura_on, bot.can_cast, get_stance (auras for spec), etc.
-- Follows exact warrior/hunter/mage Strategy/Trigger/Action shapes.

local strategy_mod = dofile("scripts/ai/core/strategy.lua")
local Strategy = strategy_mod.Strategy
local utils = dofile("scripts/ai/core/utils.lua")

local M = {}

local data_ok, data = pcall(dofile, "scripts/ai/data/paladin_spells.lua")
local SPELLS = (data_ok and data.SPELLS) or {
  HOLY_LIGHT = 635, FLASH_OF_LIGHT = 19750, JUDGEMENT = 20271, CONSECRATION = 26573,
  CRUSADER_STRIKE = 35395, DIVINE_STORM = 53385, AVENGERS_SHIELD = 31935,
  RIGHTEOUS_FURY = 25780, SEAL_OF_COMMAND = 20375, SEAL_OF_RIGHTEOUSNESS = 21084,
  DEVOTION_AURA = 465, RETRIBUTION_AURA = 7294, BLESSING_OF_MIGHT = 19740,
  HAND_OF_RECKONING = 62124,
}

-- Generic paladin
local GenericPaladin = Strategy:new({name = "generic_paladin"})

function GenericPaladin:getName() return "generic_paladin" end
function GenericPaladin:getType() return {"combat", "dps", "tank", "healer", "melee", "paladin"} end

function GenericPaladin:getDefaultActions()
  return {
    {name = "paladin_blessing_might", relevance = 9},
    {name = "paladin_seal", relevance = 8},
    {name = "paladin_judgement", relevance = 7},
    {name = "paladin_auto", relevance = 0.5},
  }
end

function GenericPaladin:getTriggers()
  return {
    {
      name = "missing_blessing",
      IsActive = function(ctx)
        return not bot.has_aura_on(0, SPELLS.BLESSING_OF_MIGHT)
      end,
      getHandlers = function() return {{name="paladin_blessing_might", relevance=18}} end,
    },
    {
      name = "missing_seal",
      IsActive = function(ctx)
        return not bot.has_aura_on(0, SPELLS.SEAL_OF_RIGHTEOUSNESS) and not bot.has_aura_on(0, SPELLS.SEAL_OF_COMMAND)
      end,
      getHandlers = function() return {{name="paladin_seal", relevance=12}} end,
    },
  }
end

-- Retribution spec
local Retribution = Strategy:new({name = "retribution"})

function Retribution:getName() return "retribution" end
function Retribution:getType() return {"combat", "dps", "melee", "ret"} end

function Retribution:getDefaultActions()
  return {
    {name = "cast_crusader_strike", relevance = 18},
    {name = "cast_divine_storm", relevance = 15},
    {name = "cast_consecration", relevance = 10},
  }
end

function Retribution:getTriggers()
  return {
    {
      name = "judgement_ready",
      IsActive = function(ctx)
        local t = bot.get_target() or 0
        return t ~= 0 and bot.is_spell_ready(SPELLS.JUDGEMENT)
      end,
      getHandlers = function() return {{name="paladin_judgement", relevance=14}} end,
    },
  }
end

-- Protection spec
local Protection = Strategy:new({name = "protection"})

function Protection:getName() return "protection" end
function Protection:getType() return {"combat", "tank", "melee", "prot"} end

function Protection:getDefaultActions()
  return {
    {name = "cast_avengers_shield", relevance = 17},
    {name = "cast_consecration", relevance = 13},
    {name = "paladin_judgement", relevance = 11},
    {name = "paladin_righteous_fury", relevance = 20},
    {name = "paladin_taunt", relevance = 6}, -- light threat mgmt / taunt for prot
  }
end

function Protection:getTriggers()
  return {
    {
      name = "righteous_fury_check",
      IsActive = function(ctx)
        return not bot.has_aura_on(0, SPELLS.RIGHTEOUS_FURY)
      end,
      getHandlers = function() return {{name="paladin_righteous_fury", relevance=25}} end,
    },
    {
      name = "paladin_low_threat",
      IsActive = function(ctx)
        local th = ctx.get_value and ctx:get_value("threat") or 100
        return th < 60 and bot.is_spell_ready and bot.is_spell_ready(SPELLS.HAND_OF_RECKONING)
      end,
      getHandlers = function() return {{name="paladin_taunt", relevance=18}} end,
    },
  }
end

-- Holy spec (heals + some dps)
local Holy = Strategy:new({name = "holy"})

function Holy:getName() return "holy" end
function Holy:getType() return {"combat", "healer", "paladin"} end

function Holy:getDefaultActions()
  return {
    {name = "cast_flash_of_light", relevance = 12},
    {name = "cast_holy_light", relevance = 10},
  }
end

function Holy:getTriggers()
  return {
    {
      name = "low_self_health_holy",
      IsActive = function(ctx)
        local hp = ctx:get_value("health_pct") or 100
        return hp < 60 and bot.is_spell_ready(SPELLS.FLASH_OF_LIGHT)
      end,
      getHandlers = function() return {{name="cast_flash_of_light", relevance=22}} end,
    },
  }
end

-- Register
function M.register(ctx)
  if not ctx then return end
  ctx:register_strategy("generic_paladin", GenericPaladin)
  ctx:register_strategy("retribution", Retribution)
  ctx:register_strategy("protection", Protection)
  ctx:register_strategy("holy", Holy)

  ctx:register_action("paladin_blessing_might", function(ctx2)
    if bot.is_spell_ready(SPELLS.BLESSING_OF_MIGHT) then
      utils.log_decision("paladin: blessing of might")
      return bot.cast_spell(SPELLS.BLESSING_OF_MIGHT, 0)
    end
    return false
  end)

  ctx:register_action("paladin_seal", function(ctx2)
    local seal = SPELLS.SEAL_OF_COMMAND or SPELLS.SEAL_OF_RIGHTEOUSNESS
    if bot.is_spell_ready(seal) then
      utils.log_decision("paladin: seal")
      return bot.cast_spell(seal, 0)
    end
    return false
  end)

  ctx:register_action("paladin_judgement", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.can_cast(SPELLS.JUDGEMENT, t) then
      utils.log_decision("paladin: judgement")
      return bot.cast_spell(SPELLS.JUDGEMENT, t)
    end
    return false
  end)

  ctx:register_action("cast_crusader_strike", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.can_cast(SPELLS.CRUSADER_STRIKE, t) then
      utils.log_decision("paladin(ret): crusader strike")
      return bot.cast_spell(SPELLS.CRUSADER_STRIKE, t)
    end
    return false
  end)

  ctx:register_action("cast_divine_storm", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.is_spell_ready(SPELLS.DIVINE_STORM) then
      utils.log_decision("paladin(ret): divine storm")
      return bot.cast_spell(SPELLS.DIVINE_STORM, t)
    end
    return false
  end)

  ctx:register_action("cast_consecration", function(ctx2)
    if bot.is_spell_ready(SPELLS.CONSECRATION) then
      utils.log_decision("paladin: consecration")
      return bot.cast_spell(SPELLS.CONSECRATION, 0)
    end
    return false
  end)

  ctx:register_action("cast_avengers_shield", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.can_cast(SPELLS.AVENGERS_SHIELD, t) then
      utils.log_decision("paladin(prot): avengers shield")
      return bot.cast_spell(SPELLS.AVENGERS_SHIELD, t)
    end
    return false
  end)

  ctx:register_action("paladin_taunt", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.is_spell_ready(SPELLS.HAND_OF_RECKONING) then
      local th = (ctx2.get_value and ctx2:get_value("threat")) or 0
      utils.log_decision("paladin(prot): taunt (threat=" .. th .. ")")
      return bot.cast_spell(SPELLS.HAND_OF_RECKONING, t)
    end
    return false
  end)

  ctx:register_action("paladin_righteous_fury", function(ctx2)
    if bot.is_spell_ready(SPELLS.RIGHTEOUS_FURY) then
      utils.log_decision("paladin(prot): righteous fury")
      return bot.cast_spell(SPELLS.RIGHTEOUS_FURY, 0)
    end
    return false
  end)

  ctx:register_action("cast_flash_of_light", function(ctx2)
    local t = bot.get_target() or 0
    if bot.is_spell_ready(SPELLS.FLASH_OF_LIGHT) then
      utils.log_decision("paladin(holy): flash of light")
      local u = (t ~= 0 and bot.get_unit) and bot.get_unit(t) or nil
      local tgt = (u and u.is_alive) and t or 0
      return bot.cast_spell(SPELLS.FLASH_OF_LIGHT, tgt)
    end
    return false
  end)

  ctx:register_action("cast_holy_light", function(ctx2)
    if bot.is_spell_ready(SPELLS.HOLY_LIGHT) then
      utils.log_decision("paladin(holy): holy light")
      return bot.cast_spell(SPELLS.HOLY_LIGHT, 0)
    end
    return false
  end)

  ctx:register_action("paladin_auto", function(ctx2)
    local t = bot.get_target() or 0
    if t == 0 then return false end
    local u = bot.get_unit(t)
    if not u or not u.is_alive then return false end
    local d = u.distance or 0
    if d > 5 then
      bot.move_to(u.x or 0, u.y or 0, u.z or 0)
    else
      bot.stop_moving()
      if bot.set_target then pcall(function() bot.set_target(t) end) end
      -- Set target before attacking for visibility to external players
      bot.attack(t)
    end
    return true
  end)

  utils.log_decision("paladin class registered (ret/prot/holy + generic)")
end

M.SPELLS = SPELLS
M.GenericPaladin = GenericPaladin
M.Retribution = Retribution
M.Protection = Protection
M.Holy = Holy

return M
