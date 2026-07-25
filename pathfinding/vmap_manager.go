package pathfinding

import (
	"bufio"
	"container/list"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"sync"
	"time"
)

// VMAP support for accurate height queries matching AzerothCore.
// We implement enough to support GetHeight (downward ray for ground Z).
// Uses BIH trees and model instances from .vmtree / .vmtile + .vmo models.

// Constants from AC
const (
	VMAP_MAGIC     = "VMAP_3.0"
	VMAP_MAGIC_48  = "VMAP_4.8"
	INVALID_HEIGHT = -100000.0
	VMAP_mid       = 32.0 * 533.3333 // for internal <-> game conversion used inside vmaps

	defaultVMapTileLoadRadius = 1
	defaultVMapModelCacheMB   = 512
	vmapCacheLogInterval      = 30 * time.Second

	maxVMapBIHEntries        = 16 * 1024 * 1024
	maxVMapChunkBytes        = 256 * 1024 * 1024
	maxVMapGroupModels       = 100000
	maxVMapVerticesPerGroup  = 8 * 1024 * 1024
	maxVMapTrianglesPerGroup = 8 * 1024 * 1024
)

// Vector3 simple
type Vector3 struct {
	X, Y, Z float32
}

func (v Vector3) Sub(o Vector3) Vector3 {
	return Vector3{v.X - o.X, v.Y - o.Y, v.Z - o.Z}
}

func (v Vector3) Add(o Vector3) Vector3 {
	return Vector3{v.X + o.X, v.Y + o.Y, v.Z + o.Z}
}

func (v Vector3) Mul(s float32) Vector3 {
	return Vector3{v.X * s, v.Y * s, v.Z * s}
}

func (v Vector3) Dot(o Vector3) float32 {
	return v.X*o.X + v.Y*o.Y + v.Z*o.Z
}

func (v Vector3) Cross(o Vector3) Vector3 {
	return Vector3{
		v.Y*o.Z - v.Z*o.Y,
		v.Z*o.X - v.X*o.Z,
		v.X*o.Y - v.Y*o.X,
	}
}

// Ray
type Ray struct {
	Origin, Direction Vector3
}

func NewRay(origin, dir Vector3) Ray {
	return Ray{Origin: origin, Direction: dir}
}

// AABox
type AABox struct {
	Low, High Vector3
}

func (b AABox) IntersectRay(ray Ray, maxDist *float32) bool {
	// slab method simplified for our use
	tmin := float32(0)
	tmax := *maxDist
	for i := 0; i < 3; i++ {
		var t1, t2 float32
		switch i {
		case 0:
			t1 = (b.Low.X - ray.Origin.X) / ray.Direction.X
			t2 = (b.High.X - ray.Origin.X) / ray.Direction.X
		case 1:
			t1 = (b.Low.Y - ray.Origin.Y) / ray.Direction.Y
			t2 = (b.High.Y - ray.Origin.Y) / ray.Direction.Y
		case 2:
			t1 = (b.Low.Z - ray.Origin.Z) / ray.Direction.Z
			t2 = (b.High.Z - ray.Origin.Z) / ray.Direction.Z
		}
		if t1 > t2 {
			t1, t2 = t2, t1
		}
		if t1 > tmin {
			tmin = t1
		}
		if t2 < tmax {
			tmax = t2
		}
		if tmin > tmax {
			return false
		}
	}
	*maxDist = tmax
	return true
}

// BIH node encoding (from AC)
const (
	BIH_LEAF_MASK = 3 << 30
	BIH_BVH2_MASK = 1 << 29
)

// BIH simple port for ray intersect (for height we use downward)
type BIH struct {
	bounds AABox
	tree   []uint32
	objs   []uint32
}

func (b *BIH) readFromFile(f *os.File) error {
	return b.readFrom(f)
}

func (b *BIH) readFrom(r io.Reader) error {
	var lo, hi [3]float32
	if err := binary.Read(r, binary.LittleEndian, &lo); err != nil {
		return err
	}
	if err := binary.Read(r, binary.LittleEndian, &hi); err != nil {
		return err
	}
	b.bounds = AABox{Low: Vector3{lo[0], lo[1], lo[2]}, High: Vector3{hi[0], hi[1], hi[2]}}

	var treeSize uint32
	if err := binary.Read(r, binary.LittleEndian, &treeSize); err != nil {
		return err
	}
	if treeSize > maxVMapBIHEntries {
		return fmt.Errorf("vmap BIH tree too large: %d entries", treeSize)
	}
	b.tree = make([]uint32, treeSize)
	if err := binary.Read(r, binary.LittleEndian, &b.tree); err != nil {
		return err
	}
	var count uint32
	if err := binary.Read(r, binary.LittleEndian, &count); err != nil {
		return err
	}
	if count > maxVMapBIHEntries {
		return fmt.Errorf("vmap BIH object list too large: %d entries", count)
	}
	b.objs = make([]uint32, count)
	if err := binary.Read(r, binary.LittleEndian, &b.objs); err != nil {
		return err
	}
	return nil
}

