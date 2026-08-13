package e2eharness

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/walkline/AzerothGhost/client"
)

// Session is a logged-in world session for one bot character.
type Session struct {
	World *client.WorldClient
	GUID  uint64
	Name  string
	User  string

	// logf is optional (usually t.Logf-backed) for waiter-drop warnings.
	logf func(format string, args ...interface{})

	mu          sync.Mutex
	signCh      chan *client.PetitionSignResults
	showCh      chan *client.PetitionShowSignatures
	turnCh      chan uint32
	bankCh      chan *client.GuildBankList
	moneyCh     chan int32
	itemPushCh  chan *client.ItemPushResult
	spellCh     chan SpellCastResult
	waitersOn   bool
	spellHookOn bool // AddSpellCastResultHook installed once
}

// LoginOptions controls character login/create.
type LoginOptions struct {
	AuthAddr string
	User     string
	Password string
	CharName string
	Race     uint8
	Class    uint8
}

// LoginBot authenticates, connects to the first realm, creates a character if needed, and enters world.
func LoginBot(t *testing.T, opt LoginOptions) (*Session, error) {
	t.Helper()
	if opt.AuthAddr == "" {
		opt.AuthAddr = AuthAddr
	}
	if opt.Password == "" {
		opt.Password = DefaultPassword
	}
	if opt.Race == 0 {
		opt.Race = 1 // human
	}
	if opt.Class == 0 {
		opt.Class = 1 // warrior
	}

	auth := client.NewAuthClient(opt.User, opt.Password)
	realms, err := auth.Authenticate(opt.AuthAddr)
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	if len(realms) == 0 {
		return nil, fmt.Errorf("no realms")
	}
	t.Logf("%s realm=%s addr=%s", opt.User, realms[0].Name, realms[0].Address)

	// Guard: readLoop may outlive the test after Close; never call t.Logf then.
	var logClosed atomic.Bool
	logFn := func(format string, args ...interface{}) {
		if logClosed.Load() {
			return
		}
		t.Logf("[%s] "+format, append([]interface{}{opt.User}, args...)...)
	}
	w := client.NewWorldClient(strings.ToUpper(opt.User), auth.SessionKey(), logFn)
	// Default LogInfo: phase/trade/login/GM short lines; not per-spell or combat thrash.
	// Override with E2E_WORLD_LOG=debug|trace|warn|error|silent for deeper dumps.
	if raw, ok := os.LookupEnv("E2E_WORLD_LOG"); ok {
		if lvl, ok := client.ParseLogLevel(raw); ok {
			w.SetLogLevel(lvl)
		}
	} else {
		w.SetLogLevel(client.LogInfo)
	}
	t.Cleanup(func() {
		logClosed.Store(true)
	})
	// Race drives GM-command chat language (Horde cannot speak Common).
	w.SetCharRace(opt.Race)

	bs := &Session{World: w, Name: opt.CharName, User: opt.User, logf: logFn}
	charListCh := make(chan []client.CharEnumEntry, 2)
	createCh := make(chan uint8, 1)
	w.OnCharList = func(chars []client.CharEnumEntry) {
		cp := append([]client.CharEnumEntry(nil), chars...)
		select {
		case charListCh <- cp:
		default:
			select {
			case <-charListCh:
			default:
			}
			charListCh <- cp
		}
	}
	w.OnCharCreateResult = func(data []byte) {
		code := uint8(0xFF)
		if len(data) > 0 {
			code = data[0]
		}
		select {
		case createCh <- code:
		default:
		}
	}

	if err := w.Connect(realms[0].Address); err != nil {
		return nil, fmt.Errorf("connect world: %w", err)
	}
	go func() { _ = w.Run() }()
	// Wait for SMSG_AUTH_RESPONSE → PhaseAuthed (no fixed sleep).
	if err := w.WaitForSessionPhase(client.PhaseAuthed, 20*time.Second); err != nil {
		return nil, fmt.Errorf("wait authed: %w", err)
	}
	_ = w.SendReadyForAccountDataTimes()
	_ = w.SendRealmSplit()
	if err := w.RequestCharList(); err != nil {
		return nil, fmt.Errorf("char enum: %w", err)
	}

	var chars []client.CharEnumEntry
	select {
	case chars = <-charListCh:
	case <-time.After(15 * time.Second):
		return nil, fmt.Errorf("timeout char enum")
	}

	// Login isolation: only enter the character matching opt.CharName.
	// Never fall back to chars[0] when the requested name is absent — create it.
	charName := opt.CharName
	var guid uint64
	for _, c := range chars {
		if strings.EqualFold(c.Name, charName) {
			guid = c.GUID
			charName = c.Name
			if c.Race != 0 {
				w.SetCharRace(c.Race)
			}
			break
		}
	}
	if guid == 0 {
		if len(chars) > 0 {
			t.Logf("%s requested char %q not on account (%d existing) — creating requested name (not reusing chars[0]=%q)",
				opt.User, charName, len(chars), chars[0].Name)
		}
		// Names must be pure letters (digits => CHAR_NAME_MIXED_LANGUAGES 0x5D).
		names := UniqueLetterNames(charName, 24)
		var created bool
		for _, tryName := range names {
			select {
			case <-createCh:
			default:
			}
			if err := w.CreateCharacter(tryName, opt.Race, opt.Class, 0, 1, 1, 1, 1, 1, 0); err != nil {
				return nil, fmt.Errorf("create char: %w", err)
			}
			var code uint8
			select {
			case code = <-createCh:
			case <-time.After(20 * time.Second):
				// Under multi-bot load the create ACK can lag; try next name.
				t.Logf("%s timeout char create result for %s — trying next name", opt.User, tryName)
				continue
			}
			t.Logf("%s create %s result=0x%02X", opt.User, tryName, code)
			// 0x2F = CHAR_CREATE_SUCCESS on 3.3.5a
			if code == 0x2F || code == 0x00 {
				charName = tryName
				created = true
				break
			}
		}
		if !created {
			return nil, fmt.Errorf("character create failed for all names")
		}
		// SMSG_CHAR_CREATE already arrived; re-enum for GUID (wait on SMSG_CHAR_ENUM).
		if err := w.RequestCharList(); err != nil {
			return nil, err
		}
		select {
		case chars = <-charListCh:
		case <-time.After(15 * time.Second):
			return nil, fmt.Errorf("timeout char enum after create")
		}
		for _, c := range chars {
			if strings.EqualFold(c.Name, charName) {
				guid = c.GUID
				charName = c.Name
				break
			}
		}
		if guid == 0 {
			return nil, fmt.Errorf("created char %q not found in enum (not falling back to chars[0])", charName)
		}
	}
	bs.GUID = guid
	bs.Name = charName

	if err := w.LoginCharacter(guid); err != nil {
		return nil, fmt.Errorf("login: %w", err)
	}
	// WaitForLogin is closed on SMSG_LOGIN_VERIFY_WORLD (PhaseInWorld).
	if err := w.WaitForLogin(60 * time.Second); err != nil {
		return nil, fmt.Errorf("wait login: %w", err)
	}
	return bs, nil
}

