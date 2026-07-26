-- scripts/ai/init.lua
-- Bootstrap for Lua AI Core Framework + class libs (warrior/hunter/mage).
-- Returns an engine instance with Tick(), enable(), disable(), etc.
--
-- Example:
--   local ai = dofile("scripts/ai/init.lua")
--   ai:enable_default_strategies()
--   function on_tick() ai:Tick() end
--
-- Or (if helpers attached): local ai = dofile("scripts/ai/init.lua").load_for_bot()
-- Main usage always: local ai = dofile("scripts/ai/init.lua"); ai:enable_default_strategies()
-- Class registration auto based on bot.get_class() (1=warrior,2=paladin,3=hunter,4=rogue,5=priest,6=dk,7=shaman,8=mage,9=warlock,11=druid).
-- full 10 classes + spec detection heuristics (high level spells known, stance/form auras, power usage).
-- IMPORTANT TIMING: bot.get_class() must be available at dofile time for auto class wiring (see load_for_bot + advanced examples).

local engine_mod = dofile("scripts/ai/core/engine.lua")
local survive = dofile("scripts/ai/generic/survive.lua")
local melee = dofile("scripts/ai/generic/melee.lua")
local ranged = dofile("scripts/ai/generic/ranged.lua")
local loot = dofile("scripts/ai/generic/loot.lua")
local grind = dofile("scripts/ai/generic/grind.lua")
-- generics
local follow = dofile("scripts/ai/generic/follow.lua")
local buff = dofile("scripts/ai/generic/buff.lua")
local rest = dofile("scripts/ai/generic/rest.lua")
local rpg = dofile("scripts/ai/generic/rpg.lua")
-- pet management generic (full pet strats: call/revive/mend/modes)
local pet = dofile("scripts/ai/generic/pet.lua")
local spells = dofile("scripts/ai/data/spells.lua")
local utils = dofile("scripts/ai/core/utils.lua")
local siege = dofile("scripts/ai/generic/siege.lua") -- siege / pvp aware generic
local values = dofile("scripts/ai/core/values.lua") -- for enemy/ally finders

local M = {}

-- Spec strategy names that must not all run at once (only one primary per class).
local CLASS_SPEC_NAMES = {
  "arms", "fury", "prot",
  "retribution", "protection", "holy",
  "beast_mastery", "marksmanship", "survival",
  "assassination", "combat", "subtlety",
  "shadow", "holy_priest", "discipline",
  "blood", "frost_dk", "unholy",
  "elemental", "enhancement", "resto_shaman",
  "fire", "frost", "arcane",
  "destruction", "affliction", "demonology",
  "balance", "feral", "resto_druid",
}

-- Forward declare: detect_spec is defined below enable_class_defaults.
local detect_spec

-- Class generic + single primary spec only. Enabling every spec at once made
-- the bot try shield slam / bloodthirst / mortal strike every tick and starve
-- the real rotation (this is why the "advanced" grind felt broken).
local function enable_class_defaults(ai, cls)
  cls = cls or (bot and bot.get_class and bot.get_class()) or 0
  local primary = detect_spec and detect_spec(cls) or nil

  local function enable_if(name)
    if name and ai.registered_strategies[name] then
      ai:enable(name)
      return true
    end
    return false
  end

  if cls == 1 then
    enable_if("generic_warrior")
    enable_if(primary or "arms")
  elseif cls == 2 then
    enable_if("generic_paladin")
    enable_if(primary or "retribution")
  elseif cls == 3 then
    enable_if("hunter_generic")
    enable_if(primary or "beast_mastery")
    enable_if("pet_management")
    enable_if("ranged")
  elseif cls == 4 then
    enable_if("rogue_generic")
    enable_if(primary or "assassination")
  elseif cls == 5 then
    enable_if("priest_generic")
    enable_if(primary or "shadow")
  elseif cls == 6 then
    enable_if("dk_generic")
    enable_if(primary or "blood")
  elseif cls == 7 then
    enable_if("shaman_generic")
    enable_if(primary or "elemental")
  elseif cls == 8 then
    enable_if("mage_generic")
    enable_if(primary or "fire")
  elseif cls == 9 then
    enable_if("warlock_generic")
    enable_if(primary or "destruction")
    enable_if("pet_management")
  elseif cls == 11 then
    enable_if("druid_generic")
    enable_if(primary or "balance")
  else
    -- Class not known yet: enable whatever generics were registered (rare).
    enable_if("generic_warrior")
    enable_if("arms")
  end

  if primary then
    utils.log_decision("class defaults: primary spec='" .. tostring(primary) .. "' cls=" .. tostring(cls))
  end
