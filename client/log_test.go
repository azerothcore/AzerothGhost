package client

import (
	"encoding/binary"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestLogLevelFilter(t *testing.T) {
	var mu sync.Mutex
	var lines []string
	w := NewWorldClient("u", nil, func(format string, args ...interface{}) {
		mu.Lock()
		lines = append(lines, format)
		mu.Unlock()
	})
	w.SetLogLevel(LogInfo)
	w.logAt(LogTrace, "trace-line")
	w.logAt(LogDebug, "debug-line")
	w.logAt(LogInfo, "info-line")
	w.logAt(LogWarn, "warn-line")
	mu.Lock()
	got := append([]string(nil), lines...)
	mu.Unlock()
	for _, s := range got {
		if strings.Contains(s, "trace") || strings.Contains(s, "debug") {
			t.Fatalf("unexpected verbose log at Info: %v", got)
		}
	}
	if len(got) != 2 {
		t.Fatalf("want info+warn only, got %v", got)
	}
}

func TestLearnedSpellAggregatesAtInfo(t *testing.T) {
	var mu sync.Mutex
	var lines []string
	w := NewWorldClient("u", nil, func(format string, args ...interface{}) {
		mu.Lock()
		lines = append(lines, format)
		mu.Unlock()
	})
	w.SetLogLevel(LogInfo)
	for id := uint32(1); id <= 5; id++ {
		buf := make([]byte, 6)
		binary.LittleEndian.PutUint32(buf[0:4], id)
		w.handleLearnedSpell(buf)
		if !w.KnowsSpell(id) {
			t.Fatalf("KnowsSpell(%d) false after learn packet", id)
		}
	}
	mu.Lock()
	if len(lines) != 0 {
		t.Fatalf("expected no per-id logs at Info before flush, got %v", lines)
	}
	mu.Unlock()
	w.FlushLearnedSpellLog()
	mu.Lock()
	defer mu.Unlock()
	if len(lines) != 1 || !strings.Contains(lines[0], "count=") {
		t.Fatalf("want one summary line, got %v", lines)
	}
}

func TestLearnedSpellPerIDAtTrace(t *testing.T) {
	var n int
	w := NewWorldClient("u", nil, func(string, ...interface{}) { n++ })
	w.SetLogLevel(LogTrace)
	buf := make([]byte, 6)
	binary.LittleEndian.PutUint32(buf[0:4], 688)
	w.handleLearnedSpell(buf)
	if n != 1 {
		t.Fatalf("want 1 per-id log at Trace, got %d", n)
	}
}

func TestLogSilent(t *testing.T) {
	n := 0
	w := NewWorldClient("u", nil, func(string, ...interface{}) { n++ })
	w.SetLogLevel(LogSilent)
	w.logAt(LogError, "err")
	w.logAt(LogInfo, "info")
	if n != 0 {
		t.Fatalf("silent must suppress all, got %d", n)
	}
}

func TestParseLogLevel(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want LogLevel
		ok   bool
	}{
		{"info", LogInfo, true},
		{"DEBUG", LogDebug, true},
		{"warn", LogWarn, true},
		{"silent", LogSilent, true},
		{"nope", LogInfo, false},
	} {
		got, ok := ParseLogLevel(tc.in)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Fatalf("%q => %v,%v want %v,%v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestPrepGMCommand(t *testing.T) {
	if !isPrepGMCommand(".gm on") || !isPrepGMCommand(".combatstop") || !isPrepGMCommand(".cheat god on") {
		t.Fatal("expected prep cmds")
	}
	if isPrepGMCommand(".go xyz 1 2 3 0") || isPrepGMCommand(".npc add 1") || isPrepGMCommand(".learn 688") {
		t.Fatal("action cmds must not be prep")
	}
}

func TestSpellFailReasonName(t *testing.T) {
	if got := SpellFailReasonName(85); got != "NO_POWER" {
		t.Fatalf("85 => %q want NO_POWER", got)
	}
	if got := SpellFailReasonName(27); got != "DONT_REPORT" {
		t.Fatalf("27 => %q want DONT_REPORT", got)
	}
	if got := SpellFailReasonName(255); got != "REASON_255" {
		t.Fatalf("255 => %q want REASON_255", got)
	}
}

func TestCastFailedAtDebugOnly(t *testing.T) {
	var mu sync.Mutex
	var lines []string
	w := NewWorldClient("u", nil, func(format string, args ...interface{}) {
		mu.Lock()
		lines = append(lines, fmt.Sprintf(format, args...))
		mu.Unlock()
	})
	w.SetLogLevel(LogInfo)
	buf := make([]byte, 6)
	buf[0] = 1
	binary.LittleEndian.PutUint32(buf[1:5], 133)
	buf[5] = 85 // NO_POWER
	w.handleCastFailed(buf)
	mu.Lock()
	if len(lines) != 0 {
		t.Fatalf("CAST_FAILED must be silent at Info, got %v", lines)
	}
	mu.Unlock()
	w.SetLogLevel(LogDebug)
	w.handleCastFailed(buf)
	mu.Lock()
	defer mu.Unlock()
	if len(lines) != 1 || !strings.Contains(lines[0], "NO_POWER") {
		t.Fatalf("want one CAST_FAILED with reason name at Debug, got %v", lines)
	}
}
