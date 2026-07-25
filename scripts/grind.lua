-- grind.lua — reliable single-bot grind loop (3.3.5a / AzerothCore).
--
-- Lessons from live validation timelines:
--   * SPELL_FAILED_NO_POWER (85)  — do not cast rage spenders without rage
--   * SPELL_FAILED_UNIT_NOT_INFRONT (134) — always face before swing/cast
--   * set_target spam — only re-select when the GUID changes
--   * Heroic Strike is next-melee: throttle + rage gate; auto-attack is the filler
--   * 2457 is Battle Stance, not Battle Shout (shout spell ranks start at 6673)
--
-- Usage:
--   ./azghost --profile local-ac cli --bot-mode lua --lua-script scripts/grind.lua

------------------------------------------------------------------------
-- Spell data (level-1-safe ranks where possible)
------------------------------------------------------------------------
local SPELL = {
  HEROIC_STRIKE = 78,     -- next melee, 15 rage (r1)
  REND          = 772,    -- 10 rage
  CHARGE        = 100,    -- free, requires out of combat + range
  EXECUTE       = 5308,   -- 15 rage + dumps; target <20%
  BATTLE_SHOUT  = 6673,   -- real Battle Shout r1 (NOT 2457 stance)
  VICTORY_RUSH  = 34428,  -- free after kill proc
  SUNDER_ARMOR  = 7386,   -- 15 rage
  THUNDER_CLAP  = 6343,   -- 20 rage, AoE
  HAMSTRING     = 1715,   -- 10 rage
}

-- Minimum rage before we attempt the cast (rank-1 costs; safer than is_spell_ready alone).
local RAGE_COST = {
  [SPELL.HEROIC_STRIKE] = 15,
  [SPELL.REND]          = 10,
  [SPELL.EXECUTE]       = 15,
  [SPELL.BATTLE_SHOUT]  = 10,
  [SPELL.SUNDER_ARMOR]  = 15,
  [SPELL.THUNDER_CLAP]  = 20,
  [SPELL.HAMSTRING]     = 10,
  [SPELL.CHARGE]        = 0,
  [SPELL.VICTORY_RUSH]  = 0,
}

-- Friendly / non-attackable filters (aligned with scripts/ai/init.lua grind)
local FRIENDLY_FACTIONS = {
  [35]=true, [11]=true, [12]=true, [13]=true, [55]=true, [57]=true, [59]=true, [60]=true,
  [4]=true, [5]=true, [6]=true, [161]=true, [162]=true,
}

------------------------------------------------------------------------
-- Tunables
------------------------------------------------------------------------
local MELEE_IN      = 3.2   -- stop + swing inside this
local MELEE_OUT     = 5.0   -- resume chase beyond this (hysteresis)
local SCAN_RANGE    = 35
local CHARGE_MIN    = 8
local CHARGE_MAX    = 25
local CAST_GCD      = 1.4   -- client-side GCD throttle (seconds)
local LOOT_COOLDOWN = 3.0
local WANDER_PERIOD = 4.0
local WANDER_RADIUS = 28
local RESELECT_DIST = 12    -- re-pick if current is much worse / missing
local SCAN_LOG_PERIOD = 5.0

------------------------------------------------------------------------
-- State
------------------------------------------------------------------------
local current_target = "0"
local last_cast_at   = 0
local last_loot_at   = 0
local last_loot_guid = "0"
local last_wander_at = 0
local last_shout_at  = 0
local last_scan_log  = 0
local engaged        = false  -- true once we started swinging this pull

------------------------------------------------------------------------
-- Helpers
------------------------------------------------------------------------
local function now()
  return os.clock()
end

local function guid_str(g)
  if g == nil or g == 0 or g == "0" or g == "" then
    return "0"
  end
  return tostring(g)
end

local function is_zero(g)
  return guid_str(g) == "0"
end

local function rage()
  local cur, maxp = bot.get_power()
  cur = cur or 0
  maxp = maxp or 100
  -- Some sessions report 0 max until first update; treat as 100 for warriors.
  if maxp == 0 then maxp = 100 end
  return cur, maxp
end

local function health_pct_self()
  local h, m = bot.get_health()
  h, m = h or 0, m or 1
  if m <= 0 then return 100 end
  return (h / m) * 100
end

