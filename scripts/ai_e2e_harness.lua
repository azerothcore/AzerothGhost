-- Real E2E harness for live AzerothCore server (verifies combat, movement, target selection, revive-via-guild).
-- Run example:
--   cd AzerothGhost
--   ./azghost --profile local-ac cli --char-name TestAI --class 1 --race 1 --bot-mode lua \
--     --lua-script scripts/ai_e2e_harness.lua --log-decisions-to-chat --delete-existing-chars
-- Use longer timeout e.g. timeout 120s ...
-- IMPORTANT: character names must contain ONLY LETTERS, NO DIGITS/NUMBERS (server rejects create with code 0x5D).

print("E2E_HARNESS: Loading full Lua AI...")

local ai = dofile("scripts/ai/init.lua")
ai:enable_default_strategies()

-- GM setup + guild (guild chat is required to send commands while dead) + level + spells + location
pcall(function()
  bot.send_command(".gm on")
  bot.send_command(".guild create E2ETestGuild")
  bot.send_command(".level 6")
  bot.send_command(".learn 772")   -- Rend (warrior)
  bot.send_command(".learn 78")    -- Heroic Strike
  bot.send_command(".learn 2457")  -- Battle Shout
  bot.send_command(".learn 284")   -- Heroic Strike rank2 etc if avail
  -- Tele + explicit go to a spot known for hostiles that will fight back (Northshire/Elwynn wolves/boars area)
  bot.send_command(".tele 1429")
  bot.send_command(".go -8920 -145 83 0")
  bot.send_command(".go -9100 -1000 70 0")  -- fallback Elwynn farm/wolf area if needed
  bot.send_command(".go -9916 507 32 0")    -- boar/wolf farm area south of Goldshire - real aggressive hostiles
  -- Spawn a definitely aggressive hostile to guarantee combat entry
  bot.send_command(".npc add 6")  -- kobold vermin (aggressive)
  bot.send_command(".npc add 299") -- wolf
  bot.send_command(".gm off")  -- must turn GM off for normal combat/targeting rules and valid attack swings
  -- Equip a basic weapon so melee swings and rend can function (fresh chars have no gear)
  bot.send_command(".additem 25")
  bot.send_command(".equip 25")
  -- Unsheathe for combat (some servers require explicit sheath state before attack swings register)
  if bot.set_sheath then bot.set_sheath(0) end
end)

-- Move a bit away from spawn point so spawns are at melee range (~5yd) instead of dist=0
pcall(function()
  local x,y,z,o = bot.get_position()
  if x then
    bot.move_to(x + 5, y, z)
  end
end)

local tick = 0
local last_status = 0
local last_x, last_y, last_z = 0, 0, 0
local last_tgt = "0"
local last_combat = false
local forced_death = false
local revive_attempts = 0
local engaged_count = 0
local last_forced_target = "0"
local combat_achieved_ticks = 0
local forced_target_initial_hp = 0
local forced_target_last_hp_check = 0
local combat_lock_until = 0   -- tick until which we lock onto one target for combat verification

local function disable_ai_for_verification()
  if ai and ai.disable then
    pcall(function() ai:disable("grind") end)
    pcall(function() ai:disable("loot") end)
    pcall(function() ai:disable("melee") end)
    pcall(function() ai:disable("ranged") end)
  end
end

local function enable_ai_default()
  if ai and ai.enable then
    pcall(function() ai:enable("grind") end)
    pcall(function() ai:enable("loot") end)
    pcall(function() ai:enable("melee") end)
  end
end

local function force_set_and_attack(guid)
  if not guid or guid == "0" then return end
  if bot.set_sheath then pcall(function() bot.set_sheath(0) end) end
  if bot.face_target then pcall(function() bot.face_target(guid) end) end
  if bot.set_target then
    pcall(function() bot.set_target(guid) end)
    bot.log("E2E_SET_TARGET before attack: " .. tostring(guid))
  end
  -- Verify the set actually took on client side (get_target should return it)
  local after = "?"
  if bot.get_target then after = tostring(bot.get_target() or "?") end
  if after ~= tostring(guid) then
    bot.log("E2E_SET_MISMATCH: intended=" .. tostring(guid) .. " client_get_target=" .. after)
  end
  if bot.attack then pcall(function() bot.attack(guid) end) end
