-- Legacy / convenience entry for Orgrimmar Siege.
-- Prefer: scenarios/orgrimmar_siege.lua
-- This file simply requires the full scenario for backward compat.

local ok, mod = pcall(dofile, "scenarios/orgrimmar_siege.lua")
if ok and mod then return mod end

-- Fallback minimal
print("siege.lua: falling back to scenarios/orgrimmar_siege.lua")
dofile("scenarios/orgrimmar_siege.lua")
return { ok = true }
