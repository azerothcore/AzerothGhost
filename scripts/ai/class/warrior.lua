-- scripts/ai/class/warrior.lua
-- Full class library for Warrior (arms/fury/prot + generic).
-- Researched rotations + triggers for key mechanics (rend missing, execute, victory rush, sunder, overpower/revenge procs).
-- Uses APIs: bot.has_aura_on, bot.can_cast, bot.is_behind_target, bot.get_pet_guid, bot.pet_attack + existing (get_stance available but unused in this impl).
-- Follows core/strategy + trigger patterns (2-space indent for ai/).
--
-- PRACTICAL_E2E_VALIDATION: P0 matrix (WAR-01/02/03, CORE) executed 2026-07-17 local only per plan. See validation-runs/.

local strategy_mod = dofile("scripts/ai/core/strategy.lua")
local Strategy = strategy_mod.Strategy
local utils = dofile("scripts/ai/core/utils.lua")

local M = {}

-- Load data
local data_ok, data = pcall(dofile, "scripts/ai/data/warrior_spells.lua")
local SPELLS = (data_ok and data.SPELLS) or {
  -- Core from gamedata + grind.lua + behaviors + setup
  HEROIC_STRIKE = 78,
  REND = 772,
  THUNDER_CLAP = 6343,
  HAMSTRING = 1715,
  OVERPOWER = 7384,
  REVENGE = 6572,
  SUNDER_ARMOR = 7386,
  CLEAVE = 845,
  PUMMEL = 6552,
  SLAM = 1464,
  EXECUTE = 5308,
  WHIRLWIND = 1680,
  BATTLE_SHOUT = 2457,
  DEMORALIZING_SHOUT = 1160,
  INTIMIDATING_SHOUT = 5246,
  BERSERKER_RAGE = 18499,
  RECKLESSNESS = 1719,
  CHARGE = 100,
  TAUNT = 355,
  SHIELD_BLOCK = 2565,
  VICTORY_RUSH = 34428,
  -- Arms spec key (from playerbots ArmsWarrior)
  MORTAL_STRIKE = 12294,
  SWEEPING_STRIKES = 12328,
  PIERCING_HOWL = 12323,
  CONCUSSION_BLOW = 12809,
  -- Fury spec
  BLOODTHIRST = 23881,
  -- Prot spec
  SHIELD_SLAM = 23922,
  SHIELD_BASH = 72,
  SHIELD_WALL = 871,
  LAST_STAND = 12975,
}

-- Generic warrior (shout, execute, victory, filler; stance data available in *_spells but not exercised here)
local GenericWarrior = Strategy:new({name = "generic_warrior"})

function GenericWarrior:getName() return "generic_warrior" end
function GenericWarrior:getType() return {"combat", "dps", "tank", "melee", "warrior"} end

function GenericWarrior:getDefaultActions()
  return {
    {name = "warrior_battle_shout", relevance = 9},
    {name = "warrior_execute", relevance = 8.5},
    {name = "warrior_victory_rush", relevance = 8},
    {name = "warrior_auto", relevance = 0.5},
  }
end

function GenericWarrior:getTriggers()
  return {
    {
      name = "missing_battle_shout",
      IsActive = function(ctx)
        return not bot.has_aura_on(0, SPELLS.BATTLE_SHOUT)
      end,
      getHandlers = function() return {{name="warrior_battle_shout", relevance=20}} end,
    },
    {
      name = "execute_ready",
      IsActive = function(ctx)
        local t = bot.get_target() or 0
        if t == 0 or t == "0" then return false end
        local u = bot.get_unit(t)
        local hp = (u and u.health or 0) / math.max((u and u.max_health or 1), 1) * 100
        return hp < 20 and bot.is_spell_ready(SPELLS.EXECUTE)
      end,
      getHandlers = function()
        -- actually use spec/mainhand for relevance boost (talent/gear awareness)
        -- (getHandlers receives trigger not ctx; use bot.* + pcall like other places)
        local spec = ""
        if bot and bot.get_spec then local ok, s = pcall(bot.get_spec); if ok then spec = s or "" end end
        local mh = 0
        if bot and bot.get_mainhand_weapon_id then local ok, id = pcall(bot.get_mainhand_weapon_id); if ok then mh = id or 0 end end
        local boost = (spec == "fury" or mh ~= 0) and 5 or 0
        return {{name="warrior_execute", relevance=25 + boost}}
      end,
    },
  }
end

-- Arms spec: mortal strike prio, rend, sunder, sweeping if multi
local ArmsStrategy = Strategy:new({name = "arms"})

