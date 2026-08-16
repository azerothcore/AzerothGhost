package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/azerothcore/AzerothGhost/bot"
	"github.com/azerothcore/AzerothGhost/scenario"
)

// Server is the HTTP API server for managing bot clients (node mode).
type Server struct {
	bots                      sync.Map
	nextID                    atomic.Int64
	server                    *http.Server
	defaultDataDir            string
	defaultPathfindingAddress string
}

// NewServer creates a new API server
func NewServer() *Server {
	return NewServerWithDefaults("", "")
}

// NewServerWithDefaults creates a node API server with node-local navigation defaults.
// Docker workers use this to keep container paths such as /data even when the
// orchestrator runs on a host with a different filesystem layout.
func NewServerWithDefaults(dataDir, pathfindingAddress string) *Server {
	return &Server{
		defaultDataDir:            dataDir,
		defaultPathfindingAddress: pathfindingAddress,
	}
}

// LaunchRequest is the request body for launching a new bot (extended for AI/Scenario support).
type LaunchRequest struct {
	Username            string            `json:"username"`
	Password            string            `json:"password"`
	AuthServer          string            `json:"auth_server"`
	CharacterName       string            `json:"character_name"`
	RealmIndex          int               `json:"realm_index"`
	Race                uint8             `json:"race"`
	Class               uint8             `json:"class"`
	Gender              uint8             `json:"gender"`
	Mode                string            `json:"mode"`
	DungeonName         string            `json:"dungeon_name"`
	DataDir             string            `json:"data_dir"`
	PathfindingAddr     string            `json:"pathfinding_addr"`
	LuaScript           string            `json:"lua_script"`
	LuaCode             string            `json:"lua_code"`
	AIBundle            scenario.AIBundle `json:"ai_bundle"`
	BotID               string            `json:"bot_id"`
	DeleteExistingChars bool              `json:"delete_existing_chars"`
	DisableTargetCache  bool              `json:"disable_target_cache"`
	LogDecisionsToChat  bool              `json:"log_decisions_to_chat"`

	// Validation tooling (propagated from orchestrator/scenario; default false = no perf cost)
	ValidationMode      bool   `json:"validation_mode"`
	ValidationLogPath   string `json:"validation_log"`
	EnablePacketTrace   bool   `json:"enable_packet_trace"`
	EnableDetailedAuras bool   `json:"enable_detailed_auras"`
}

// LaunchResponse is the response body for a launch request.
type LaunchResponse struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

// LuaUpdateRequest is the request body for updating Lua code on a running bot.
type LuaUpdateRequest struct {
	BotID   string `json:"bot_id"`
	LuaCode string `json:"lua_code"`
}

// NodeLaunchRequest mirrors the orchestrator version for wire compatibility in the node/server path.
type NodeLaunchRequest struct {
	BotID               string            `json:"bot_id"`
	Username            string            `json:"username"`
	Password            string            `json:"password"`
	AuthServer          string            `json:"auth_server"`
	CharacterName       string            `json:"character_name"`
	Race                uint8             `json:"race"`
	Class               uint8             `json:"class"`
	Mode                string            `json:"mode"`
	DungeonName         string            `json:"dungeon_name"`
	DataDir             string            `json:"data_dir"`
	PathfindingAddr     string            `json:"pathfinding_addr"`
	LuaScript           string            `json:"lua_script"`
	LuaCode             string            `json:"lua_code"`
	AIBundle            scenario.AIBundle `json:"ai_bundle"`
	DeleteExistingChars bool              `json:"delete_existing_chars"`
	LogDecisionsToChat  bool              `json:"log_decisions_to_chat"`
	DisableTargetCache  bool              `json:"disable_target_cache"`

	// Validation tooling (off by default)
	ValidationMode      bool   `json:"validation_mode"`
	ValidationLogPath   string `json:"validation_log"`
	EnablePacketTrace   bool   `json:"enable_packet_trace"`
	EnableDetailedAuras bool   `json:"enable_detailed_auras"`
}

// Start starts the HTTP server.
func (s *Server) Start(addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/launch", s.handleLaunch)
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/status/all", s.handleStatusAll)
	mux.HandleFunc("/stop", s.handleStop)
	mux.HandleFunc("/lua", s.handleLuaUpdate)
	mux.HandleFunc("/events", s.handleEvents)

	s.server = &http.Server{Addr: addr, Handler: mux}
	fmt.Printf("Node server starting on %s (data_dir=%q pathfinding_addr=%q)\n", addr, s.defaultDataDir, s.defaultPathfindingAddress)
	return s.server.ListenAndServe()
}