// Close closes the world socket.
func (s *Session) Close() {
	if s != nil && s.World != nil {
		s.World.Close()
	}
}

// SetLogf rebinds session diagnostic logging (e.g. when a package-shared leader
// is reused across tests so waiter warnings attach to the active testing.T).
func (s *Session) SetLogf(logf func(format string, args ...interface{})) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logf = logf
}

// installPacketWaiters installs a single OnPacket dispatcher for petition + bank + item push.
// Safe to call multiple times; re-arm only recreates channels (does not nest handlers).
//
// Waiter contract: Arm → Send → Wait. Never Arm (recreate channels) while a
// Wait* is outstanding — that drops the old channel and loses the packet.
// If a buffered channel is full, the packet is dropped with a session log warning.
func (s *Session) installPacketWaiters() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.signCh = make(chan *client.PetitionSignResults, 8)
	s.showCh = make(chan *client.PetitionShowSignatures, 4)
	s.turnCh = make(chan uint32, 4)
	s.bankCh = make(chan *client.GuildBankList, 8)
	s.moneyCh = make(chan int32, 4)
	s.itemPushCh = make(chan *client.ItemPushResult, 8)
	if s.waitersOn {
		return
	}
	s.waitersOn = true
	// Race-safe multi-subscriber registration (never clobber other OnPacket users).
	s.World.AddPacketHook(func(op uint16, data []byte) {
		switch op {
		case client.SmsgPetitionSignResults:
			if r, err := client.ParsePetitionSignResults(data); err == nil {
				s.mu.Lock()
				ch := s.signCh
				s.mu.Unlock()
				select {
				case ch <- r:
				default:
					s.warnWaiterDrop("sign")
				}
			}
		case client.SmsgPetitionShowSignatures:
			if r, err := client.ParsePetitionShowSignatures(data); err == nil {
				s.mu.Lock()
				ch := s.showCh
				s.mu.Unlock()
				select {
				case ch <- r:
				default:
					s.warnWaiterDrop("show-signatures")
				}
			}
		case client.SmsgTurnInPetitionResults:
			if r, err := client.ParseTurnInPetitionResults(data); err == nil {
				s.mu.Lock()
				ch := s.turnCh
				s.mu.Unlock()
				select {
				case ch <- r:
				default:
					s.warnWaiterDrop("turn-in")
				}
			}
		case client.SmsgGuildBankList:
			if r, err := client.ParseGuildBankList(data); err == nil {
				s.mu.Lock()
				ch := s.bankCh
				s.mu.Unlock()
				select {
				case ch <- r:
				default:
					s.warnWaiterDrop("bank-list")
				}
			}
		case client.MsgGuildBankMoneyWithdrawn:
			if r, err := client.ParseGuildBankMoneyWithdrawn(data); err == nil {
				s.mu.Lock()
				ch := s.moneyCh
				s.mu.Unlock()
				select {
				case ch <- r:
				default:
					s.warnWaiterDrop("money-withdrawn")
				}
			}
		case client.SmsgItemPushResult:
			if r, err := client.ParseItemPushResult(data); err == nil {
				s.mu.Lock()
				ch := s.itemPushCh
				s.mu.Unlock()
				select {
				case ch <- r:
				default:
					s.warnWaiterDrop(fmt.Sprintf("item-push entry=%d", r.Entry))
				}
			}
		}
	})
}