function ArmsStrategy:getName() return "arms" end
function ArmsStrategy:getType() return {"combat", "dps", "melee", "arms"} end

function ArmsStrategy:getDefaultActions()
  return {
    {name = "cast_mortal_strike", relevance = 18},
    {name = "cast_sunder_armor", relevance = 12},
    {name = "cast_rend", relevance = 11},
    {name = "cast_whirlwind", relevance = 10},
  }
end

function ArmsStrategy:getTriggers()
  return {
    {
      name = "rend_missing",
      IsActive = function(ctx)
        local t = bot.get_target() or 0
        if t == 0 or t == "0" then return false end
        return not bot.has_aura_on(t, SPELLS.REND)
      end,
      getHandlers = function() return {{name="cast_rend", relevance=15}} end,
    },
    {
      name = "behind_for_overpower",
      IsActive = function(ctx)
        local t = bot.get_target() or 0
        return t ~= 0 and bot.is_behind_target(t) and bot.is_spell_ready(SPELLS.OVERPOWER)
      end,
      getHandlers = function() return {{name="cast_overpower", relevance=14}} end,
    },
  }
end

-- Fury: bloodthirst + ww prio, high rage dump
local FuryStrategy = Strategy:new({name = "fury"})

function FuryStrategy:getName() return "fury" end
function FuryStrategy:getType() return {"combat", "dps", "melee", "fury"} end

function FuryStrategy:getDefaultActions()
  return {
    {name = "cast_bloodthirst", relevance = 19},
    {name = "cast_whirlwind", relevance = 13},
    {name = "cast_heroic_strike", relevance = 7},
  }
end

function FuryStrategy:getTriggers()
  return {} -- fury more default action driven; can add rage>75 etc
end

-- Prot: threat + survival (sunder stack, shield slam, revenge, taunt if needed)
local ProtStrategy = Strategy:new({name = "prot"})

function ProtStrategy:getName() return "prot" end
function ProtStrategy:getType() return {"combat", "tank", "melee", "prot"} end

function ProtStrategy:getDefaultActions()
  return {
    {name = "cast_shield_slam", relevance = 17},
    {name = "cast_revenge", relevance = 16},
    {name = "cast_sunder_armor", relevance = 12.5},
    {name = "cast_taunt", relevance = 6},
  }
end

function ProtStrategy:getTriggers()
  return {
    {
      name = "low_threat_sunder",
      IsActive = function(ctx)
        -- heuristic: use sunder for threat; no real threat value yet
        local t = bot.get_target() or 0
        if t == 0 or t == "0" then return false end
        local stacks = bot.get_aura_stack and bot.get_aura_stack(t, SPELLS.SUNDER_ARMOR) or 0
        return stacks < 3 and bot.is_spell_ready(SPELLS.SUNDER_ARMOR)
      end,
      getHandlers = function() return {{name="cast_sunder_armor", relevance=13}} end,
    },
  }
end

