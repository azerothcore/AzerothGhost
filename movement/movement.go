package movement

import (
	"math"
	"time"

	"github.com/walkline/AzerothGhost/client"
	"github.com/walkline/AzerothGhost/navigation"
)

// MovementSender is the interface used by MovementController to emit movement packets.
// This decouples the movement logic from the concrete WorldClient.
type MovementSender interface {
	MoveStartForward(at time.Time, x, y, z, o float32)
	MoveStop(at time.Time, x, y, z, o float32)
	SetFacing(at time.Time, x, y, z, o float32)
	SetFacingMoving(at time.Time, x, y, z, o float32)
	SendHeartbeat(at time.Time, x, y, z, o float32)
	SendMovementHeartbeat(at time.Time, x, y, z, o float32)
	// SendMovementHeartbeatWithJump sends a moving heartbeat with MOVEMENTFLAG_FALLING set
	// and the extra jump info (used for the initial move in the observed manual log).
	SendMovementHeartbeatWithJump(at time.Time, x, y, z, o float32, fallTime uint32, zspeed, sinAngle, cosAngle, xyspeed float32)
	SendFallLand(at time.Time, x, y, z, o float32)
}

// MovementConfig tunes the client-like movement behavior.
type MovementConfig struct {
	// HeartbeatInterval is the target interval between movement heartbeats while moving.
	HeartbeatInterval time.Duration
	// TurnThresholdRad is the angle difference at which we consider a turn at waypoint.
	TurnThresholdRad float32
	// WaypointRadius is how close we must be to advance to next waypoint.
	WaypointRadius float32
	// StopForTurns emits a stop/facing/restart sequence at sharp waypoints.
	// It is useful for replaying manual traces, but disabled by default because
	// sparse server extrapolation can make stops at corners look like tiny rewinds.
	StopForTurns bool
	// StopAtEnd sends a final MoveStop when path completes.
	StopAtEnd bool
}

// DefaultMovementConfig returns conservative load-test defaults that avoid
// flooding movement heartbeats while still keeping the server position fresh.
func DefaultMovementConfig() MovementConfig {
	return MovementConfig{
		HeartbeatInterval: 500 * time.Millisecond,
		TurnThresholdRad:  0.6, // ~34 degrees; smaller changes use in-place facing while moving
		WaypointRadius:    1.5,
		StopForTurns:      false,
		StopAtEnd:         true,
	}
}

// MovementController is the separate struct responsible for all player movement logic.
// It manages path following, position interpolation over simulated time, and decides
// exactly when and what movement packets to emit to match real client patterns (heartbeats,
// stops, facing changes at turns, etc).
type MovementController struct {
	sender MovementSender
	nav    navigation.Navigator // optional, used for ground height snapping if present
	cfg    MovementConfig
	speed  float32

	// Path data (2D horizontal parameterization for speed)
	path      []navigation.Point3D
	segLens   []float32 // 2D length of each segment i -> i+1
	cumLens   []float32 // cumulative 2D length up to start of segment i
	totalDist float32
	mapID     uint32 // for GetHeight snapping on elevation changes

	// Simulation state (driven by external time ticks for determinism in tests)
	startTime  time.Time
	travelDist float32 // distance along path that should be covered by now

	isMoving bool

	// Current pose (updated during advance)
	curX, curY, curZ, curO float32

	// Last times for throttling
	lastHBTime     time.Time
	lastFacingTime time.Time
	lastSentZ      float32
	lastSentX      float32
	lastSentY      float32
	lastSentO      float32
	lastSentTime   time.Time
	haveLastSent   bool

	// jump/fall info (used to reproduce the special first heartbeat seen in manual logs)
	hasJumpInfo bool
	jumpZSpeed  float32
	jumpSin     float32
	jumpCos     float32
	jumpXY      float32

	// Turn handling state machine to emulate manual client turns at ladder corners etc.
	turnPhase       int // 0=normal, 1=waiting to face, 3=will restart
	turnTargetO     float32
	turnUntil       time.Time
	nextSegIdx      int // when we decide to turn at a vertex, remember the upcoming segment
	turnFacingsSent int // how many incremental facings sent for current turn (to space them)

	// For first-move special case (jump/fall like in log)
	firstMoveSent       bool
	firstHBWithJumpSent bool
}

