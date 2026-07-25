-- scripts/ai/class/hunter.lua
-- Hunter class lib, BM focus on pet management + sting + shot prio.
-- Triggers: no_pet, pet_dead (stub), serpent_sting_missing, aspect_missing.
-- Uses: bot.get_pet_guid, bot.pet_attack, bot.has_aura_on, bot.can_cast.

local strategy_mod = dofile("scripts/ai/core/strategy.lua")
local Strategy = strategy_mod.Strategy
local utils = dofile("scripts/ai/core/utils.lua")

local M = {}

local data_ok, data = pcall(dofile, "scripts/ai/data/hunter_spells.lua")
local SPELLS = (data_ok and data.SPELLS) or {
  RAPTOR_STRIKE = 2973,
  AUTO_SHOT = 75,
  CALL_PET = 883,
  REVIVE_PET = 982,
  DISMISS_PET = 2641,
  FEED_PET = 6991,
  CONCUSSIVE_SHOT = 5116,
  FREEZING_TRAP = 1499,
  VOLLEY = 1510,
  ARCANE_SHOT = 3044,
  SERPENT_STING = 1978,
  STEADY_SHOT = 56641,
  AIMED_SHOT = 19434,
  KILL_COMMAND = 34026,
  MULTI_SHOT = 2643,
  -- aspects
  ASPECT_HAWK = 13165,
  ASPECT_MONKEY = 13163,
  ASPECT_VIPER = 34074,
  ASPECT_DRAGONHAWK = 61846,
  -- pet mend
  MEND_PET = 136,
  GROWL = 2649,
}

local HunterGeneric = Strategy:new({name = "hunter_generic"})

function HunterGeneric:getName() return "hunter_generic" end
function HunterGeneric:getType() return {"combat", "dps", "ranged", "hunter"} end

function HunterGeneric:getDefaultActions()
  return {
    {name = "hunter_pet_attack", relevance = 7},
    {name = "hunter_serpent_sting", relevance = 10},
    {name = "hunter_arcane_shot", relevance = 8},
    {name = "hunter_raptor", relevance = 6},
    -- pet
    {name = "hunter_feed_pet", relevance = 4},
  }
end

function HunterGeneric:getTriggers()
  return {
    {
      name = "serpent_sting_missing",
      IsActive = function(ctx)
        local t = bot.get_target() or 0
        if t == 0 then return false end
        return not bot.has_aura_on(t, SPELLS.SERPENT_STING)
      end,
      getHandlers = function() return {{name="hunter_serpent_sting", relevance=14}} end,
    },
    {
      name = "no_aspect",
      IsActive = function(ctx)
        return not bot.has_aura_on(0, SPELLS.ASPECT_HAWK)
      end,
      getHandlers = function() return {{name="hunter_aspect_hawk", relevance=5}} end,
    },
    -- pet threat gen light
    {
      name = "pet_can_threat",
      IsActive = function(ctx)
        local pg = bot.get_pet_guid and bot.get_pet_guid() or 0
        local th = (ctx and ctx.get_value and ctx:get_value("threat")) or 0
        return pg ~= 0 and th < 30
      end,
      getHandlers = function() return {{name="hunter_pet_cast_growl", relevance=9}} end,
    },
  }
end

-- BM specific (pet focus) -- full pet mgmt strats (happiness proxy, revive, feed, modes, cast_pet, threat awareness)
local BeastMastery = Strategy:new({name = "beast_mastery"})

function BeastMastery:getName() return "beast_mastery" end
function BeastMastery:getType() return {"combat", "dps", "ranged", "bm", "pet"} end

function BeastMastery:getDefaultActions()
  return {
    {name = "hunter_call_pet", relevance = 30}, -- high to ensure pet
    {name = "hunter_mend_pet", relevance = 9},
    {name = "hunter_pet_attack", relevance = 8},
    {name = "hunter_kill_command", relevance = 12},
    {name = "hunter_pet_defensive", relevance = 5}, -- pet mode
  }
end

function BeastMastery:getTriggers()
  return {
    {
      name = "no_pet",
      IsActive = function(ctx)
        local pg = bot.get_pet_guid and bot.get_pet_guid() or 0
        return pg == 0
      end,
      getHandlers = function() return {{name="hunter_call_pet", relevance=35}} end,
    },
    {
      name = "pet_dead_or_low",
      IsActive = function(ctx)
        local exists = ctx.get_value and ctx:get_value("pet_exists")
        local php = ctx.get_value and ctx:get_value("pet_health_pct") or 100
        return not exists or php <= 0 or php < 30
      end,
      getHandlers = function() return {{name="hunter_revive_pet", relevance=32}, {name="hunter_mend_pet", relevance=20}} end,
    },
  }
end