end

-- Attackability flags from AzerothCore UnitDefines.h (for player-controlled attacker)
local UNIT_FLAG_NON_ATTACKABLE     = 0x00000002
local UNIT_FLAG_NOT_ATTACKABLE_1   = 0x00000080
local UNIT_FLAG_IMMUNE_TO_PC       = 0x00000100
local UNIT_FLAG_NON_ATTACKABLE_2   = 0x00010000
local UNIT_FLAG_NOT_SELECTABLE     = 0x02000000

local function is_attackable(u)
  if not u then return false end
  if not u.is_alive then return false end
  if u.is_player then return false end
  -- Extra defensive check: health 0 almost always means dead (catches stale is_alive)
  if (u.health or 1) == 0 then return false end
  local f = u.flags or 0
  -- Check against non-attackable / immune / unselectable flags (from AC _IsValidAttackTarget + UnitDefines.h)
  if (f % 4 >= 2) then return false end                          -- NON_ATTACKABLE (0x2)
  if (f % 2097152 >= 1048576) then return false end              -- TAXI_FLIGHT (0x100000)
  if (f % 256 >= 128) then return false end                      -- NOT_ATTACKABLE_1 (0x80)
  if (f % 512 >= 256) then return false end                      -- IMMUNE_TO_PC (0x100)
  if (f % 131072 >= 65536) then return false end                 -- NON_ATTACKABLE_2 (0x10000)
  if (f % 33554432 >= 16777216) then return false end            -- NOT_SELECTABLE (0x2000000)
  local npc = u.npc_flags or 0
  if npc ~= 0 then return false end   -- skip questgivers, vendors, trainers, guards etc. (from AC logic)
  local fac = u.faction or 0
  -- skip known friendly factions (from bot isHostileFaction and AC). Use table for reliable matching.
  local friendlyFacs = { [35]=true, [11]=true, [12]=true, [13]=true, [55]=true, [57]=true, [59]=true, [60]=true,
                         [4]=true, [5]=true, [6]=true, [161]=true, [162]=true }
  if friendlyFacs[fac] then
    return false
  end
  return true
end

