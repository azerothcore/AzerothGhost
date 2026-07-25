-- scripts/ai/class/druid.lua
-- Druid: balance/feral/resto .
-- Moonfire/wrath for bal, mangle/rip for cat/bear (use get_stance or form auras), heals.
-- Spec detect via form auras + high spells.

local strategy_mod = dofile("scripts/ai/core/strategy.lua")
local Strategy = strategy_mod.Strategy
local utils = dofile("scripts/ai/core/utils.lua")

local M = {}

local data_ok, data = pcall(dofile, "scripts/ai/data/druid_spells.lua")
local SPELLS = (data_ok and data.SPELLS) or {
  WRATH = 5176, MOONFIRE = 8921, STARFIRE = 2912, BEAR_FORM = 5487, CAT_FORM = 768,
  MANGLE_BEAR = 33917, CLAW = 1082, RIP = 1079, RAKE = 1822, HEALING_TOUCH = 5185,
  REJUVENATION = 774, MOONKIN_FORM = 24858, MARK_OF_THE_WILD = 1126,
}

local DruidGeneric = Strategy:new({name = "druid_generic"})

function DruidGeneric:getName() return "druid_generic" end
function DruidGeneric:getType() return {"combat", "dps", "healer", "melee", "ranged", "druid"} end

function DruidGeneric:getDefaultActions()
  return {
    {name = "druid_wrath", relevance = 8},
    {name = "druid_moonfire", relevance = 9},
    {name = "druid_rejuv", relevance = 7},
    {name = "druid_mark", relevance = 6},
  }
end

function DruidGeneric:getTriggers()
  return {
    {
      name = "moonfire_missing",
      IsActive = function(ctx)
        local t = bot.get_target() or 0
        if t == 0 then return false end
        return not bot.has_aura_on(t, SPELLS.MOONFIRE)
      end,
      getHandlers = function() return {{name="druid_moonfire", relevance=13}} end,
    },
    {
      name = "mark_missing",
      IsActive = function(ctx)
        return not bot.has_aura_on(0, SPELLS.MARK_OF_THE_WILD)
      end,
      getHandlers = function() return {{name="druid_mark", relevance=11}} end,
    },
  }
end

-- Balance
local Balance = Strategy:new({name = "balance"})

function Balance:getName() return "balance" end
function Balance:getType() return {"combat", "dps", "ranged", "balance"} end

function Balance:getDefaultActions()
  return {
    {name = "druid_wrath", relevance = 15},
    {name = "druid_starfire", relevance = 14},
    {name = "druid_moonfire", relevance = 12},
  }
end

function Balance:getTriggers()
  return {
    {
      name = "enter_moonkin",
      IsActive = function(ctx)
        local form = ctx:get_value("stance") or 0
        return form ~= 3 and bot.is_spell_ready(SPELLS.MOONKIN_FORM) -- moonkin form id heuristic
      end,
      getHandlers = function() return {{name="druid_moonkin", relevance=20}} end,
    },
  }
end

-- Feral (cat/bear)
local Feral = Strategy:new({name = "feral"})

function Feral:getName() return "feral" end
function Feral:getType() return {"combat", "dps", "tank", "melee", "feral"} end

function Feral:getDefaultActions()
  return {
    {name = "druid_mangle", relevance = 16},
    {name = "druid_rip", relevance = 14},
    {name = "druid_claw", relevance = 12},
  }
end

function Feral:getTriggers()
  return {
    {
      name = "shift_bear_or_cat",
      IsActive = function(ctx)
        local form = ctx:get_value("stance") or 0
        return form == 0 and bot.is_spell_ready(SPELLS.CAT_FORM)
      end,
      getHandlers = function() return {{name="druid_cat_form", relevance=18}} end,
    },
    {
      name = "rip_missing_feral",
      IsActive = function(ctx)
        local t = bot.get_target() or 0
        if t == 0 then return false end
        return not bot.has_aura_on(t, SPELLS.RIP)
      end,
      getHandlers = function() return {{name="druid_rip", relevance=15}} end,
    },
  }
end

-- Resto Druid
local RestoDruid = Strategy:new({name = "resto_druid"})

