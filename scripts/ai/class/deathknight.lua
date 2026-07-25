-- scripts/ai/class/deathknight.lua
-- Death Knight: blood/unholy/frost.
-- Runes, presences (stance like via aura), death strike, oblit, scourge, grip.
-- Spec heur: presence auras + high spells. Uses t~=0 guards + pcall patterns from prior classes.

local strategy_mod = dofile("scripts/ai/core/strategy.lua")
local Strategy = strategy_mod.Strategy
local utils = dofile("scripts/ai/core/utils.lua")

local M = {}

local data_ok, data = pcall(dofile, "scripts/ai/data/deathknight_spells.lua")
local SPELLS = (data_ok and data.SPELLS) or {
  ICY_TOUCH = 45477, PLAGUE_STRIKE = 45462, BLOOD_STRIKE = 45902, DEATH_COIL = 47541,
  DEATH_STRIKE = 49998, OBLITERATE = 49020, SCOURGE_STRIKE = 55090, DEATH_GRIP = 49576,
  HORN_OF_WINTER = 57330, BLOOD_PRESENCE = 48266, FROST_PRESENCE = 48263, UNHOLY_PRESENCE = 48265,
}

local DKGeneric = Strategy:new({name = "dk_generic"})

function DKGeneric:getName() return "dk_generic" end
function DKGeneric:getType() return {"combat", "dps", "tank", "melee", "dk"} end

function DKGeneric:getDefaultActions()
  return {
    {name = "dk_horn", relevance = 9},
    {name = "dk_icy_touch", relevance = 8},
    {name = "dk_plague_strike", relevance = 8},
    {name = "dk_death_coil", relevance = 6},
    {name = "dk_grip", relevance = 1},
  }
end

function DKGeneric:getTriggers()
  return {
    {
      name = "horn_missing",
      IsActive = function(ctx)
        return not bot.has_aura_on(0, SPELLS.HORN_OF_WINTER)
      end,
      getHandlers = function() return {{name="dk_horn", relevance=16}} end,
    },
  }
end

-- Blood (tank/dps)
local Blood = Strategy:new({name = "blood"})

function Blood:getName() return "blood" end
function Blood:getType() return {"combat", "tank", "dps", "blood"} end

function Blood:getDefaultActions()
  return {
    {name = "dk_death_strike", relevance = 16},
    {name = "dk_heart_strike", relevance = 14},
    {name = "dk_blood_boil", relevance = 10},
  }
end

function Blood:getTriggers()
  return {
    {
      name = "blood_presence",
      IsActive = function(ctx)
        return not bot.has_aura_on(0, SPELLS.BLOOD_PRESENCE) and bot.is_spell_ready(SPELLS.BLOOD_PRESENCE)
      end,
      getHandlers = function() return {{name="dk_blood_presence", relevance=22}} end,
    },
  }
end

-- Frost
local FrostDK = Strategy:new({name = "frost_dk"})

function FrostDK:getName() return "frost_dk" end
function FrostDK:getType() return {"combat", "dps", "melee", "frost"} end

function FrostDK:getDefaultActions()
  return {
    {name = "dk_obliterate", relevance = 18},
    {name = "dk_frost_strike", relevance = 13},
    {name = "dk_howling_blast", relevance = 11},
  }
end

-- Unholy
local Unholy = Strategy:new({name = "unholy"})

function Unholy:getName() return "unholy" end
function Unholy:getType() return {"combat", "dps", "melee", "unholy"} end

function Unholy:getDefaultActions()
  return {
    {name = "dk_scourge_strike", relevance = 17},
    {name = "dk_death_coil", relevance = 10},
  }
end

function Unholy:getTriggers()
  return {
    {
      name = "unholy_presence",
      IsActive = function(ctx)
        return not bot.has_aura_on(0, SPELLS.UNHOLY_PRESENCE) and bot.is_spell_ready(SPELLS.UNHOLY_PRESENCE)
      end,
      getHandlers = function() return {{name="dk_unholy_presence", relevance=19}} end,
    },
  }
end