local function unit_hp_pct(u)
  if not u then return 100 end
  local h, m = u.health or 0, u.max_health or 0
  if m <= 0 then return 100 end
  return (h / m) * 100
end

local function non_attackable(u)
  local flags = u.flags or 0
  local npc = u.npc_flags or 0
  if npc ~= 0 then return true end
  -- UNIT_FLAG bits (common AC masks)
  if (flags % 4) >= 2 then return true end                 -- NON_ATTACKABLE 0x2
  if (flags % 256) >= 128 then return true end             -- NOT_ATTACKABLE_1 0x80
  if (flags % 512) >= 256 then return true end             -- IMMUNE_TO_PC 0x100
  if (flags % 131072) >= 65536 then return true end        -- NON_ATTACKABLE_2 0x10000
  if (flags % 2097152) >= 1048576 then return true end     -- TAXI 0x100000
  if (flags % 33554432) >= 16777216 then return true end   -- NOT_SELECTABLE 0x2000000
  local fac = u.faction or 0
  if FRIENDLY_FACTIONS[fac] then return true end
  return false
end

local function is_valid_grind_target(u, my_level)
  if not u then return false end
  if u.is_player then return false end
  if not u.is_alive then return false end
  if (u.health or 0) <= 0 then return false end
  if non_attackable(u) then return false end
  local lvl = u.level or 1
  -- Skip greys that give no XP only if much lower; allow +3 over us
  if lvl > my_level + 3 then return false end
  if lvl + 6 < my_level then return false end
  local d = u.distance or 999
  if d <= 0 or d > SCAN_RANGE then return false end
  return true
end

local function find_best_target()
  local units = bot.get_nearby_units(SCAN_RANGE) or {}
  local my_level = bot.get_level() or 1
  local best, best_score = nil, 1e9
  for _, u in ipairs(units) do
    if is_valid_grind_target(u, my_level) then
      local d = u.distance or 999
      local lvl_diff = math.abs((u.level or 1) - my_level)
      -- Prefer close + similar level
      local score = d + lvl_diff * 2.5
      if score < best_score then
        best, best_score = u, score
      end
    end
  end
  return best
end

local function ensure_target(guid)
  guid = guid_str(guid)
  if is_zero(guid) then return false end
  local cur = guid_str(bot.get_target and bot.get_target() or 0)
  if cur ~= guid then
    bot.set_target(guid)
  end
  current_target = guid
  return true
end

local function prepare_melee(guid)
  if bot.set_sheath then pcall(function() bot.set_sheath(0) end) end
  if bot.face_target then pcall(function() bot.face_target(guid) end) end
end

--- Try cast once if known/ready, enough rage, and GCD free.
local function try_cast(spell_id, target_guid, opts)
  opts = opts or {}
  if not spell_id or spell_id == 0 then return false end
  if not bot.is_spell_ready or not bot.is_spell_ready(spell_id) then
    return false
  end
  local need = RAGE_COST[spell_id] or 0
  local r = rage()
  if r < need then
    return false
  end
  local t = now()
  if (t - last_cast_at) < CAST_GCD and not opts.ignore_gcd then
    return false
  end
  if target_guid and not is_zero(target_guid) then
    prepare_melee(target_guid)
  end
  local ok = bot.cast_spell(spell_id, target_guid or 0)
  -- cast_spell returns bool on success of send; still mark GCD to avoid spam
  last_cast_at = t
  return ok ~= false
end

local function maybe_battle_shout()
  local t = now()
  if (t - last_shout_at) < 30 then return end
  local has = false
  if bot.has_aura_on and bot.get_own_guid then
    local own = bot.get_own_guid()
    has = bot.has_aura_on(own, SPELL.BATTLE_SHOUT)
  end
  if has then
    last_shout_at = t
    return
  end
  if try_cast(SPELL.BATTLE_SHOUT, 0) then
    last_shout_at = t
  end
end