func (s *Server) Stop(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func (s *Server) effectiveDataDir(requested string) string {
	if s.defaultDataDir != "" {
		return s.defaultDataDir
	}
	return requested
}

func (s *Server) effectivePathfindingAddress(requested string) string {
	if s.defaultPathfindingAddress != "" {
		return s.defaultPathfindingAddress
	}
	return requested
}

// LaunchFromOrchestrator starts a bot from an orchestrator request.
func (s *Server) LaunchFromOrchestrator(req NodeLaunchRequest) string {
	id := req.BotID
	if id == "" {
		id = fmt.Sprintf("bot-%d", s.nextID.Add(1))
	}

	dataDir := s.effectiveDataDir(req.DataDir)
	pathfindingAddr := s.effectivePathfindingAddress(req.PathfindingAddr)

	fmt.Printf("[Node] LaunchFromOrchestrator: bot=%s char=%s race=%d class=%d delete=%v\n",
		id, req.CharacterName, req.Race, req.Class, req.DeleteExistingChars)

	config := bot.Config{
		Username:                 req.Username,
		Password:                 req.Password,
		AuthServer:               req.AuthServer,
		CharacterName:            req.CharacterName,
		Race:                     req.Race,
		Class:                    req.Class,
		Mode:                     req.Mode,
		DungeonName:              req.DungeonName,
		DataDir:                  dataDir,
		PathfindingAddress:       pathfindingAddr,
		LuaScript:                req.LuaScript,
		LuaCode:                  req.LuaCode,
		AIBundle:                 req.AIBundle,
		DeleteExistingCharacters: req.DeleteExistingChars,
		LogDecisionsToChat:       req.LogDecisionsToChat,
		DisableTargetCache:       req.DisableTargetCache,
		ValidationMode:           req.ValidationMode,
		ValidationLogPath:        req.ValidationLogPath,
		EnablePacketTrace:        req.EnablePacketTrace,
		EnableDetailedAuras:      req.EnableDetailedAuras,
	}

	b := bot.NewBot(id, config)
	s.bots.Store(id, b)
	go b.Run()
	return id
}

func (s *Server) handleLaunch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req LaunchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Username == "" || req.Password == "" || req.AuthServer == "" || req.CharacterName == "" {
		http.Error(w, "username, password, auth_server, and character_name are required", http.StatusBadRequest)
		return
	}

	id := req.BotID
	if id == "" {
		id = fmt.Sprintf("bot-%d", s.nextID.Add(1))
	}

	dataDir := s.effectiveDataDir(req.DataDir)
	pathfindingAddr := s.effectivePathfindingAddress(req.PathfindingAddr)

	config := bot.Config{
		Username:                 req.Username,
		Password:                 req.Password,
		AuthServer:               req.AuthServer,
		CharacterName:            req.CharacterName,
		RealmIndex:               req.RealmIndex,
		Race:                     req.Race,
		Class:                    req.Class,
		Gender:                   req.Gender,
		Mode:                     req.Mode,
		DungeonName:              req.DungeonName,
		DataDir:                  dataDir,
		PathfindingAddress:       pathfindingAddr,
		LuaScript:                req.LuaScript,
		LuaCode:                  req.LuaCode,
		AIBundle:                 req.AIBundle,
		DeleteExistingCharacters: req.DeleteExistingChars,
		LogDecisionsToChat:       req.LogDecisionsToChat,
		DisableTargetCache:       req.DisableTargetCache,
		ValidationMode:           req.ValidationMode,
		ValidationLogPath:        req.ValidationLogPath,
		EnablePacketTrace:        req.EnablePacketTrace,
		EnableDetailedAuras:      req.EnableDetailedAuras,
	}

	b := bot.NewBot(id, config)
	s.bots.Store(id, b)

	fmt.Printf("[Node] handleLaunch: starting bot id=%s char=%s\n", id, req.CharacterName)
	go b.Run()

	resp := LaunchResponse{ID: id, Message: "bot launched"}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "id query parameter required", http.StatusBadRequest)
		return
	}

	val, ok := s.bots.Load(id)
	if !ok {
		http.Error(w, "bot not found", http.StatusNotFound)
		return
	}

	bot := val.(*bot.Bot)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(bot.Status())
}

func (s *Server) handleStatusAll(w http.ResponseWriter, r *http.Request) {
	var results []bot.BotResult
	s.bots.Range(func(key, value interface{}) bool {
		b := value.(*bot.Bot)
		results = append(results, b.Status())
		return true
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		// Stop all
		s.bots.Range(func(key, value interface{}) bool {
			b := value.(*bot.Bot)
			b.Stop()
			return true
		})
		w.Write([]byte(`{"message":"all bots stopped"}`))
		return
	}

	val, ok := s.bots.Load(id)
	if !ok {
		http.Error(w, "bot not found", http.StatusNotFound)
		return
	}
	b := val.(*bot.Bot)
	b.Stop()
	w.Write([]byte(fmt.Sprintf(`{"message":"bot %s stopped"}`, id)))
}

func (s *Server) handleLuaUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req LuaUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.BotID != "" {
		val, ok := s.bots.Load(req.BotID)
		if !ok {
			http.Error(w, "bot not found", http.StatusNotFound)
			return
		}
		b := val.(*bot.Bot)
		if err := b.LoadLuaScript(req.LuaCode); err != nil {
			http.Error(w, "lua error: "+err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		// Update all bots
		s.bots.Range(func(key, value interface{}) bool {
			b := value.(*bot.Bot)
			b.LoadLuaScript(req.LuaCode)
			return true
		})
	}

	w.Write([]byte(`{"message":"lua updated"}`))
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")

	w.Header().Set("Content-Type", "application/json")

	if id != "" {
		val, ok := s.bots.Load(id)
		if !ok {
			http.Error(w, "bot not found", http.StatusNotFound)
			return
		}
		b := val.(*bot.Bot)
		json.NewEncoder(w).Encode(b.Events())
	} else {
		// All events from all bots
		allEvents := make(map[string][]bot.BotEvent)
		s.bots.Range(func(key, value interface{}) bool {
			b := value.(*bot.Bot)
			allEvents[key.(string)] = b.Events()
			return true
		})
		json.NewEncoder(w).Encode(allEvents)
	}
}