// NewMovementController creates a movement controller.
func NewMovementController(sender MovementSender, speed float32, nav navigation.Navigator, cfg MovementConfig) *MovementController {
	if cfg.HeartbeatInterval <= 0 {
		cfg = DefaultMovementConfig()
	}
	if speed <= 0 {
		speed = 7.0
	}
	return &MovementController{
		sender: sender,
		nav:    nav,
		cfg:    cfg,
		speed:  speed,
	}
}

func (m *MovementController) SetNavigator(nav navigation.Navigator) {
	m.nav = nav
}

// SetPath installs a new path (from pathfinder) and starts movement from the first point.
// The caller is responsible for providing a path that starts near current position.
// mapID is used (when > 0 and nav is available) to snap Z to real ground height on elevation.
func (m *MovementController) SetPath(path []navigation.Point3D, startTime time.Time, initialO float32, mapID uint32) {
	if len(path) == 0 {
		return
	}
	wasMoving := m.isMoving && m.turnPhase == 0 && len(m.path) >= 2
	m.path = make([]navigation.Point3D, len(path))
	copy(m.path, path)
	m.mapID = mapID
	if m.nav != nil {
		snapPathToGround(m.nav, mapID, m.path)
	}

	// Precompute 2D segment lengths and cumulatives
	n := len(m.path)
	m.segLens = make([]float32, n-1)
	m.cumLens = make([]float32, n)
	m.totalDist = 0
	for i := 0; i < n-1; i++ {
		d := m.path[i].DistanceTo2D(m.path[i+1])
		m.segLens[i] = d
		m.cumLens[i] = m.totalDist
		m.totalDist += d
	}
	m.cumLens[n-1] = m.totalDist

	m.startTime = startTime
	m.travelDist = 0
	m.isMoving = true
	if !wasMoving {
		m.firstMoveSent = false
		m.lastHBTime = time.Time{}
		m.lastFacingTime = time.Time{}
		m.haveLastSent = false
	}
	m.turnPhase = 0

	p0 := m.path[0]
	m.curX, m.curY, m.curZ = p0.X, p0.Y, p0.Z
	m.curO = initialO
	m.clampCurrentZToGround()

	if wasMoving {
		// Mid-path retarget: keep continuous forward motion. Only emit a
		// facing correction for a meaningful heading change — micro-adjusts
		// from frequent Lua move_to repaths make movement look jerky.
		const repathFacingMinRad = 0.15 // ~8.5 degrees
		if len(m.path) >= 2 {
			p1 := m.path[1]
			targetO := float32(math.Atan2(float64(p1.Y-p0.Y), float64(p1.X-p0.X)))
			if angleDelta(targetO, m.curO) > repathFacingMinRad || angleDelta(targetO, m.lastSentO) > repathFacingMinRad {
				m.curO = targetO
				m.sender.SetFacingMoving(startTime, m.curX, m.curY, m.curZ, m.curO)
				m.lastHBTime = startTime
				m.lastFacingTime = startTime
				m.recordSentPose(startTime)
			} else {
				m.curO = targetO
			}
		}
		m.firstMoveSent = true
		return
	}

	m.sender.MoveStartForward(startTime, m.curX, m.curY, m.curZ, m.curO)
	m.lastHBTime = startTime
	m.lastSentZ = m.curZ
	m.recordSentPose(startTime)
	m.firstMoveSent = true
	m.firstHBWithJumpSent = false
}

