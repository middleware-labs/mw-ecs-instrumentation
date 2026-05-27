# mw-ecs-instrument

A CLI tool to auto-instrument AWS ECS task definitions with [Middleware](https://middleware.io) observability — APM tracing, sidecar agent, and FireLens log routing.

## What it does

Given an existing ECS task definition, the tool injects:

| Component | Container | Purpose |
|---|---|---|
| **MW Agent sidecar** | `mw-agent` | Collects and forwards telemetry to Middleware |
| **APM init container** | `instrumentation-init` | Copies language-specific auto-instrumentation libraries via a shared volume |
| **FireLens log router** | `log_router` | Fluent Bit sidecar with `awsfirelens` log driver on app containers |

For APM, it also patches app containers with the required environment variables, volume mounts, and startup dependencies — all without touching your application code.

### Supported languages

| Language | Init image | Injected env vars |
|---|---|---|
| Java | `aws-ecs-java-autoinstrumentation` | `JAVA_TOOL_OPTIONS` (javaagent) |
| Node.js | `aws-ecs-node-autoinstrumentation` | `NODE_OPTIONS`, `NODE_PATH` |
| Python | `aws-ecs-python-autoinstrumentation` | `PYTHONPATH` |

## Installation

```bash
# Build from source
cd mw-ecs-instrumentation
go build -o mw-ecs-instrument .

# Move to PATH (optional)
sudo mv mw-ecs-instrument /usr/local/bin/
```

**Prerequisites:** Go 1.21+, AWS credentials configured (`aws configure` or environment variables).

## Commands

### `instrument` — Inject MW instrumentation

```bash
# Interactive mode — prompts for APM language, logs, service name
mw-ecs-instrument instrument \
  --task-definition my-app:3 \
  --mw-api-key <key> \
  --mw-target https://<uid>.middleware.io

# Non-interactive — Java APM with FireLens logs, register immediately
mw-ecs-instrument instrument \
  --task-definition my-app:3 \
  --mw-api-key <key> \
  --mw-target https://<uid>.middleware.io \
  --language java --enable-apm --enable-logs --register

# Batch mode — discover and instrument all task definitions
mw-ecs-instrument instrument \
  --all \
  --mw-api-key <key> \
  --mw-target https://<uid>.middleware.io \
  --language node --enable-apm --enable-logs --dry-run
```

#### Flags

| Flag | Required | Description |
|---|---|---|
| `--task-definition` | Yes* | Task definition family:revision or full ARN |
| `--all` | Yes* | Discover and instrument all active families |
| `--mw-api-key` | Yes | Middleware API key |
| `--mw-target` | Yes | Middleware target URL |
| `--language` | No | APM language: `java`, `node`, `python` (interactive if omitted) |
| `--enable-apm` | No | Add APM init container (interactive if omitted) |
| `--enable-logs` | No | Add FireLens log routing (interactive if omitted) |
| `--service-name` | No | `MW_SERVICE_NAME` for the app (defaults to family name) |
| `--region` | No | AWS region (defaults to AWS CLI config) |
| `--output` | No | Output file path (defaults to `<family>-instrumented.json`) |
| `--register` | No | Register the new revision with ECS |
| `--dry-run` | No | Print modified task definition to stdout without writing |

\* One of `--task-definition` or `--all` is required.

### `discover` — List instrumentation status

Shows all active task definition families and whether each has the MW agent, APM init container, and FireLens log router.

```bash
mw-ecs-instrument discover
mw-ecs-instrument discover --region us-west-2
```

Example output:

```
  FAMILY                                               MW-AGENT   APM-INIT   FIRELENS
  ──────────────────────────────────────────────────   ────────   ────────   ────────
  my-java-app:5                                        ✔ yes      ✔ yes      ✘ no
  my-node-app:12                                       ✘ no       ✘ no       ✘ no
  nginx-task:1                                         ✘ no       ✘ no       ✘ no

  Instrumented: 1  |  Not instrumented: 2
```

### `rollback` — Revert to previous revision

Re-registers the previous revision as a new revision, effectively undoing the instrumentation.

```bash
mw-ecs-instrument rollback --task-definition my-app:5
```

## How it works

1. Fetches the existing task definition via `DescribeTaskDefinition`
2. Detects existing MW containers — prompts to replace or keep
3. Injects `mw-agent` sidecar with `MW_API_KEY` and `MW_TARGET`
4. (If APM enabled) Adds `instrumentation-init` container with a shared volume, patches app containers with language-specific env vars, mount points, and `dependsOn`
5. (If logs enabled) Adds `log_router` (Fluent Bit) sidecar and sets `awsfirelens` logConfiguration on app containers. Existing `awslogs` configs are replaced; other log drivers are left untouched unless you confirm
6. Recalculates task-level CPU and memory as the sum of all containers
7. Outputs a clean, registration-ready JSON (strips server-side-only fields)
8. Optionally registers the new revision via `RegisterTaskDefinition`

## Safe by default

- **No silent overrides** — detects existing `mw-agent`, `instrumentation-init`, and `log_router` containers and asks before replacing
- **Env var merge** — MW env vars are merged by key; existing app env vars with different names are preserved
- **Mount/volume dedup** — checks before adding to avoid duplicates
- **Port mappings untouched** — existing port mappings on app containers are never modified
- **Dry run** — preview changes with `--dry-run` before committing

## Project structure

```
mw-ecs-instrumentation/
├── main.go
├── cmd/
│   ├── root.go          # CLI root command
│   ├── instrument.go    # instrument subcommand
│   ├── discover.go      # discover subcommand
│   └── rollback.go      # rollback subcommand
├── internal/
│   ├── aws/
│   │   └── client.go    # ECS API client wrapper
│   ├── instrument/
│   │   ├── constants.go # Images, volumes, language config
│   │   ├── containers.go# Container/env var builders
│   │   ├── patch.go     # Core patching logic
│   │   └── serialize.go # Clean JSON output serializer
│   └── prompt/
│       └── prompt.go    # Interactive terminal prompts
└── go.mod
```