function on_tick()
  tick = tick + 1

  -- Give the server time to process the initial .tele/.go/.npc/.gm off/.equip etc.
  -- Commands are async; early ticks often see stale position or pre-tele units.
  -- This prevents swinging at "targets" that the server doesn't consider valid for us yet.
  if tick < 25 then
    if tick % 5 == 0 then
      bot.log("E2E_SETUP: waiting for tele/go/npc/position sync (tick=" .. tick .. ")")
    end
    -- periodically re-send go to the hostile area in case first ones were queued before login fully settled
    if tick == 10 or tick == 20 then
      pcall(function()
        bot.send_command(".go -9916 507 32 0")
        bot.send_command(".level 6")  -- ensure low level for starting zone hostiles (high level chars can cause trivial/grey mob issues with combat registration)
      end)
    end
    return
  end

  -- === REVIVE TEST: must use guild channel while dead ===
  local alive = true
  if bot.is_alive then
    alive = bot.is_alive()
  end
  if not alive then
    revive_attempts = revive_attempts + 1
    bot.log(string.format("E2E_REVIVE: DEAD (tick=%d) - sending .revive VIA GUILD CHAT (attempt %d)", tick, revive_attempts))
    if bot.send_guild_command then
      pcall(function() bot.send_guild_command(".revive") end)
    else
      bot.log("E2E_REVIVE: send_guild_command not available, falling back")
      pcall(function() bot.send_command(".revive") end)
    end
    return
  end

  -- Periodic rich status for verification of target sel / movement / combat
  if tick - last_status >= 5 then
    last_status = tick

    -- Health
    local hp_pct = "??"
    if bot.get_health then
      local h, mh = bot.get_health()
      if mh and mh > 0 then
        hp_pct = tostring(math.floor((h or 0) / mh * 100))
      end
    end

    -- Target -- keep as STRING, never number (64-bit GUIDs lose precision in Lua numbers)
    local tgtRaw = "0"
    if bot.get_target then tgtRaw = tostring(bot.get_target() or "0") end
    local tgt = tgtRaw
    -- log alive/hp of current target so we can see if we're on a corpse
    if tgt ~= "0" and bot.get_unit then
      local tu = bot.get_unit(tgt)
      if tu then
        bot.log(string.format("E2E_TARGET_STATUS: guid=%s alive=%s hp=%d/%d", tgtRaw, tostring(tu.is_alive), tu.health or 0, tu.max_health or 0))
      end
    end
    -- if current (non-forced) target is dead, let grind pick a live one (don't keep it)
    if tgt ~= "0" and bot.get_unit then
      local tu = bot.get_unit(tgt)
      if tu and (not tu.is_alive or (tu.health or 0) <= 0) then
        -- clear so game UI doesn't show dead kobold as selected
        if bot.set_target then pcall(function() bot.set_target(0) end) end
      end
    end

    -- Combat
    local in_c = false
    if bot.in_combat then in_c = bot.in_combat() end

    -- Position + movement delta (verifies movement system)
    local x, y, z, o = 0, 0, 0, 0
    if bot.get_position then
      x, y, z, o = bot.get_position()
    end
    local dx = math.abs(x - last_x)
    local dy = math.abs(y - last_y)
    local dz = math.abs(z - last_z)
    local moved = (dx > 0.5 or dy > 0.5 or dz > 0.5)
    last_x, last_y, last_z = x, y, z

    bot.log(string.format(
      "E2E_STATUS: tick=%d hp=%s%% target=%s combat=%s moved=%s pos=%.1f,%.1f,%.1f facing=%.2f",
      tick, hp_pct, tgtRaw, tostring(in_c), tostring(moved), x, y, z, o
    ))

    -- Target selection change logging (explicit verification)
    if tgtRaw ~= last_tgt then
      bot.log("E2E_TARGET_SEL: changed from=" .. last_tgt .. " to=" .. tgtRaw)
      last_tgt = tgtRaw
      if tgtRaw ~= "0" then engaged_count = engaged_count + 1 end
    end

    -- Combat state transition logging
    if in_c ~= last_combat then
      bot.log("E2E_COMBAT: state=" .. tostring(in_c) .. " (was " .. tostring(last_combat) .. ")")
      last_combat = in_c
    end

    -- Warrior rend observation (combat + aura) -- prefer the one we forced
    local rend_check_guid = (last_forced_target ~= 0) and last_forced_target or tgt
    if bot.get_class and bot.get_class() == 1 and rend_check_guid ~= 0 and bot.has_aura_on then
      local has_rend = bot.has_aura_on(rend_check_guid, 772)
      bot.log("E2E_WARRIOR: target=" .. tostring(rend_check_guid) .. " has_rend=" .. tostring(has_rend) .. " in_combat=" .. tostring(in_c))
      if has_rend then
        bot.log("E2E_COMBAT_VERIFIED: rend aura landed on target")
      end
    end

    -- Check if the current target has entered combat (IN_COMBAT flag 0x80000)
    if tgtRaw ~= "0" and bot.get_unit then
      local tu = bot.get_unit(tgtRaw)
      if tu and tu.flags and (tu.flags % 0x100000 >= 0x80000) then
        bot.log("E2E_TARGET_IN_COMBAT: engaged target has IN_COMBAT flag")
      end
      -- Log facing delta for the target (if > ~0.3 rad, server may send BADFACING on swing)
      if tu and tu.x and bot.get_facing then
        local myo = o
        local dx = (tu.x or 0) - x
        local dy = (tu.y or 0) - y
        local desired = math.atan2(dy, dx)
        local fdelta = math.abs(myo - desired)
        if fdelta > math.pi then fdelta = 2*math.pi - fdelta end
        if fdelta > 0.1 then
          bot.log(string.format("E2E_FACING: target=%s delta=%.2f (my=%.2f desired=%.2f)", tgtRaw, fdelta, myo, desired))
        end
      end
    end

    if in_c then
      combat_achieved_ticks = combat_achieved_ticks + 1
      if combat_achieved_ticks == 1 then
        bot.log("E2E_COMBAT_VERIFIED: in_combat() became true")
      end
    end

    -- Health delta proof for the forced target (if health dropped after our attacks/casts, combat action worked)
    if last_forced_target ~= "0" and bot.get_unit then
      local u2 = bot.get_unit(last_forced_target)
      if u2 and u2.health then
        if forced_target_last_hp_check == 0 then forced_target_last_hp_check = u2.health end
        if u2.health < forced_target_last_hp_check then
          bot.log("E2E_COMBAT_ACTION: forced target health dropped " .. forced_target_last_hp_check .. " -> " .. u2.health)
          forced_target_last_hp_check = u2.health
        end
        if u2.health == 0 or not u2.is_alive then
          bot.log("E2E_TARGET_DEAD: forced target " .. tostring(last_forced_target) .. " health=0 or not alive in status check")
          -- don't clear here, the lock block will handle on next hammer
        end
      end
    end

    -- Nearby units summary (target selection evidence) - now using proper attackability
    if bot.get_nearby_units then
      local units = bot.get_nearby_units(20) or {}
      local attackable = 0
      for _, u in ipairs(units) do
        if is_attackable(u) then attackable = attackable + 1 end
      end
      if #units > 0 then
        bot.log("E2E_NEARBY: count=" .. #units .. " attackable=" .. attackable)
      end
    end
  end

  -- === Explicit force-engagement + combat lock ===
  -- Once we pick a target for combat testing, we lock it for many ticks so the grind
  -- strategy cannot immediately steal the target back. We hammer attack + rend on it.
  if tick <= combat_lock_until and last_forced_target ~= "0" then
    -- Locked window: keep pressure on the chosen target
    -- IMPORTANT: re-validate alive every tick. We may have killed it; don't keep attacking corpses.
    if bot.get_unit then
      local fu = bot.get_unit(last_forced_target)
      if not fu or not fu.is_alive or (fu.health or 0) <= 0 then
        bot.log("E2E_TARGET_DEAD: forced target " .. tostring(last_forced_target) .. " is now dead (is_alive=false or health=0), releasing lock to avoid attacking corpse")
        last_forced_target = "0"
        combat_lock_until = 0
        enable_ai_default()
        if bot.set_target then pcall(function() bot.set_target("0") end) end -- clear in-game target so you don't see the dead kobold selected
      end
    end
    if last_forced_target ~= "0" then
      disable_ai_for_verification()
      -- ensure close melee range for attack to land and SMSG_ATTACKSTART
      if bot.get_unit then
        local u = bot.get_unit(last_forced_target)
        if u and u.distance and u.distance > 1.0 then
          pcall(function() bot.move_to(u.x, u.y, u.z) end)
        end
      end
      -- Set target BEFORE attacking (using helper for visibility + post-set check)
      force_set_and_attack(last_forced_target)
      if bot.cast_spell and bot.is_spell_ready and bot.is_spell_ready(772) then
        pcall(function() bot.cast_spell(772, last_forced_target) end)
      end
      -- Health sampling for proof
      if bot.get_unit then
        local u2 = bot.get_unit(last_forced_target)
        if u2 and u2.health and forced_target_last_hp_check > 0 and u2.health < forced_target_last_hp_check then
          bot.log("E2E_COMBAT_ACTION: locked target health dropped " .. forced_target_last_hp_check .. " -> " .. u2.health)
          forced_target_last_hp_check = u2.health
        end
        if u2 and (u2.health == 0 or not u2.is_alive) then
          bot.log("E2E_TARGET_DEAD: forced target health reached 0, releasing lock")
          last_forced_target = "0"
          combat_lock_until = 0
          enable_ai_default()
          enable_ai_default()
        end
      end
    end
  elseif tick > 8 and tick % 3 == 0 then
    local cur_tgt = "0"
    if bot.get_target then cur_tgt = tostring(bot.get_target() or "0") end
    local ic = bot.in_combat and bot.in_combat() or false
    if (cur_tgt == "0" or not ic or tick > combat_lock_until) and bot.get_nearby_units then
      local units = bot.get_nearby_units(25) or {}
      for _, u in ipairs(units) do
        local d = u.distance or 99
        if is_attackable(u) and d > 1 and d < 18 then
          local target_guid = tostring(u.guid)
          local fac = u.faction or 0
          local npc = u.npc_flags or 0
          bot.log(string.format("E2E_FORCE_ENGAGE: guid=%s dist=%.0f name=%s fac=%d npc=0x%x flags=0x%x alive=%s hp=%d", target_guid, u.distance or 0, tostring(u.name or u.entry), fac, npc, u.flags or 0, tostring(u.is_alive), u.health or 0))
          last_forced_target = target_guid
          combat_lock_until = tick + 22   -- lock for a solid window
          disable_ai_for_verification()
          local unit = bot.get_unit and bot.get_unit(target_guid) or nil
          forced_target_initial_hp = (unit and unit.health) or 0
          forced_target_last_hp_check = forced_target_initial_hp
          -- Use helper which logs + verifies get_target after set, and sets before attack
          if bot.move_to and (u.distance or 0) > 2.5 then
            pcall(function() bot.move_to(u.x, u.y, u.z) end)
          end
          if bot.cast_spell and bot.is_spell_ready and bot.is_spell_ready(772) then
            bot.log("E2E_FORCE_COMBAT: casting rend (locked window)")
            pcall(function() bot.cast_spell(772, target_guid) end)
          end
          for i=1,5 do
            force_set_and_attack(target_guid)
          end
          break
        end
      end
    end
  end

  -- Dedicated combat verification poll on some ticks (helps catch the state even if status interval misses it)
  if tick % 3 == 0 and last_forced_target ~= "0" then
    -- quick alive guard in poll too
    local fu = bot.get_unit and bot.get_unit(last_forced_target) or nil
    if not fu or not fu.is_alive or (fu.health or 0) <= 0 then
      bot.log("E2E_TARGET_DEAD: poll detected forced target dead, clearing")
      last_forced_target = "0"
      combat_lock_until = 0
      enable_ai_default()
    else
      local ic = bot.in_combat and bot.in_combat() or false
      if ic then
        bot.log("E2E_COMBAT_POLL: in_combat=true on forced target " .. tostring(last_forced_target))
      else
        -- keep the pressure on (helper ensures set before attack + visibility)
        disable_ai_for_verification()
        force_set_and_attack(last_forced_target)
      end
    end
  end

  -- maintenance: if not locked and no live target but attackable exist, force one to keep combat going
  if last_forced_target == "0" and tick % 2 == 0 then
    local ct = tostring( (bot.get_target and bot.get_target() or "0") )
    local has_live = false
    if ct ~= "0" then
      local uu = bot.get_unit and bot.get_unit(ct) or nil
      if uu and uu.is_alive and (uu.health or 0) > 0 then has_live = true end
    end
    if not has_live and bot.get_nearby_units then
      local us = bot.get_nearby_units(20) or {}
      for _, uuu in ipairs(us) do
        if is_attackable(uuu) and (uuu.distance or 99) >1 and (uuu.distance or 99) < 18 then
          last_forced_target = tostring(uuu.guid)
          combat_lock_until = tick + 15
          disable_ai_for_verification()
          force_set_and_attack(last_forced_target)
          break
        end
      end
    end
  end

  -- Pre-tick re-affirm of forced (kept for compatibility); main re-affirm now after the conditional Tick using helper
  if last_forced_target ~= "0" and tick <= combat_lock_until then
    -- use helper to ensure set+log+get check
    force_set_and_attack(last_forced_target)
  end

  -- === Force death test (after some engagement) to verify guild-channel revive ===
  if not forced_death and tick >= 200 and engaged_count >= 1 and bot.is_alive and bot.is_alive() then
    forced_death = true
    bot.log("E2E_DEATH_TEST: forcing .die to enter dead state and test guild revive")
    pcall(function() bot.send_command(".die") end)
    -- next ticks will hit the !alive branch and use send_guild_command
  end

  -- Run the real AI (strategies, triggers, actions that also do move_to / set_target / attack / cast)
  -- Skip during forced lock so our E2E verification target is not overridden by grind select etc.
  if last_forced_target == "0" or tick > combat_lock_until then
    ai:Tick()
  end

  -- During lock, re-affirm the forced target (no AI tick means less chance of override).
  -- We still do this for external visibility of UNIT_FIELD_TARGET.
  if last_forced_target ~= "0" and tick <= combat_lock_until then
    force_set_and_attack(last_forced_target)
    bot.log("E2E_SET_TARGET (post tick re-affirm for visibility): " .. tostring(last_forced_target))
  end
end

print("E2E_HARNESS: Full AI + verification harness ready. Combat/Movement/Target/Revive(guild) will be exercised and logged.")
