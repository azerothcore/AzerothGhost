-- scripts/validation/harness_base.lua
-- Reusable base for focused validation scripts.
-- Provides:
--   setup()  -- common GM/guild/tele/learn skeleton
--   on_validation_tick() pattern hook
--   counters, final summary printer, PASS/FAIL emitter
--   tick loop skeleton (callers still own function on_tick usually)
--
-- Example usage skeleton in a test file:
--   local h = dofile("scripts/validation/harness_base.lua")
--   local v = dofile("scripts/validation/setup.lua")
--   h.init({name="core-survive"})
--   v.setup...()
--   function on_tick()
--     h.tick()
--     -- test specific logic
--     if condition then h.pass("reason") end
--   end

local M = {}

M.state = {
  name = "unnamed-validation",
  tick = 0,
  start_tick = 0,
  passed = false,
  pass_reason = "",
  fail_reason = "",
  counters = {},
  validation = false,
  max_ticks = 200,
}

function M.init(opts)
  opts = opts or {}
  M.state.name = opts.name or "val-test"
  M.state.max_ticks = opts.max_ticks or 200
  M.state.validation = (bot and bot.validation_mode and bot.validation_mode()) or false
  M.state.counters = {}
  M.log("harness init name=" .. M.state.name .. " validation=" .. tostring(M.state.validation))
end

function M.log(msg)
  if bot and bot.log then
    bot.log("[VAL:" .. M.state.name .. "] " .. tostring(msg))
  end
end

function M.inc(k, n)
  n = n or 1
  M.state.counters[k] = (M.state.counters[k] or 0) + n
end

function M.get(k)
  return M.state.counters[k] or 0
end

function M.tick()
  M.state.tick = M.state.tick + 1
  if M.state.tick == 1 then
    M.state.start_tick = M.state.tick
    M.log("start")
  end
end

function M.is_validation()
  return M.state.validation
end

function M.pass(reason)
  if not M.state.passed then
    M.state.passed = true
    M.state.pass_reason = reason or "criteria met"
    M.log("PASS: " .. M.state.pass_reason)
    if M.state.validation then
      M.log("VALIDATION_RESULT: PASS " .. M.state.name .. " " .. M.state.pass_reason)
    end
  end
end

function M.fail(reason)
  if not M.state.passed then
    M.state.fail_reason = reason or "criteria not met"
    M.log("FAIL: " .. M.state.fail_reason)
    if M.state.validation then
      M.log("VALIDATION_RESULT: FAIL " .. M.state.name .. " " .. M.state.fail_reason)
    end
  end
end

function M.summary()
  local s = M.state
  local cstr = ""
  for k, v in pairs(s.counters) do
    cstr = cstr .. k .. "=" .. v .. " "
  end
  local status = s.passed and ("PASS " .. s.pass_reason) or (s.fail_reason ~= "" and ("FAIL " .. s.fail_reason) or "INCOMPLETE")
  local line = string.format("VALIDATION_SUMMARY: name=%s ticks=%d status=%s counters=%s", s.name, s.tick, status, cstr)
  if bot and bot.log then bot.log(line) end
  print(line)  -- also to stdout for harness consumers
  return s.passed
end

-- Optional: callers can call this at end of on_tick to auto-timeout
function M.auto_timeout()
  if M.state.tick > M.state.max_ticks then
    if not M.state.passed then
      M.fail("timeout without pass condition")
    end
    M.summary()
    -- Note: actual exit controlled by --duration on CLI
  end
end

return M
