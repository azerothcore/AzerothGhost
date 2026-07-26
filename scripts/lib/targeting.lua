-- scripts/lib/targeting.lua — hostile selection + short blacklist
local util = dofile("scripts/lib/util.lua")

local M = {}

-- Factions we never grind (guards, civilians common on AC)
M.FRIENDLY_FACTIONS = {
  [35] = true, [11] = true, [12] = true, [13] = true, [55] = true, [57] = true,
  [59] = true, [60] = true, [4] = true, [5] = true, [6] = true, [161] = true, [162] = true,
}

local blacklist = {} -- guid -> expire time

function M.blacklist(guid, ttl)
  blacklist[util.guid_str(guid)] = util.now() + (ttl or 20)
end

function M.is_blacklisted(guid)
  local g = util.guid_str(guid)
  local exp = blacklist[g]
  if not exp then
    return false
  end
  if util.now() > exp then
    blacklist[g] = nil
    return false
  end
  return true
end

function M.clear_blacklist()
  blacklist = {}
end

--- Returns false if unit should not be ground-farmed.
--- Intentionally permissive: missing health fields are OK when is_alive.
function M.is_attackable_mob(u, opts)
  opts = opts or {}
  local max_level_above = opts.max_level_above or 5
  local min_level_below = opts.min_level_below or 10
  local my_level = opts.my_level or (bot.get_level and bot.get_level()) or 1
  local max_dist = opts.max_dist or 40

  if not u or u.is_player then
    return false, "player"
  end
  if M.is_blacklisted(u.guid) then
    return false, "blacklist"
  end

  local hp = u.health
  local mhp = u.max_health or 0
  -- Only treat as dead when maxHP is known and current is 0.
  if mhp > 0 and (hp or 0) <= 0 then
    return false, "dead"
  end
  if mhp == 0 and u.is_alive == false then
    return false, "dead_flag"
  end
  -- Critters / ambient (rabbits entry 721, etc.): tiny HP — never grind these.
  if mhp > 0 and mhp <= 10 then
    return false, "critter"
  end
  if mhp == 0 and (hp or 0) > 0 and (hp or 0) <= 10 then
    return false, "critter_hp"
  end
  -- Known ambient critter entries (Elwynn / start zones).
  local entry = tonumber(u.entry) or 0
  if entry == 721 or entry == 883 or entry == 2620 or entry == 4075 then
    -- 4075 is rat — actually attackable; only skip true critters
    if entry ~= 4075 then
      return false, "critter_entry"
    end
  end

  local npc = u.npc_flags or 0
  -- Skip clear civilians only (not every non-zero npc flag)
  if util.band(npc, 0x1) ~= 0 then return false, "gossip" end
  if util.band(npc, 0x2) ~= 0 then return false, "quest" end
  if util.band(npc, 0x10) ~= 0 then return false, "trainer" end
  if util.band(npc, 0x80) ~= 0 then return false, "vendor" end

  local flags = u.flags or 0
  if util.band(flags, 0x2) ~= 0 then return false, "non_attackable" end
  if util.band(flags, 0x100) ~= 0 then return false, "immune_pc" end
  if util.band(flags, 0x2000000) ~= 0 then return false, "not_selectable" end

  if M.FRIENDLY_FACTIONS[u.faction or 0] then
    return false, "friendly_fac"
  end

  local lvl = u.level or 0
  if lvl > 0 and my_level > 0 then
    if lvl > my_level + max_level_above then
      return false, "highlevel"
    end
    if my_level > lvl + min_level_below then
      return false, "lowlevel"
    end
  end

  local d = u.distance
  if d ~= nil and d > max_dist then
    return false, "far"
  end

  return true, "ok"
end

function M.find_best_hostile(opts)
  opts = opts or {}
  local max_dist = opts.max_dist or 40
  local units = bot.get_nearby_units and (bot.get_nearby_units(max_dist) or {}) or {}
  local my_level = bot.get_level and bot.get_level() or 1
  opts.my_level = my_level
  opts.max_dist = max_dist

  local best, best_score, sample = nil, 1e9, nil
  for _, u in ipairs(units) do
    local ok, why = M.is_attackable_mob(u, opts)
    if ok then
      local d = u.distance or 1
      if d < 0.5 then d = 0.5 end
      local score = d + math.abs((u.level or 1) - my_level) * 2
      if score < best_score then
        best, best_score = u, score
      end
    elseif not sample then
      sample = {
        entry = u.entry,
        hp = u.health,
        mhp = u.max_health,
        alive = u.is_alive,
        d = u.distance,
        flags = u.flags,
        npc = u.npc_flags,
        fac = u.faction,
        why = why,
      }
    end
  end
  return best, sample, #units
end

function M.ensure_target(guid)
  guid = util.guid_str(guid)
  if util.is_zero_guid(guid) then
    return false
  end
  local cur = util.guid_str(bot.get_target and bot.get_target() or 0)
  if cur ~= guid and bot.set_target then
    bot.set_target(guid)
  end
  return true
end

function M.clear_target()
  if bot.set_target then
    bot.set_target(0)
  end
  if bot.stop_attack then
    pcall(function()
      bot.stop_attack()
    end)
  end
end

return M
