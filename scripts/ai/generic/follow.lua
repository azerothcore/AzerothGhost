-- scripts/ai/generic/follow.lua
-- Generic follow: follow a leader (from blackboard or nearest player) when not in combat.
-- Uses blackboard for scenario coordination (e.g. ai:set_blackboard("leader_guid", g); full usage opt-in via scenario per review).

local strategy_mod = dofile("scripts/ai/core/strategy.lua")
local Strategy = strategy_mod.Strategy

local M = {}

local FollowStrategy = Strategy:new({name = "follow"})

function FollowStrategy:getName()
  return "follow"
end

function FollowStrategy:getType()
  return {"generic", "noncombat", "follow", "rpg"}
end

function FollowStrategy:getDefaultActions()
  return {
    {name = "follow_leader", relevance = 5},
  }
end

-- actions registered in init.lua

M.FollowStrategy = FollowStrategy
return M
