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
  BATTLE_SHOUT = 6673,
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
    {name = "warrior_charge", relevance = 16},
    {name = "warrior_execute", relevance = 8.5},
    {name = "warrior_victory_rush", relevance = 8},
    {name = "cast_heroic_strike", relevance = 5}, -- rage dump only after higher prio fail
    {name = "warrior_auto", relevance = 0.5},
  }
end

function GenericWarrior:getTriggers()
  return {
    {
      name = "charge_gap",
      IsActive = function(ctx)
        local t = bot.get_target and bot.get_target() or 0
        if t == 0 or t == "0" then return false end
        local u = bot.get_unit and bot.get_unit(t) or nil
        if not u then return false end
        local d = tonumber(u.distance) or 99
        if bot.get_position and u.x ~= nil then
          local px, py = bot.get_position()
          local dx = (tonumber(u.x) or 0) - (px or 0)
          local dy = (tonumber(u.y) or 0) - (py or 0)
          local d2 = math.sqrt(dx * dx + dy * dy)
          if d2 > 0.5 then d = d2 end
        end
        -- Openers and mid-chase gaps (ignore sticky in_combat flag after kills).
        return d >= 8 and d <= 24
      end,
      getHandlers = function() return {{ name = "warrior_charge", relevance = 28 }} end,
    },
    {
      name = "missing_battle_shout",
      IsActive = function(ctx)
        local aura = (data_ok and data.AURAS and data.AURAS.BATTLE_SHOUT) or SPELLS.BATTLE_SHOUT
        local own = bot.get_own_guid and bot.get_own_guid() or 0
        if bot.has_aura_on(own, aura) then return false end
        if own ~= 0 and bot.has_aura_on(0, aura) then return false end
        return true
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
        if hp >= 20 then return false end
        -- Need rage or we only spam CAST_FAILED NO_POWER.
        local rage = 0
        if bot.get_power then
          local cur = bot.get_power()
          rage = tonumber(cur) or 0
        end
        if rage < 15 then return false end
        return bot.is_spell_ready and bot.is_spell_ready(SPELLS.EXECUTE)
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
    {name = "cast_rend", relevance = 14},
    {name = "cast_sunder_armor", relevance = 9}, -- below engage(7)? no 9>7; range-gated so OK
    {name = "cast_whirlwind", relevance = 8},
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

  -- WotLK base rage costs (rank-1). is_spell_ready is NOT enough: AC still
  -- returns ready with 0 rage and then CAST_FAILED NO_POWER (85).
  local RAGE_COST = {
    [SPELLS.BATTLE_SHOUT] = 10,
    [SPELLS.REND] = 10,
    [SPELLS.HEROIC_STRIKE] = 15,
    [SPELLS.SUNDER_ARMOR] = 15,
    [SPELLS.EXECUTE] = 15,
    [SPELLS.THUNDER_CLAP] = 20,
    [SPELLS.CLEAVE] = 20,
    [SPELLS.WHIRLWIND] = 25,
    [SPELLS.MORTAL_STRIKE] = 30,
    [SPELLS.BLOODTHIRST] = 20,
    [SPELLS.SHIELD_SLAM] = 20,
    [SPELLS.REVENGE] = 5,
    [SPELLS.OVERPOWER] = 5,
    [SPELLS.HAMSTRING] = 10,
    [SPELLS.DEMORALIZING_SHOUT] = 10,
    [SPELLS.CHARGE] = 0,
    [SPELLS.VICTORY_RUSH] = 0,
    [SPELLS.TAUNT] = 0,
  }

  local function rage_now()
    if not bot.get_power then return 0 end
    local cur = bot.get_power()
    return tonumber(cur) or 0
  end

  local function can_afford(spell_id)
    local need = RAGE_COST[spell_id]
    if need == nil then need = 0 end
    return rage_now() >= need
  end

  -- After NO_POWER, back off that spell briefly (server power updates lag).
  local function power_blocked(ctx2, spell_id)
    if not ctx2 or not ctx2.get_blackboard then return false end
    local until_t = ctx2:get_blackboard("nopower_" .. tostring(spell_id))
    if not until_t then return false end
    local now = (bot.now_ms and bot.now_ms() / 1000) or os.time()
    return now < until_t
  end

  local function melee_target(max_dist)
    max_dist = max_dist or 5
    local t = bot.get_target() or 0
    if t == 0 or t == "0" then return nil, nil end
    local u = bot.get_unit and bot.get_unit(t) or nil
    if not u or u.is_alive == false then return nil, nil end
    if (u.distance or 99) > max_dist then return nil, nil end
    return t, u
  end

  -- Single gate for all rage abilities. Never cast without enough power.
  local function try_rage_cast(ctx2, spell_id, target, label, opts)
    opts = opts or {}
    if not spell_id or spell_id == 0 then return false end
    if power_blocked(ctx2, spell_id) then return false end
    if not can_afford(spell_id) then return false end
    if bot.is_spell_ready and not bot.is_spell_ready(spell_id) then return false end
    if bot.can_cast and target and target ~= 0 and target ~= "0" then
      if not bot.can_cast(spell_id, target) then return false end
    end
    if opts.face and target and bot.face_target then
      pcall(function() bot.face_target(target) end)
    end
    local r = rage_now()
    utils.log_decision(string.format("%s (rage=%.0f need=%d)", label, r, RAGE_COST[spell_id] or 0))
    bot.cast_spell(spell_id, target or 0)
    -- Do not optimistically mark_nopower here: a range/facing/LOS fail would
    -- mis-block retries. Confirmed NO_POWER is handled server-side
    -- (noteSpellNoPower → is_spell_ready / can_cast).
    return true
  end

  ctx:register_action("warrior_battle_shout", function(ctx2)
    local aura = (data_ok and data.AURAS and data.AURAS.BATTLE_SHOUT) or SPELLS.BATTLE_SHOUT
    local own = bot.get_own_guid and bot.get_own_guid() or 0
    if bot.has_aura_on then
      if own ~= 0 and bot.has_aura_on(own, aura) then return false end
      if bot.has_aura_on(0, aura) then return false end
    end
    local now = (bot.now_ms and bot.now_ms() / 1000) or os.time()
    local last = ctx2.get_blackboard and ctx2:get_blackboard("shout_try_at")
    if last and (now - last) < 20 then return false end
    if try_rage_cast(ctx2, SPELLS.BATTLE_SHOUT, 0, "warrior: battle shout") then
      if ctx2.set_blackboard then ctx2:set_blackboard("shout_try_at", now) end
      return true
    end
    return false
  end)

  ctx:register_action("warrior_charge", function(ctx2)
    local t = bot.get_target() or 0
    if t == 0 or t == "0" then return false end
    local u = bot.get_unit and bot.get_unit(t) or nil
    if not u or u.is_alive == false then return false end

    -- Prefer geometric distance (u.distance can lag).
    local d = tonumber(u.distance) or 99
    if bot.get_position and u.x ~= nil then
      local px, py = bot.get_position()
      local dx = (tonumber(u.x) or 0) - (px or 0)
      local dy = (tonumber(u.y) or 0) - (py or 0)
      local d2 = math.sqrt(dx * dx + dy * dy)
      if d2 > 0.5 then d = d2 end
    end
    -- Charge range ~8–25 yd.
    if d < 8 or d > 24 then return false end

    -- Skip if already in melee brawl (close + fighting) — Charge is for openers.
    if d < 10 and ctx2:get_value("in_combat") then return false end

    local now = (bot.now_ms and bot.now_ms() / 1000) or os.time()
    local key = "charge_try_" .. tostring(t)
    local last = ctx2.get_blackboard and ctx2:get_blackboard(key)
    if last and (now - last) < 2.5 then return false end

    -- Do not hard-require is_spell_ready: after .learn it can lag a few ticks.
    -- Still skip if we know it's on a long CD via no-power/block map.
    if power_blocked(ctx2, SPELLS.CHARGE) then return false end

    -- Stop path + face so AC accepts the cast (moving/pathing often fails Charge).
    if bot.stop_moving then pcall(function() bot.stop_moving() end) end
    if bot.face_target then pcall(function() bot.face_target(t) end) end
    if bot.set_sheath then pcall(function() bot.set_sheath(0) end) end
    if bot.set_target then pcall(function() bot.set_target(t) end) end

    utils.log_decision(string.format("warrior: charge d=%.1f", d))
    if ctx2.set_blackboard then ctx2:set_blackboard(key, now) end
    bot.cast_spell(SPELLS.CHARGE, t)
    -- Consume this tick so we do not repath over the cast.
    return true
  end)

  ctx:register_action("warrior_execute", function(ctx2)
    local t, u = melee_target(5)
    if not t then return false end
    local hp = 100
    if (u.max_health or 0) > 0 then
      hp = ((u.health or 0) / u.max_health) * 100
    end
    if hp >= 20 then return false end
    return try_rage_cast(ctx2, SPELLS.EXECUTE, t, "warrior: execute", { face = true })
  end)

  ctx:register_action("warrior_victory_rush", function(ctx2)
    local t = melee_target(5)
    if not t then return false end
    return try_rage_cast(ctx2, SPELLS.VICTORY_RUSH, t, "warrior: victory rush", { face = true })
  end)

  ctx:register_action("cast_rend", function(ctx2)
    local t = melee_target(8)
    if not t then return false end
    if bot.has_aura_on and bot.has_aura_on(t, SPELLS.REND) then return false end
    return try_rage_cast(ctx2, SPELLS.REND, t, "warrior: rend", { face = true })
  end)

  ctx:register_action("cast_mortal_strike", function(ctx2)
    local t = melee_target(5)
    if not t then return false end
    return try_rage_cast(ctx2, SPELLS.MORTAL_STRIKE, t, "warrior(arms): mortal strike", { face = true })
  end)

  ctx:register_action("cast_sunder_armor", function(ctx2)
    local t = melee_target(5)
    if not t then return false end
    return try_rage_cast(ctx2, SPELLS.SUNDER_ARMOR, t, "warrior: sunder", { face = true })
  end)

  ctx:register_action("cast_whirlwind", function(ctx2)
    local t = melee_target(6)
    if not t then return false end
    return try_rage_cast(ctx2, SPELLS.WHIRLWIND, t, "warrior: whirlwind")
  end)

  ctx:register_action("cast_bloodthirst", function(ctx2)
    local t = melee_target(5)
    if not t then return false end
    return try_rage_cast(ctx2, SPELLS.BLOODTHIRST, t, "warrior(fury): bloodthirst", { face = true })
  end)

  ctx:register_action("cast_shield_slam", function(ctx2)
    local t = melee_target(5)
    if not t then return false end
    return try_rage_cast(ctx2, SPELLS.SHIELD_SLAM, t, "warrior(prot): shield slam", { face = true })
  end)

  ctx:register_action("cast_revenge", function(ctx2)
    local t = melee_target(5)
    if not t then return false end
    return try_rage_cast(ctx2, SPELLS.REVENGE, t, "warrior(prot): revenge", { face = true })
  end)

  ctx:register_action("cast_heroic_strike", function(ctx2)
    local t = melee_target(5)
    if not t then return false end
    -- Dump only with surplus rage (next-swing; still fails with NO_POWER).
    if rage_now() < 40 then return false end
    return try_rage_cast(ctx2, SPELLS.HEROIC_STRIKE, t, "warrior: heroic strike")
  end)

  ctx:register_action("cast_overpower", function(ctx2)
    local t = melee_target(5)
    if not t then return false end
    return try_rage_cast(ctx2, SPELLS.OVERPOWER, t, "warrior(arms): overpower", { face = true })
  end)

  ctx:register_action("cast_taunt", function(ctx2)
    local t = bot.get_target() or 0
    if t == 0 or t == "0" then return false end
    return try_rage_cast(ctx2, SPELLS.TAUNT, t, "warrior(prot): taunt")
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