// Update advances simulated position to the time 'now' and emits any due packets (HB, turns, stops).
// Call this regularly with monotonically increasing simulated time.
func (m *MovementController) Update(now time.Time) {
	if len(m.path) < 2 {
		return
	}
	if !m.isMoving && m.turnPhase == 0 {
		return
	}

	// Compute how far we should have traveled by wall/sim time.
	elapsed := now.Sub(m.startTime).Seconds()
	targetDist := m.speed * float32(elapsed)
	if targetDist < 0 {
		targetDist = 0
	}

	// Handle turn phase first (client-like pauses for reorientation at major turns)
	if m.turnPhase != 0 {
		m.handleTurnPhase(now)
		// During turn we still may want to emit stationary HB rarely; skip normal HB for now.
		return
	}

	// Advance pose along path
	m.advanceAlongPath(targetDist)
	m.clampCurrentZToGround()

	sentCorrection := m.maybeSendExtrapolationCorrection(now)

	// Emit periodic heartbeat, or an earlier correction when terrain drops enough
	// that delayed extrapolation would visibly float above a downhill slope.
	const groundCorrectionMinInterval = 500 * time.Millisecond
	zCorrectionDue := m.lastSentZ-m.curZ >= 1.25 &&
		(m.lastHBTime.IsZero() || now.Sub(m.lastHBTime) >= groundCorrectionMinInterval)
	if !sentCorrection && (m.lastHBTime.IsZero() || now.Sub(m.lastHBTime) >= m.cfg.HeartbeatInterval || zCorrectionDue) {
		if m.hasJumpInfo && !m.firstHBWithJumpSent {
			// Special first moving heartbeat with jump/fall data (matches the observed manual log's initial HB)
			m.sender.SendMovementHeartbeatWithJump(now, m.curX, m.curY, m.curZ, m.curO, 194,
				m.jumpZSpeed, m.jumpSin, m.jumpCos, m.jumpXY)
			m.firstHBWithJumpSent = true
		} else {
			m.sender.SendMovementHeartbeat(now, m.curX, m.curY, m.curZ, m.curO)
		}
		m.lastHBTime = now
		m.lastSentZ = m.curZ
		m.recordSentPose(now)
	}

	// Detect if we are at (or passed) a waypoint that requires a direction change.
	// If yes, schedule a client-like stop + facing turns + restart.
	m.maybeScheduleTurnAtWaypoint(now, targetDist)

	// Check arrival at final destination
	if m.travelDist >= m.totalDist-0.05 {
		m.isMoving = false
		if m.cfg.StopAtEnd {
			m.sender.MoveStop(now, m.curX, m.curY, m.curZ, m.curO)
			m.recordSentPose(now)
		}
	}
}

// advanceAlongPath sets curX/Y/Z/O by walking the precomputed 2D distances.
func (m *MovementController) advanceAlongPath(dist float32) {
	if dist <= 0 || len(m.path) == 0 {
		p := m.path[0]
		m.curX, m.curY = p.X, p.Y
		m.curZ = p.Z // use path Z
		return
	}
	if dist >= m.totalDist {
		last := m.path[len(m.path)-1]
		m.curX, m.curY = last.X, last.Y
		m.curZ = last.Z // use path Z
		m.travelDist = m.totalDist
		// Face toward last direction if possible
		if len(m.path) >= 2 {
			prev := m.path[len(m.path)-2]
			m.curO = float32(math.Atan2(float64(last.Y-prev.Y), float64(last.X-prev.X)))
		}
		return
	}

	m.travelDist = dist

	// Find segment
	for i := 0; i < len(m.segLens); i++ {
		segStart := m.cumLens[i]
		segEnd := m.cumLens[i] + m.segLens[i]
		if dist >= segStart && dist < segEnd {
			p0 := m.path[i]
			p1 := m.path[i+1]
			segFrac := float32(0)
			if m.segLens[i] > 1e-6 {
				segFrac = (dist - segStart) / m.segLens[i]
			}
			m.curX = p0.X + (p1.X-p0.X)*segFrac
			m.curY = p0.Y + (p1.Y-p0.Y)*segFrac
			m.curZ = p0.Z + (p1.Z-p0.Z)*segFrac // use path Z (includes generator height correction)
			// Face along current segment
			m.curO = float32(math.Atan2(float64(p1.Y-p0.Y), float64(p1.X-p0.X)))
			return
		}
	}
	// Fallback to last
	last := m.path[len(m.path)-1]
	m.curX, m.curY, m.curZ = last.X, last.Y, last.Z
	// Z taken from path (no snap to avoid wrong floor selection in multi-level areas).
}

