-- scripts/ai/core/values.lua
-- Core values (state providers) using existing bot.* API.
-- value caching (per-tick snapshot for perf), threat improvements (use has_aggro_on/get_threat when available).
-- Throttled recompute via engine tick.

local M = {}

-- performance: simple per-tick cache (now per-ctx/engine for isolation; falls back to module if no ctx)
local _module_cache = {}
local _module_last_tick = -1

function M.update_cache(tick, ctx)
  tick = tick or 0
  if ctx then
    if not ctx._value_cache then ctx._value_cache = {} end
    if ctx._value_last_tick == tick then return end
    ctx._value_last_tick = tick
    ctx._value_cache = {}
    return
  end
  if tick == _module_last_tick then return end
  _module_last_tick = tick
  _module_cache = {}
end

local function cached(name, fn, ctx)
  local c = (ctx and ctx._value_cache) or _module_cache
  if c[name] ~= nil then return c[name] end
  local v = fn()
  c[name] = v
  return v
end


local function get_health_pct(ctx)
  return cached("health_pct", function()
    if not bot or not bot.get_health then return 100 end
    local h, mh = bot.get_health()
    return (h or 0) / math.max(mh or 1, 1) * 100
  end, ctx)
end

local function get_power_pct(ctx)
  return cached("power_pct", function()
    if not bot or not bot.get_power then return 100 end
    local p, mp = bot.get_power()
    return (p or 0) / math.max(mp or 1, 1) * 100
  end, ctx)
end

local function get_target_health_pct(ctx)
  return cached("target_health_pct", function()
    if not bot or not bot.get_target then return 100 end
    local tg = bot.get_target()
    if not tg or tg == 0 or tg == "0" then return 100 end
    local u = bot.get_unit(tg)
    if not u then return 100 end
    local th = u.health or 0
    local tmh = u.max_health or 1
    return th / math.max(tmh, 1) * 100
  end, ctx)
end

-- Siege / PvP value helpers (exposed for use by generic/siege.lua and classes)
function M.find_nearby_enemy_players(dist, my_faction)
  dist = dist or 40
  if not bot.get_nearby_players and not bot.get_nearby_units then return {} end
  local units = (bot.get_nearby_players and bot.get_nearby_players(dist)) or (bot.get_nearby_units and bot.get_nearby_units(dist)) or {}
  local enemies = {}
  for _, u in ipairs(units) do
    if u and u.is_player and (u.health or 0) > 0 then
      local fac = u.faction or 0
      local myf = my_faction or (bot.get_faction and bot.get_faction()) or 0
      -- rough opposite faction check (can be overridden by bot.is_enemy if present)
      if bot.is_enemy and bot.is_enemy(u.guid) then
        table.insert(enemies, u)
      elseif bot.is_enemy_player and bot.is_enemy_player(u.guid) then
        table.insert(enemies, u)
      elseif fac ~= myf and fac > 0 then
        table.insert(enemies, u)
      end
    end
  end
  return enemies
end

function M.find_lowest_hp_ally(dist, my_faction)
  dist = dist or 40
  if not bot.get_nearby_players and not bot.get_nearby_units then return nil end
  local units = (bot.get_nearby_players and bot.get_nearby_players(dist)) or (bot.get_nearby_units and bot.get_nearby_units(dist)) or {}
  local best, best_hp = nil, 101
  for _, u in ipairs(units) do
    if u and u.is_player and (u.health or 0) > 0 then
      local fac = u.faction or 0
      local myf = my_faction or (bot.get_faction and bot.get_faction()) or 0
      local is_ally = (fac == myf) or (not (bot.is_enemy and bot.is_enemy(u.guid)))
      if is_ally then
        local hp = ((u.health or 0) / math.max(u.max_health or 1, 1)) * 100
        if hp < best_hp then best, best_hp = u, hp end
      end
    end
  end
  return best
end

function M.get_faction(ctx)
  return cached("my_faction", function()
    if bot.get_faction then return bot.get_faction() end
    return 0
  end, ctx)
end


local function get_distance_to_target(ctx)
  return cached("distance_to_target", function()
    if not bot or not bot.get_target then return 999 end
    local tg = bot.get_target()
    if not tg or tg == 0 or tg == "0" then return 999 end
    local u = bot.get_unit(tg)
    if not u then return 999 end
    return u.distance or 999
  end, ctx)
end

local function get_in_combat(ctx)
  return cached("in_combat", function()
    if not bot or not bot.in_combat then return false end
    return bot.in_combat()
  end, ctx)
end

local function get_is_alive(ctx)
  return cached("is_alive", function()
    if not bot or not bot.is_alive then return false end
    return bot.is_alive()
  end, ctx)
end

local function get_enemy_count(range, ctx)
  range = range or 30
  return cached("enemy_count_"..range, function()
    if not bot or not bot.get_nearby_units then return 0 end
    local units = bot.get_nearby_units(range)
    local cnt = 0
    for _, u in ipairs(units) do
      if u.is_alive and not u.is_player then cnt = cnt + 1 end
    end
    return cnt
  end, ctx)
end

