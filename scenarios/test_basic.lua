-- Basic scenario test script. Calls registered orch.* functions.
-- Advanced AI framework (ai/core) available for custom bundles via dofile("scripts/ai/init.lua").
-- E2E calls to new bot.* APIs (will run if lua path active against live)
orch.log("scenario started")

asgs = orch.prepare_accounts()
orch.log("prepared " .. #asgs .. " assignments (basic)")

-- Launch a trivial idle-like inline script to the local path (will just NewBot+Run in bg)
orch.launch_group("local", {}, [[
  function on_tick()
    -- minimal no-op for test
    -- E2E calls to new bot.* APIs (will run if lua path active against live)
    if bot.has_aggro_on then
      bot.log("has_aggro_on(0)=" .. tostring(bot.has_aggro_on(0)))
      bot.log("can_cast(78,0)=" .. tostring(bot.can_cast(78, 0)))
      bot.log("get_facing=" .. tostring(bot.get_facing()))
      bot.log("get_stance=" .. tostring(bot.get_stance()))
      bot.log("has_aura_on(self,2457)=" .. tostring(bot.has_aura_on(bot.get_target() or 0, 2457)))
      bot.log("get_distance to target=" .. tostring(bot.get_distance(bot.get_target() or 0)))
    end
  end
]])

orch.sleep(50)
orch.log("scenario basic steps complete")
return { ok = true }