func (m *MovementController) maybeSendExtrapolationCorrection(now time.Time) bool {
	if !m.haveLastSent || m.lastSentTime.IsZero() {
		return false
	}
	since := now.Sub(m.lastSentTime)
	if since < 350*time.Millisecond {
		return false
	}

	dt := float32(since.Seconds())
	predX := m.lastSentX + float32(math.Cos(float64(m.lastSentO)))*m.speed*dt
	predY := m.lastSentY + float32(math.Sin(float64(m.lastSentO)))*m.speed*dt
	dx := m.curX - predX
	dy := m.curY - predY
	extrapolationErr := float32(math.Sqrt(float64(dx*dx + dy*dy)))
	headingErr := angleDelta(m.curO, m.lastSentO)

	if extrapolationErr < 1.25 && headingErr < 0.22 {
		return false
	}

	if headingErr >= 0.12 {
		m.sender.SetFacingMoving(now, m.curX, m.curY, m.curZ, m.curO)
	} else {
		m.sender.SendMovementHeartbeat(now, m.curX, m.curY, m.curZ, m.curO)
	}
	m.lastHBTime = now
	m.lastSentZ = m.curZ
	m.recordSentPose(now)
	return true
}

func angleDelta(a, b float32) float32 {
	d := float32(math.Abs(float64(a - b)))
	for d > math.Pi {
		d = float32(math.Abs(float64(d - 2*math.Pi)))
	}
	return d
}

func (m *MovementController) recordSentPose(now time.Time) {
	m.lastSentX = m.curX
	m.lastSentY = m.curY
	m.lastSentZ = m.curZ
	m.lastSentO = m.curO
	m.lastSentTime = now
	m.haveLastSent = true
}

// maybeScheduleTurnAtWaypoint looks ahead at the next waypoint and if the incoming
// and outgoing directions differ significantly, we emulate a player stopping to turn.
func (m *MovementController) maybeScheduleTurnAtWaypoint(now time.Time, targetDist float32) {
	if len(m.path) < 3 {
		return
	}
	// Find current segment we are on or just finishing
	curSeg := 0
	for i := 0; i < len(m.segLens); i++ {
		if targetDist < m.cumLens[i]+m.segLens[i] {
			curSeg = i
			break
		}
	}

	// Are we close to the *end* of current segment (i.e. a vertex)?
	distToVertex := (m.cumLens[curSeg] + m.segLens[curSeg]) - targetDist
	if distToVertex > m.cfg.WaypointRadius {
		return
	}

	// Is there a next segment after this vertex?
	nextSeg := curSeg + 1
	if nextSeg >= len(m.segLens) {
		return
	}

	// Compute direction of current seg and next seg
	dx0 := m.path[curSeg+1].X - m.path[curSeg].X
	dy0 := m.path[curSeg+1].Y - m.path[curSeg].Y
	dx1 := m.path[nextSeg+1].X - m.path[nextSeg].X
	dy1 := m.path[nextSeg+1].Y - m.path[nextSeg].Y

	// If zero length skip
	if (dx0*dx0+dy0*dy0) < 1e-6 || (dx1*dx1+dy1*dy1) < 1e-6 {
		return
	}

	ang0 := math.Atan2(float64(dy0), float64(dx0))
	ang1 := math.Atan2(float64(dy1), float64(dx1))
	delta := float32(math.Abs(ang1 - ang0))
	if delta > math.Pi {
		delta = 2*math.Pi - delta
	}

	if delta < m.cfg.TurnThresholdRad {
		// Small change: allow facing correction while moving (real client does this too).
		// Throttle to avoid flooding like a real client on a dense path.
		targetO := float32(ang1)
		deltaO := float32(math.Abs(float64(m.curO - targetO)))
		sinceFacing := now.Sub(m.lastFacingTime)
		if deltaO > 0.12 && (m.lastFacingTime.IsZero() || sinceFacing > 70*time.Millisecond) {
			m.sender.SetFacingMoving(now, m.curX, m.curY, m.curZ, targetO)
			m.curO = targetO
			m.lastFacingTime = now
			m.lastHBTime = now
			m.recordSentPose(now)
		}
		return
	}

	if !m.cfg.StopForTurns {
		targetO := float32(ang1)
		deltaO := angleDelta(m.curO, targetO)
		sinceFacing := now.Sub(m.lastFacingTime)
		if deltaO > 0.12 && (m.lastFacingTime.IsZero() || sinceFacing > 250*time.Millisecond) {
			m.sender.SetFacingMoving(now, m.curX, m.curY, m.curZ, targetO)
			m.curO = targetO
			m.lastFacingTime = now
			m.lastHBTime = now
			m.recordSentPose(now)
		}
		return
	}

	// Large turn -> schedule stop + turn + restart, similar to observed manual client behavior
	// We set state so that next Updates will emit the STOP / multiple SET_FACING / START
	m.turnPhase = 1
	m.turnTargetO = float32(ang1)
	m.turnUntil = now.Add(60 * time.Millisecond)
	m.nextSegIdx = nextSeg
	m.turnFacingsSent = 0 // reset counter for spaced facings

	// Immediately send the stop at current (near vertex) position
	m.sender.MoveStop(now, m.curX, m.curY, m.curZ, m.curO)
	m.recordSentPose(now)
	m.isMoving = false // pause simulation advance until turn done
}

