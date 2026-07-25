-- scripts/lib/siege_prep.lua
-- Full realistic preparation for Orgrimmar Siege bots (lvl 80, equipped, talented, consumables).
-- Preferred method: GM commands (works distributed, no direct DB writes on workers).
-- Called from scenario during "prep" phase. Idempotent where possible.
--
-- Usage in scenario:
--   local prep = dofile("scripts/lib/siege_prep.lua")
--   prep.for_siege_bot(scenario_data)
--
-- scenario_data expected keys (injected by orchestrator via AIBundle.Data or scenario_data):
--   faction = "alliance" | "horde"
--   class (optional numeric or string)
--   spec (e.g. "fury", "shadow")
--   role (optional "melee", "ranged", "healer", "tank")
--   start_pos (optional from positions)

local gear_data = dofile("scripts/ai/data/siege_gear.lua")
local talent_data = dofile("scripts/ai/data/siege_talents.lua")
local pos_data = dofile("scripts/ai/data/siege_positions.lua")
local setup = dofile("scripts/lib/setup.lua") -- fallback to base

local M = {}

-- one-time setup flag to avoid spamming .additem .learn .equip every tick while in prep phase
local setup_commands_sent = false

local CLASS_ID = {
  warrior=1, paladin=2, hunter=3, rogue=4, priest=5, dk=6, shaman=7, mage=8, warlock=9, druid=11
}

local function to_class_id(c)
  if type(c) == "number" then return c end
  if type(c) == "string" then return CLASS_ID[string.lower(c)] or 1 end
  return (bot and bot.get_class and bot.get_class()) or 1
end

local function send(cmd)
  -- Prefer guild command when dead (allowed on many servers)
  if not bot.is_alive() and bot.send_guild_command then
    bot.send_guild_command(cmd)
  else
    bot.send_command(cmd)
  end
end

-- Learn a list of spells/talents safely
local function learn_list(ids)
  for _, id in ipairs(ids or {}) do
    send(".learn " .. tostring(id))
  end
end

-- Add items for a list (qty 1 each)
local function add_items(ids, qty)
  qty = qty or 1
  for _, id in ipairs(ids or {}) do
    send(".additem " .. tostring(id) .. " " .. qty)
  end
end

function M.for_siege_bot(data)
  data = data or {}
  local faction = string.lower(data.faction or "horde")
  local cls = to_class_id(data.class)
  local spec = data.spec or data.role or "default"

  bot.log("SIEGE_PREP: start faction=" .. faction .. " class=" .. cls .. " spec=" .. spec)

  send(".gm on")

  -- Always do teleport to gate (with spread) while in prep phase, to ensure we are at the right place.
  local gate = { map=1, x=1368, y=-4373, z=26.057, o=0 }
  local pos_mod_ok, pos_mod = pcall(dofile, "scripts/ai/data/siege_positions.lua")
  local target
  if pos_mod_ok and pos_mod and pos_mod.get_orgrimmar_gate_spread then
    target = pos_mod.get_orgrimmar_gate_spread()
  else
    local spread = 17
    target = {
      map = 1,
      x = gate.x + (math.random() * 2 - 1) * spread,
      y = gate.y + (math.random() * 2 - 1) * spread,
      z = gate.z,
      o = (math.random() * 6.28)
    }
  end
  send(string.format(".go %d %d %d %d",
    math.floor(target.x), math.floor(target.y), math.floor(target.z), math.floor(target.o or 0)))
  bot.log("SIEGE_PREP: teleported near Orgrimmar gate")

  -- One time heavy setup (spells, gear, equip, consumables) to avoid spam while waiting for phase advance.
  if not setup_commands_sent then
    send(".level 80")
    send(".level 80")
    send(".maxskill")
    if bot.set_level then
      bot.set_level(80)
    end

    send(".additem 41599 4")

    local ok, spells_mod = pcall(dofile, "scripts/ai/data/spells.lua")
    if ok and spells_mod and spells_mod.CLASS_SPELLS then
      local spells = spells_mod.CLASS_SPELLS[cls] or {}
      learn_list(spells)
    end

    local talents = talent_data.get_talents(cls, spec)
    learn_list(talents)

    local gear_list = gear_data.get_gear(cls, spec)
    add_items(gear_list, 1)

    for _, iid in ipairs(gear_list) do
      send(".equip " .. tostring(iid))
    end

    local cons = gear_data.get_consumables(cls)
    add_items(cons, 20)

    send(".learn 34090")
    send(".learn 34091")
    send(".additem 34060 1")

    send(".maxskill")
    send(".pvp")

    setup_commands_sent = true
  end

  -- Re-affirm spread position (light, can repeat)
  local spread_pos
  if pos_data and pos_data.get_orgrimmar_gate_spread then
    spread_pos = pos_data.get_orgrimmar_gate_spread()
  else
    local spread = 16
    spread_pos = {
      x = 1368 + (math.random()*2-1)*spread,
      y = -4373 + (math.random()*2-1)*spread,
      z = 26.057,
      o = math.random()*6.28
    }
  end
  send(string.format(".go %d %d %d %d",
    math.floor(spread_pos.x), math.floor(spread_pos.y),
    math.floor(spread_pos.z), math.floor(spread_pos.o or 0)))

  bot.log("SIEGE_PREP: final spread position near gate")

  if bot.send_guild_command then
    bot.send_guild_command(".revive")
  else
    bot.send_command(".revive")
  end

  if bot.set_blackboard then
    pcall(function() bot.set_blackboard("prepared", true) end)
  end

  bot.log("SIEGE_PREP: complete class=" .. cls .. " spec=" .. spec .. " faction=" .. faction)

  -- Optional: small delay friendly emote
  if bot.send_chat then
    local yell = (faction == "alliance") and "For the Alliance!" or "For the Horde!"
    pcall(function() bot.send_chat(yell) end)
  end
end

-- Legacy friendly alias
M.for_siege = function(level) setup.for_siege(level or 80) end

return M