end

-- Switch primary combat spec (disables sibling specs, enables `name`).
local function set_primary_spec(ai, name)
  if not name or not ai.registered_strategies[name] then
    return false
  end
  for _, s in ipairs(CLASS_SPEC_NAMES) do
    if s ~= name then
      ai:disable(s)
    end
  end
  ai:enable(name)
  utils.log_decision("set_primary_spec: " .. tostring(name))
  return true
end

-- spec detection heuristics (called optionally from examples or after load; uses known high spells, auras, power)
detect_spec = function(cls)
  cls = cls or (bot and bot.get_class and bot.get_class()) or 0
  if not bot then return nil end
  -- wrap bot.* calls with pcall for safety (per review; avoids tick crashes if API throws)
  local function safe_spell_ready(id)
    if not bot.is_spell_ready then return false end
    local ok, res = pcall(bot.is_spell_ready, id)
    return ok and res
  end
  local function safe_has_aura(g, id)
    if not bot.has_aura_on then return false end
    local ok, res = pcall(bot.has_aura_on, g, id)
    return ok and res
  end
  local function safe_get_stance()
    if not bot.get_stance then return 0 end
    local ok, res = pcall(bot.get_stance)
    return (ok and res) or 0
  end
  local function safe_get_power_type()
    if not bot.get_power_type then return 0 end
    local ok, res = pcall(bot.get_power_type)
    return (ok and res) or 0
  end
  if cls == 1 then -- warrior
    if safe_spell_ready(12294) then return "arms" end
    if safe_spell_ready(23881) then return "fury" end
    if safe_spell_ready(23922) then return "prot" end
    return "arms"
  elseif cls == 2 then -- paladin
    if safe_spell_ready(35395) then return "retribution" end
    if safe_spell_ready(31935) then return "protection" end
    if safe_spell_ready(20473) then return "holy" end
    return "retribution"
  elseif cls == 4 then -- rogue
    if safe_spell_ready(1329) or safe_spell_ready(32645) then return "assassination" end
    if safe_spell_ready(53) and safe_spell_ready(16511) then return "combat" end
    if safe_spell_ready(36554) then return "subtlety" end
    return "assassination"
  elseif cls == 5 then -- priest
    if safe_spell_ready(15473) or safe_has_aura(0, 15473) then return "shadow" end
    if safe_spell_ready(47540) then return "discipline" end
    if safe_spell_ready(2061) then return "holy_priest" end
    return "shadow"
  elseif cls == 6 then -- dk
    local stance = safe_get_stance()
    if safe_spell_ready(55050) or stance == 1 then return "blood" end
    if safe_spell_ready(49184) then return "frost_dk" end
    if safe_spell_ready(55090) then return "unholy" end
    return "blood"
  elseif cls == 7 then -- shaman
    if safe_get_power_type() == 0 and safe_spell_ready(51505) then return "elemental" end
    if safe_spell_ready(17364) then return "enhancement" end
    if safe_spell_ready(1064) then return "resto_shaman" end
    return "elemental"
  elseif cls == 9 then -- warlock
    if safe_spell_ready(47241) then return "demonology" end
    if safe_spell_ready(17962) then return "destruction" end
    if safe_spell_ready(1014) then return "affliction" end
    return "destruction"
  elseif cls == 11 then -- druid
    local stance = safe_get_stance()
    if stance == 1 or stance == 3 then return "feral" end
    if safe_spell_ready(24858) or safe_has_aura(0, 24858) then return "balance" end
    if safe_spell_ready(774) then return "resto_druid" end
    return "balance"
  end
  return nil
end