// handleTurnPhase advances the stop+face+start sequence over simulated time.
func (m *MovementController) handleTurnPhase(now time.Time) {
	switch m.turnPhase {
	case 1: // stopping done, time to send next facing (spaced across ticks like manual input)
		if now.After(m.turnUntil) {
			o := m.curO
			delta := m.turnTargetO - o
			for delta < -math.Pi {
				delta += 2 * math.Pi
			}
			for delta > math.Pi {
				delta -= 2 * math.Pi
			}
			// Emit one incremental facing per time slice
			stepsRemaining := 3 - m.turnFacingsSent
			if stepsRemaining < 1 {
				stepsRemaining = 1
			}
			step := delta / float32(stepsRemaining)
			o += step
			m.sender.SetFacing(now, m.curX, m.curY, m.curZ, o)
			m.curO = o
			m.recordSentPose(now)
			m.turnFacingsSent++

			if m.turnFacingsSent >= 3 {
				m.turnPhase = 3
				m.turnUntil = now.Add(40 * time.Millisecond)
			} else {
				// schedule next facing soon
				m.turnUntil = now.Add(70 * time.Millisecond)
			}
		}
	case 3: // restart forward
		if now.After(m.turnUntil) {
			// resume at the vertex; advance our logical travelDist to start of next seg
			vertexDist := m.cumLens[m.nextSegIdx]
			m.travelDist = vertexDist
			p := m.path[m.nextSegIdx]
			m.curX, m.curY, m.curZ = p.X, p.Y, p.Z
			m.curO = m.turnTargetO
			m.clampCurrentZToGround()

			m.sender.MoveStartForward(now, m.curX, m.curY, m.curZ, m.curO)
			m.lastHBTime = now
			m.lastFacingTime = now
			m.recordSentPose(now)
			m.isMoving = true
			m.turnPhase = 0
			m.turnFacingsSent = 0
			m.startTime = now.Add(-time.Duration((m.travelDist / m.speed) * float32(time.Second))) // adjust base so elapsed math continues correctly
		}
	}
}

// Stop forces a stop.
func (m *MovementController) Stop(now time.Time) {
	if !m.isMoving {
		return
	}
	m.isMoving = false
	m.turnPhase = 0
	m.sender.MoveStop(now, m.curX, m.curY, m.curZ, m.curO)
	m.recordSentPose(now)
}

// CurrentPosition returns the controller's view of current location (for assertions in tests).
func (m *MovementController) CurrentPosition() (x, y, z, o float32) {
	return m.curX, m.curY, m.curZ, m.curO
}

// IsMoving reports if still following path.
func (m *MovementController) IsMoving() bool { return m.isMoving }

// TravelDist returns how far along the path (2D) we have simulated.
func (m *MovementController) TravelDist() float32 { return m.travelDist }

// Destination returns the final path waypoint when a path is installed.
func (m *MovementController) Destination() (x, y, z float32, ok bool) {
	if len(m.path) == 0 {
		return 0, 0, 0, false
	}
	last := m.path[len(m.path)-1]
	return last.X, last.Y, last.Z, true
}

// InitPositionFromWorld seeds the controller's internal position with the current world position
// right after creation / login. This prevents zeroing out the spawn location before the first path.
func (m *MovementController) InitPositionFromWorld(x, y, z, o float32) {
	m.curX = x
	m.curY = y
	m.curZ = z
	m.curO = o
}

func (m *MovementController) clampCurrentZToGround() {
	if m.nav == nil {
		return
	}
	gh, _, ok := movementGroundHeightWithSource(m.nav, m.mapID, m.curX, m.curY, m.curZ)
	if !ok || math.IsNaN(float64(gh)) || math.IsInf(float64(gh), 0) {
		return
	}
	m.curZ = gh
}