// intersectRay for height: we want the smallest positive t for hit in negative Z dir.
func (b *BIH) intersectRay(ray Ray, maxDist *float32, stopAtFirst bool) bool {
	// Simplified stack based traversal ported from AC BIH
	// For downward rays we can optimize but keep general for correctness.
	invDir := Vector3{1 / ray.Direction.X, 1 / ray.Direction.Y, 1 / ray.Direction.Z}
	org := ray.Origin

	intervalMin := float32(0)
	intervalMax := *maxDist

	for i := 0; i < 3; i++ {
		var t1, t2 float32
		switch i {
		case 0:
			t1 = (b.bounds.Low.X - org.X) * invDir.X
			t2 = (b.bounds.High.X - org.X) * invDir.X
		case 1:
			t1 = (b.bounds.Low.Y - org.Y) * invDir.Y
			t2 = (b.bounds.High.Y - org.Y) * invDir.Y
		case 2:
			t1 = (b.bounds.Low.Z - org.Z) * invDir.Z
			t2 = (b.bounds.High.Z - org.Z) * invDir.Z
		}
		if t1 > t2 {
			t1, t2 = t2, t1
		}
		if t1 > intervalMin {
			intervalMin = t1
		}
		if t2 < intervalMax {
			intervalMax = t2
		}
		if intervalMax <= 0 || intervalMin >= *maxDist {
			return false
		}
	}
	if intervalMin > intervalMax {
		return false
	}
	intervalMin = max32(intervalMin, 0)
	intervalMax = min32(intervalMax, *maxDist)

	// offsets for signs
	offsetFront := [3]uint32{}
	offsetBack := [3]uint32{}
	for i := 0; i < 3; i++ {
		dirI := ray.Direction.X
		if i == 1 {
			dirI = ray.Direction.Y
		} else if i == 2 {
			dirI = ray.Direction.Z
		}
		bits := floatToRawIntBits(dirI) >> 31
		offsetFront[i] = bits
		offsetBack[i] = bits ^ 1
		offsetFront[i]++
		offsetBack[i]++
	}

	type stackNode struct {
		node int
		tmin float32
		tmax float32
	}
	stack := make([]stackNode, 0, 64)
	stack = append(stack, stackNode{0, intervalMin, intervalMax})

	hit := false
	for len(stack) > 0 {
		sn := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		node := sn.node
		tmin := sn.tmin
		tmax := sn.tmax

		for {
			if node >= len(b.tree) {
				break
			}
			tn := b.tree[node]
			axis := (tn & (3 << 30)) >> 30
			bvh2 := (tn & (1 << 29)) != 0
			offset := int(tn & ^(uint32(7) << 29))

			if !bvh2 {
				if axis < 3 {
					// interior
					tf := intBitsToFloat(b.tree[node+int(offsetFront[axis])]) - getCoord(org, axis)
					tf *= getInv(invDir, axis)
					tb := intBitsToFloat(b.tree[node+int(offsetBack[axis])]) - getCoord(org, axis)
					tb *= getInv(invDir, axis)

					if tf < tmin && tb > tmax {
						break
					}
					back := offset + int(offsetBack[axis])*3
					// push back if needed
					if tb >= tmin && tb <= tmax {
						stack = append(stack, stackNode{back, tmin, min32(tmax, tb)})
					}
					if tf <= tmax && tf >= tmin {
						node = offset + int(offsetFront[axis])*3
						tmax = min32(tmax, tf)
						continue
					}
					break
				} else {
					// leaf
					primStart := offset
					primCount := int(tn & 0x1FFFFFFF) // approx, actual encoding
					// For our simplified, we will handle in caller via objects
					// To make height work, we fall to objects for now.
					// Full impl would intersect primitives here.
					for j := 0; j < primCount && primStart+j < len(b.objs); j++ {
						// caller will use objects[b.objs[primStart+j]]
						_ = b.objs[primStart+j]
					}
					break
				}
			} else {
				// BVH2 node simplified handling
				break
			}
		}
	}
	return hit
}

// Helpers for bits
func floatToRawIntBits(f float32) uint32 {
	bits := math.Float32bits(f)
	return bits
}

func intBitsToFloat(i uint32) float32 {
	return math.Float32frombits(i)
}

func max32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func min32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func getCoord(v Vector3, axis uint32) float32 {
	switch axis {
	case 0:
		return v.X
	case 1:
		return v.Y
	default:
		return v.Z
	}
}

func getInv(v Vector3, axis uint32) float32 {
	switch axis {
	case 0:
		return v.X
	case 1:
		return v.Y
	default:
		return v.Z
	}
}

