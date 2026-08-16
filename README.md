# AzerothGhost

Distributed load testing tool for [AzerothCore](https://www.azerothcore.org/).

Simulate many concurrent WoW clients (bots) against an AzerothCore realm —
for stress testing, AI/scenario validation, and mechanics checks. Bots speak
the real client protocol, pathfind with embedded mmaps, and can be driven by
Lua AI scripts.

## Build

Requires Go 1.26+.

```bash
make build
# or: go build -o azghost ./cmd/azghost
```

```bash
make test
```

## Prerequisites

- Running AzerothCore authserver + worldserver(s)
- Pathfinding data (`data-dir` containing mmaps/, maps/, vmaps/)
- Auth database DSN (the orchestrator can create test accounts for you with `--db-dsn` + `--account-prefix`)

## Distributed Approach

### Start nodes on worker machines

```bash
./azghost node --listen :8888 --data-dir /path/to/ac-data
```

### Run the orchestrator pointing at the nodes

```bash
./azghost --profile local-ac orchestrator \
  --nodes "worker1:8888,worker2:8888,worker3:8888" \
  --num-bots 1000 \
  --duration 20m \
  --spawn-rate-limit 30
```

Omit `--nodes` (or use `--nodes local`) to run locally instead.

## Load Testing Tips

Create accounts automatically:

```bash
./azghost --profile local-ac orchestrator \
  --db-dsn "acore:acore@tcp(127.0.0.1:3306)/acore_auth" \
  --account-prefix loadbot --account-password loadbot \
  --num-bots 500
```

You can pass `--lua-script` and validation flags to the orchestrator.

## Single Bot (Debug / Validation)

```bash
./azghost --profile local-ac cli \
  --char-name DebugBot \
  --delete-existing-chars \
  --duration 2m
```

## Lua AI

Run with a custom Lua script:

```bash
./azghost --profile local-ac cli \
  --char-name LuaBot \
  --lua-script scripts/grind.lua
```

Basic pattern inside Lua:

```lua
local ai = dofile("scripts/ai/init.lua")
ai:enable_default_strategies()

function on_tick()
  ai:Tick()
end
```

## Profiles

Recommended: `~/.config/azghost/profiles/local-ac.yml`

```yaml
auth-server: "127.0.0.1:3724"
data-dir: "/path/to/ac-data"
username: "admin"
password: "admin"
```

Load any profile with `--profile name`.

## Other Modes

- `cli` — single bot (useful for debugging)
- `orchestrator` — load test controller (local or distributed)
- `node` — bot execution node (HTTP server that receives work from orchestrator)
- `scenario` — run high-level Lua scenarios that can control the orchestrator

Example scenario:

```bash
./azghost scenario run scenarios/test_basic.lua
```

## Validation

For mechanic testing:

```bash
./azghost --profile local-ac cli \
  --lua-script scripts/validation/warrior_rend_execute.lua \
  --validation-mode --validation-log=val.jsonl \
  --duration 30s
```

See `scripts/ai/` for the Lua AI framework and `scripts/validation/` for mechanic checks.

## License

MIT — see [LICENSE](LICENSE).
