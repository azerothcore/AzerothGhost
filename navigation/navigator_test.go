package navigation

import (
	"testing"

	"github.com/azerothcore/AzerothGhost/pathfinding"
)

func TestConvertResultRejectsSyntheticStraightPaths(t *testing.T) {
	got := convertResult(&pathfinding.PathResult{
		Type: pathfinding.PathfindNormal | pathfinding.PathfindNotUsingPath,
		Points: []pathfinding.Point3D{
			{X: 0, Y: 0, Z: 0},
			{X: 20, Y: 0, Z: 5},
		},
	})
	if got.Found {
		t.Fatalf("synthetic path was marked found")
	}
}

func TestConvertResultAcceptsCappedNavPaths(t *testing.T) {
	got := convertResult(&pathfinding.PathResult{
		Type: pathfinding.PathfindNormal | pathfinding.PathfindShort,
		Points: []pathfinding.Point3D{
			{X: 0, Y: 0, Z: 0},
			{X: 3, Y: 0, Z: 0},
			{X: 6, Y: 0, Z: 0},
		},
	})
	if !got.Found {
		t.Fatalf("capped nav path was marked missing")
	}
}