// ModelSpawn
type ModelSpawn struct {
	Flags  uint32
	ADTId  uint16
	ID     uint32
	iPos   Vector3
	iRot   Vector3
	iScale float32
	iBound [2]Vector3 // low,high if MOD_HAS_BOUND
	name   string
}

func (s *ModelSpawn) readFromFile(f *os.File) error {
	return s.readFrom(f)
}

func (s *ModelSpawn) readFrom(r io.Reader) error {
	if err := binary.Read(r, binary.LittleEndian, &s.Flags); err != nil {
		return err
	}
	if err := binary.Read(r, binary.LittleEndian, &s.ADTId); err != nil {
		return err
	}
	if err := binary.Read(r, binary.LittleEndian, &s.ID); err != nil {
		return err
	}
	var pos [3]float32
	if err := binary.Read(r, binary.LittleEndian, &pos); err != nil {
		return err
	}
	s.iPos = Vector3{pos[0], pos[1], pos[2]}
	var rot [3]float32
	if err := binary.Read(r, binary.LittleEndian, &rot); err != nil {
		return err
	}
	s.iRot = Vector3{rot[0], rot[1], rot[2]}
	if err := binary.Read(r, binary.LittleEndian, &s.iScale); err != nil {
		return err
	}
	hasBound := (s.Flags & 0x4) != 0 // MOD_HAS_BOUND
	if hasBound {
		var lo, hi [3]float32
		if err := binary.Read(r, binary.LittleEndian, &lo); err != nil {
			return err
		}
		if err := binary.Read(r, binary.LittleEndian, &hi); err != nil {
			return err
		}
		// vmap bounds are in internal rep too; convert + swap min/max because x' = mid - x (sign flip)
		s.iBound[0] = Vector3{VMAP_mid - hi[0], VMAP_mid - hi[1], lo[2]}
		s.iBound[1] = Vector3{VMAP_mid - lo[0], VMAP_mid - lo[1], hi[2]}
	}
	var nameLen uint32
	if err := binary.Read(r, binary.LittleEndian, &nameLen); err != nil {
		return err
	}
	nameBytes := make([]byte, nameLen)
	if _, err := io.ReadFull(r, nameBytes); err != nil {
		return err
	}
	s.name = string(nameBytes)
	return nil
}

// ModelInstance (simplified for height)
type ModelInstance struct {
	spawn     ModelSpawn
	iPos      Vector3
	iInvRot   [3][3]float32 // inverse rotation matrix
	iInvScale float32
}

func (mi *ModelInstance) initTransform() {
	mi.iPos = mi.spawn.iPos
	scale := mi.spawn.iScale
	if scale == 0 {
		scale = 1
	}
	mi.iInvScale = 1 / scale
	mi.iInvRot = inverseRotationMatrix(mi.spawn.iRot)
}

// inverseRotationMatrix builds the inverse of the ZYX euler rotation matrix used by AC (G3D fromEulerAnglesZYX).
// AC: fromEulerAnglesZYX( pi*rot.y/180, pi*rot.x/180, pi*rot.z/180 ).inverse()
// For orthonormal rotation, inverse == transpose.
func inverseRotationMatrix(rot Vector3) [3][3]float32 {
	// Build forward rotation R = Z(y) * Y(x) * X(z) matching G3D, then transpose for inverse.
	ry := rot.Y * (math.Pi / 180.0)
	rx := rot.X * (math.Pi / 180.0)
	rz := rot.Z * (math.Pi / 180.0)

	cz, sz := float32(math.Cos(float64(ry))), float32(math.Sin(float64(ry)))
	cy, sy := float32(math.Cos(float64(rx))), float32(math.Sin(float64(rx)))
	cx, sx := float32(math.Cos(float64(rz))), float32(math.Sin(float64(rz)))

	// kZ (arg0=ry)
	kz := [3][3]float32{
		{cz, -sz, 0},
		{sz, cz, 0},
		{0, 0, 1},
	}
	// kY (arg1=rx)
	ky := [3][3]float32{
		{cy, 0, sy},
		{0, 1, 0},
		{-sy, 0, cy},
	}
	// kX (arg2=rz)
	kx := [3][3]float32{
		{1, 0, 0},
		{0, cx, -sx},
		{0, sx, cx},
	}

	// R = kz * (ky * kx)
	tmp := mat3Mul(ky, kx)
	r := mat3Mul(kz, tmp)

	// inverse for pure rotation is transpose
	return [3][3]float32{
		{r[0][0], r[1][0], r[2][0]},
		{r[0][1], r[1][1], r[2][1]},
		{r[0][2], r[1][2], r[2][2]},
	}
}

func mat3Mul(a, b [3][3]float32) [3][3]float32 {
	var c [3][3]float32
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			c[i][j] = a[i][0]*b[0][j] + a[i][1]*b[1][j] + a[i][2]*b[2][j]
		}
	}
	return c
}