function M.register(ctx)
  if not ctx then return end
  ctx:register_strategy("dk_generic", DKGeneric)
  ctx:register_strategy("blood", Blood)
  ctx:register_strategy("frost_dk", FrostDK)
  ctx:register_strategy("unholy", Unholy)

  ctx:register_action("dk_horn", function(ctx2)
    if bot.is_spell_ready(SPELLS.HORN_OF_WINTER) then
      utils.log_decision("dk: horn of winter")
      return bot.cast_spell(SPELLS.HORN_OF_WINTER, 0)
    end
    return false
  end)

  ctx:register_action("dk_icy_touch", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.can_cast(SPELLS.ICY_TOUCH, t) then
      utils.log_decision("dk: icy touch")
      return bot.cast_spell(SPELLS.ICY_TOUCH, t)
    end
    return false
  end)

  ctx:register_action("dk_plague_strike", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.can_cast(SPELLS.PLAGUE_STRIKE, t) then
      utils.log_decision("dk: plague strike")
      return bot.cast_spell(SPELLS.PLAGUE_STRIKE, t)
    end
    return false
  end)

  ctx:register_action("dk_death_coil", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.can_cast(SPELLS.DEATH_COIL, t) then
      utils.log_decision("dk: death coil")
      return bot.cast_spell(SPELLS.DEATH_COIL, t)
    end
    return false
  end)

  ctx:register_action("dk_death_strike", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.is_spell_ready(SPELLS.DEATH_STRIKE) then
      utils.log_decision("dk(blood): death strike")
      return bot.cast_spell(SPELLS.DEATH_STRIKE, t)
    end
    return false
  end)

  ctx:register_action("dk_heart_strike", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.is_spell_ready(SPELLS.HEART_STRIKE or SPELLS.BLOOD_STRIKE) then
      utils.log_decision("dk(blood): heart strike")
      return bot.cast_spell(SPELLS.HEART_STRIKE or SPELLS.BLOOD_STRIKE, t)
    end
    return false
  end)

  ctx:register_action("dk_blood_boil", function(ctx2)
    if bot.is_spell_ready(SPELLS.BLOOD_BOIL) then
      utils.log_decision("dk(blood): blood boil")
      return bot.cast_spell(SPELLS.BLOOD_BOIL, 0)
    end
    return false
  end)

  ctx:register_action("dk_obliterate", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.is_spell_ready(SPELLS.OBLITERATE) then
      utils.log_decision("dk(frost): obliterate")
      return bot.cast_spell(SPELLS.OBLITERATE, t)
    end
    return false
  end)

  ctx:register_action("dk_frost_strike", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.is_spell_ready(SPELLS.FROST_STRIKE or SPELLS.OBLITERATE) then
      utils.log_decision("dk(frost): frost strike")
      return bot.cast_spell(SPELLS.FROST_STRIKE or SPELLS.OBLITERATE, t)
    end
    return false
  end)

  ctx:register_action("dk_scourge_strike", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.is_spell_ready(SPELLS.SCOURGE_STRIKE) then
      utils.log_decision("dk(unholy): scourge strike")
      return bot.cast_spell(SPELLS.SCOURGE_STRIKE, t)
    end
    return false
  end)

  ctx:register_action("dk_howling_blast", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.is_spell_ready(SPELLS.HOWLING_BLAST or SPELLS.ICY_TOUCH) then
      utils.log_decision("dk(frost): howling blast")
      return bot.cast_spell(SPELLS.HOWLING_BLAST or SPELLS.ICY_TOUCH, t)
    end
    return false
  end)

  ctx:register_action("dk_blood_presence", function(ctx2)
    if bot.is_spell_ready(SPELLS.BLOOD_PRESENCE) then
      utils.log_decision("dk: blood presence")
      return bot.cast_spell(SPELLS.BLOOD_PRESENCE, 0)
    end
    return false
  end)

  ctx:register_action("dk_unholy_presence", function(ctx2)
    if bot.is_spell_ready(SPELLS.UNHOLY_PRESENCE) then
      utils.log_decision("dk: unholy presence")
      return bot.cast_spell(SPELLS.UNHOLY_PRESENCE, 0)
    end
    return false
  end)

  ctx:register_action("dk_grip", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.is_spell_ready(SPELLS.DEATH_GRIP) then
      utils.log_decision("dk: death grip")
      return bot.cast_spell(SPELLS.DEATH_GRIP, t)
    end
    return false
  end)

  utils.log_decision("deathknight class registered (blood/unholy/frost + generic)")
end

M.SPELLS = SPELLS
M.DKGeneric = DKGeneric
M.Blood = Blood
M.FrostDK = FrostDK
M.Unholy = Unholy

return M
