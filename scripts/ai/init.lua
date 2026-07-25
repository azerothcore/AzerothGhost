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

local function enable_class_defaults(ai, cls)
  cls = cls or (bot and bot.get_class and bot.get_class()) or 0
  -- registered-based (for enable_default after wiring)
  if ai.registered_strategies["generic_warrior"] then
    ai:enable("generic_warrior"); ai:enable("arms"); ai:enable("fury"); ai:enable("prot")
  end
  if ai.registered_strategies["hunter_generic"] then
    ai:enable("hunter_generic"); ai:enable("beast_mastery"); ai:enable("pet_management")
  end
  if ai.registered_strategies["mage_generic"] then
    ai:enable("mage_generic"); ai:enable("fire")
  end
  -- remaining classes (enable main + some declared variants)
  if ai.registered_strategies["generic_paladin"] then ai:enable("generic_paladin"); ai:enable("retribution"); ai:enable("protection"); ai:enable("holy") end
  if ai.registered_strategies["rogue_generic"] then ai:enable("rogue_generic"); ai:enable("assassination"); ai:enable("combat"); ai:enable("subtlety") end
  if ai.registered_strategies["priest_generic"] then ai:enable("priest_generic"); ai:enable("shadow"); ai:enable("holy_priest"); ai:enable("discipline") end
  if ai.registered_strategies["dk_generic"] then ai:enable("dk_generic"); ai:enable("blood"); ai:enable("frost_dk"); ai:enable("unholy") end
  if ai.registered_strategies["shaman_generic"] then ai:enable("shaman_generic"); ai:enable("elemental"); ai:enable("enhancement"); ai:enable("resto_shaman") end
  if ai.registered_strategies["warlock_generic"] then ai:enable("warlock_generic"); ai:enable("destruction"); ai:enable("affliction"); ai:enable("demonology"); ai:enable("pet_management") end
  if ai.registered_strategies["druid_generic"] then ai:enable("druid_generic"); ai:enable("balance"); ai:enable("feral"); ai:enable("resto_druid") end

  -- cls-based extras (for load_for_bot)
  if cls == 1 then
    ai:enable("generic_warrior")
    ai:enable("arms")
  elseif cls == 2 then
    ai:enable("generic_paladin")
    ai:enable("retribution")
  elseif cls == 3 then
    ai:enable("hunter_generic")
    ai:enable("beast_mastery")
    ai:enable("ranged")
    ai:enable("pet_management")
  elseif cls == 4 then
    ai:enable("rogue_generic")
    ai:enable("assassination")
  elseif cls == 5 then
    ai:enable("priest_generic")
    ai:enable("shadow")
  elseif cls == 6 then
    ai:enable("dk_generic")
    ai:enable("blood")
  elseif cls == 7 then
    ai:enable("shaman_generic")
    ai:enable("elemental")
  elseif cls == 8 then
    ai:enable("mage_generic")
    ai:enable("fire")
  elseif cls == 9 then
    ai:enable("warlock_generic")
    ai:enable("destruction")
    ai:enable("pet_management")
  elseif cls == 11 then
    ai:enable("druid_generic")
    ai:enable("balance")
  end
  -- also ensure some additional declared specs are enabled for coverage (user can disable)
  if cls == 1 then ai:enable("fury"); ai:enable("prot") end
  if cls == 2 then ai:enable("protection"); ai:enable("holy") end
  if cls == 4 then ai:enable("combat"); ai:enable("subtlety") end
  if cls == 5 then ai:enable("holy_priest"); ai:enable("discipline") end
  if cls == 6 then ai:enable("frost_dk"); ai:enable("unholy") end
  if cls == 7 then ai:enable("enhancement"); ai:enable("resto_shaman") end
  if cls == 9 then ai:enable("affliction"); ai:enable("demonology") end
  if cls == 11 then ai:enable("feral"); ai:enable("resto_druid") end

end