// SetInitialJumpInfo configures the jump/fall trajectory data to be sent with the
// very first movement heartbeat (reproduces the special first HB with j_* fields
// seen in manual movement logs when a small drop/fall occurs right after starting forward).
func (m *MovementController) SetInitialJumpInfo(zspeed, sinAngle, cosAngle, xyspeed float32) {
	m.hasJumpInfo = true
	m.jumpZSpeed = zspeed
	m.jumpSin = sinAngle
	m.jumpCos = cosAngle
	m.jumpXY = xyspeed
	m.firstHBWithJumpSent = false
}

// ============================================================
// WorldClient adapter so Bot can drive movement via the controller.
// ============================================================

// worldMovementSender adapts a *client.WorldClient to the MovementSender interface.
type worldMovementSender struct {
	w *client.WorldClient
}

// NewWorldMovementSender returns a MovementSender backed by a WorldClient.
// Used by bot to attach a movement controller without duplicating adapter logic.
func NewWorldMovementSender(w *client.WorldClient) MovementSender {
	return &worldMovementSender{w: w}
}

func (s *worldMovementSender) MoveStartForward(at time.Time, x, y, z, o float32) {
	_ = s.w.MoveForwardAtTime(x, y, z, o, at)
}

func (s *worldMovementSender) MoveStop(at time.Time, x, y, z, o float32) {
	_ = s.w.MoveStopAtTime(x, y, z, o, at)
}

func (s *worldMovementSender) SetFacing(at time.Time, x, y, z, o float32) {
	_ = s.w.SetFacingAtTime(x, y, z, o, at)
}

func (s *worldMovementSender) SetFacingMoving(at time.Time, x, y, z, o float32) {
	_ = s.w.SetFacingMovingAtTime(x, y, z, o, at)
}

func (s *worldMovementSender) SendHeartbeat(at time.Time, x, y, z, o float32) {
	_ = s.w.SendHeartbeatAtTime(x, y, z, o, at)
}

func (s *worldMovementSender) SendMovementHeartbeat(at time.Time, x, y, z, o float32) {
	_ = s.w.SendMovementHeartbeatAtTime(x, y, z, o, at)
}

func (s *worldMovementSender) SendMovementHeartbeatWithJump(at time.Time, x, y, z, o float32, fallTime uint32, zspeed, sinAngle, cosAngle, xyspeed float32) {
	_ = s.w.SendMovementHeartbeatWithJumpAtTime(x, y, z, o, at, fallTime, zspeed, sinAngle, cosAngle, xyspeed)
}

func (s *worldMovementSender) SendFallLand(at time.Time, x, y, z, o float32) {
	// Fall land is a specific opcode; send via generic if available or heartbeat as approximation for now.
	// For fidelity we can expose from world if needed. Use stationary HB with position.
	_ = s.w.SendHeartbeatAtTime(x, y, z, o, at)
}

// ============================================================
// Ground helpers (moved from bot.go for isolation; used by movement + bot later)
// ============================================================

func movementGroundHeightWithSource(nav navigation.Navigator, mapID uint32, x, y, fallbackHintZ float32) (float32, bool, bool) {
	if gh, ok := nav.GetHeight(mapID, x, y, fallbackHintZ); ok {
		return gh, false, true
	}
	if gh, ok := nav.GetTerrainHeight(mapID, x, y); ok {
		return gh, true, true
	}
	return 0, false, false
}

func snapPathToGround(nav navigation.Navigator, mapID uint32, pts []navigation.Point3D) {
	if nav == nil {
		return
	}
	for i := range pts {
		origZ := pts[i].Z
		gh, _, ok := movementGroundHeightWithSource(nav, mapID, pts[i].X, pts[i].Y, origZ)
		if !ok || math.IsNaN(float64(gh)) || math.IsInf(float64(gh), 0) {
			continue
		}
		pts[i].Z = gh
	}
}

// ============================================================
// Wander path helpers (for bot AI use of random nav; placed here for isolation + tests)
// These were in bot.go; moved/copied to support movement+nav standalone tests.
// ============================================================

const (
	wanderRandomPathAttempts    = 8
	wanderRetryCooldown         = 500 * time.Millisecond
	wanderMaxPathLengthFactor   = 3.0
	wanderMaxSimplifiedPathSize = 260
)