func mat3MulVec(m [3][3]float32, v Vector3) Vector3 {
	return Vector3{
		m[0][0]*v.X + m[0][1]*v.Y + m[0][2]*v.Z,
		m[1][0]*v.X + m[1][1]*v.Y + m[1][2]*v.Z,
		m[2][0]*v.X + m[2][1]*v.Y + m[2][2]*v.Z,
	}
}

// WorldModel simplified
type WorldModel struct {
	RootWMOID   uint32
	groupModels []GroupModel
	groupTree   BIH
}

func (w *WorldModel) readFile(filename string) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	br := bufio.NewReader(f)

	var chunk [8]byte
	if _, err := io.ReadFull(br, chunk[:]); err != nil {
		return err
	}
	magic := string(chunk[:])
	if magic != VMAP_MAGIC && magic != VMAP_MAGIC_48 {
		return fmt.Errorf("invalid vmap model magic %q", magic)
	}

	if err := readFourCC(br, "WMOD"); err != nil {
		return err
	}
	wmodSize, err := readUint32(br)
	if err != nil {
		return err
	}
	if wmodSize < 4 || wmodSize > 64 {
		return fmt.Errorf("invalid WMOD chunk size %d", wmodSize)
	}
	rootID, err := readUint32(br)
	if err != nil {
		return err
	}
	w.RootWMOID = rootID

	if err := readFourCC(br, "GMOD"); err != nil {
		return err
	}
	count, err := readUint32(br)
	if err != nil {
		return err
	}
	if count > maxVMapGroupModels {
		return fmt.Errorf("too many vmap group models: %d", count)
	}
	w.groupModels = make([]GroupModel, count)
	for i := uint32(0); i < count; i++ {
		if err := w.groupModels[i].readFrom(br); err != nil {
			return err
		}
	}
	if err := readFourCC(br, "GBIH"); err != nil {
		return err
	}
	if err := w.groupTree.readFrom(br); err != nil {
		return err
	}
	return nil
}

func readFourCC(r io.Reader, expected string) error {
	var chunk [4]byte
	if _, err := io.ReadFull(r, chunk[:]); err != nil {
		return err
	}
	got := string(chunk[:])
	if got != expected {
		return fmt.Errorf("unexpected vmap chunk %q, want %q", got, expected)
	}
	return nil
}

func readUint32(r io.Reader) (uint32, error) {
	var v uint32
	if err := binary.Read(r, binary.LittleEndian, &v); err != nil {
		return 0, err
	}
	return v, nil
}

func readFloat32(r io.Reader) (float32, error) {
	var v float32
	if err := binary.Read(r, binary.LittleEndian, &v); err != nil {
		return 0, err
	}
	return v, nil
}

func (w *WorldModel) getHeight(ray Ray, maxDist float32) float32 {
	// return min t , caller computes height
	minT := maxDist
	for i := range w.groupModels {
		t := w.groupModels[i].getHeight(ray, maxDist)
		if t < minT {
			minT = t
		}
	}
	return minT
}

func (w *WorldModel) estimatedBytes() int64 {
	if w == nil {
		return 0
	}
	total := int64(128 + len(w.groupModels)*128)
	total += int64(len(w.groupTree.tree))*4 + int64(len(w.groupTree.objs))*4
	for i := range w.groupModels {
		total += w.groupModels[i].estimatedBytes()
	}
	if total <= 0 {
		return 1
	}
	return total
}

// GroupModel
type GroupModel struct {
	TriFlags  uint32
	Mesh      []MeshTriangle
	Vertices  []Vector3
	bounds    AABox
	groupTree BIH // for triangles
}

func (g *GroupModel) readFromFile(f *os.File) error {
	return g.readFrom(f)
}

func (g *GroupModel) readFrom(r io.Reader) error {
	// VMAP_4.8 group layout:
	// bounds low/high, flags, group id, then VERT/TRIM/MBIH chunks.
	var lo, hi [3]float32
	for i := 0; i < 3; i++ {
		v, err := readFloat32(r)
		if err != nil {
			return err
		}
		lo[i] = v
	}
	for i := 0; i < 3; i++ {
		v, err := readFloat32(r)
		if err != nil {
			return err
		}
		hi[i] = v
	}
	g.bounds = AABox{Low: Vector3{lo[0], lo[1], lo[2]}, High: Vector3{hi[0], hi[1], hi[2]}}

	flags, err := readUint32(r)
	if err != nil {
		return err
	}
	g.TriFlags = flags
	if _, err := readUint32(r); err != nil {
		return err
	}

	if err := readFourCC(r, "VERT"); err != nil {
		return err
	}
	vertChunkSize, err := readUint32(r)
	if err != nil {
		return err
	}
	nVerts, err := readUint32(r)
	if err != nil {
		return err
	}
	if err := validateVMapVectorChunk("VERT", vertChunkSize, nVerts, 12, maxVMapVerticesPerGroup); err != nil {
		return err
	}
	g.Vertices = make([]Vector3, nVerts)
	for i := range g.Vertices {
		var v [3]float32
		if err := binary.Read(r, binary.LittleEndian, &v); err != nil {
			return err
		}
		g.Vertices[i] = Vector3{v[0], v[1], v[2]}
	}

	if err := readFourCC(r, "TRIM"); err != nil {
		return err
	}
	triChunkSize, err := readUint32(r)
	if err != nil {
		return err
	}
	nTris, err := readUint32(r)
	if err != nil {
		return err
	}
	if err := validateVMapVectorChunk("TRIM", triChunkSize, nTris, 12, maxVMapTrianglesPerGroup); err != nil {
		return err
	}
	g.Mesh = make([]MeshTriangle, nTris)
	for i := range g.Mesh {
		var tri [3]uint32
		if err := binary.Read(r, binary.LittleEndian, &tri); err != nil {
			return err
		}
		g.Mesh[i] = MeshTriangle{tri[0], tri[1], tri[2]}
	}

	if err := readFourCC(r, "MBIH"); err != nil {
		return err
	}
	return g.groupTree.readFrom(r)
}