function RestoDruid:getName() return "resto_druid" end
function RestoDruid:getType() return {"combat", "healer", "druid"} end

function RestoDruid:getDefaultActions()
  return {
    {name = "druid_rejuv", relevance = 16},
    {name = "druid_healing_touch", relevance = 13},
  }
end

function RestoDruid:getTriggers()
  return {
    {
      name = "reju_missing",
      IsActive = function(ctx)
        return not bot.has_aura_on(0, SPELLS.REJUVENATION)
      end,
      getHandlers = function() return {{name="druid_rejuv", relevance=17}} end,
    },
  }
end

function M.register(ctx)
  if not ctx then return end
  ctx:register_strategy("druid_generic", DruidGeneric)
  ctx:register_strategy("balance", Balance)
  ctx:register_strategy("feral", Feral)
  ctx:register_strategy("resto_druid", RestoDruid)

  ctx:register_action("druid_wrath", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.can_cast(SPELLS.WRATH, t) then
      utils.log_decision("druid: wrath")
      return bot.cast_spell(SPELLS.WRATH, t)
    end
    return false
  end)

  ctx:register_action("druid_moonfire", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.can_cast(SPELLS.MOONFIRE, t) then
      utils.log_decision("druid: moonfire")
      return bot.cast_spell(SPELLS.MOONFIRE, t)
    end
    return false
  end)

  ctx:register_action("druid_starfire", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.is_spell_ready(SPELLS.STARFIRE or SPELLS.WRATH) then
      utils.log_decision("druid(balance): starfire")
      return bot.cast_spell(SPELLS.STARFIRE or SPELLS.WRATH, t)
    end
    return false
  end)

  ctx:register_action("druid_mangle", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.is_spell_ready(SPELLS.MANGLE_BEAR or SPELLS.CLAW) then
      utils.log_decision("druid(feral): mangle")
      return bot.cast_spell(SPELLS.MANGLE_BEAR or SPELLS.CLAW, t)
    end
    return false
  end)

  ctx:register_action("druid_rip", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.is_spell_ready(SPELLS.RIP) then
      utils.log_decision("druid(feral): rip")
      return bot.cast_spell(SPELLS.RIP, t)
    end
    return false
  end)

  ctx:register_action("druid_claw", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.is_spell_ready(SPELLS.CLAW) then
      utils.log_decision("druid(feral): claw")
      return bot.cast_spell(SPELLS.CLAW, t)
    end
    return false
  end)

  ctx:register_action("druid_rejuv", function(ctx2)
    if bot.is_spell_ready(SPELLS.REJUVENATION) then
      utils.log_decision("druid: rejuv")
      return bot.cast_spell(SPELLS.REJUVENATION, 0)
    end
    return false
  end)

  ctx:register_action("druid_healing_touch", function(ctx2)
    if bot.is_spell_ready(SPELLS.HEALING_TOUCH) then
      utils.log_decision("druid(resto): healing touch")
      return bot.cast_spell(SPELLS.HEALING_TOUCH, 0)
    end
    return false
  end)

  ctx:register_action("druid_mark", function(ctx2)
    if bot.is_spell_ready(SPELLS.MARK_OF_THE_WILD) then
      utils.log_decision("druid: mark of wild")
      return bot.cast_spell(SPELLS.MARK_OF_THE_WILD, 0)
    end
    return false
  end)

  ctx:register_action("druid_cat_form", function(ctx2)
    if bot.is_spell_ready(SPELLS.CAT_FORM) then
      utils.log_decision("druid(feral): cat form")
      return bot.cast_spell(SPELLS.CAT_FORM, 0)
    end
    return false
  end)

  ctx:register_action("druid_moonkin", function(ctx2)
    if bot.is_spell_ready(SPELLS.MOONKIN_FORM) then
      utils.log_decision("druid(balance): moonkin form")
      return bot.cast_spell(SPELLS.MOONKIN_FORM, 0)
    end
    return false
  end)

  utils.log_decision("druid class registered (balance/feral/resto + generic)")
end

M.SPELLS = SPELLS
M.DruidGeneric = DruidGeneric
M.Balance = Balance
M.Feral = Feral
M.RestoDruid = RestoDruid

return M