func findWanderPath(nav navigation.Navigator, mapID uint32, center navigation.Point3D, radius float32) []navigation.Point3D {
	if nav == nil {
		return nil
	}

	for attempt := 0; attempt < wanderRandomPathAttempts; attempt++ {
		attemptRadius := wanderAttemptRadius(radius, attempt)
		result, err := nav.FindRandomPath(mapID, center, attemptRadius)
		if err != nil || result == nil || !result.Found || len(result.Points) <= 1 {
			continue
		}

		pts := simplifyAndDensifyPath(result.Points, 3.0, 1.0)
		snapPathToGround(nav, mapID, pts)
		if len(pts) > 0 {
			pts[0] = center
		}
		if wanderPathUsable(center, pts) {
			return pts
		}
	}

	return nil
}

func wanderAttemptRadius(radius float32, attempt int) float32 {
	switch attempt % 4 {
	case 1:
		radius *= 0.75
	case 2:
		radius *= 0.5
	case 3:
		radius *= 1.25
	}
	if radius < 8 {
		return 8
	}
	return radius
}

func wanderPathUsable(center navigation.Point3D, pts []navigation.Point3D) bool {
	if len(pts) < 2 || len(pts) > wanderMaxSimplifiedPathSize {
		return false
	}

	for _, p := range pts {
		if math.IsNaN(float64(p.X)) || math.IsNaN(float64(p.Y)) || math.IsNaN(float64(p.Z)) ||
			math.IsInf(float64(p.X), 0) || math.IsInf(float64(p.Y), 0) || math.IsInf(float64(p.Z), 0) {
			return false
		}
	}

	straight := center.DistanceTo2D(pts[len(pts)-1])
	if straight < 2.0 {
		return false
	}
	pathLen := pathLength2D(pts)
	if straight > 5.0 && pathLen > straight*wanderMaxPathLengthFactor {
		return false
	}

	return true
}

func pathLength2D(pts []navigation.Point3D) float32 {
	var length float32
	for i := 1; i < len(pts); i++ {
		length += pts[i-1].DistanceTo2D(pts[i])
	}
	return length
}

// simplifyAndDensifyPath reduces zig-zags (simplify collinear) and adds intermediate points
// for smoother following on uneven terrain like Durotar hills.
func simplifyAndDensifyPath(pts []navigation.Point3D, maxStep, collinearTol float32) []navigation.Point3D {
	if len(pts) < 2 {
		return pts
	}
	// densify first (use 2D horizontal distance for step size; Z will be ground-snapped live)
	dense := []navigation.Point3D{pts[0]}
	for i := 1; i < len(pts); i++ {
		p0 := pts[i-1]
		p1 := pts[i]
		d := p0.DistanceTo2D(p1)
		if d > maxStep && d > 0.001 {
			n := int(d / maxStep)
			for k := 1; k <= n; k++ {
				t := float32(k) / float32(n+1)
				dense = append(dense, navigation.Point3D{
					X: p0.X + (p1.X-p0.X)*t,
					Y: p0.Y + (p1.Y-p0.Y)*t,
					Z: p0.Z + (p1.Z-p0.Z)*t, // interp; snapPathToGround corrects to terrain
				})
			}
		}
		dense = append(dense, p1)
	}
	if len(dense) <= 2 {
		return dense
	}
	// simplify collinear-ish points (keep changes in direction or steep Z)
	simp := []navigation.Point3D{dense[0]}
	for i := 1; i < len(dense)-1; i++ {
		a := simp[len(simp)-1]
		b := dense[i]
		c := dense[i+1]
		abx := b.X - a.X
		aby := b.Y - a.Y
		abz := b.Z - a.Z
		bcx := c.X - b.X
		bcy := c.Y - b.Y
		bcz := c.Z - b.Z
		// 3D cross product mag
		crx := aby*bcz - abz*bcy
		cry := abz*bcx - abx*bcz
		crz := abx*bcy - aby*bcx
		cross := float32(math.Sqrt(float64(crx*crx + cry*cry + crz*crz)))
		if cross > collinearTol || math.Abs(float64(abz)) > 1.5 || math.Abs(float64(bcz)) > 1.5 {
			simp = append(simp, b)
		}
	}
	simp = append(simp, dense[len(dense)-1])
	return simp
}
