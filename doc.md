# CLI Reference

## Commands

- [`instrument`](#instrument) — Inject MW agent sidecar, APM init container, and FireLens log routing
- [`detect`](#detect) — Auto-detect language and libc from a container image
- [`discover`](#discover) — List all active task definition families and their instrumentation status
- [`register`](#register) — Register task definition JSON files with ECS
- [`run`](#run) — Run ECS tasks with given task definitions
- [`rollback`](#rollback) — Revert to the previous revision of task definitions

---

## instrument

Instrument an ECS task definition by injecting:
- mw-agent sidecar container (Fargate or EC2 variant)
- APM init container (java/node/python) with copy command and shared volume
- FireLens log_router sidecar with awsfirelens logConfiguration

The tool uses provided flags, auto-detects what's missing, and prompts for the rest.

Use `--task-definition` for one or more task definitions, or `--all` to discover and instrument every active task definition family in the account.

### Flags

| Flag | Required | Description |
|---|---|---|
| `--task-definition` | Yes | Task definition family:revision or full ARN (repeatable or comma-separated). Required if `--all` is not used |
| `--all` | No | Discover and instrument all active families. Required if `--task-definition` is not provided |
| `--mw-api-key` | Yes | Middleware API key |
| `--mw-target` | Yes | Middleware target URL |
| `--language` | No | APM language: `java`, `node`, `python` (auto-detected or prompted if omitted) |
| `--libc` | No | C library variant: `glibc`, `musl` (auto-detected if omitted) |
| `--enable-apm` | No | Add APM init container (prompted if omitted) |
| `--enable-logs` | No | Add FireLens log routing (prompted if omitted) |
| `--service-name` | No | `MW_SERVICE_NAME` for the app container (defaults to family name) |
| `--region` | No | AWS region (defaults to AWS CLI config) |
| `--fargate` | No | Configure for Fargate (awsvpc network mode, auto-detected from task definition) |
| `--output` | No | Output file path (defaults to `<family>-instrumented.json`) |
| `--register` | No | Register the new revision with ECS |
| `--run` | No | Run a task after registering (requires `--register`) |
| `--cluster` | No | ECS cluster for `--run` (prompted if omitted) |
| `--subnets` | No | Subnet IDs for Fargate `--run`, comma-separated |
| `--security-groups` | No | Security group IDs for Fargate `--run`, comma-separated |
| `--dry-run` | No | Print modified task definition to stdout without writing |

`--task-definition` is the primary required flag. Use `--all` instead when you want to discover and instrument every active family without specifying them individually.

### Examples

**Auto-detects language, prompts for remaining options:**
```bash
mw-ecs-instrument instrument \
  --task-definition my-app:3 \
  --mw-api-key abc123 \
  --mw-target https://uid.middleware.io
```

**All flags provided — no prompts:**
```bash
mw-ecs-instrument instrument \
  --task-definition my-app:3 \
  --mw-api-key abc123 \
  --mw-target https://uid.middleware.io \
  --language java --libc glibc --enable-apm --enable-logs --register
```

**Multiple task definitions** — repeatable flag:
```bash
mw-ecs-instrument instrument \
  --task-definition my-app:3 --task-definition my-api:2 \
  --mw-api-key abc123 \
  --mw-target https://uid.middleware.io \
  --enable-apm --enable-logs
```

**Multiple task definitions** — comma-separated:
```bash
mw-ecs-instrument instrument \
  --task-definition my-app:3,my-api:2,my-worker:1 \
  --mw-api-key abc123 \
  --mw-target https://uid.middleware.io \
  --enable-apm --enable-logs
```

**Batch mode** — discover and instrument all task definitions, dry run:
```bash
mw-ecs-instrument instrument \
  --all \
  --mw-api-key abc123 \
  --mw-target https://uid.middleware.io \
  --enable-apm --enable-logs --dry-run
```

**Register and run** — instrument, register new revision, then run a task:
```bash
mw-ecs-instrument instrument \
  --task-definition my-app:3 \
  --mw-api-key abc123 \
  --mw-target https://uid.middleware.io \
  --language node --enable-apm --register --run \
  --cluster my-cluster
```

**Fargate** — configure for Fargate with network settings:
```bash
mw-ecs-instrument instrument \
  --task-definition my-app:3 \
  --mw-api-key abc123 \
  --mw-target https://uid.middleware.io \
  --language python --enable-apm --fargate --register --run \
  --cluster my-cluster --subnets subnet-abc,subnet-def --security-groups sg-123
```

### Example CLI output

```
━━━ Fetching Task Definition ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  Fetching: my-app:3
  Family:       my-app
  Network mode: bridge
  Containers:   my-app-container, mw-agent, log_router, instrumentation-init

━━━ APM Configuration ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  ➜ Enable APM auto-instrumentation? [Y/n] y

  Detecting language from image: docker.io/myuser/my-app:latest
  ✔ Language: python  |  LibC: glibc

━━━ Log Configuration ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  ! Existing log config detected: firelens

  ➜ Override with awsfirelens? [y/N] y

━━━ Infrastructure ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  ➜ Select the launch type:
    › 1) EC2
    › 2) FARGATE
    Enter choice [1-2]: 1

  ➜ Service name (default: my-app)
    ›
```

---

## detect

Inspect a container image's metadata (Entrypoint, Cmd, Env) to detect the runtime language and C library variant. Useful for verifying auto-detection works for a given image before running `instrument`.

Supports ECR, Docker Hub, GHCR, and any OCI-compliant registry. Credentials from `~/.docker/config.json` are used automatically if available.

### Examples

**Docker Hub image:**
```bash
mw-ecs-instrument detect docker.io/advait11/demo-node-app
```

Output:
```
➜  Inspecting image: docker.io/advait11/demo-node-app
➜  Using credentials from ~/.docker/config.json for registry-1.docker.io
✔  Language: node
✔  LibC:     glibc
```

**Image with tag:**
```bash
mw-ecs-instrument detect nginx:alpine
```

**GHCR image:**
```bash
mw-ecs-instrument detect ghcr.io/myorg/my-app:latest
```

**ECR image** (uses AWS credentials):
```bash
mw-ecs-instrument detect 123456789.dkr.ecr.us-east-1.amazonaws.com/my-app:latest
```

**When detection fails:**
```
➜  Inspecting image: docker.io/myuser/custom-binary
✘  Could not detect language from image: ...
```

**When language is ambiguous:**
```
➜  Inspecting image: docker.io/myuser/multi-runtime
!  Could not determine language from image metadata
```

---

## discover

List all active ECS task definition families in the account and show whether each one already has Middleware instrumentation (mw-agent, init container, log configuration) and the launch type.

### Flags

| Flag | Required | Description |
|---|---|---|
| `--region` | No | AWS region (defaults to AWS CLI config) |

### Examples

**Discover in default region:**
```bash
mw-ecs-instrument discover
```

**Discover in a specific region:**
```bash
mw-ecs-instrument discover --region us-west-2
```

Output:
```
  FAMILY                                               MW-AGENT   APM-INIT   FIRELENS   LAUNCH   LOG-CONFIG
  ──────────────────────────────────────────────────   ────────   ────────   ────────   ──────   ──────────
  my-java-app:5                                        ✔ yes      ✔ yes      ✔ yes      FARGATE  firelens
  my-node-app:12                                       ✘ no       ✘ no       ✘ no       EC2      cloudwatch
  nginx-task:1                                         ✘ no       ✘ no       ✘ no       EC2      —

  Instrumented: 1  |  Not instrumented: 2
```

---

## register

Register task definitions from JSON files as new ECS revisions. Useful when you ran `instrument` without `--register` and have local JSON files to register.

### Flags

| Flag | Required | Description |
|---|---|---|
| `--file` | Yes | Path to task definition JSON file (repeatable or comma-separated) |
| `--region` | No | AWS region (defaults to AWS CLI config) |

### Examples

**Register a single file:**
```bash
mw-ecs-instrument register --file my-app-instrumented.json
```

**Register multiple files:**
```bash
mw-ecs-instrument register \
  --file my-app-instrumented.json \
  --file my-api-instrumented.json
```

**Register with comma-separated files and specific region:**
```bash
mw-ecs-instrument register \
  --file my-app-instrumented.json,my-api-instrumented.json \
  --region us-west-2
```

---

## run

Run ECS tasks using the specified task definitions. Supports one or more task definitions (repeatable or comma-separated). Useful for testing instrumented task definitions after registration.

### Flags

| Flag | Required | Description |
|---|---|---|
| `--task-definition` | Yes | Task definition family:revision or ARN (repeatable or comma-separated) |
| `--cluster` | No | ECS cluster name (prompted if omitted) |
| `--launch-type` | No | Launch type: `EC2` or `FARGATE` (prompted if omitted) |
| `--subnets` | No | Subnet IDs, comma-separated (required for Fargate) |
| `--security-groups` | No | Security group IDs, comma-separated (required for Fargate) |
| `--region` | No | AWS region (defaults to AWS CLI config) |

### Examples

**Run a single task:**
```bash
mw-ecs-instrument run --task-definition my-app:6
```

**Run multiple tasks:**
```bash
mw-ecs-instrument run \
  --task-definition my-app:6 \
  --task-definition my-api:3
```

**Run with all options specified:**
```bash
mw-ecs-instrument run \
  --task-definition my-app:6,my-api:3 \
  --cluster my-cluster \
  --launch-type EC2
```

**Run on Fargate with network config:**
```bash
mw-ecs-instrument run \
  --task-definition my-app:6 \
  --cluster my-cluster \
  --launch-type FARGATE \
  --subnets subnet-abc123,subnet-def456 \
  --security-groups sg-123456
```

---

## rollback

Roll back to the previous revision of one or more task definitions. Fetches the revision before the specified one and re-registers it as a new revision.

Useful for undoing an instrumentation if something went wrong.

### Flags

| Flag | Required | Description |
|---|---|---|
| `--task-definition` | Yes | Task definition family:revision (repeatable or comma-separated) |
| `--region` | No | AWS region (defaults to AWS CLI config) |

### Examples

**Roll back a single task definition:**
```bash
mw-ecs-instrument rollback --task-definition my-app:5
```

**Roll back multiple task definitions:**
```bash
mw-ecs-instrument rollback \
  --task-definition my-app:5 \
  --task-definition my-api:3
```

**Comma-separated:**
```bash
mw-ecs-instrument rollback --task-definition my-app:5,my-api:3
```

---

## Global Behavior

### Auto-Detection

When `--language` is not provided to `instrument`, the tool automatically:
1. Extracts the image URI from the task definition's essential container
2. Fetches image metadata from the registry (ECR, Docker Hub, GHCR, etc.) using credentials from `~/.docker/config.json` if available
3. Detects language from `Entrypoint`/`Cmd` keywords and environment variables
4. Detects libc variant (glibc/musl) from `ALPINE_VERSION` env or image name
5. Falls back to interactive prompt if detection fails

### AWS Authentication

All commands that interact with AWS (instrument, discover, register, run, rollback) use the standard AWS credential chain:
- Environment variables (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`)
- Shared credentials file (`~/.aws/credentials`)
- IAM role (EC2/ECS instance profile)

Override the region with `--region` on any command.

### Flag-driven flow

The tool uses a unified flow: provided flags are used as-is, missing values are auto-detected or prompted. There is no separate interactive/non-interactive mode. To skip all prompts, provide all flags:

```bash
mw-ecs-instrument instrument \
  --task-definition my-app:3 \
  --mw-api-key abc123 \
  --mw-target https://uid.middleware.io \
  --language node --libc glibc \
  --enable-apm --enable-logs \
  --register
```
