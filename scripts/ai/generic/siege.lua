-- scripts/ai/generic/siege.lua
-- PvP / Siege specific strategies: faction-aware target selection, healer support, focus fire.
-- Designed for mass battle in Orgrimmar (prefer opposite faction players, healers/casters, etc.).

local strategy_mod = dofile("scripts/ai/core/strategy.lua")
local Strategy = strategy_mod.Strategy
local trigger_mod = dofile("scripts/ai/core/trigger.lua")
local utils = dofile("scripts/ai/core/utils.lua")

local M = {}

-- Helper: determine if a unit is an enemy player of opposite faction.
-- Faction templates rough: Alliance-ish 1-5/11-12-ish, Horde 2/5/6/8/10 common.
-- Prefer using bot.is_enemy / unit.faction when available from Go side.
local function is_enemy_player(u, my_faction)
  if not u or not u.is_player then return false end
  if u.is_alive == false or (u.health or 0) <= 0 then return false end
  if u.guid == (bot.get_guid and bot.get_guid()) then return false end

  -- If engine exposes a direct helper (preferred)
  if bot.is_enemy and bot.is_enemy(u.guid) then return true end
  if bot.is_enemy_player and bot.is_enemy_player(u.guid) then return true end

  local fac = u.faction or 0
  local myf = my_faction or (bot.get_faction and bot.get_faction()) or 0

  -- Simple faction hostility heuristic (expand as needed)
  local alliance = {1,3,4,7,11,12,55,57,59,60}
  local horde    = {2,5,6,8,10,14,15,16}
  local function is_alliance(f) for _,v in ipairs(alliance) do if v==f then return true end end return false end
  local function is_horde(f)    for _,v in ipairs(horde) do if v==f then return true end end return false end

  if is_alliance(myf) and is_horde(fac) then return true end
  if is_horde(myf) and is_alliance(fac) then return true end
  if fac ~= myf and (is_alliance(fac) or is_horde(fac)) then return true end
  return false
end

local function is_ally_player(u, my_faction)
  if not u or not u.is_player then return false end
  if u.is_alive == false or (u.health or 0) <= 0 then return false end
  local fac = u.faction or 0
  local myf = my_faction or (bot.get_faction and bot.get_faction()) or 0
  return fac == myf or not is_enemy_player(u, myf)
end

local function find_nearby_enemy_players(dist, my_faction)
  dist = dist or 40
  if not bot.get_nearby_players and not bot.get_nearby_units then return {} end
  local units = (bot.get_nearby_players and bot.get_nearby_players(dist)) or (bot.get_nearby_units and bot.get_nearby_units(dist)) or {}
  local enemies = {}
  for _, u in ipairs(units) do
    if is_enemy_player(u, my_faction) then
      table.insert(enemies, u)
    end
  end
  return enemies
end

local function find_lowest_hp_ally(dist, my_faction)
  dist = dist or 40
  if not bot.get_nearby_players and not bot.get_nearby_units then return nil end
  local units = (bot.get_nearby_players and bot.get_nearby_players(dist)) or (bot.get_nearby_units and bot.get_nearby_units(dist)) or {}
  local best, best_hp = nil, 101
  for _, u in ipairs(units) do
    if is_ally_player(u, my_faction) then
      local hp = ((u.health or 0) / math.max(u.max_health or 1, 1)) * 100
      if hp < best_hp then best, best_hp = u, hp end
    end
  end
  return best
end

-- Siege / PvP target selector action (higher relevance for players)
local function select_siege_target(ctx)
  local myf = (bot.get_faction and bot.get_faction()) or 0
  local enemies = find_nearby_enemy_players(45, myf)
  if #enemies == 0 then return false end

  -- Score: low hp first, then healers/casters (heuristic by class or recent if tracked), proximity
  table.sort(enemies, function(a, b)
    local ha = ((a.health or 100) / math.max(a.max_health or 1,1)) * 100
    local hb = ((b.health or 100) / math.max(b.max_health or 1,1)) * 100
    if math.abs(ha - hb) > 15 then return ha < hb end
    -- prefer non-tanks / casters lightly
    local ca = (a.class or 0); local cb = (b.class or 0)
    local healer_bonus_a = (ca==5 or ca==2 or ca==7 or ca==11) and 1 or 0
    local healer_bonus_b = (cb==5 or cb==2 or cb==7 or cb==11) and 1 or 0
    if healer_bonus_a ~= healer_bonus_b then return healer_bonus_a > healer_bonus_b end
    return (a.distance or 999) < (b.distance or 999)
  end)

  local best = enemies[1]
  if best and best.guid then
    bot.set_target(best.guid)
    if bot.face_target then pcall(function() bot.face_target(best.guid) end) end
    utils.log_decision("siege target player " .. tostring(best.guid) .. " hp=" .. math.floor(((best.health or 0)/math.max(best.max_health or 1,1))*100))
    if (best.distance or 0) > 8 then
      bot.move_to(best.x, best.y, best.z)
    else
      bot.stop_moving()
      bot.attack(best.guid)
    end
    return true
  end
  return false
end

local SiegeStrategy = Strategy:new({name = "siege"})

function SiegeStrategy:getName() return "siege" end
function SiegeStrategy:getType() return {"generic", "pvp", "siege"} end

function SiegeStrategy:getDefaultActions()
  return {
    {name = "select_siege_target", relevance = 55}, -- high priority vs grind
    {name = "heal_lowest_ally", relevance = 40},
  }
end

function SiegeStrategy:getTriggers()
  return {
    trigger_mod.Trigger:new({
      name = "enemy_player_near",
      IsActive = function(ctx)
        local myf = (bot.get_faction and bot.get_faction()) or 0
        return #find_nearby_enemy_players(50, myf) > 0
      end,
      getHandlers = function()
        return {{name = "select_siege_target", relevance = 70}}
      end,
    }),
  }
end

-- Register action for engine use
-- (engine will pick up via register_action in init or class if desired)

M.SiegeStrategy = SiegeStrategy
M.find_nearby_enemy_players = find_nearby_enemy_players
M.find_lowest_hp_ally = find_lowest_hp_ally
M.is_enemy_player = is_enemy_player
M.select_siege_target = select_siege_target

return M