// warnWaiterDrop logs when a waiter channel is full and a packet is discarded.
func (s *Session) warnWaiterDrop(kind string) {
	msg := fmt.Sprintf("WARNING: waiter drop kind=%s (channel full; use Arm→Send→Wait, never Arm during Wait)", kind)
	if s.logf != nil {
		s.logf("%s", msg)
		return
	}
	// Synthetic sessions (no LoginBot) still surface drops on stderr.
	fmt.Printf("[%s] %s\n", s.User, msg)
}

// ArmAllWaiters installs / re-arms the unified packet dispatcher for petition +
// bank + item push. Preferred entry point (ArmPetitionWaiters / ArmBankWaiters
// are thin aliases for call-site readability).
//
// Contract: Arm → Send → Wait. Do not re-Arm while Wait is outstanding.
func (s *Session) ArmAllWaiters() {
	s.installPacketWaiters()
}

// ArmPetitionWaiters is an alias of ArmAllWaiters (unified dispatcher).
func (s *Session) ArmPetitionWaiters() {
	s.ArmAllWaiters()
}

// ArmBankWaiters is an alias of ArmAllWaiters (unified dispatcher).
func (s *Session) ArmBankWaiters() {
	s.ArmAllWaiters()
}

// WaitSignResults waits for SMSG_PETITION_SIGN_RESULTS.
func (s *Session) WaitSignResults(d time.Duration) (*client.PetitionSignResults, error) {
	s.mu.Lock()
	ch := s.signCh
	s.mu.Unlock()
	if ch == nil {
		return nil, fmt.Errorf("sign waiter not armed")
	}
	select {
	case r := <-ch:
		return r, nil
	case <-time.After(d):
		return nil, fmt.Errorf("timeout sign results")
	}
}

// WaitShowSignatures waits for SMSG_PETITION_SHOW_SIGNATURES.
func (s *Session) WaitShowSignatures(d time.Duration) (*client.PetitionShowSignatures, error) {
	s.mu.Lock()
	ch := s.showCh
	s.mu.Unlock()
	if ch == nil {
		return nil, fmt.Errorf("show signatures waiter not armed")
	}
	select {
	case r := <-ch:
		return r, nil
	case <-time.After(d):
		return nil, fmt.Errorf("timeout show signatures")
	}
}