function M.register(ctx)
  if not ctx then return end
  ctx:register_strategy("hunter_generic", HunterGeneric)
  ctx:register_strategy("beast_mastery", BeastMastery)

  ctx:register_action("hunter_call_pet", function(ctx2)
    -- guard: do not spam call if pet exists (fixes unconditional default high-rel)
    local exists = (ctx2 and ctx2.get_value and ctx2:get_value("pet_exists")) or (bot.get_pet_guid and bot.get_pet_guid() or 0) ~= 0
    if exists then return false end
    if bot.is_spell_ready(SPELLS.CALL_PET) then
      utils.log_decision("hunter: call pet")
      return bot.cast_spell(SPELLS.CALL_PET, 0)
    end
    return false
  end)

  ctx:register_action("hunter_pet_attack", function(ctx2)
    local t = bot.get_target() or 0
    local pg = bot.get_pet_guid and bot.get_pet_guid() or 0
    if t ~= 0 and pg ~= 0 then
      utils.log_decision("hunter: pet attack")
      bot.pet_attack(t)
      return true
    end
    return false
  end)

  ctx:register_action("hunter_serpent_sting", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.can_cast(SPELLS.SERPENT_STING, t) then
      utils.log_decision("hunter: serpent sting")
      return bot.cast_spell(SPELLS.SERPENT_STING, t)
    end
    return false
  end)

  ctx:register_action("hunter_arcane_shot", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.can_cast(SPELLS.ARCANE_SHOT, t) then
      utils.log_decision("hunter: arcane shot")
      return bot.cast_spell(SPELLS.ARCANE_SHOT, t)
    end
    return false
  end)

  ctx:register_action("hunter_raptor", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.is_spell_ready(SPELLS.RAPTOR_STRIKE) then
      utils.log_decision("hunter: raptor strike")
      return bot.cast_spell(SPELLS.RAPTOR_STRIKE, t)
    end
    return false
  end)

  ctx:register_action("hunter_mend_pet", function(ctx2)
    local pg = bot.get_pet_guid and bot.get_pet_guid() or 0
    if pg ~= 0 and bot.is_spell_ready(SPELLS.MEND_PET) then
      utils.log_decision("hunter: mend pet")
      return bot.cast_spell(SPELLS.MEND_PET, pg)
    end
    return false
  end)

  -- full pet: revive, feed (happiness proxy via low hp or always feed), pet modes, pet spell (e.g. growl if known)
  ctx:register_action("hunter_revive_pet", function(ctx2)
    if bot.is_spell_ready(SPELLS.REVIVE_PET) then
      utils.log_decision("hunter: revive pet")
      return bot.cast_spell(SPELLS.REVIVE_PET, 0)
    end
    return false
  end)

  ctx:register_action("hunter_feed_pet", function(ctx2)
    if bot.is_spell_ready(SPELLS.FEED_PET) then
      local php = (ctx2.get_value and ctx2:get_value("pet_health_pct")) or 100
      utils.log_decision("hunter: feed pet (happiness proxy hp="..math.floor(php).."%)")
      return bot.cast_spell(SPELLS.FEED_PET, 0)
    end
    return false
  end)

  ctx:register_action("hunter_pet_defensive", function(ctx2)
    local pg = bot.get_pet_guid and bot.get_pet_guid() or 0
    if pg ~= 0 and bot.pet_passive then
      utils.log_decision("hunter: pet defensive mode")
      bot.pet_follow() -- pragmatic: follow as defensive proxy (real would toggle state)
      return true
    end
    return false
  end)

  ctx:register_action("hunter_pet_cast_growl", function(ctx2) -- example pet spell cast
    local pg = bot.get_pet_guid and bot.get_pet_guid() or 0
    local t = bot.get_target() or 0
    if pg ~= 0 and t ~= 0 and bot.cast_pet_spell then
      utils.log_decision("hunter: cast pet growl (threat gen)")
      bot.cast_pet_spell(SPELLS.GROWL, t)
      return true
    end
    return false
  end)

  ctx:register_action("hunter_aspect_hawk", function(ctx2)
    if bot.is_spell_ready(SPELLS.ASPECT_HAWK) then
      utils.log_decision("hunter: aspect hawk")
      return bot.cast_spell(SPELLS.ASPECT_HAWK, 0)
    end
    return false
  end)

  ctx:register_action("hunter_kill_command", function(ctx2)
    local t = bot.get_target() or 0
    if t ~= 0 and bot.is_spell_ready(SPELLS.KILL_COMMAND) then
      utils.log_decision("hunter(bm): kill command")
      return bot.cast_spell(SPELLS.KILL_COMMAND, t)
    end
    return false
  end)

  utils.log_decision("hunter class registered (bm + generic)")
end

M.SPELLS = SPELLS
M.HunterGeneric = HunterGeneric
M.BeastMastery = BeastMastery

return M
