-- dungeon.lua - Simple dungeon runner example (placeholder)
-- light pull, cc, boss mechanics (avoid/interrupt via triggers + blackboard)
-- Use with ai framework: set bb hints like "need_cc", "pull_now" from scenario.

local ai = nil
local function ensure_ai()
  if ai then return ai end
  local ok, mod = pcall(dofile, "scripts/ai/init.lua")
  if ok and mod then
    ai = mod
    ai:enable_default_strategies()
    -- dungeon specific
    if ai.enable then
      -- could enable more
    end
  end
  return ai
end

function on_tick()
    if not bot.is_alive() then
        bot.send_command(".revive")
        return
    end
    local a = ensure_ai()
    if a and a.set_blackboard then
      -- example: scenario can set, or simple detect multi for cc
      -- light: clear when not needed (hints only; real scenarios drive + clear after cc action)
      local cnt = (a.get_value and a:get_value("enemy_count")) or 0
      if cnt > 2 then
        a:set_blackboard("need_cc", true)
      else
        a:set_blackboard("need_cc", false)
      end
    end
    if a and a.Tick then
      a:Tick()
      return
    end
    -- fallback stub
    bot.log("dungeon mode: light pull/cc via ai")
    -- pull logic light: if no target, pick close
    if (bot.get_target and bot.get_target() or 0) == 0 and bot.get_nearby_units then
      local us = bot.get_nearby_units(25)
      for _,u in ipairs(us) do if u.is_alive and not u.is_player then bot.set_target(u.guid); break end end
    end
end