func validateVMapVectorChunk(name string, chunkSize, count, elemBytes, maxCount uint32) error {
	if count > maxCount {
		return fmt.Errorf("%s count too large: %d", name, count)
	}
	if chunkSize > maxVMapChunkBytes {
		return fmt.Errorf("%s chunk too large: %d bytes", name, chunkSize)
	}
	expected := uint64(4) + uint64(count)*uint64(elemBytes)
	if expected > uint64(maxVMapChunkBytes) {
		return fmt.Errorf("%s expected chunk too large: %d bytes", name, expected)
	}
	if uint64(chunkSize) != expected {
		return fmt.Errorf("%s chunk size mismatch: got %d want %d", name, chunkSize, expected)
	}
	return nil
}

func (g *GroupModel) getHeight(ray Ray, maxDist float32) float32 {
	// Simple brute force triangle intersection for height (slow but correct for correctness; can optimize later with tree)
	bestT := maxDist
	for _, tri := range g.Mesh {
		if tri.idx0 >= uint32(len(g.Vertices)) || tri.idx1 >= uint32(len(g.Vertices)) || tri.idx2 >= uint32(len(g.Vertices)) {
			continue
		}
		v0 := g.Vertices[tri.idx0]
		v1 := g.Vertices[tri.idx1]
		v2 := g.Vertices[tri.idx2]

		// Möller–Trumbore intersection for ray
		edge1 := v1.Sub(v0)
		edge2 := v2.Sub(v0)
		h := ray.Direction.Cross(edge2)
		a := edge1.Dot(h)
		if math.Abs(float64(a)) < 1e-5 {
			continue // parallel
		}
		f := 1.0 / a
		s := ray.Origin.Sub(v0)
		u := f * s.Dot(h)
		if u < 0 || u > 1 {
			continue
		}
		q := s.Cross(edge1)
		v := f * ray.Direction.Dot(q)
		if v < 0 || u+v > 1 {
			continue
		}
		t := f * edge2.Dot(q)
		if t > 0.00001 && t < bestT {
			bestT = t
		}
	}
	if bestT < maxDist {
		return bestT
	}
	return math.MaxFloat32
}

func (g *GroupModel) estimatedBytes() int64 {
	if g == nil {
		return 0
	}
	total := int64(128)
	total += int64(len(g.Vertices)) * 12
	total += int64(len(g.Mesh)) * 12
	total += int64(len(g.groupTree.tree)) * 4
	total += int64(len(g.groupTree.objs)) * 4
	if total <= 0 {
		return 1
	}
	return total
}

type MeshTriangle struct {
	idx0, idx1, idx2 uint32
}

// VMapManager manages vmap data per map for height queries.
type VMapManager struct {
	basePath        string
	tileLoadRadius  int
	modelCacheLimit int64
	mu              sync.RWMutex
	trees           map[uint32]*StaticMapTree // per mapID
}

func NewVMapManager(vmapsDir string) *VMapManager {
	return &VMapManager{
		basePath:        ensureTrailingSlash(vmapsDir),
		tileLoadRadius:  vmapTileLoadRadiusFromEnv(),
		modelCacheLimit: vmapModelCacheLimitFromEnv(),
		trees:           make(map[uint32]*StaticMapTree),
	}
}

func vmapTileLoadRadiusFromEnv() int {
	radius := defaultVMapTileLoadRadius
	if raw := os.Getenv("VMAP_TILE_LOAD_RADIUS"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			radius = parsed
		}
	}
	if radius < 0 {
		return 0
	}
	if radius > 2 {
		return 2
	}
	return radius
}

