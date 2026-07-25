// Package navigation provides pathfinding for the bot, supporting primarily
// an embedded mmap-based pathfinder (vendored). Remote gRPC is optional and
// deprioritized (stubbed here; full support can be added behind a build tag).
package navigation

import (
	"fmt"
	"math"
	"sync"

	"github.com/walkline/AzerothGhost/pathfinding"
)

// Point3D is a 3D position in game world coordinates.
type Point3D struct {
	X, Y, Z float32
}

// DistanceTo computes 3D distance.
func (p Point3D) DistanceTo(o Point3D) float32 {
	dx := p.X - o.X
	dy := p.Y - o.Y
	dz := p.Z - o.Z
	return float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))
}

// DistanceTo2D computes 2D distance (ignoring Z).
func (p Point3D) DistanceTo2D(o Point3D) float32 {
	dx := p.X - o.X
	dy := p.Y - o.Y
	return float32(math.Sqrt(float64(dx*dx + dy*dy)))
}

// PathResult holds the result of a pathfinding query.
type PathResult struct {
	Found  bool
	Points []Point3D
}

// Navigator is the interface for pathfinding.
type Navigator interface {
	// FindPath computes a path from start to dest on the given map.
	FindPath(mapID uint32, start, dest Point3D) (*PathResult, error)

	// FindRandomPath finds a random walkable point near center.
	FindRandomPath(mapID uint32, center Point3D, radius float32) (*PathResult, error)

	// GetHeight returns the ground height (Z) at the given point using the pathfinding data (maps + baked vmaps).
	GetHeight(mapID uint32, x, y, z float32) (float32, bool)

	// GetTerrainHeight returns terrain-only height (no vmaps) if available.
	GetTerrainHeight(mapID uint32, x, y float32) (float32, bool)

	// Close releases resources.
	Close()
}

// ============================================================
// Embedded navigator (uses mmap files directly)
// ============================================================

var (
	embeddedMu   sync.Mutex
	embeddedSvcs = make(map[string]*pathfinding.PathFindingService)
)

func embeddedCacheKey(dataDir string) string {
	return dataDir
}

func getOrCreateEmbeddedService(dataDir string) *pathfinding.PathFindingService {
	embeddedMu.Lock()
	defer embeddedMu.Unlock()
	key := embeddedCacheKey(dataDir)
	if svc, ok := embeddedSvcs[key]; ok {
		return svc
	}

	var svc *pathfinding.PathFindingService
	if dataDir == "" {
		svc = pathfinding.NewPathFindingServiceWithMMapMgr(pathfinding.NewMMapManager(""), nil, nil)
	} else {
		svc = pathfinding.NewPathFindingServiceFromDataDir(dataDir)
	}

	embeddedSvcs[key] = svc
	return svc
}

type embeddedNavigator struct {
	svc *pathfinding.PathFindingService
}

// NewEmbeddedNavigator creates a navigator using a single data directory root
// (containing mmaps/, maps/, vmaps/ subdirectories).
// All navigators for the same dataDir share a single underlying service.
func NewEmbeddedNavigator(dataDir string) Navigator {
	return &embeddedNavigator{
		svc: getOrCreateEmbeddedService(dataDir),
	}
}

func (n *embeddedNavigator) FindPath(mapID uint32, start, dest Point3D) (*PathResult, error) {
	result, err := n.svc.FindPath(mapID, pathfinding.Point3D{X: start.X, Y: start.Y, Z: start.Z},
		pathfinding.Point3D{X: dest.X, Y: dest.Y, Z: dest.Z})
	if err != nil {
		return nil, err
	}
	return convertResult(result), nil
}

func (n *embeddedNavigator) FindRandomPath(mapID uint32, center Point3D, radius float32) (*PathResult, error) {
	result, err := n.svc.FindRandomPath(mapID, pathfinding.Point3D{X: center.X, Y: center.Y, Z: center.Z}, radius)
	if err != nil {
		return nil, err
	}
	return convertResult(result), nil
}

func (n *embeddedNavigator) GetHeight(mapID uint32, x, y, z float32) (float32, bool) {
	return n.svc.GetHeight(mapID, x, y, z)
}

func (n *embeddedNavigator) GetTerrainHeight(mapID uint32, x, y float32) (float32, bool) {
	return n.svc.GetTerrainHeight(mapID, x, y)
}

func (n *embeddedNavigator) Close() {}

func convertResult(r *pathfinding.PathResult) *PathResult {
	if r == nil {
		return &PathResult{Found: false}
	}
	unusable := pathfinding.PathfindNopath | pathfinding.PathfindNotUsingPath
	found := r.Type&unusable == 0 && len(r.Points) > 0
	points := make([]Point3D, len(r.Points))
	for i, p := range r.Points {
		points[i] = Point3D{X: p.X, Y: p.Y, Z: p.Z}
	}
	return &PathResult{Found: found, Points: points}
}

// ============================================================
// Remote navigator (gRPC client) - stubbed / optional
// ============================================================

// NewRemoteNavigator creates a navigator that connects to a remote pathfinding service.
// In this extraction, remote is deprioritized in favor of embedded.
// Full gRPC support (requiring gen/pb + grpc deps) can be restored behind a build tag
// or separate optional package if needed for bridge scenarios.
func NewRemoteNavigator(address string) (Navigator, error) {
	return nil, fmt.Errorf("remote pathfinding (gRPC) is optional and not built-in for core use; use NewEmbeddedNavigator(dataDir) instead")
}
