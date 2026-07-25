-- scripts/lib/behaviors.lua
-- Deep generic class combat behaviors.
-- Research from playerbots: each class has dedicated Ai/Class/XXX/ with Actions, Triggers, Strategy.
-- Here we provide realistic priority-based rotations per class for level 80.
-- Uses bot.get_class(), power, spell ready, etc.
-- Generic and reusable.

local M = {}

local CLASS = { WARRIOR=1, PALADIN=2, HUNTER=3, ROGUE=4, PRIEST=5, DK=6, SHAMAN=7, MAGE=8, WARLOCK=9, DRUID=11 }

-- Helper
local function find_target(dist)
  if find_nearby_enemy then return find_nearby_enemy(dist or 30) end
  local units = bot.get_nearby_units(dist or 30)
  for _, u in ipairs(units) do
    if u.is_alive and not u.is_player then return u end
  end
  return nil
end

local function warrior_dps()
  local t = find_target(8)
  if not t then return end
  bot.set_target(t.guid)
  local hp = bot.get_health and select(1, bot.get_health()) or 100
  if bot.is_spell_ready(5308) and hp < 20 then bot.cast_spell(5308, t.guid) -- Execute
  elseif bot.is_spell_ready(1680) then bot.cast_spell(1680, t.guid) -- Whirlwind
  elseif bot.is_spell_ready(845) then bot.cast_spell(845, t.guid) -- Cleave
  elseif bot.is_spell_ready(772) then bot.cast_spell(772, t.guid) -- Rend
  elseif bot.is_spell_ready(7386) then bot.cast_spell(7386, t.guid) -- Sunder
  else 
    if bot.set_target then pcall(function() bot.set_target(t.guid) end) end
    bot.attack(t.guid) 
  end
end

local function hunter()
  local t = find_target(35)
  if not t then return end
  bot.set_target(t.guid)
  if bot.is_spell_ready(2973) then bot.cast_spell(2973, t.guid) -- Raptor
  else 
    if bot.set_target then pcall(function() bot.set_target(t.guid) end) end
    bot.attack(t.guid) 
  end
  -- Pet: bot.send_command(".cast 883") if needed, but assume summoned in setup
end

local function rogue()
  local t = find_target(5)
  if not t then return end
  bot.set_target(t.guid)
  if bot.is_spell_ready(1752) then bot.cast_spell(1752, t.guid) -- Sinister Strike
  elseif bot.is_spell_ready(2098) then bot.cast_spell(2098, t.guid) -- Evis
  else 
    if bot.set_target then pcall(function() bot.set_target(t.guid) end) end
    bot.attack(t.guid) 
  end
end

local function mage()
  local t = find_target(30)
  if not t then return end
  bot.set_target(t.guid)
  if bot.is_spell_ready(133) then bot.cast_spell(133, t.guid) -- Fireball
  elseif bot.is_spell_ready(116) then bot.cast_spell(116, t.guid)
  else 
    if bot.set_target then pcall(function() bot.set_target(t.guid) end) end
    bot.attack(t.guid) 
  end
end

-- Expand with research from playerbots Class/ dirs (full rotations for other classes can be added similarly)
local BEHAVIORS = {
  [CLASS.WARRIOR] = warrior_dps,
  [CLASS.HUNTER] = hunter,
  [CLASS.ROGUE] = rogue,
  [CLASS.MAGE] = mage,
  -- Add Paladin (ret), DK (blood/frost), etc. using similar priority from their Actions
}

function M.on_tick()
  local class = bot.get_class and bot.get_class() or 0
  local fn = BEHAVIORS[class]
  if fn then fn() else
    local t = find_target(25)
    if t then bot.set_target(t.guid); bot.attack(t.guid) end
  end
end

function M.register(classID, fn) BEHAVIORS[classID] = fn end

return M