func vmapModelCacheLimitFromEnv() int64 {
	mb := int64(defaultVMapModelCacheMB)
	if raw := os.Getenv("VMAP_MODEL_CACHE_MB"); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil {
			mb = parsed
		}
	}
	if mb <= 0 {
		return 0
	}
	return mb * 1024 * 1024
}

func ensureTrailingSlash(p string) string {
	if len(p) > 0 && p[len(p)-1] != '/' && p[len(p)-1] != '\\' {
		return p + "/"
	}
	return p
}

func (vm *VMapManager) getTree(mapID uint32) (*StaticMapTree, error) {
	vm.mu.RLock()
	t, ok := vm.trees[mapID]
	vm.mu.RUnlock()
	if ok {
		return t, nil
	}

	vm.mu.Lock()
	defer vm.mu.Unlock()
	if t, ok = vm.trees[mapID]; ok {
		return t, nil
	}

	tree := &StaticMapTree{
		mapID:           mapID,
		basePath:        vm.basePath,
		modelCacheLimit: vm.modelCacheLimit,
	}
	mapFile := fmt.Sprintf("%03d.vmtree", mapID) // e.g. 001.vmtree for map 1
	if !tree.InitMap(mapFile) {
		// no vmap for this map or error
		vm.trees[mapID] = nil
		return nil, fmt.Errorf("no vmap for map %d", mapID)
	}
	vm.trees[mapID] = tree
	return tree, nil
}

// GetHeight returns ground height by downward ray using vmap if available, else -inf.
func (vm *VMapManager) GetHeight(mapID uint32, x, y, z float32, maxSearch float32) (float32, bool) {
	tree, err := vm.getTree(mapID)
	if err != nil || tree == nil {
		return 0, false
	}
	// Load tiles around the query point using same grid coords as mmaps.
	// Radius is intentionally bounded because movement height queries are hot
	// and each extra ring can retain many vmap spawns/models under load.
	gx, gy := gameToGridCoords(x, y)
	for dx := -vm.tileLoadRadius; dx <= vm.tileLoadRadius; dx++ {
		for dy := -vm.tileLoadRadius; dy <= vm.tileLoadRadius; dy++ {
			tileGX := gx + dx
			tileGY := gy + dy
			if tileGX < 0 || tileGY < 0 || tileGX > 63 || tileGY > 63 {
				continue
			}
			tileX := uint32(tileGX)
			tileY := uint32(tileGY)
			_ = tree.LoadMapTile(tileX, tileY)
		}
	}

	pos := Vector3{X: x, Y: y, Z: z}
	h := tree.getHeight(pos, maxSearch)
	if h > INVALID_HEIGHT {
		return h, true
	}
	return 0, false
}

// StaticMapTree minimal port focused on height.
type StaticMapTree struct {
	mapID           uint32
	basePath        string
	isTiled         bool
	tree            BIH
	treeValues      map[uint32]*ModelInstance
	loadedTiles     map[uint32]bool
	loadMu          sync.Mutex // serializes tile loads and lazy model cache updates
	modelCache      map[string]*modelCacheEntry
	modelLRU        *list.List
	modelCacheBytes int64
	modelCacheLimit int64
	lastCacheLog    time.Time

	dataMu sync.RWMutex // protects treeValues and ModelInstance spawn/transform state
}

type modelCacheEntry struct {
	filename string
	model    *WorldModel
	bytes    int64
	elem     *list.Element
}

func (s *StaticMapTree) InitMap(fname string) bool {
	full := s.basePath + fname
	f, err := os.Open(full)
	if err != nil {
		return false
	}
	defer f.Close()

	var chunk [8]byte
	if _, err := io.ReadFull(f, chunk[:]); err != nil {
		return false
	}
	var tiled byte
	if err := binary.Read(f, binary.LittleEndian, &tiled); err != nil {
		return false
	}
	s.isTiled = tiled != 0
	s.modelCache = make(map[string]*modelCacheEntry)
	s.modelLRU = list.New()

	// NODE
	if _, err := io.ReadFull(f, chunk[:4]); err != nil {
		return false
	}
	if err := s.tree.readFromFile(f); err != nil {
		return false
	}
	s.treeValues = make(map[uint32]*ModelInstance)

	// GOBJ
	if _, err := io.ReadFull(f, chunk[:4]); err != nil {
		return false
	}

	// For non-tiled, read global spawn
	if !s.isTiled {
		var spawn ModelSpawn
		if err := spawn.readFromFile(f); err == nil {
			spawn.iPos.X = VMAP_mid - spawn.iPos.X
			spawn.iPos.Y = VMAP_mid - spawn.iPos.Y
			model := &WorldModel{}
			modelPath := s.basePath + spawn.name
			if err := model.readFile(modelPath); err == nil {
				inst := &ModelInstance{spawn: spawn}
				inst.initTransform()
				s.addModelToCacheLocked(modelPath, model)
				s.dataMu.Lock()
				s.treeValues[0] = inst
				s.dataMu.Unlock()
			}
		}
	}

	s.loadedTiles = make(map[uint32]bool)
	return true
}

