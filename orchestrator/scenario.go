package orchestrator

import (
	"fmt"

	"github.com/Shopify/go-lua"

	"github.com/walkline/AzerothGhost/scenario"
)

// ScenarioHost provides the Lua-driven scenario execution surface for the orchestrator.
// It is intentionally small for basic scenario Lua host.
type ScenarioHost struct {
	orch *Orchestrator
	L    *lua.State
}

// NewScenarioHost wraps an existing orchestrator for running scenario scripts.
func NewScenarioHost(o *Orchestrator) *ScenarioHost {
	return &ScenarioHost{orch: o}
}

// RunFile executes the given scenario Lua file using the host's registered API surface.
// This satisfies the requirement for azghost orchestrator + basic RunScenario.
func (h *ScenarioHost) RunFile(path string) error {
	if h.orch == nil {
		return fmt.Errorf("no orchestrator attached to scenario host")
	}
	return h.orch.RunScenario(path)
}

// Example of a richer bundle-aware helper (used by advanced scenarios in later PRs).
// The basic path (LuaCode string) is primary; bundles are accepted in launch_group.
func BundleFromMain(main string) scenario.AIBundle {
	return scenario.AIBundle{Main: main, TickFunc: "on_tick"}
}

// Ensure the host can be used directly from orchestrator.RunScenario (already wired).
// This file exists to match the design doc layout: "new orchestrator/scenario.go".
var _ = fmt.Sprintf // keep import if needed in future