local function get_has_aggro_approx(ctx)
  return cached("has_aggro", function()
    -- improved threat using packets/API if avail (has_aggro_on + get_threat)
    local tg = bot and bot.get_target and bot.get_target() or 0
    if tg == 0 or tg == "0" then return false end
    if bot and bot.has_aggro_on then
      local ok, res = pcall(bot.has_aggro_on, tg)
      if ok and res then return true end
    end
    if bot and bot.get_threat then
      local ok, th = pcall(bot.get_threat, tg)
      if ok and th and th > 50 then return true end
    end
    -- fallback approx
    if not get_in_combat(ctx) then return false end
    return true
  end, ctx)
end

local function get_threat_value(guid, ctx)
  local tg = guid or (bot and bot.get_target and bot.get_target() or 0)
  local key = "threat_" .. tostring(tg or "current")
  return cached(key, function()
    if tg == 0 or tg == "0" or not bot or not bot.get_threat then return 0 end
    local ok, th = pcall(bot.get_threat, tg)
    return (ok and th) or 0
  end, ctx)
end

local function get_power_type(ctx)
  return cached("power_type", function()
    if not bot or not bot.get_power_type then return 0 end
    local ok, pt = pcall(bot.get_power_type)
    return (ok and pt) or 0
  end, ctx)
end

local function get_stance_value(ctx)
  return cached("stance", function()
    if not bot or not bot.get_stance then return 0 end
    local ok, s = pcall(bot.get_stance)
    return (ok and s) or 0
  end, ctx)
end

local function get_is_moving(ctx)
  -- no direct, assume false for basic; could track pos delta in future
  return cached("is_moving", function() return false end, ctx)
end

-- pet values, gear/talent proxies, enhanced threat (for pet mgmt + tank strats + hybrid)
local function get_pet_exists(ctx)
  return cached("pet_exists", function()
    if not bot or not bot.get_pet_guid then return false end
    local ok, pg = pcall(bot.get_pet_guid)
    return ok and pg and pg ~= 0
  end, ctx)
end

local function get_pet_health_pct(ctx)
  return cached("pet_health_pct", function()
    if not bot or not bot.get_pet_health then return 100 end
    local ok, cur, mx = pcall(bot.get_pet_health)
    if not ok or not mx or mx == 0 then return 100 end
    return (cur or 0) / mx * 100
  end, ctx)
end

local function get_spec(ctx)
  return cached("spec", function()
    if not bot or not bot.get_spec then return "" end
    local ok, s = pcall(bot.get_spec)
    return (ok and s) or ""
  end, ctx)
end

local function get_has_shield(ctx)
  return cached("has_shield", function()
    if not bot or not bot.has_shield_equipped then return false end
    local ok, res = pcall(bot.has_shield_equipped)
    return ok and res
  end, ctx)
end

local function get_mainhand(ctx)
  return cached("mainhand", function()
    if not bot or not bot.get_mainhand_weapon_id then return 0 end
    local ok, id = pcall(bot.get_mainhand_weapon_id)
    return (ok and id) or 0
  end, ctx)
end

local function get_threat_list_count(ctx)
  -- light: count of units that may be threatening (using nearby + has_aggro approx)
  -- fix: pcall + guards for consistency with other bot.* in file
  return cached("threat_list_count", function()
    if not bot or not bot.get_nearby_units then return 0 end
    local ok, units = pcall(bot.get_nearby_units, 30)
    if not ok or not units then return 0 end
    local cnt = 0
    for _, u in ipairs(units) do
      if u and u.is_alive and not u.is_player and u.guid then
        -- crude: if unit targets us (via threat api or approx)
        if bot.has_aggro_on then
          local ok2, res = pcall(bot.has_aggro_on, u.guid)
          if ok2 and res then cnt = cnt + 1 end
        end
      end
    end
    return cnt
  end, ctx)
end

function M.get_value(name, ctx)
  local tick = (ctx and ctx._tick) or 0
  M.update_cache(tick, ctx)
  if name == "health_pct" then return get_health_pct(ctx) end
  if name == "power_pct" then return get_power_pct(ctx) end
  if name == "target_health_pct" then return get_target_health_pct(ctx) end
  if name == "distance_to_target" then return get_distance_to_target(ctx) end
  if name == "in_combat" then return get_in_combat(ctx) end
  if name == "is_alive" then return get_is_alive(ctx) end
  if name == "enemy_count" then return get_enemy_count(nil, ctx) end
  if name == "has_aggro" then return get_has_aggro_approx(ctx) end
  if name == "threat" then return get_threat_value(nil, ctx) end
  if name == "power_type" then return get_power_type(ctx) end
  if name == "stance" then return get_stance_value(ctx) end
  if name == "is_moving" then return get_is_moving(ctx) end
  -- advanced
  if name == "pet_exists" then return get_pet_exists(ctx) end
  if name == "pet_health_pct" then return get_pet_health_pct(ctx) end
  if name == "spec" then return get_spec(ctx) end
  if name == "has_shield" then return get_has_shield(ctx) end
  if name == "mainhand" then return get_mainhand(ctx) end
  if name == "threat_list_count" then return get_threat_list_count(ctx) end
  return nil
end


return M
