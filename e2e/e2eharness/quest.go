package e2eharness

import (
	"database/sql"
	"fmt"
	"testing"
	"time"
)

// QuestStatus returns character_queststatus.status for guid+quest, or
// (0, false) if no row (quest never taken / wiped).
//
// Call SaveCharacter first for online players so the worldserver flushes.
func QuestStatus(db *sql.DB, charGUID uint64, questID uint32) (status uint8, ok bool, err error) {
	err = db.QueryRow(
		`SELECT status FROM character_queststatus WHERE guid=? AND quest=?`,
		charGUID, questID,
	).Scan(&status)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return status, true, nil
}

// MustQuestStatus fails the test on SQL error.
func MustQuestStatus(t *testing.T, db *sql.DB, charGUID uint64, questID uint32) (status uint8, ok bool) {
	t.Helper()
	status, ok, err := QuestStatus(db, charGUID, questID)
	if err != nil {
		t.Fatalf("quest status guid=%d quest=%d: %v", charGUID, questID, err)
	}
	return status, ok
}

// WaitQuestStatus polls DB until status matches want.
// Prefer AssertQuestStatusEqual after SaveCharacter for one-shot checks.
func WaitQuestStatus(t *testing.T, db *sql.DB, charGUID uint64, questID uint32, want uint8, timeout time.Duration) uint8 {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last uint8
	var lastOK bool
	for time.Now().Before(deadline) {
		st, ok := MustQuestStatus(t, db, charGUID, questID)
		last, lastOK = st, ok
		if ok && st == want {
			return st
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !lastOK {
		t.Fatalf("quest %d: no character_queststatus row for guid=%d within %s", questID, charGUID, timeout)
	}
	t.Fatalf("quest %d status=%d want=%d within %s", questID, last, want, timeout)
	return last
}

// AssertQuestStatusEqual saves and asserts quest status.
func AssertQuestStatusEqual(t *testing.T, db *sql.DB, sess *Session, questID uint32, want uint8) {
	t.Helper()
	SaveCharacter(t, sess.World)
	// Extra poll — .save is not always instant on all cores.
	deadline := time.Now().Add(5 * time.Second)
	var last uint8
	var lastOK bool
	for time.Now().Before(deadline) {
		st, ok := MustQuestStatus(t, db, sess.GUID, questID)
		last, lastOK = st, ok
		if ok && st == want {
			t.Logf("quest %d status=%d (want %d) OK", questID, st, want)
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !lastOK {
		t.Fatalf("quest %d: no DB row after save (guid=%d) — want status=%d", questID, sess.GUID, want)
	}
	t.Fatalf("quest %d status=%d want=%d (guid=%d)", questID, last, want, sess.GUID)
}

// QuestStatusName is a debug label for status bytes.
func QuestStatusName(status uint8) string {
	switch status {
	case QuestStatusNone:
		return "NONE"
	case QuestStatusComplete:
		return "COMPLETE"
	case QuestStatusIncomplete:
		return "INCOMPLETE"
	case QuestStatusFailed:
		return "FAILED"
	default:
		return fmt.Sprintf("STATUS_%d", status)
	}
}