func (s *StaticMapTree) LoadMapTile(tileX, tileY uint32) error {
	s.loadMu.Lock()
	defer s.loadMu.Unlock()
	tid := packTileID(int32(tileX), int32(tileY))
	if !s.isTiled {
		s.loadedTiles[tid] = false
		return nil
	}
	if _, loaded := s.loadedTiles[tid]; loaded {
		return nil
	}
	tileFile := s.basePath + getTileFileName(s.mapID, tileX, tileY)
	f, err := os.Open(tileFile)
	if err != nil {
		s.loadedTiles[tid] = false
		return nil // optional tile
	}
	defer f.Close()

	// bufio makes the many small reads in spawn parsing (1k+ per tile) much faster
	br := bufio.NewReader(f)

	var chunk [8]byte
	io.ReadFull(br, chunk[:]) // magic

	var numSpawns uint32
	binary.Read(br, binary.LittleEndian, &numSpawns)
	loadedRefs := 0
	for i := uint32(0); i < numSpawns; i++ {
		var spawn ModelSpawn
		if err := spawn.readFrom(br); err != nil {
			break
		}
		// vmap stores positions in internal rep (mid - game). Convert to game coords for our logic.
		spawn.iPos.X = VMAP_mid - spawn.iPos.X
		spawn.iPos.Y = VMAP_mid - spawn.iPos.Y
		var ref uint32
		binary.Read(br, binary.LittleEndian, &ref)
		// Only keep spawns that can contribute to height (have explicit bounds).
		// This keeps treeValues small, reduces snapshot allocs in getHeight, and
		// avoids tracking thousands of tiny doodads.
		if (spawn.Flags & 0x4) != 0 {
			inst := &ModelInstance{spawn: spawn}
			inst.initTransform()
			s.dataMu.Lock()
			if _, exists := s.treeValues[ref]; !exists {
				s.treeValues[ref] = inst
				loadedRefs++
			}
			s.dataMu.Unlock()
		}
	}
	s.loadedTiles[tid] = true
	return nil
}

func getTileFileName(mapID, tileX, tileY uint32) string {
	// AC: map_y_x.vmtile
	return fmt.Sprintf("%03d_%02d_%02d.vmtile", mapID, tileY, tileX)
}

func (s *StaticMapTree) getHeight(pos Vector3, maxSearch float32) float32 {
	candidates := s.heightCandidates(pos)
	if len(candidates) == 0 {
		return INVALID_HEIGHT
	}

	// Find smallest positive world-space intersection distance (closest hit when shooting down).
	minT := maxSearch
	for _, mi := range candidates {
		model, ok := s.modelForInstance(mi)
		if !ok {
			continue
		}
		t := s.intersectModelHeight(mi, model, pos, maxSearch)
		if t > 0 && t < minT {
			minT = t
		}
	}
	if minT >= maxSearch {
		return INVALID_HEIGHT
	}
	return pos.Z - minT
}

func (s *StaticMapTree) heightCandidates(pos Vector3) []*ModelInstance {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()
	if len(s.treeValues) == 0 {
		return nil
	}

	// Only snapshot nearby height-capable instances. The previous full snapshot
	// allocated len(treeValues) pointers per height query; with hundreds of bots
	// and many loaded vmap tiles this looked like a leak and could trigger OOM.
	candidates := make([]*ModelInstance, 0, 8)
	for _, mi := range s.treeValues {
		if s.modelInstanceNearLocked(mi, pos) {
			candidates = append(candidates, mi)
		}
	}
	return candidates
}

func (s *StaticMapTree) modelInstanceNearLocked(mi *ModelInstance, pos Vector3) bool {
	if mi == nil || mi.spawn.name == "" {
		return false
	}
	if !s.isTiled {
		return true
	}
	if (mi.spawn.Flags & 0x4) == 0 {
		return false
	}

	lo := mi.spawn.iBound[0]
	hi := mi.spawn.iBound[1]
	return pos.X >= lo.X-1 && pos.X <= hi.X+1 && pos.Y >= lo.Y-1 && pos.Y <= hi.Y+1
}

func (s *StaticMapTree) modelForInstance(mi *ModelInstance) (*WorldModel, bool) {
	s.dataMu.RLock()
	if mi == nil || mi.spawn.name == "" {
		s.dataMu.RUnlock()
		return nil, false
	}
	modelFile := s.basePath + mi.spawn.name
	s.dataMu.RUnlock()
	if modelFile == s.basePath {
		return nil, false
	}

	// Serialize lazy model loads and cache mutation. Without this, hundreds of
	// bots can notice the same absent model and simultaneously read the same
	// large .vmo into memory.
	s.loadMu.Lock()
	defer s.loadMu.Unlock()

	s.dataMu.RLock()
	if mi == nil || mi.spawn.name == "" {
		s.dataMu.RUnlock()
		return nil, false
	}
	modelFile = s.basePath + mi.spawn.name
	s.dataMu.RUnlock()

	if entry := s.modelCache[modelFile]; entry != nil {
		s.modelLRU.MoveToFront(entry.elem)
		return entry.model, true
	}

	model := &WorldModel{}
	if err := model.readFile(modelFile); err != nil {
		s.dataMu.Lock()
		if mi.spawn.name != "" && s.basePath+mi.spawn.name == modelFile {
			mi.spawn.name = ""
		}
		s.dataMu.Unlock()
		return nil, false
	}

	s.addModelToCacheLocked(modelFile, model)
	return model, true
}