-- spec detection heuristics (called optionally from examples or after load; uses known high spells, auras, power)
local function detect_spec(cls)
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
    if hp < 25 then
      utils.log_decision("low health (" .. math.floor(hp) .. "%) - attempting flee/revive logic")
      -- basic: stop and perhaps move back or just log; potions require items not in basic API
      bot.stop_moving()
      -- could send .cooldown or eat but keep minimal and non-breaking
      return true
    end
    return false
  end)

  -- grind: improved target selection using values
  ai:register_action("select_grind_target", function(ctx)
    -- gate: if already have live target, don't consume (let melee/ranged/loot participate)
    local tg = bot and bot.get_target and bot.get_target() or 0
    if tg ~= 0 and tg ~= "0" then
      local u = bot and bot.get_unit and bot.get_unit(tg) or nil
      if u and u.is_alive and (u.health or 0) > 0 then return false end
    end
    if not bot.get_nearby_units then return false end
    local units = bot.get_nearby_units(30)
    local best = nil
    local best_score = 999999
    local my_level = bot.get_level and bot.get_level() or 1

    for _, u in ipairs(units) do
      local flags = u.flags or 0
      local npc = u.npc_flags or 0
      local non_attack = (flags % 4 >= 2)           -- NON_ATTACKABLE (0x2)
                      or (flags % 2097152 >= 1048576) -- TAXI_FLIGHT (0x100000) from AC _IsValidAttackTarget
                      or (flags % 256 >= 128)       -- NOT_ATTACKABLE_1 (0x80)
                      or (flags % 512 >= 256)       -- IMMUNE_TO_PC (0x100)
                      or (flags % 131072 >= 65536)  -- NON_ATTACKABLE_2 (0x10000)
                      or (flags % 33554432 >= 16777216) -- NOT_SELECTABLE (0x2000000)
      local fac = u.faction or 0
      local friendlyFacs = { [35]=true, [11]=true, [12]=true, [13]=true, [55]=true, [57]=true, [59]=true, [60]=true,
                             [4]=true, [5]=true, [6]=true, [161]=true, [162]=true }
      if friendlyFacs[fac] then
        non_attack = true
      end
      if u.is_alive and not u.is_player and not non_attack and npc == 0 then
        -- extra: health 0 means dead even if is_alive flag lags (prevents attacking corpses)
        if (u.health or 0) > 0 then
          local dist = u.distance or 999
          local lvl = u.level or 1
          local lvl_diff = math.abs(lvl - my_level)
          -- improved scoring: favor close + similar level (better than basic grind)
          local score = dist + (lvl_diff * 3)
          if score < best_score and dist > 1 and dist < 30 then
            best = u
            best_score = score
          end
        end
      end
    end

    if best then
      bot.set_target(best.guid)
      utils.log_decision("grind target: " .. tostring(best.guid) .. " dist=" .. math.floor(best.distance or 0) .. " fac=" .. tostring(best.faction or 0))
      -- Face before committing to attack (critical: incorrect target/facing -> server sends SMSG_ATTACKSWING_* notifying packet)
      if bot.set_sheath then pcall(function() bot.set_sheath(0) end) end
      if bot.face_target then pcall(function() bot.face_target(best.guid) end) end
      -- initiate attack/move here too for responsiveness
      if (best.distance or 0) > 3.5 then
        bot.move_to(best.x, best.y, best.z)
      else
        bot.stop_moving()
        -- Set target immediately before attack so external observers can see the bot's current attack target
        if bot.set_target then pcall(function() bot.set_target(best.guid) end) end
        bot.attack(best.guid)
      end
      return true
    end
    return false
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

  -- melee basics
  ai:register_action("engage_melee", function(ctx)
    local tg = bot.get_target and bot.get_target() or 0
    if tg == 0 or tg == "0" then return false end
    local u = bot.get_unit and bot.get_unit(tg) or nil
    if not u or not u.is_alive or (u.health or 0) <= 0 then return false end
    -- Face the target before attacking (server will notify via SMSG_ATTACKSWING_BADFACING etc if bad facing)
    if bot.set_sheath then pcall(function() bot.set_sheath(0) end) end -- unsheathe for melee
    if bot.face_target then pcall(function() bot.face_target(tg) end) end
    local d = u.distance or 0
    if d > 3.5 then
      bot.move_to(u.x, u.y, u.z)
    else
      bot.stop_moving()
      -- Set target immediately before attack so external observers can see the bot's current attack target
      if bot.set_target then pcall(function() bot.set_target(tg) end) end
      bot.attack(tg)
    end
    return true
  end)

  -- ranged basics (same engage logic; future will use cast at range)
  ai:register_action("engage_ranged", function(ctx)
    local tg = bot.get_target and bot.get_target() or 0
    if tg == 0 or tg == "0" then return false end
    local u = bot.get_unit and bot.get_unit(tg) or nil
    if not u or not u.is_alive or (u.health or 0) <= 0 then return false end
    if bot.face_target then pcall(function() bot.face_target(tg) end) end
    local d = u.distance or 0
    if d > 25 then
      bot.move_to(u.x, u.y, u.z)
    elseif d > 8 then
      -- try keep range but basic: stop and attack (auto shot equiv via attack)
      bot.stop_moving()
      -- Set target immediately before attack so external observers can see the bot's current attack target
      if bot.set_target then pcall(function() bot.set_target(tg) end) end
      bot.attack(tg)
    else
      bot.stop_moving()
      -- Set target immediately before attack so external observers can see the bot's current attack target
      if bot.set_target then pcall(function() bot.set_target(tg) end) end
      bot.attack(tg)
    end
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

  -- rest
  ai:register_action("rest_if_low", function(ctx)
    if ctx:get_value("in_combat") then return false end
    local hp = ctx:get_value("health_pct") or 100
    local pp = ctx:get_value("power_pct") or 100
    if hp < 40 or pp < 30 then
      utils.log_decision("rest: low resources, pausing")
      bot.stop_moving()
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
    self:enable("grind")
    self:enable("loot")
    self:enable("melee")
    self:enable("ranged")
    -- optionals (users can enable("follow") etc explicitly for simple rpg modes)
    -- if class strats were registered, enable a basic set for the class (shared logic)
    enable_class_defaults(self)
    return self
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
