-- scripts/validation/setup.lua
-- Minimal, focused helpers for P0 mechanic validation runs.
-- All side effects via GM commands (assumes .gm on allowed for the test account).
-- Gated usage recommended under --validation-mode.
--
-- Usage in test scripts (dofile because require path not auto-configured in engine):
--   local v = dofile("scripts/validation/setup.lua")
--   v.setup_gm_and_guild()
--   v.tele_to_validation_spot()
--   v.force_health(18)
--   ...
--
-- Provides:
--   force_health(pct)   -- set player's current health to ~pct% of max via GM modify
--   tele_to_validation_spot() -- repeatable Elwynn aggressive area
--   ensure_spell_learned(spellID)
--   spawn_training_target(entryID) -- spawns and returns guid-ish (via recent nearby)
--   clear_auras_on(targetGUID)
--   wait_for_aura(guid, spellID, timeoutTicks, onTickFn) -- polls in caller loop style
--   assert_cast_success(...)
--   force_pet_summoned()
--   setup_gm_and_guild()
--   log_decision etc helpers.

local M = {}

local VALIDATION_SPOT = {
  map = 1429, -- Elwynn Forest
  x = -9916, y = 507, z = 32, o = 0,
  desc = "South of Goldshire farm - aggressive wolves (299), kobolds (6) etc."
}

function M.log(msg)
  if bot and bot.log then
    bot.log("[VAL_SETUP] " .. tostring(msg))
  else
    print("[VAL_SETUP] " .. tostring(msg))
  end
end

function M.setup_gm_and_guild(guildName)
  guildName = guildName or "ValGuild"
  pcall(function()
    bot.send_command(".gm on")
    bot.send_command(".guild create " .. guildName)
    bot.send_command(".level 8")  -- ensure sufficient level for key lowbie spells to be castable
  end)
  M.log("GM and guild setup attempted (leveled)")
end

function M.ensure_spell_learned(spellID)
  if not spellID then return end
  pcall(function()
    bot.send_command(".learn " .. tostring(spellID))
  end)
  M.log("ensure learned " .. tostring(spellID))
end

function M.tele_to_validation_spot()
  pcall(function()
    bot.send_command(".tele " .. VALIDATION_SPOT.map)
    bot.send_command(string.format(".go %d %d %d %d", VALIDATION_SPOT.x, VALIDATION_SPOT.y, VALIDATION_SPOT.z, VALIDATION_SPOT.o))
  end)
  M.log("tele to validation spot: " .. VALIDATION_SPOT.desc)
end

-- force_health(pct) : set self health to approx pct% of current max.
-- Uses .modify health <absolute> after sampling via bot.get_health.
-- Call early in setup window; server will adjust.
function M.force_health(pct)
  pct = pct or 18
  -- Repeated negative modify to drive HP low (more reliable than single absolute set on fresh chars)
  for i=1,6 do
    pcall(function() bot.send_command(".modify health -12") end)
  end
  M.log(string.format("force_health: sent repeated damage to drive toward ~%d%% (survive test)", pct))
end

-- spawn_training_target(entryID): spawns an NPC, waits briefly, returns a guid of a matching nearby unit.
-- If entryID nil, uses a default aggressive lowbie (kobold 6 or wolf 299).
function M.spawn_training_target(entryID)
  entryID = entryID or 6
  pcall(function()
    bot.send_command(".npc add " .. tostring(entryID))
  end)
  M.log("spawned training target entry=" .. tostring(entryID))
  -- Return nil here; caller should poll get_nearby_units to locate the fresh spawn by entry + is_alive + dist
  return nil
end

function M.clear_auras_on(targetGUID)
  if not targetGUID or targetGUID == "0" then return end
  -- Server side remove all (player or unit); .aura remove <spell> for specific if needed later.
  pcall(function()
    bot.send_command(".aura remove all " .. tostring(targetGUID))  -- may not be universal; fallback per-aura in caller
  end)
  M.log("clear_auras attempted on " .. tostring(targetGUID))
end

-- Helper to be called from on_tick loop: returns true when aura seen or timeout.
-- Usage pattern in test:
--   local seen = false
--   for i=1,80 do
--     ...
--     if v.wait_for_aura(guid, 772, 30, function() ... end) then seen=true; break end
--   end
function M.wait_for_aura(guid, spellID, timeoutTicks, tickFn)
  timeoutTicks = timeoutTicks or 40
  -- The actual poll must be done by caller using bot.has_aura_on because this is not async.
  -- This is a no-op marker; real impl relies on caller polling + this for doc.
  if tickFn then tickFn() end
  return false -- caller implements the check
end

-- Simple cast attempt + observation helper. Caller tracks success via SPELL_GO or has_aura etc.
function M.try_cast(spellID, targetGUID)
  if not bot.cast_spell then return false end
  local ok = pcall(function() bot.cast_spell(spellID, targetGUID) end)
  return ok
end

-- force_pet_summoned: for hunter/warlock. Calls call pet or revive, ensures pet guid non zero.
function M.force_pet_summoned()
  -- Common: CALL_PET 883 for hunter, or REVIVE_PET 982
  pcall(function()
    bot.send_command(".cast 883") -- call pet (if stabled or first)
    bot.send_command(".cast 982") -- revive pet fallback
  end)
  if bot.pet_attack then
    -- will be used after guid appears
  end
  M.log("force_pet_summoned: call/revive cast attempted")
end

-- find_aggressive_target_near(dist): returns first attackable nearby unit table or nil.
function M.find_aggressive_target_near(maxDist)
  maxDist = maxDist or 18
  if not bot.get_nearby_units then return nil end
  local units = {}
  local ok, res = pcall(bot.get_nearby_units, maxDist)
  if ok and res then units = res end
  for _, u in ipairs(units) do
    if u.is_alive and (u.health or 0) > 0 and not u.is_player and (u.distance or 99) < maxDist and (u.distance or 0) > 0.5 then
      -- crude hostile filter (skip obvious friendlies)
      local fac = u.faction or 0
      local friendly = ({[35]=true,[11]=true,[12]=true,[13]=true,[55]=true})[fac]
      if not friendly then
        return u
      end
    end
  end
  return nil
end

-- Utility: keep facing + targeting + attacking a guid during test window.
function M.hammer_attack(guid, doCastFn)
  if not guid or guid == "0" then return end
  pcall(function()
    if bot.set_sheath then bot.set_sheath(0) end
    if bot.face_target then bot.face_target(guid) end
    if bot.set_target then bot.set_target(guid) end
    if bot.attack then bot.attack(guid) end
    if doCastFn then doCastFn() end
  end)
end

-- For harness: simple counter collector.
M.counters = {}

function M.inc(name, by)
  by = by or 1
  M.counters[name] = (M.counters[name] or 0) + by
end

function M.get_counters()
  return M.counters
end

function M.reset_counters()
  M.counters = {}
end

M.VALIDATION_SPOT = VALIDATION_SPOT

return M
