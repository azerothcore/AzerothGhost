# AzerothGhost debug playbook

## Classify every failure

| Class | Meaning | What to do |
|-------|---------|------------|
| **P** Protocol / AC precondition | Wrong phase, flags, unlearned spell, missing ACK | Fix client encode/phase/ACK; use AC logs |
| **R** Our reliability | Bad cache (false dead), AI thrash, blocking sleeps | Fix bot state machine / scripts |

## One-shot validation run

```bash
make build
./azghost --profile local-ac cli \
  --char-name DebugBot \
  --bot-mode lua \
  --lua-script scripts/grind.lua \
  --duration 30s \
  --validation-mode \
  --validation-log=run.jsonl \
  --trace-packets
```

Requires: auth+world up, AC data at profile `data_dir`, account from profile.

## Read the timeline

```bash
# Session phases (must reach in_world)
jq -c 'select(.type=="phase" and .kind=="session")' run.jsonl

# Transient vs terminal combat rejects
jq -c 'select(.type=="reject")' run.jsonl

# Cast outcomes
jq -c 'select(.type=="cast")' run.jsonl

# Packet flow (high-value only)
jq -c 'select(.type=="cmsg" or .type=="smsg") | {ts,type,opcode}' run.jsonl

# Protocol warnings (gameplay while not in_world)
jq -c 'select(.type=="protocol_warn")' run.jsonl
```

## Pair with AzerothCore

Enable in `worldserver.conf` for the repro window:

```
Logger.network=4,Console Server
Logger.network.opcode=4,Console Server
Logger.network.kick=4,Console Server
```

Optional: `.packetlog BotName` as GM, or `PacketLogFile`.

Correlate by approximate time + character name/GUID.

## Decision tree

1. **Login stuck** → phase never leaves `loading`; AC kicks in `network.kick`
2. **Bot frozen after tele** → missing `WORLDPORT_ACK` / `TELEPORT_ACK`
3. **Swing fails** → `reject` class: `transient` (face/range) vs `terminal` (dead/cant)
4. **Spell spam fail** → `cast` with `fail_reason`; fix AI preconditions
5. **No SMSG after CMSG** → silent AC drop; enable AC opcode DEBUG

## Unit tests (offline)

```bash
go test ./client/ ./bot/ ./movement/
AC_DATA_DIR=/path/to/ac-data go test ./pathfinding/
```
