-- grind.lua — thin warrior entrypoint for melee grind.
-- Core logic lives in scripts/lib/* so other scripts can reuse it.
--
--   ./azghost --profile local-ac cli --bot-mode lua --lua-script scripts/grind.lua

local melee_grind = dofile("scripts/lib/melee_grind.lua")

-- Prefer shared spell table when present
local SPELLS = {
  HEROIC_STRIKE = 78,
  REND = 772,
  CHARGE = 100,
  EXECUTE = 5308,
  BATTLE_SHOUT = 6673, -- real shout (not 2457 stance)
  VICTORY_RUSH = 34428,
  SUNDER_ARMOR = 7386,
}
local ok, data = pcall(dofile, "scripts/ai/data/warrior_spells.lua")
if ok and data and data.SPELLS then
  for k, v in pairs(data.SPELLS) do
    if SPELLS[k] == nil then
      SPELLS[k] = v
    end
  end
  -- Prefer researched aura id for shout when provided
  if data.AURAS and data.AURAS.BATTLE_SHOUT then
    SPELLS.BATTLE_SHOUT_AURA = data.AURAS.BATTLE_SHOUT
  end
end

local COSTS = {
  [SPELLS.HEROIC_STRIKE] = 15,
  [SPELLS.REND] = 10,
  [SPELLS.EXECUTE] = 15,
  [SPELLS.BATTLE_SHOUT] = 10,
  [SPELLS.SUNDER_ARMOR] = 15,
  [SPELLS.CHARGE] = 0,
  [SPELLS.VICTORY_RUSH] = 0,
}

local controller = melee_grind.new({
  spells = SPELLS,
  costs = COSTS,
  charge_spell = SPELLS.CHARGE,
  shout_spell = SPELLS.BATTLE_SHOUT,
  scan_range = 40,
  melee_stop = 2.8,
  melee_chase = 4.5,
  settle = 0.5,
  wander = { period = 2.0, radius = 24 },
  chase = { repath_period = 1.2, dest_slack = 5.0, min_gap = 0.5 },
  rotation = function(ctx)
    local S = ctx.spells
    local c = ctx.caster
    local r = ctx.rage
    if S.EXECUTE and ctx.hp_pct < 20 and r >= 15 then
      if c:try_cast(S.EXECUTE, ctx.guid) then
        return
      end
    end
    if S.VICTORY_RUSH and c:try_cast(S.VICTORY_RUSH, ctx.guid) then
      return
    end
    if S.REND and bot.has_aura_on and not bot.has_aura_on(ctx.guid, S.REND) and r >= 10 then
      if c:try_cast(S.REND, ctx.guid) then
        return
      end
    end
    -- Surplus rage only — avoids NO_POWER spam on HS
    if S.HEROIC_STRIKE and r >= 45 then
      if c:try_cast(S.HEROIC_STRIKE, ctx.guid) then
        return
      end
    end
    if S.SUNDER_ARMOR and r >= 30 then
      c:try_cast(S.SUNDER_ARMOR, ctx.guid)
    end
  end,
})

function on_tick()
  controller:tick()
end