func (s *StaticMapTree) addModelToCacheLocked(modelFile string, model *WorldModel) {
	if s.modelCache == nil {
		s.modelCache = make(map[string]*modelCacheEntry)
	}
	if s.modelLRU == nil {
		s.modelLRU = list.New()
	}

	bytes := model.estimatedBytes()
	entry := &modelCacheEntry{
		filename: modelFile,
		model:    model,
		bytes:    bytes,
	}
	entry.elem = s.modelLRU.PushFront(entry)
	s.modelCache[modelFile] = entry
	s.modelCacheBytes += bytes
	s.evictModelCacheLocked()
}

func (s *StaticMapTree) evictModelCacheLocked() {
	if s.modelCacheLimit <= 0 {
		return
	}
	evicted := 0
	for s.modelCacheBytes > s.modelCacheLimit && s.modelLRU != nil && s.modelLRU.Len() > 1 {
		elem := s.modelLRU.Back()
		if elem == nil {
			break
		}
		entry, ok := elem.Value.(*modelCacheEntry)
		if !ok || entry == nil {
			s.modelLRU.Remove(elem)
			continue
		}
		delete(s.modelCache, entry.filename)
		s.modelCacheBytes -= entry.bytes
		s.modelLRU.Remove(elem)
		evicted++
	}
	if evicted > 0 {
		s.logModelCacheEvictionLocked(evicted)
	}
}

func (s *StaticMapTree) logModelCacheEvictionLocked(evicted int) {
	now := time.Now()
	if !s.lastCacheLog.IsZero() && now.Sub(s.lastCacheLog) < vmapCacheLogInterval {
		return
	}
	s.lastCacheLog = now
	fmt.Printf("[vmap] map=%d evicted_models=%d cached_models=%d cache_mb=%.1f cache_limit_mb=%.1f\n",
		s.mapID, evicted, len(s.modelCache), bytesToMiB(s.modelCacheBytes), bytesToMiB(s.modelCacheLimit))
}

func bytesToMiB(bytes int64) float64 {
	return float64(bytes) / (1024.0 * 1024.0)
}

func (s *StaticMapTree) intersectModelHeight(mi *ModelInstance, model *WorldModel, pos Vector3, maxSearch float32) float32 {
	if model == nil {
		return INVALID_HEIGHT
	}

	s.dataMu.RLock()
	if mi == nil {
		s.dataMu.RUnlock()
		return INVALID_HEIGHT
	}
	iPos := mi.iPos
	iInvRot := mi.iInvRot
	iInvScale := mi.iInvScale
	scale := mi.spawn.iScale
	s.dataMu.RUnlock()

	p := Vector3{
		pos.X - iPos.X,
		pos.Y - iPos.Y,
		pos.Z - iPos.Z,
	}
	p = mat3MulVec(iInvRot, p)
	p = p.Mul(iInvScale)

	d := mat3MulVec(iInvRot, Vector3{0, 0, -1})
	ray := NewRay(p, d)
	modelMax := maxSearch * iInvScale
	t := model.getHeight(ray, modelMax)
	if t < modelMax {
		return t * scale
	}
	return INVALID_HEIGHT
}

func (s *StaticMapTree) getHeightACStyle(pos Vector3, maxSearch float32) float32 {
	return s.getHeight(pos, maxSearch)
}

// Integrate with TerrainManager: combined height.
func (tm *TerrainManager) getCombinedHeightWithVMap(mapID uint32, x, y, z float32, vmap *VMapManager) (float32, bool) {
	terrainH, hasT := tm.GetHeight(mapID, x, y, z)
	var vmapH float32
	hasV := false
	if vmap != nil {
		if vh, ok := vmap.GetHeight(mapID, x, y, z, 1000); ok && vh > INVALID_HEIGHT {
			vmapH = vh
			hasV = true
		}
	}

	if hasV && hasT {
		// AC style choice
		if vmapH > terrainH || math.Abs(float64(terrainH-z)) > math.Abs(float64(vmapH-z)) {
			return vmapH, true
		}
		return terrainH, true
	}
	if hasV {
		return vmapH, true
	}
	if hasT {
		return terrainH, true
	}
	return 0, false
}