local function combat_rotation(guid, dist)
  guid = guid_str(guid)
  local u = bot.get_unit and bot.get_unit(guid) or nil
  if not u or not u.is_alive or (u.health or 0) <= 0 then
    return "dead"
  end

  prepare_melee(guid)

  -- Chase with hysteresis so we don't thrash stop/move every tick
  if dist > MELEE_OUT or (dist > MELEE_IN and not engaged) then
    bot.move_to(u.x or 0, u.y or 0, u.z or 0)
    return "chase"
  end

  bot.stop_moving()
  engaged = true

  -- Start auto-attack once; Go side skips re-swing if already attacking this GUID.
  if bot.attack then
    bot.attack(guid)
  end

  local thp = unit_hp_pct(u)
  local r = rage()

  -- Execute window
  if thp < 20 and r >= 15 then
    if try_cast(SPELL.EXECUTE, guid) then return "cast" end
  end

  -- Free proc
  if try_cast(SPELL.VICTORY_RUSH, guid) then return "cast" end

  -- Keep Rend up
  local has_rend = bot.has_aura_on and bot.has_aura_on(guid, SPELL.REND)
  if not has_rend and r >= 10 then
    if try_cast(SPELL.REND, guid) then return "cast" end
  end

  -- Dump rage on Heroic Strike only when we have surplus (avoid NO_POWER spam)
  -- Keep a buffer so Rend/Execute still have rage.
  if r >= 40 then
    if try_cast(SPELL.HEROIC_STRIKE, guid) then return "cast" end
  end

  -- Sunder when we still have moderate rage
  if r >= 25 then
    if try_cast(SPELL.SUNDER_ARMOR, guid) then return "cast" end
  end

  return "melee"
end

local function loot_corpse(guid)
  guid = guid_str(guid)
  local t = now()
  if guid == last_loot_guid and (t - last_loot_at) < LOOT_COOLDOWN then
    return
  end
  if (t - last_loot_at) < 1.0 then
    return
  end
  if bot.loot_all then
    bot.loot_all(guid)
  end
  last_loot_at = t
  last_loot_guid = guid
end

local function wander()
  local t = now()
  if (t - last_wander_at) < WANDER_PERIOD then return end
  last_wander_at = t
  local x, y, z = bot.get_position()
  x, y, z = x or 0, y or 0, z or 0
  -- Deterministic-ish offset from clock
  local a = (t * 12.9898) % 6.28318
  local dist = 12 + (t * 7) % WANDER_RADIUS
  local nx = x + math.cos(a) * dist
  local ny = y + math.sin(a) * dist
  bot.move_to(nx, ny, z)
  if bot.log then bot.log(string.format("wander -> (%.0f,%.0f)", nx, ny)) end
end

local function clear_target()
  current_target = "0"
  engaged = false
  if bot.set_target then bot.set_target(0) end
end

------------------------------------------------------------------------
-- Main tick (~200ms)
------------------------------------------------------------------------
function on_tick()
  if not bot.is_alive or not bot.is_alive() then
    if bot.send_command then bot.send_command(".revive") end
    clear_target()
    return
  end

  maybe_battle_shout()

  -- Resolve sticky target
  local tgt = guid_str(bot.get_target and bot.get_target() or 0)
  if is_zero(tgt) then
    tgt = current_target
  end

  local u = nil
  if not is_zero(tgt) then
    u = bot.get_unit and bot.get_unit(tgt) or nil
  end

  -- Dead / invalid sticky target → loot then clear
  if not is_zero(tgt) and (not u or not u.is_alive or (u.health or 0) <= 0) then
    loot_corpse(tgt)
    clear_target()
    tgt, u = "0", nil
  end

  -- Acquire if needed
  if is_zero(tgt) or not u then
    local best = find_best_target()
    if best then
      tgt = guid_str(best.guid)
      ensure_target(tgt)
      u = best
      engaged = false
    else
      if not bot.in_combat or not bot.in_combat() then
        local t = now()
        if (t - last_scan_log) >= SCAN_LOG_PERIOD then
          last_scan_log = t
          if bot.log then bot.log("grind: no valid targets in range, wandering") end
        end
        wander()
      end
      return
    end
  end

  ensure_target(tgt)
  local dist = (u and u.distance) or 999

  -- Out of combat pull logic
  if not bot.in_combat or not bot.in_combat() then
    -- Charge when in band and not already engaged
    if dist >= CHARGE_MIN and dist <= CHARGE_MAX then
      prepare_melee(tgt)
      if try_cast(SPELL.CHARGE, tgt) then
        engaged = true
        return
      end
    end
    if dist > MELEE_OUT then
      bot.move_to(u.x or 0, u.y or 0, u.z or 0)
      return
    end
  end

  local result = combat_rotation(tgt, dist)
  if result == "dead" then
    loot_corpse(tgt)
    clear_target()
  end
end
