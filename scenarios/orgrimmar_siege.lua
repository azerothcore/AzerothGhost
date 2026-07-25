-- Orgrimmar Siege scenario (large scale Alliance vs Horde PvP battle)
-- Run via: azghost ... scenario run scenarios/orgrimmar_siege.lua
--
-- IMPORTANT: The bot logic must be delivered via AIBundle.Main (DoString on bot Lua engine).
-- The top-level on_tick below is only for documentation / if used as LuaScript.
-- The orchestrator block builds a self-contained MAIN_PAYLOAD string containing
-- the requires + on_tick for the bots.

-- ============ BOT PAYLOAD (this string will be DoString'ed into each bot) ============
local BOT_MAIN = [[
-- This code runs inside each bot's Lua engine via AIBundle.Main
local prep = dofile("scripts/lib/siege_prep.lua")
local ai = dofile("scripts/ai/init.lua")
local positions = dofile("scripts/ai/data/siege_positions.lua")

-- Make values available if fallback needed (registered actions are preferred)
local ok_values, values_mod = pcall(dofile, "scripts/ai/core/values.lua")
local values = ok_values and values_mod or nil

local prep_sent = false

function on_tick()
  if not bot.is_alive() then
    if bot.send_guild_command then
      bot.send_guild_command(".revive")
    else
      bot.send_command(".revive")
    end
    return
  end

  local data = scenario_data or {}
  local phase = data.phase or "prep"
  local spec = data.spec or "default"
  local current_level = (bot.get_level and bot.get_level()) or 0

  bot.log("SIEGE_TICK phase=" .. tostring(phase) .. " level=" .. tostring(current_level) .. " faction=" .. tostring(data.faction or "auto"))

  -- Give bots a chance to be on opposite sides even if the orchestrator sends the same data to all.
  -- Deterministic per-bot using GUID so we get mixed factions for real battles.
  local my_guid_str = tostring( (bot.get_guid and bot.get_guid()) or (bot.get_level and bot.get_level() or 0) )
  local last_digit = tonumber(my_guid_str:sub(-1)) or 0
  local faction = data.faction or (last_digit % 2 == 0 and "horde" or "alliance")

  -- Call prep only once to send all GM commands (level, go to gate, gear, equip, etc.)
  -- Do not loop on level (which may lag in bot state); the orchestrator advances the phase after time.
  if phase == "prep" or not data.prepared or not prep_sent then
    prep.for_siege_bot({
      faction = faction,
      class = bot.get_class and bot.get_class(),
      spec = spec,
      start_pos = data.start_pos,
    })
    prep_sent = true
    bot.log("SIEGE_PREP done for faction=" .. faction .. " current_level=" .. tostring(current_level))
    return
  end

  if phase == "position" then
    -- We should already be near the gate from prep teleport.
    -- Hold position with very small wander so they don't look completely frozen.
    local gate = { x = 1368, y = -4373, z = 26.057 }
    local dist = 999
    if bot.get_distance_to_point then
      dist = bot.get_distance_to_point(gate.x, gate.y, gate.z) or 999
    end
    if dist > 12 then
      bot.move_to(gate.x + (math.random()-0.5)*8, gate.y + (math.random()-0.5)*8, gate.z)
    else
      bot.stop_moving()
      if (os.time() % 15 == 0) and bot.send_chat then
        bot.send_chat("Holding the line.")
      end
    end
    return
  end

  if phase == "battle" or phase == "engage" then
    -- Battle must start here. Force combat AI and target selection.
    pcall(function()
      ai:enable("siege")
      ai:enable("survive")
      ai:enable("melee")
      ai:enable("ranged")
      ai:enable("grind") -- fallback if no players
    end)

    -- Explicitly try to pick a good target (players preferred via siege strategy)
    pcall(function()
      if ai and ai.Tick then
        ai:Tick()
      end
    end)

    -- If no target yet, force a siege-style player hunt
    local tg = bot.get_target and bot.get_target() or 0
    if (tg == 0 or tg == "0") and values and values.find_nearby_enemy_players then
      local enemies = values.find_nearby_enemy_players(45, faction)
      if #enemies > 0 and enemies[1].guid then
        bot.set_target(enemies[1].guid)
        if bot.face_target then pcall(function() bot.face_target(enemies[1].guid) end) end
        bot.attack(enemies[1].guid)
        bot.log("SIEGE_BATTLE: forcing attack on player " .. tostring(enemies[1].guid))
      end
    end

    -- flavor + consumables
    if (os.time() % 18 == 0) and bot.send_chat then
      local cls = (bot.get_class and bot.get_class()) or 0
      local yell = (faction == "alliance") and "For the Alliance!" or "Lok'tar ogar!"
      if cls == 1 then yell = (faction == "alliance") and "Victory or death!" or "Blood and thunder!" end
      pcall(function() bot.send_chat(yell) end)
    end
    return
  end

  -- fallback to AI
  pcall(function() ai:Tick() end)
end
]]
-- ============ END BOT PAYLOAD ============

-- Config (orchestrator side)
local CFG = {
  alliance = tonumber(os.getenv("ALLIANCE_BOTS")) or 8,
  horde = tonumber(os.getenv("HORDE_BOTS")) or 8,
  duration_min = tonumber(os.getenv("SIEGE_DURATION_MIN")) or 3,  -- short for testing
  use_siege_ai = true,
}

-- The rest of this file runs in the *orchestrator* Lua state when you do "scenario run"
if orch and orch.prepare_accounts then
  orch.log("ORGRIMMAR_SIEGE: starting (alliance=" .. CFG.alliance .. " horde=" .. CFG.horde .. ")")

  local asgs = orch.prepare_accounts()
  orch.log("prepared " .. #asgs .. " assignments")

  -- Build a good bundle with the self-contained bot main + initial data
  -- All bots start "prep" and are told to go to the Orgrimmar gate area.
  local initial_data = {
    phase = "prep",
    prepared = false,
    start_pos = { map=1, x=1368, y=-4373, z=26.057 }
  }
  local bundle = {
    main = BOT_MAIN,
    data = initial_data,
    tick_func = "on_tick",
  }

  -- Launch (the launch_group impl will DoString the main into bots and set scenario_data)
  orch.launch_group("local", { num = CFG.alliance + CFG.horde }, bundle)

  -- Give them time to login + start their on_tick (which will do prep)
  orch.sleep(8000)

  -- Advance phase. Include prepared=true so bots stop re-prepping.
  orch.log("SIEGE: -> POSITION phase")
  orch.send_ai_update("all", { data = { phase = "position", prepared = true } })

  orch.sleep(6000)

  orch.log("SIEGE: -> BATTLE phase")
  orch.send_ai_update("all", { data = { phase = "battle", prepared = true } })

  orch.sleep(CFG.duration_min * 60 * 1000)

  orch.log("SIEGE: complete. (use CTRL-C or stop nodes to end bots)")
end

-- If someone does "lua scripts or direct load" the BOT_MAIN is the usable part.
return { ok = true, scenario = "orgrimmar_siege", bot_main_len = #BOT_MAIN }

-- Orchestrator-side script portion (executed in host context by RunScenario).
-- (The old driver block was replaced by the self-contained version above.)
-- When this file is run via "azghost scenario run", the `if orch` block near the end of the file
-- (the one with the BOT_MAIN construction) executes the driver.
