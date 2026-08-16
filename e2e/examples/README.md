# Example live-stack tests

Runnable patterns for authors (and LLMs) that import
`github.com/azerothcore/AzerothGhost/e2e/e2eharness`.

These are simplified from real AC regression scenarios. Prefer copying a file
into **your** module rather than depending on this package path forever.

## Run

```bash
# From AzerothGhost repo root; requires AzerothCore + MySQL
go test -tags=e2e ./e2e/examples -count=1 -v -timeout 30m -parallel 2
make test-e2e-examples
```

## Files

| File | Based on | Shows |
|------|----------|--------|
| `example_quest_death_e2e_test.go` | AC #26549 | Solo, quest, `DieAndRepop`, DB after `Save` |
| `example_aura_mount_e2e_test.go` | AC #26130 | `ApplyAura`, `CastOrGM`, `AssertAuraRemains` |
| `example_relog_e2e_test.go` | AC #25793 | `Relog`, character DB flags |
| `example_guild_charter_e2e_test.go` | Guild charter suite | Session path, `BuyGuildCharter` |
| `example_boss_engage_e2e_test.go` | AC #27095 Freya | `TeleNamed`, `CombatReady`, `Engage`, wave tracker, `DamageKill` |

Full API cookbook: [`../EXAMPLES.md`](../EXAMPLES.md).  
Private local suite: `../local/` (not in git).
