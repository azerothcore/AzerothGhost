package client

import "strings"

// LogLevel filters WorldClient diagnostic output when logFunc is non-nil.
// nil logFunc is always silent (unchanged contract).
//
// Ordering: higher value = more verbose. A message emits when
// messageLevel <= configured LogLevel (Error is most important).
type LogLevel int

const (
	// LogSilent disables all WorldClient logs even when logFunc is set.
	LogSilent LogLevel = 0
	// LogError auth/hard failures only.
	LogError LogLevel = 1
	// LogWarn protocol warnings, parse errors, terminal swing rejects.
	LogWarn LogLevel = 2
	// LogInfo lifecycle / sparse e2e signal (default when logFunc != nil).
	LogInfo LogLevel = 3
	// LogDebug hot paths: selection, swings, GM verbose, trade status, damage.
	LogDebug LogLevel = 4
	// LogTrace per-spell learn lines and opcode hex dumps.
	LogTrace LogLevel = 5
)

// ParseLogLevel maps common names to LogLevel (case-insensitive).
func ParseLogLevel(s string) (LogLevel, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "silent", "off", "none", "0":
		return LogSilent, true
	case "error", "err", "1":
		return LogError, true
	case "warn", "warning", "2":
		return LogWarn, true
	case "info", "3", "":
		return LogInfo, true
	case "debug", "4":
		return LogDebug, true
	case "trace", "5":
		return LogTrace, true
	default:
		return LogInfo, false
	}
}

// String returns a short level name.
func (l LogLevel) String() string {
	switch l {
	case LogSilent:
		return "silent"
	case LogError:
		return "error"
	case LogWarn:
		return "warn"
	case LogInfo:
		return "info"
	case LogDebug:
		return "debug"
	case LogTrace:
		return "trace"
	default:
		return "info"
	}
}