// WaitTurnIn waits for SMSG_TURN_IN_PETITION_RESULTS.
func (s *Session) WaitTurnIn(d time.Duration) (uint32, error) {
	s.mu.Lock()
	ch := s.turnCh
	s.mu.Unlock()
	if ch == nil {
		return 0, fmt.Errorf("turn-in waiter not armed")
	}
	select {
	case r := <-ch:
		return r, nil
	case <-time.After(d):
		return 0, fmt.Errorf("timeout turn-in")
	}
}

// WaitBankList waits for SMSG_GUILD_BANK_LIST.
func (s *Session) WaitBankList(d time.Duration) (*client.GuildBankList, error) {
	s.mu.Lock()
	ch := s.bankCh
	s.mu.Unlock()
	if ch == nil {
		return nil, fmt.Errorf("bank list waiter not armed")
	}
	select {
	case r := <-ch:
		return r, nil
	case <-time.After(d):
		return nil, fmt.Errorf("timeout bank list")
	}
}

// DrainBankLists consumes any buffered bank list packets.
func (s *Session) DrainBankLists() {
	s.mu.Lock()
	ch := s.bankCh
	s.mu.Unlock()
	if ch == nil {
		return
	}
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

// WaitMoneyWithdrawn waits for MSG_GUILD_BANK_MONEY_WITHDRAWN.
func (s *Session) WaitMoneyWithdrawn(d time.Duration) (int32, error) {
	s.mu.Lock()
	ch := s.moneyCh
	s.mu.Unlock()
	if ch == nil {
		return 0, fmt.Errorf("money withdrawn waiter not armed")
	}
	select {
	case r := <-ch:
		return r, nil
	case <-time.After(d):
		return 0, fmt.Errorf("timeout money withdrawn")
	}
}

// DrainItemPushes consumes any buffered item-push packets.
func (s *Session) DrainItemPushes() {
	s.mu.Lock()
	ch := s.itemPushCh
	s.mu.Unlock()
	if ch == nil {
		return
	}
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

// WaitItemPush waits for any SMSG_ITEM_PUSH_RESULT.
func (s *Session) WaitItemPush(d time.Duration) (*client.ItemPushResult, error) {
	s.mu.Lock()
	ch := s.itemPushCh
	s.mu.Unlock()
	if ch == nil {
		return nil, fmt.Errorf("item push waiter not armed")
	}
	select {
	case r := <-ch:
		return r, nil
	case <-time.After(d):
		return nil, fmt.Errorf("timeout item push")
	}
}

// WaitItemPushEntry waits for SMSG_ITEM_PUSH_RESULT with the given item entry.
func (s *Session) WaitItemPushEntry(entry uint32, d time.Duration) (*client.ItemPushResult, error) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		rem := time.Until(deadline)
		if rem <= 0 {
			break
		}
		r, err := s.WaitItemPush(rem)
		if err != nil {
			return nil, err
		}
		if r.Entry == entry {
			return r, nil
		}
	}
	return nil, fmt.Errorf("timeout item push entry=%d", entry)
}

// MustGM sends a GM command. No fixed sleep — callers that need completion
// must wait on the matching packet (teleport seq, item push, bank list, …).
func MustGM(t *testing.T, w *client.WorldClient, cmd string) {
	t.Helper()
	if err := w.SendGMCommand(cmd); err != nil {
		t.Fatalf("gm %q: %v", cmd, err)
	}
}

// MustGMTeleport sends a GM teleport (`.go …`) and waits for self near/far
// teleport completion via SMSG_MOVE_TELEPORT / SMSG_NEW_WORLD phase cycle.
func MustGMTeleport(t *testing.T, w *client.WorldClient, cmd string) {
	t.Helper()
	before := w.TeleportSeq()
	MustGM(t, w, cmd)
	if err := w.WaitForTeleportAfter(before, 12*time.Second); err != nil {
		// Soft: some cores apply .go without a full tele opcode if already near;
		// callers that need nearby objects still poll the object cache.
		t.Logf("teleport wait after %q: %v (continuing)", cmd, err)
	}
}