-- internal helper to build a fresh engine wired with generics
local function create_ai_engine()
  local ai = engine_mod.Engine:new()

  -- register the generic strategies (no class depth)
  ai:register_strategy("survive", survive.SurviveStrategy)
  ai:register_strategy("melee", melee.MeleeStrategy)
  ai:register_strategy("ranged", ranged.RangedStrategy)
  ai:register_strategy("loot", loot.LootStrategy)
  ai:register_strategy("grind", grind.GrindStrategy)
  -- generics
  ai:register_strategy("follow", follow.FollowStrategy)
  ai:register_strategy("buff", buff.BuffStrategy)
  ai:register_strategy("rest", rest.RestStrategy)
  ai:register_strategy("rpg", rpg.RpgStrategy)
  -- pet
  ai:register_strategy("pet_management", pet.PetStrategy)
  -- siege / pvp mass battle (high relevance player targeting + ally support)
  if siege and siege.SiegeStrategy then
    ai:register_strategy("siege", siege.SiegeStrategy)
  end

  -- wire class libs based on bot.get_class() (foundation + 3 classes)
  -- TIMING NOTE (Issue 10): this runs at dofile time. Ensure `bot` global + get_class() is populated before dofile("scripts/ai/init.lua")
  -- (as done in load_for_bot callers, advanced examples, and lua tick setup). If cls==0 here, classes skipped; load_for_bot recreates later.
  local cls = (bot and bot.get_class and bot.get_class()) or 0
  -- uniform helper for class loading (all 10 classes) that captures pcall error (2nd return value)
  local function safe_dofile_class(path, clsname)
    local ok, mod = pcall(dofile, path)
    if ok and mod and mod.register then
      mod.register(ai)
    else
      utils.log_decision("WARN class load failed for " .. clsname .. " ok=" .. tostring(ok) .. " err=" .. tostring(mod))
    end
  end
  if cls == 1 then
    safe_dofile_class("scripts/ai/class/warrior.lua", "warrior cls=1")
  elseif cls == 2 then
    safe_dofile_class("scripts/ai/class/paladin.lua", "paladin cls=2")
  elseif cls == 3 then
    safe_dofile_class("scripts/ai/class/hunter.lua", "hunter cls=3")
  elseif cls == 4 then
    safe_dofile_class("scripts/ai/class/rogue.lua", "rogue cls=4")
  elseif cls == 5 then
    safe_dofile_class("scripts/ai/class/priest.lua", "priest cls=5")
  elseif cls == 6 then
    safe_dofile_class("scripts/ai/class/deathknight.lua", "dk cls=6")
  elseif cls == 7 then
    safe_dofile_class("scripts/ai/class/shaman.lua", "shaman cls=7")
  elseif cls == 8 then
    safe_dofile_class("scripts/ai/class/mage.lua", "mage cls=8")
  elseif cls == 9 then
    safe_dofile_class("scripts/ai/class/warlock.lua", "warlock cls=9")
  elseif cls == 11 then
    safe_dofile_class("scripts/ai/class/druid.lua", "druid cls=11")
  end
  -- attach detect_spec for callers (dynamic, re-queries bot to avoid stale closure over load-time cls)
  ai.detect_spec = function() return detect_spec() end


  -- register concrete actions (fns or tables) referenced by name/relevance
  -- Note: generics use plain fns(ctx) returning bool (pragmatic); table Action form from core/action.lua supported in engine but unused.
  -- survive actions (high prio)
  ai:register_action("survive_check_alive", function(ctx)
    if not bot.is_alive() then
      utils.log_decision("dead - reviving")
      -- Guild chat is allowed while dead on most servers (regular /say is not).
      -- Prefer send_guild_command if the bridge provides it (added for live E2E revive tests).
      if bot.send_guild_command then
        bot.send_guild_command(".revive")
      else
        bot.send_command(".revive")
      end
      return true
    end
    return false
  end)

  ai:register_action("survive_low_health", function(ctx)
    local hp = ctx:get_value("health_pct") or 100
    if hp >= 25 then
      return false
    end
    local in_combat = ctx:get_value("in_combat")
    -- In combat: do not consume the tick (would starve rotation/melee) and do
    -- not stop_moving — that thrash-repaths with engage_melee every ~200ms.
    if in_combat then
      return false
    end
    -- Out of combat + critical HP: force rest window, drop target, no new pulls.
    utils.log_decision("low health (" .. math.floor(hp) .. "%) OOC — rest before next pull")
    if bot.stop_moving then bot.stop_moving() end
    if bot.stop_attack then pcall(function() bot.stop_attack() end) end
    if bot.set_target then pcall(function() bot.set_target(0) end) end
    local now = (bot.now_ms and (bot.now_ms() / 1000)) or os.time()
    if ctx.set_blackboard then
      ctx:set_blackboard("rest_until", now + 10)
    end
    return true
  end)

  -- Shared sticky chase for grind/melee (avoids repath thrash; uses interpolated unit.x/y/z).
  local movement_lib = nil
  local grind_chase = nil
  do
    local okm, mod = pcall(dofile, "scripts/lib/movement.lua")
    if okm and mod then
      movement_lib = mod
      grind_chase = mod.new_chase({ repath_period = 1.0, dest_slack = 3.5, min_gap = 0.35 })
    end
  end
  local targeting_lib = nil
  do
    local okt, mod = pcall(dofile, "scripts/lib/targeting.lua")
    if okt and mod then targeting_lib = mod end
  end

  -- grind: prefer scripts/lib/targeting (permissive + blacklist); fall back to legacy scan
  ai:register_action("select_grind_target", function(ctx)
    -- Rest window after low-HP: do not pull until rest_until expires.
    local rest_until = ctx.get_blackboard and ctx:get_blackboard("rest_until")
    if rest_until then
      local now = (bot.now_ms and (bot.now_ms() / 1000)) or os.time()
      if now < rest_until then
        return false
      end
      if ctx.set_blackboard then ctx:set_blackboard("rest_until", nil) end
    end

    -- gate: if already have live target, don't consume (let melee/ranged/loot participate)
    local tg = bot and bot.get_target and bot.get_target() or 0
    if tg ~= 0 and tg ~= "0" then
      local u = bot and bot.get_unit and bot.get_unit(tg) or nil
      local live = u and u.is_alive ~= false and ((u.max_health or 0) == 0 or (u.health or 0) > 0)
      if live then
        return false
      end
      -- Dead / missing: short blacklist so we don't re-stick to the corpse.
      if targeting_lib and targeting_lib.blacklist then
        targeting_lib.blacklist(tg, 12)
      end
      if bot.set_target then pcall(function() bot.set_target(0) end) end
      if bot.stop_attack then pcall(function() bot.stop_attack() end) end
    end

    -- Always use shared targeting (critter / vendor / dead filters). Never fall
    -- back to a permissive scan that picks 1-HP rabbits and then path-fails.
    local best = nil
    if targeting_lib and targeting_lib.find_best_hostile then
      best = targeting_lib.find_best_hostile({ max_dist = 40 })
    end
    if not best then
      return false
    end
    -- Hard reject tiny critters even if a filter was bypassed.
    if (best.max_health or 0) > 0 and (best.max_health or 0) <= 5 then
      if targeting_lib.blacklist then targeting_lib.blacklist(best.guid, 60) end
      return false
    end

    bot.set_target(best.guid)
    utils.log_decision(
      string.format(
        "grind target entry=%s dist=%.1f fac=%s hp=%s/%s pos=(%.1f,%.1f,%.1f)",
        tostring(best.entry),
        best.distance or -1,
        tostring(best.faction or 0),
        tostring(best.health or "?"),
        tostring(best.max_health or "?"),
        best.x or 0,
        best.y or 0,
        best.z or 0
      )
    )
    if bot.set_sheath then pcall(function() bot.set_sheath(0) end) end
    -- Always path toward the unit first; engage_melee will swing when close.
    -- Never open with ATTACKSWING at range (causes "stare" + NOT_IN_RANGE spam).
    if grind_chase then
      grind_chase:to_unit(best)
    elseif best.x ~= nil and best.y ~= nil and bot.move_to then
      bot.move_to(tonumber(best.x) or 0, tonumber(best.y) or 0, tonumber(best.z) or 0)
    end
    if bot.face_target then pcall(function() bot.face_target(best.guid) end) end
    return true
  end)

  -- Siege / PvP actions (registered always so scenario can enable("siege"))
  ai:register_action("select_siege_target", (siege and siege.select_siege_target) or function(ctx)
    -- fallback if module not providing
    local enemies = (values and values.find_nearby_enemy_players and values.find_nearby_enemy_players(45)) or {}
    if #enemies > 0 then
      local best = enemies[1]
      if best and best.guid then bot.set_target(best.guid); bot.attack(best.guid); return true end
    end
    return false
  end)

  ai:register_action("heal_lowest_ally", function(ctx)
    local ally = (values and values.find_lowest_hp_ally and values.find_lowest_hp_ally(40)) or nil
    if ally and ally.guid and bot.is_spell_ready then
      -- common healer spells (flash, renew, chain, etc) - class libs will have better
      if bot.is_spell_ready(2061) then return bot.cast_spell(2061, ally.guid) end -- flash
      if bot.is_spell_ready(139) then return bot.cast_spell(139, ally.guid) end
    end
    return false
  end)

  -- melee basics (sticky chase via grind_chase when available)
  ai:register_action("engage_melee", function(ctx)
    local tg = bot.get_target and bot.get_target() or 0
    if tg == 0 or tg == "0" then return false end
    local u = bot.get_unit and bot.get_unit(tg) or nil
    if not u or u.is_alive == false or ((u.max_health or 0) > 0 and (u.health or 0) <= 0) then
      return false
    end
    -- Skip 1-HP ambient units that slip past filters.
    if (u.max_health or 0) > 0 and (u.max_health or 0) <= 5 then
      if targeting_lib and targeting_lib.blacklist then targeting_lib.blacklist(tg, 30) end
      if bot.set_target then pcall(function() bot.set_target(0) end) end
      return false
    end
    if bot.set_sheath then pcall(function() bot.set_sheath(0) end) end

    -- Prefer geometric 3D distance (Z matters on hills; 2D can look "in melee"
    -- while the server rejects with NOT_IN_RANGE).
    local d = tonumber(u.distance) or 99
    if bot.get_position and u.x ~= nil and u.y ~= nil then
      local px, py, pz = bot.get_position()
      px, py, pz = px or 0, py or 0, pz or 0
      local dx = (tonumber(u.x) or 0) - px
      local dy = (tonumber(u.y) or 0) - py
      local dz = (tonumber(u.z) or pz) - pz
      local d3 = math.sqrt(dx * dx + dy * dy + dz * dz)
      if d3 > d then d = d3 end
    end

    if d > 3.2 then
      if grind_chase then
        grind_chase:to_unit(u)
      elseif bot.move_to and u.x ~= nil and u.y ~= nil then
        bot.move_to(tonumber(u.x) or 0, tonumber(u.y) or 0, tonumber(u.z) or 0)
      end
      return true
    end
    if bot.face_target then pcall(function() bot.face_target(tg) end) end
    if grind_chase then grind_chase:reset() end
    if movement_lib and movement_lib.stop_if_moving then
      movement_lib.stop_if_moving()
    elseif bot.stop_moving then
      bot.stop_moving()
    end
    if bot.set_target then pcall(function() bot.set_target(tg) end) end
    bot.attack(tg)
    return true
  end)

  -- ranged basics: only for true ranged classes (enabled selectively).
  -- Never white-swing from 8–25y — that freezes melee bots in "look at mob" pose.
  ai:register_action("engage_ranged", function(ctx)
    local tg = bot.get_target and bot.get_target() or 0
    if tg == 0 or tg == "0" then return false end
    local u = bot.get_unit and bot.get_unit(tg) or nil
    if not u or u.is_alive == false or ((u.max_health or 0) > 0 and (u.health or 0) <= 0) then
      return false
    end
    local d = u.distance or 99
    if bot.get_position and u.x ~= nil then
      local px, py, pz = bot.get_position()
      local dx = (tonumber(u.x) or 0) - (px or 0)
      local dy = (tonumber(u.y) or 0) - (py or 0)
      local dz = (tonumber(u.z) or 0) - (pz or 0)
      local d3 = math.sqrt(dx * dx + dy * dy + dz * dz)
      if d3 > d then d = d3 end
    end
    if bot.face_target then pcall(function() bot.face_target(tg) end) end
    -- Close into shoot range (≤30); only stop+auto when actually in range.
    if d > 30 then
      if bot.move_to and u.x ~= nil then
        bot.move_to(tonumber(u.x) or 0, tonumber(u.y) or 0, tonumber(u.z) or 0)
      end
      return true
    end
    if d > 5 and d <= 30 then
      if bot.stop_moving then bot.stop_moving() end
      if bot.set_target then pcall(function() bot.set_target(tg) end) end
      bot.attack(tg)
      return true
    end
    -- Too close for comfort: step out slightly for hunters; still attack.
    if bot.set_target then pcall(function() bot.set_target(tg) end) end
    bot.attack(tg)
    return true
  end)

  -- loot
  -- throttle to avoid spamming loot on same corpse
  local recent_loot = {}
  ai:register_action("loot_nearby", function(ctx)
    if ctx:get_value("in_combat") then return false end
    if not bot.get_nearby_units then return false end
    -- don't loot if there are live attackable targets (prefer grind/combat)
    do
      local near = bot.get_nearby_units(20) or {}
      for _, uu in ipairs(near) do
        if uu.is_alive and not uu.is_player then
          local ff = uu.flags or 0
          local nna = (ff % 4 >= 2) or (ff % 2097152 >= 1048576) or (ff % 256 >= 128) or (ff % 512 >= 256) or (ff % 131072 >= 65536) or (ff % 33554432 >= 16777216)
          local nn = uu.npc_flags or 0
          local fa = uu.faction or 0
          local fr = { [35]=true, [11]=true, [12]=true, [13]=true, [55]=true, [57]=true, [59]=true, [60]=true, [4]=true, [5]=true, [6]=true, [161]=true, [162]=true }
          if not nna and nn == 0 and not fr[fa] and (uu.health or 0) > 0 then
            return false
          end
        end
      end
    end
    local units = bot.get_nearby_units(15)
    for _, u in ipairs(units) do
      -- heuristic for lootable: dead + not player + close
      if not u.is_alive and not u.is_player then
        if u.distance and u.distance < 10 then
          local gs = tostring(u.guid)
          local now = os.time()
          if recent_loot[gs] and now - recent_loot[gs] < 5 then
            -- recently tried, skip to prevent loop
          else
            utils.log_decision("looting " .. gs)
            bot.loot_all(u.guid)
            recent_loot[gs] = now
            return true
          end
        end
      end
    end
    return false
  end)

  -- follow action (use blackboard or nearest friendly player)
  ai:register_action("follow_leader", function(ctx)
    if ctx:get_value("in_combat") then return false end
    local leader = ctx:get_blackboard("leader_guid") or 0
    if leader == 0 and bot.get_nearby_players then
      local pls = bot.get_nearby_players(20) or {}
      for _, p in ipairs(pls) do
        if p.guid and p.guid ~= (bot.get_guid and bot.get_guid() or 0) then
          leader = p.guid; break
        end
      end
    end
    if leader ~= 0 and bot.get_unit then
      local u = bot.get_unit(leader)
      if u and u.is_alive and (u.distance or 0) > 3 then
        utils.log_decision("follow: moving to leader")
        bot.move_to(u.x or 0, u.y or 0, u.z or 0)
        return true
      end
    end
    return false
  end)

  -- noncombat buff
  ai:register_action("generic_buff_self", function(ctx)
    if ctx:get_value("in_combat") then return false end
    -- minimal: try common if ready (class libs handle specifics); example fort if priest etc
    if bot.is_spell_ready and bot.is_spell_ready(21562) then -- fort
      utils.log_decision("buff: fortitude")
      bot.cast_spell(21562, 0)
      return true
    end
    return false
  end)

  -- rest: OOC heal-up between pulls (beats grind select when HP/power low).
  -- Yields to lootable corpses so we do not skip loot while recovering, but
  -- still arms rest_until so select_grind_target (relevance 25–30) cannot
  -- steal the tick from loot_nearby (15–20) while HP/power is low.
  ai:register_action("rest_if_low", function(ctx)
    if ctx:get_value("in_combat") then
      if ctx.set_blackboard then ctx:set_blackboard("rest_until", nil) end
      return false
    end
    local now = (bot.now_ms and (bot.now_ms() / 1000)) or os.time()
    local hp = ctx:get_value("health_pct") or 100
    local pp = ctx:get_value("power_pct") or 100
    -- Rage/runic/energy recover in combat; only mana casters rest on low power.
    local power_type = bot.get_power_type and bot.get_power_type() or 0
    local low_power = (power_type == 0) and pp < 25 -- 0 = mana
    local needs_rest = (hp < 40) or low_power

    local function arm_rest_until()
      if not needs_rest then return end
      local secs = (hp < 25) and 10 or 6
      if ctx.set_blackboard then ctx:set_blackboard("rest_until", now + secs) end
    end

    -- Prefer looting first: return false so loot_nearby can run, but arm the
    -- rest window first so grind cannot pull while we recover.
    if bot.get_nearby_units then
      for _, u in ipairs(bot.get_nearby_units(12) or {}) do
        if not u.is_player and (u.lootable or u.is_alive == false) and (u.distance or 99) < 10 then
          arm_rest_until()
          return false
        end
      end
    end
    local rest_until = ctx.get_blackboard and ctx:get_blackboard("rest_until")
    if rest_until and now < rest_until then
      if bot.stop_moving then bot.stop_moving() end
      return true
    end
    -- Resume grinding once mostly healthy (don't stick in rest at 50% forever).
    if needs_rest then
      local secs = (hp < 25) and 10 or 6
      if ctx.set_blackboard then ctx:set_blackboard("rest_until", now + secs) end
      utils.log_decision(
        "rest: low resources (hp="
          .. math.floor(hp)
          .. "% power="
          .. math.floor(pp)
          .. "%) — wait "
          .. secs
          .. "s"
      )
      if bot.stop_moving then bot.stop_moving() end
      if bot.stop_attack then pcall(function() bot.stop_attack() end) end
      return true
    end
    return false
  end)

  -- wander when idle (no target, not resting) — small steps only (avoid huge detours)
  local wander_lib = nil
  do
    local okw, wmod = pcall(dofile, "scripts/lib/movement.lua")
    if okw and wmod then wander_lib = wmod.new_wander({ period = 3.0, radius = 12 }) end
  end
  ai:register_action("wander_idle", function(ctx)
    if ctx:get_value("in_combat") then return false end
    local rest_until = ctx.get_blackboard and ctx:get_blackboard("rest_until")
    if rest_until then
      local now = (bot.now_ms and (bot.now_ms() / 1000)) or os.time()
      if now < rest_until then return false end
    end
    local tg = bot.get_target and bot.get_target() or 0
    if tg ~= 0 and tg ~= "0" then
      local u = bot.get_unit and bot.get_unit(tg) or nil
      if u and u.is_alive ~= false and ((u.max_health or 0) == 0 or (u.health or 0) > 0) then
        return false
      end
    end
    -- Prefer another hostile nearby over wandering off into the distance.
    if targeting_lib and targeting_lib.find_best_hostile then
      local b = targeting_lib.find_best_hostile({ max_dist = 40 })
      if b then return false end
    end
    if wander_lib then
      return wander_lib:step()
    end
    if bot.get_position and bot.move_to then
      local x, y, z = bot.get_position()
      local t = (bot.now_ms and bot.now_ms() or 0) / 1000
      local a = (t * 11.3) % (2 * math.pi)
      bot.move_to((x or 0) + math.cos(a) * 8, (y or 0) + math.sin(a) * 8, z or 0)
      return true
    end
    return false
  end)

  -- simple rpg idle
  ai:register_action("rpg_idle_emote", function(ctx)
    if ctx:get_value("in_combat") then return false end
    -- occasional, use tick % for throttle
    if (ctx._tick or 0) % 30 == 0 then
      utils.log_decision("rpg: idle emote")
      if bot.send_chat then bot.send_chat("emote", "stretches") end
    end
    return false
  end)

  -- pet management actions (generic; class-aware to avoid cross-class spell IDs e.g. hunter 883 on warlock)
  ai:register_action("pet_call_or_revive", function(ctx)
    local exists = ctx:get_value("pet_exists")
    if exists then return false end
    local cls = (bot.get_class and bot.get_class()) or 0
    if cls == 3 then
      -- hunter only
      if bot.is_spell_ready and bot.is_spell_ready(982) then -- REVIVE_PET
        utils.log_decision("pet: revive pet")
        return bot.cast_spell(982, 0)
      end
      if bot.is_spell_ready and bot.is_spell_ready(883) then -- CALL_PET
        utils.log_decision("pet: call pet")
        return bot.cast_spell(883, 0)
      end
    end
    -- warlock uses its class-specific warlock_summon (guarded); other classes fall through
    return false
  end)

  ai:register_action("pet_mend", function(ctx)
    local cls = (bot.get_class and bot.get_class()) or 0
    if cls ~= 3 then return false end -- hunter-only mend
    local pg = bot.get_pet_guid and bot.get_pet_guid() or 0
    if pg ~= 0 and bot.is_spell_ready and bot.is_spell_ready(136) then -- MEND_PET
      utils.log_decision("pet: mend pet (hp=" .. math.floor(ctx:get_value("pet_health_pct") or 0) .. "%)")
      return bot.cast_spell(136, pg)
    end
    return false
  end)

  ai:register_action("pet_attack", function(ctx)
    local t = bot.get_target and bot.get_target() or 0
    local pg = bot.get_pet_guid and bot.get_pet_guid() or 0
    if (t ~= 0 and t ~= "0") and (pg ~= 0 and pg ~= "0") and bot.pet_attack then
      utils.log_decision("pet: pet attack")
      bot.pet_attack(t)
      return true
    end
    return false
  end)

  -- attach convenience for default enable (supports ai = dofile(); ai:enable_default_strategies())
  function ai:enable_default_strategies()
    self:enable("survive")
    self:enable("rest")
    self:enable("grind")
    self:enable("loot")
    self:enable("melee")
    -- Ranged engage must NOT be on for melee classes: its relevance (9) beats
    -- engage_melee (7) and it stop_moving+swing at 8–25y → "stare from range".
    local cls = (bot and bot.get_class and bot.get_class()) or 0
    if cls == 3 or cls == 5 or cls == 8 or cls == 9 then -- hunter, priest, mage, warlock
      self:enable("ranged")
    end
    -- Single primary class spec only (see enable_class_defaults).
    enable_class_defaults(self)
    return self
  end

  function ai:set_primary_spec(name)
    return set_primary_spec(self, name)
  end

  -- helper for enabling simple rpg/noncombat generics (addresses review)
  function ai:enable_rpg_mode()
    self:enable("follow")
    self:enable("rest")
    self:enable("buff")
    self:enable("rpg")
    return self
  end


  return ai
end

function M.new()
  return create_ai_engine()
end

function M.load_for_bot()
  local ai = create_ai_engine()
  local cls = (bot and bot.get_class and bot.get_class()) or 0
  utils.log_decision("load_for_bot class=" .. tostring(cls) .. " (with class libs if available)")
  ai:enable_default_strategies()
  -- (ranged + class defaults now handled inside enable_default_strategies via enable_class_defaults)
  return ai
end

-- also expose for advanced use
M.Engine = engine_mod.Engine
M.create = create_ai_engine

-- When used as: local ai = dofile("scripts/ai/init.lua")
-- we return a fresh pre-wired engine instance (with enable_default_strategies method attached).
-- This satisfies: ai = dofile(...) ; ai:enable_default_strategies() ; function on_tick() ai:Tick() end
-- Class libs auto-registered inside create_ai_engine based on bot.get_class() (requires bot ready at load time; documented).
local default_ai = create_ai_engine()
-- Attach module helpers on the instance so patterns like dofile().load_for_bot() and .new work (addresses doc)
default_ai.load_for_bot = M.load_for_bot
default_ai.new = M.new
default_ai.Engine = M.Engine
default_ai.create = M.create
return default_ai
