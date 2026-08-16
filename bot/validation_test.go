package bot

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/azerothcore/AzerothGhost/client"
)

func TestValidationTimelineJSONL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "run.jsonl")

	wc := client.NewWorldClient("u", nil, func(string, ...interface{}) {})
	b := NewHeadlessBot(wc, Config{
		Mode:              "idle",
		ValidationMode:    true,
		ValidationLogPath: path,
		EnablePacketTrace: true,
		AITickMs:          50,
		// Prevent lua load requirements: empty lua is ok
		LuaCode: `function on_tick() end`,
	})
	// Headless already called wireValidationInstrumentation

	// Simulate timeline events
	b.logDecision("test decision for timeline")
	if wc.OnAttackReject != nil {
		wc.OnAttackReject(client.AttackReject{
			GUID:   42,
			Reason: client.RejectReasonBadFacing,
			Class:  client.RejectTransient,
			Opcode: client.SmsgAttackSwingBadFacing,
		})
	}
	if wc.OnSpellCastResult != nil {
		wc.OnSpellCastResult(772, true, 0)
		wc.OnSpellCastResult(772, false, 7)
	}
	if wc.OnPacketSend != nil {
		wc.OnPacketSend(client.CmsgAttackSwing, []byte{1, 2, 3, 4, 5, 6, 7, 8})
	}
	if wc.OnPacket != nil {
		wc.OnPacket(client.SmsgSpellGo, []byte{0x01, 0x02})
	}

	b.closeValidation()

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	types := map[string]int{}
	sc := bufio.NewScanner(f)
	lines := 0
	for sc.Scan() {
		lines++
		var rec map[string]interface{}
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatalf("line %d: %v raw=%s", lines, err, sc.Text())
		}
		for _, key := range []string{"ts", "t_ns", "seq", "bot", "type"} {
			if _, ok := rec[key]; !ok {
				t.Fatalf("line %d missing %s: %v", lines, key, rec)
			}
		}
		typ, _ := rec["type"].(string)
		types[typ]++
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if lines < 5 {
		t.Fatalf("expected several timeline lines, got %d types=%v", lines, types)
	}
	for _, need := range []string{"meta", "decision", "reject", "cast", "cmsg", "smsg"} {
		if types[need] == 0 {
			t.Fatalf("missing event type %s in %v (lines=%d)", need, types, lines)
		}
	}
	// Reject class present
	// re-read for reject line
	f.Seek(0, 0)
	sc = bufio.NewScanner(f)
	foundTransient := false
	for sc.Scan() {
		var rec map[string]interface{}
		_ = json.Unmarshal(sc.Bytes(), &rec)
		if rec["type"] == "reject" && rec["class"] == "transient" {
			foundTransient = true
		}
	}
	if !foundTransient {
		t.Fatal("expected reject with class=transient")
	}
}

func TestValidationOffNoFile(t *testing.T) {
	wc := client.NewWorldClient("u", nil, nil)
	b := NewHeadlessBot(wc, Config{
		Mode:     "idle",
		LuaCode:  `function on_tick() end`,
		AITickMs: 50,
	})
	// Should not panic
	b.logDecision("no validation")
	if wc.OnPacketSend != nil {
		t.Fatal("packet send hook should not be set without validation log")
	}
}
