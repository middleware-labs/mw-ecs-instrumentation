# mw-ecs

A CLI tool to auto-instrument AWS ECS task definitions with [Middleware](https://middleware.io) observability — APM tracing, sidecar agent, and FireLens log routing.

## What it does

Given an existing ECS task definition, the tool injects:

| Component | Container | Purpose |
|---|---|---|
| **MW Agent sidecar** | `mw-agent` | Collects and forwards telemetry to Middleware via gRPC |
| **APM init container** | `instrumentation-init` | Copies language-specific OpenTelemetry auto-instrumentation libraries into a shared volume |
| **FireLens log router** | `log_router` | Fluent Bit sidecar with `awsfirelens` log driver on app containers |

For APM, it also patches app containers with the required environment variables, volume mounts, and startup dependencies — all without touching your application code.

### Supported languages

| Language | Init Image | Mount Path | Init Command |
|---|---|---|---|
| Java | `autoinstrumentation-java:2.19.0` | `/otel-auto-instrumentation-java` | `cp /javaagent.jar <mount>/javaagent.jar` |
| Node.js | `autoinstrumentation-nodejs:0.53.0` | `/otel-auto-instrumentation-nodejs` | `cp -r /autoinstrumentation/. <mount>` |
| Python | `autoinstrumentation-python:0.59b0` | `/otel-auto-instrumentation-python` | `cp -r /autoinstrumentation/. <mount>` |

All init container images are sourced from `ghcr.io/open-telemetry/opentelemetry-operator/`.

For Python with musl libc (Alpine-based images), the mount path becomes `/otel-auto-instrumentation-python-musl`.

### Telemetry flow

```
App Container ──OTLP──► mw-agent sidecar ──gRPC──► Middleware Backend
```

- In `awsvpc` or `host` network mode, the app sends OTLP to `http://localhost:9320` (the mw-agent sidecar)
- In `bridge` mode, the app sends OTLP directly to the MW backend URL

## Auto-Detection

When `--language` is not specified, the tool automatically detects the runtime language and C library variant from the app container's Docker image — no manual input needed in most cases.

**How it works:**

1. Extracts the image URI from the task definition's essential container
2. Fetches the image config via the **ECR API** (for ECR images) or the **OCI Distribution API** (for Docker Hub, GHCR, and other registries) — without pulling the full image. Uses credentials from `~/.docker/config.json` if available
3. Inspects `Entrypoint`, `Cmd`, and environment variables to classify the language:
   - `java`, `jar` in entrypoint/cmd → **Java**
   - `node`, `npm`, `yarn`, `npx` → **Node.js**
   - `python`, `gunicorn`, `uvicorn`, `flask`, `django` → **Python**
   - Falls back to env vars: `JAVA_HOME`, `NODE_VERSION`, `PYTHON_VERSION`
4. Detects the C library variant (glibc vs musl):
   - `ALPINE_VERSION` in env → **musl**
   - `alpine` or `musl` in image name/tag → **musl**
   - Otherwise → **glibc**

If detection is inconclusive, the tool falls back to an interactive prompt.

**Supported registries:** Amazon ECR, Docker Hub (`docker.io`), GitHub Container Registry (`ghcr.io`), and any OCI-compliant registry.

## Installation

### One-line install (Linux / macOS)

```bash
MW_API_KEY=<your-key> MW_TARGET=https://<uid>.middleware.io:443 \
  bash -c "$(curl -L https://install.middleware.io/scripts/mw-ecs-install.sh)"
```

This installs the `mw-ecs` binary (via `.deb`, `.rpm`, or raw binary depending on your OS) and saves `MW_API_KEY` and `MW_TARGET` to `/etc/mw-agent/mw-ecs.conf` so you don't need to pass them as flags every time.

You can also install without credentials and configure later:

```bash
bash -c "$(curl -L https://install.middleware.io/scripts/mw-ecs-install.sh)"
```

### Build from source

```bash
make build

# Or directly with Go
go build -o mw-ecs .
```

**Prerequisites:** Go 1.21+, AWS credentials configured (`aws configure` or environment variables).

## Configuration

The tool reads `MW_API_KEY` and `MW_TARGET` with the following precedence:

1. **CLI flags** (`--mw-api-key`, `--mw-target`) — highest priority
2. **Config file** (`/etc/mw-agent/mw-ecs.conf`) — written by the install script

If both are configured, flags override the config file. If the config file exists, `--mw-api-key` and `--mw-target` flags can be omitted.

**Config file format** (`/etc/mw-agent/mw-ecs.conf`):
```
MW_API_KEY=your-api-key-here
MW_TARGET=https://uid.middleware.io:443
```

## Commands

### `instrument` — Inject MW instrumentation

```bash
# If installed with MW_API_KEY and MW_TARGET, no need to pass them:
mw-ecs instrument --task-definition my-app:3

# Or pass them explicitly to override the config file:
mw-ecs instrument \
  --task-definition my-app:3 \
  --mw-api-key <key> \
  --mw-target https://<uid>.middleware.io

# All flags provided — no prompts
mw-ecs instrument \
  --task-definition my-app:3 \
  --language java --libc glibc --enable-apm --enable-logs --register

# Multiple task definitions (comma-separated)
mw-ecs instrument \
  --task-definition my-app:3,my-api:2,my-worker:1 \
  --enable-apm --enable-logs

# Batch mode — discover and instrument all task definitions
mw-ecs instrument --all --enable-apm --enable-logs --dry-run
```

#### Flags

| Flag | Required | Description |
|---|---|---|
| `--task-definition` | Yes | Task definition family:revision or full ARN (repeatable or comma-separated). Required if `--all` is not used |
| `--all` | No | Discover and instrument all active families. Required if `--task-definition` is not provided |
| `--mw-api-key` | No | Middleware API key (reads from `/etc/mw-agent/mw-ecs.conf` if omitted) |
| `--mw-target` | No | Middleware target URL (reads from `/etc/mw-agent/mw-ecs.conf` if omitted) |
| `--language` | No | APM language: `java`, `node`, `python` (auto-detected or prompted if omitted) |
| `--libc` | No | C library variant: `glibc`, `musl` (auto-detected if omitted) |
| `--enable-apm` | No | Add APM init container (prompted if omitted) |
| `--enable-logs` | No | Add FireLens log routing (prompted if omitted) |
| `--service-name` | No | `MW_SERVICE_NAME` for the app (defaults to family name) |
| `--region` | No | AWS region (defaults to AWS CLI config) |
| `--fargate` | No | Configure for Fargate (awsvpc network mode, auto-detected) |
| `--output` | No | Output file path (defaults to `<family>-instrumented.json`) |
| `--register` | No | Register the new revision with ECS |
| `--run` | No | Run a task after registering (requires `--register`) |
| `--cluster` | No | ECS cluster for `--run` (prompted if omitted) |
| `--subnets` | No | Subnet IDs for Fargate `--run`, comma-separated |
| `--security-groups` | No | Security group IDs for Fargate `--run`, comma-separated |
| `--dry-run` | No | Print modified task definition to stdout without writing |

### `detect` — Test auto-detection

Inspect a container image's metadata to detect language and libc. Useful for verifying auto-detection before instrumenting.

```bash
mw-ecs detect docker.io/advait11/demo-node-app
mw-ecs detect ghcr.io/myorg/my-app:latest
mw-ecs detect 123456789.dkr.ecr.us-east-1.amazonaws.com/my-app:latest
```

### `discover` — List instrumentation status

Shows all active task definition families and whether each has the MW agent, APM init container, and FireLens log router.

```bash
mw-ecs discover
mw-ecs discover --region us-west-2
```

### `rollback` — Revert to previous revision

Re-registers the previous revision as a new revision, effectively undoing the instrumentation.

```bash
mw-ecs rollback --task-definition my-app:5
```

## How it works

1. Fetches the existing task definition via `DescribeTaskDefinition`
2. **Auto-detects language and C library** from the app container's image metadata (ECR or OCI registry API)
3. Replaces any existing MW containers (`mw-agent`, `instrumentation-init`, `log_router`)
4. Injects `mw-agent` sidecar with platform-specific config (Fargate vs EC2)
5. (If APM enabled) Adds `instrumentation-init` container with a copy command and shared volume, patches app containers with language-specific env vars, mount points, and `dependsOn`
6. (If logs enabled) Adds `log_router` (Fluent Bit) sidecar and sets `awsfirelens` logConfiguration on app containers. Prompts before overriding existing log config
7. Recalculates task-level CPU and memory (snaps to valid Fargate tiers if applicable)
8. Outputs a clean, registration-ready JSON (strips server-side-only fields)
9. Optionally registers the new revision and runs a task

## Safe by default

- **Auto-replace MW containers** — existing `mw-agent`, `instrumentation-init`, and `log_router` are replaced silently to ensure consistent config
- **Log config prompt** — only prompts when overriding an existing log configuration (e.g., cloudwatch → awsfirelens)
- **Env var merge** — MW env vars are merged by key; existing app env vars with different names are preserved
- **Mount/volume dedup** — checks before adding to avoid duplicates
- **Network mode preserved** — the tool never modifies the task's network mode
- **Dry run** — preview changes with `--dry-run` before committing
- **Graceful detection fallback** — if auto-detection fails, falls back to interactive prompts

## Development

```bash
# Build
make build

# Run tests
make test

# Run tests with coverage
make test-coverage

# Build for all platforms
make build-all

# Lint
make lint
```

## Project structure

```
mw-ecsation/
├── main.go                          # Entrypoint
├── cmd/
│   ├── root.go                      # CLI root command
│   ├── config.go                    # Config file reader (/etc/mw-agent/mw-ecs.conf)
│   ├── instrument.go                # instrument subcommand (single/multi/batch)
│   ├── detect.go                    # detect subcommand (test auto-detection)
│   ├── discover.go                  # discover subcommand
│   ├── register.go                  # register task definition
│   ├── rollback.go                  # rollback subcommand
│   └── run.go                       # run a task
├── internal/
│   ├── aws/
│   │   └── client.go                # ECS + ECR API client wrapper
│   ├── instrument/
│   │   ├── constants.go             # Images, volumes, language config, mount paths
│   │   ├── containers.go            # Container/env var builders (Fargate + EC2)
│   │   ├── detect.go                # Language + libc auto-detection from image metadata
│   │   ├── detect_test.go           # Detection unit tests
│   │   ├── patch.go                 # Core patching logic
│   │   └── serialize.go             # Clean JSON output serializer
│   └── prompt/
│       └── prompt.go                # Interactive terminal prompts
├── architecture.md                  # Detailed architecture diagrams and flow
├── doc.md                           # CLI reference with all commands and examples
├── Makefile
└── go.mod
```
