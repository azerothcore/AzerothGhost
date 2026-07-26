-- grind.lua — production grind entry using the advanced strategy AI.
--
-- Behaviour (scripts/ai/*):
--   survive  death revive; critical HP OOC rests (no new pulls)
--   rest     OOC heal-up when HP is low (mana classes also rest on low mana)
--   grind    hostile pick (scripts/lib/targeting) + idle wander
--   loot     corpses when safe
--   melee    sticky chase + auto-attack (below class ability priority)
--   class    one primary spec rotation (not all specs at once)
--
-- Teleport/summon: engine polls bot.consume_teleport() and clears sticky state.
--
--   ./azghost --profile local-ac cli --bot-mode lua --lua-script scripts/grind.lua
--
-- Thin sticky-melee only: scripts/lib/melee_grind.lua

local boot = dofile("scripts/ai/init.lua")

-- Rebuild after bot.get_class() is valid; enables survive/rest/grind/loot/melee + one spec.
local ai
if boot.load_for_bot then
  ai = boot.load_for_bot()
else
  ai = boot
  if ai.enable_default_strategies then ai:enable_default_strategies() end
end

local boot_at = (bot and bot.now_ms and bot.now_ms() / 1000) or os.time()
local SETTLE = 0.8
local prepped = false

-- GM/dev accounts: brand-new level-1 toons have no weapon/spells and will only
-- "stare" at mobs. Best-effort prep (commands no-op if not GM).
local function ensure_combat_ready()
  if prepped or not bot or not bot.send_command then return end
  prepped = true
  local cls = bot.get_class and bot.get_class() or 0
  local level = bot.get_level and bot.get_level() or 1

  -- Always equip a white weapon and unsheathe so auto-attack can land.
  bot.send_command(".additem 25 1") -- Worn Shortsword
  bot.send_command(".equip 25")
  if bot.set_sheath then pcall(bot.set_sheath, 0) end

  if level < 6 then
    bot.send_command(".level 6")
    if bot.set_level then pcall(bot.set_level, 6) end
  end

  if cls == 1 then
    -- Learn Battle Stance once (2457). Do NOT re-cast stance every tick — it
    -- wastes GCD, can dump rage on some cores, and does nothing if already in it.
    bot.send_command(".learn 2457")
    bot.send_command(".cast 2457") -- enter stance once at prep
    -- Real Battle Shout is 6673 (not 2457).
    local spells = { 6673, 772, 100, 78, 5308, 34428, 7386, 6343, 2687 }
    for _, id in ipairs(spells) do
      bot.send_command(".learn " .. tostring(id))
    end
    if bot.log then bot.log("grind: warrior combat prep (stance once + kit + weapon)") end
  elseif bot.log then
    bot.log("grind: generic combat prep class=" .. tostring(cls) .. " (weapon+level)")
  end
end

if bot and bot.log then
  local cls = bot.get_class and bot.get_class() or "?"
  local spec = ai.detect_spec and ai.detect_spec() or nil
  bot.log(string.format(
    "grind: advanced AI ready class=%s primary_spec=%s",
    tostring(cls),
    tostring(spec or "default")
  ))
end

function on_tick()
  local now = (bot and bot.now_ms and bot.now_ms() / 1000) or os.time()
  if (now - boot_at) < SETTLE then
    return
  end

  -- Teleport first (also handled inside ai:Tick; consume once here for settle reset).
  if bot and bot.consume_teleport and bot.consume_teleport() then
    boot_at = now
    if ai.set_blackboard then
      ai:set_blackboard("rest_until", nil)
      ai:set_blackboard("teleported", true)
    end
    if bot.stop_moving then pcall(bot.stop_moving) end
    if bot.stop_attack then pcall(bot.stop_attack) end
    if bot.set_target then pcall(bot.set_target, 0) end
    if bot.log then bot.log("grind: teleport interrupt — settle + restart AI") end
    return
  end

  ensure_combat_ready()
  ai:Tick()
end