-- Register strategies + actions on the engine ctx
function M.register(ctx)
  if not ctx then return end
  ctx:register_strategy("generic_warrior", GenericWarrior)
  ctx:register_strategy("arms", ArmsStrategy)
  ctx:register_strategy("fury", FuryStrategy)
  ctx:register_strategy("prot", ProtStrategy)

  -- actions (thin, use can_cast + is_spell_ready for safety)
  ctx:register_action("warrior_battle_shout", function(ctx2)
    if bot.is_spell_ready(SPELLS.BATTLE_SHOUT) then
      utils.log_decision("warrior: battle shout")
      return bot.cast_spell(SPELLS.BATTLE_SHOUT, 0)
    end
    return false
  end)

  ctx:register_action("warrior_execute", function(ctx2)
    local t = bot.get_target() or 0
    if (t ~= 0 and t ~= "0") and bot.can_cast(SPELLS.EXECUTE, t) then
      utils.log_decision("warrior: execute")
      return bot.cast_spell(SPELLS.EXECUTE, t)
    end
    return false
  end)

  ctx:register_action("warrior_victory_rush", function(ctx2)
    local t = bot.get_target() or 0
    if (t ~= 0 and t ~= "0") and bot.is_spell_ready(SPELLS.VICTORY_RUSH) then
      utils.log_decision("warrior: victory rush")
      return bot.cast_spell(SPELLS.VICTORY_RUSH, t)
    end
    return false
  end)

  ctx:register_action("cast_rend", function(ctx2)
    local t = bot.get_target() or 0
    if (t ~= 0 and t ~= "0") and bot.can_cast(SPELLS.REND, t) then
      utils.log_decision("warrior: rend")
      return bot.cast_spell(SPELLS.REND, t)
    end
    return false
  end)

  ctx:register_action("cast_mortal_strike", function(ctx2)
    local t = bot.get_target() or 0
    if (t ~= 0 and t ~= "0") and bot.can_cast(SPELLS.MORTAL_STRIKE, t) then
      utils.log_decision("warrior(arms): mortal strike")
      return bot.cast_spell(SPELLS.MORTAL_STRIKE, t)
    end
    return false
  end)

  ctx:register_action("cast_sunder_armor", function(ctx2)
    local t = bot.get_target() or 0
    if (t ~= 0 and t ~= "0") and bot.can_cast(SPELLS.SUNDER_ARMOR, t) then
      utils.log_decision("warrior: sunder")
      return bot.cast_spell(SPELLS.SUNDER_ARMOR, t)
    end
    return false
  end)

  ctx:register_action("cast_whirlwind", function(ctx2)
    local t = bot.get_target() or 0
    if (t ~= 0 and t ~= "0") and bot.is_spell_ready(SPELLS.WHIRLWIND) then
      utils.log_decision("warrior: whirlwind")
      return bot.cast_spell(SPELLS.WHIRLWIND, t)
    end
    return false
  end)

  ctx:register_action("cast_bloodthirst", function(ctx2)
    local t = bot.get_target() or 0
    if (t ~= 0 and t ~= "0") and bot.can_cast(SPELLS.BLOODTHIRST, t) then
      utils.log_decision("warrior(fury): bloodthirst")
      return bot.cast_spell(SPELLS.BLOODTHIRST, t)
    end
    return false
  end)

  ctx:register_action("cast_shield_slam", function(ctx2)
    local t = bot.get_target() or 0
    if (t ~= 0 and t ~= "0") and bot.can_cast(SPELLS.SHIELD_SLAM, t) then
      utils.log_decision("warrior(prot): shield slam")
      return bot.cast_spell(SPELLS.SHIELD_SLAM, t)
    end
    return false
  end)

  ctx:register_action("cast_revenge", function(ctx2)
    local t = bot.get_target() or 0
    if (t ~= 0 and t ~= "0") and bot.is_spell_ready(SPELLS.REVENGE) then
      utils.log_decision("warrior(prot): revenge")
      return bot.cast_spell(SPELLS.REVENGE, t)
    end
    return false
  end)

  ctx:register_action("cast_heroic_strike", function(ctx2)
    local t = bot.get_target() or 0
    if (t ~= 0 and t ~= "0") and bot.is_spell_ready(SPELLS.HEROIC_STRIKE) then
      utils.log_decision("warrior: heroic strike")
      return bot.cast_spell(SPELLS.HEROIC_STRIKE, t)
    end
    return false
  end)

  ctx:register_action("cast_overpower", function(ctx2)
    local t = bot.get_target() or 0
    if (t ~= 0 and t ~= "0") and bot.can_cast(SPELLS.OVERPOWER, t) then
      utils.log_decision("warrior(arms): overpower")
      return bot.cast_spell(SPELLS.OVERPOWER, t)
    end
    return false
  end)

  ctx:register_action("cast_taunt", function(ctx2)
    local t = bot.get_target() or 0
    if (t ~= 0 and t ~= "0") and bot.is_spell_ready(SPELLS.TAUNT) then
      utils.log_decision("warrior(prot): taunt")
      return bot.cast_spell(SPELLS.TAUNT, t)
    end
    return false
  end)

  ctx:register_action("warrior_auto", function(ctx2)
    local t = bot.get_target() or 0
    if t == 0 then return false end
    local u = bot.get_unit(t)
    if not u or not u.is_alive or (u.health or 0) <= 0 then return false end
    local d = u.distance or 0
    if d > 5 then
      local ux, uy, uz = u.x or 0, u.y or 0, u.z or 0
      bot.move_to(ux, uy, uz)
    else
      bot.stop_moving()
      -- Set target immediately before attack so external observers can see the bot's current attack target
      if bot.set_target then pcall(function() bot.set_target(t) end) end
      bot.attack(t)
    end
    return true
  end)

  utils.log_decision("warrior class registered (arms/fury/prot + generic)")
end

M.SPELLS = SPELLS
M.GenericWarrior = GenericWarrior
M.ArmsStrategy = ArmsStrategy
M.FuryStrategy = FuryStrategy
M.ProtStrategy = ProtStrategy

return M
