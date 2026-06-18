# Architecture: ECS Auto-Instrumentation CLI

## Overview

`mw-ecs` is a CLI tool that automatically injects OpenTelemetry auto-instrumentation into AWS ECS task definitions. It adds sidecar containers, init containers, environment variables, and log routing to enable APM and log collection without modifying application code.

## High-Level Flow

```
┌─────────────────────────────────────────────────────────────────────┐
│                          CLI Entry (main.go)                        │
│                     cobra root command + subcommands                │
└──────┬──────────┬──────────┬──────────┬──────────┬─────────────────┘
       │          │          │          │          │
  ┌────▼───┐ ┌───▼────┐ ┌───▼────┐ ┌───▼────┐ ┌───▼─────┐
  │instrument│ │detect  │ │discover│ │register│ │rollback │
  └────┬───┘ └────────┘ └────────┘ └────────┘ └─────────┘
       │
  ┌────▼──────────────────────────────────────────────────────┐
  │                   Instrument Flow                          │
  │                                                            │
  │  1. Fetch task definition from ECS (internal/aws)          │
  │  2. Use flags if provided, prompt for missing options      │
  │  3. Auto-detect language + libc from container image       │
  │  4. Patch task definition (internal/instrument)            │
  │  5. Register new revision / write JSON / dry-run           │
  │  6. Optionally run a task with the new definition          │
  └────────────────────────────────────────────────────────────┘
```

## Telemetry Flow

```
┌─────────────────┐     OTLP http/protobuf     ┌──────────────────┐     gRPC      ┌────────────────┐
│  App Container   │ ──────────────────────────► │  mw-agent        │ ────────────► │  MW Backend    │
│                  │     localhost:9320           │  sidecar         │               │                │
│  OTEL_EXPORTER   │     (awsvpc/host mode)      │                  │               │                │
│  _OTLP_ENDPOINT  │                             │  MW_API_KEY      │               │                │
│                  │     or MW target URL         │  MW_TARGET       │               │                │
│                  │     (bridge mode)            │  OTEL_PROTOCOL   │               │                │
│                  │                              │  = grpc          │               │                │
└─────────────────┘                              └──────────────────┘               └────────────────┘
```

| Network Mode | App OTLP Endpoint | Routing |
|---|---|---|
| `awsvpc` (Fargate) | `http://localhost:9320` | App → mw-agent sidecar → MW backend |
| `host` (EC2) | `http://localhost:9320` | App → mw-agent sidecar → MW backend |
| `bridge` (EC2 default) | MW target URL directly | App → MW backend (no sidecar relay) |

## Instrumentation Pipeline

```
                          ┌──────────────┐
                          │  ECS Task    │
                          │  Definition  │
                          │  (original)  │
                          └──────┬───────┘
                                 │
                    ┌────────────▼────────────┐
                    │   Auto-Detect Language   │
                    │   (detect.go)            │
                    │                          │
                    │  Image URI               │
                    │    │                     │
                    │    ├─ ECR? ──► BatchGet  │
                    │    │          Image +    │
                    │    │          config     │
                    │    │          blob       │
                    │    │                     │
                    │    └─ OCI? ──► Registry  │
                    │               HTTP API   │
                    │               (auth +    │
                    │               manifest + │
                    │               config)    │
                    │                          │
                    │  Heuristics:             │
                    │   Entrypoint/Cmd ──► lang│
                    │   Env vars ──► lang      │
                    │   ALPINE_VERSION ──► musl│
                    │   image tag ──► musl     │
                    └────────────┬─────────────┘
                                 │
                                 │ (fallback: interactive prompt)
                                 │
                    ┌────────────▼────────────┐
                    │   Patch Task Definition  │
                    │   (patch.go)              │
                    │                          │
                    │  ┌────────────────────┐  │
                    │  │ Replace mw-agent   │  │
                    │  │ sidecar (Fargate   │  │
                    │  │ or EC2 variant)    │  │
                    │  └────────────────────┘  │
                    │  ┌────────────────────┐  │
                    │  │ Replace init       │  │
                    │  │ container (with    │  │
                    │  │ copy command)      │  │
                    │  └────────────────────┘  │
                    │  ┌────────────────────┐  │
                    │  │ Replace FireLens   │  │
                    │  │ log_router sidecar │  │
                    │  └────────────────────┘  │
                    │  ┌────────────────────┐  │
                    │  │ Inject env vars    │  │
                    │  │ + mount points     │  │
                    │  │ + dependsOn        │  │
                    │  │ (app containers    │  │
                    │  │  only)             │  │
                    │  └────────────────────┘  │
                    │  ┌────────────────────┐  │
                    │  │ Recalculate CPU/   │  │
                    │  │ Memory (Fargate    │  │
                    │  │ snap)              │  │
                    │  └────────────────────┘  │
                    └────────────┬─────────────┘
                                 │
                          ┌──────▼───────┐
                          │  ECS Task    │
                          │  Definition  │
                          │ (instrumented)│
                          └──────────────┘
```

## Auto-Detection Flow (detect.go)

```
  appContainerImage(td)
          │
          │  extract image URI from first essential container
          │  (skips mw-agent, instrumentation-init, log_router)
          │
          ▼
  parseImageRef(imageURI)
          │
          ├─ "123456789.dkr.ecr.us-east-1.amazonaws.com/app:v1"
          │     ──► isECR=true, region="us-east-1"
          │
          ├─ "docker.io/user/app:latest"
          │     ──► normalize to registry-1.docker.io
          │
          ├─ "ghcr.io/org/repo:tag"
          │     ──► generic OCI registry
          │
          └─ "nginx"
                ──► Docker Hub library/nginx:latest
          │
          ▼
  ┌───────────────────────────────┐
  │  Fetch Image Config           │
  │                               │
  │  Tries ~/.docker/config.json  │
  │  creds automatically          │
  │                               │
  │  ECR path:                    │
  │    BatchGetImage ──► manifest │
  │    GetAuthToken ──► bearer    │
  │    GET /v2/.../blobs/sha ──►  │
  │    image config JSON          │
  │                               │
  │  OCI path:                    │
  │    GET /v2/.../manifests/tag  │
  │    ──► 401 + WWW-Authenticate │
  │    ──► fetch bearer token     │
  │    ──► retry manifest         │
  │    ──► GET config blob        │
  │    (follows 307 redirects)    │
  └───────────────┬───────────────┘
                  │
                  ▼
  ┌───────────────────────────────┐
  │  classifyConfig()             │
  │                               │
  │  Priority 1: Entrypoint/Cmd   │
  │    "java" / "jar"    ──► Java │
  │    "node" / "npm"    ──► Node │
  │    "yarn" / "npx"    ──► Node │
  │    "python"          ──► Py   │
  │    "gunicorn"        ──► Py   │
  │    "uvicorn"         ──► Py   │
  │                               │
  │  Priority 2: Env vars         │
  │    JAVA_HOME         ──► Java │
  │    NODE_VERSION      ──► Node │
  │    PYTHON_VERSION    ──► Py   │
  │    PYTHON_PATH       ──► Py   │
  └───────────────┬───────────────┘
                  │
                  ▼
  ┌───────────────────────────────┐
  │  detectLibC()                 │
  │                               │
  │  ALPINE_VERSION env  ──► musl │
  │  "alpine" in image   ──► musl │
  │  "musl" in image     ──► musl │
  │  otherwise           ──► glibc│
  └───────────────────────────────┘
```

## Injected Containers

When instrumentation is applied, up to three containers are injected into the task definition:

```
┌──────────────────────────────────────────────────────────────┐
│                    ECS Task Definition                        │
│                                                              │
│  ┌──────────────────┐    ┌────────────────────────────────┐  │
│  │  App Container    │    │  mw-agent sidecar              │  │
│  │  (user's app)     │    │  (OTEL collector/forwarder)    │  │
│  │                   │    │                                │  │
│  │  + env vars:      │    │  Env:                          │  │
│  │    OTEL_ENDPOINT  │    │    MW_API_KEY                  │  │
│  │    OTEL_PROTOCOL  │    │    MW_TARGET                   │  │
│  │    OTEL_SERVICE   │    │    OTEL_PROTOCOL=grpc          │  │
│  │    JAVA_TOOL_OPTS │    │                                │  │
│  │    NODE_OPTIONS   │    │  Fargate: cpu=256, port 8006   │  │
│  │    PYTHONPATH     │    │           + 9320               │  │
│  │                   │    │  EC2: cpu=100, mem=512,        │  │
│  │  + mount:         │    │       ports 9319+8006+9320,    │  │
│  │    /otel-auto-*   │    │       privileged, host mounts  │  │
│  │                   │    └────────────────────────────────┘  │
│  │  + dependsOn:     │                                        │
│  │    init=SUCCESS   │    ┌────────────────────────────────┐  │
│  └──────────────────┘    │  instrumentation-init           │  │
│                          │  (copies agent files via cmd)   │  │
│  ┌──────────────────┐    │                                │  │
│  │  log_router       │    │  Command:                      │  │
│  │  (Fluent Bit)     │    │    cp -r /autoinstrumentation/ │  │
│  │                   │    │    <mount-path>                 │  │
│  │  firelens config  │    │                                │  │
│  └──────────────────┘    │  Mount: /otel-auto-instr-*     │  │
│                          └────────────────────────────────┘  │
│  ┌──────────────────────────────────────────────────────┐    │
│  │  Shared Volume: "mw-agent-instrumentation"            │    │
│  └──────────────────────────────────────────────────────┘    │
└──────────────────────────────────────────────────────────────┘
```

## Per-Language Init Container Configuration

| Language | Init Image | Mount Path | Copy Command | Key Env Var on App |
|----------|-----------|------------|-------------|---|
| Java | `autoinstrumentation-java:2.19.0` | `/otel-auto-instrumentation-java` | `cp /javaagent.jar <mount>/javaagent.jar` | `JAVA_TOOL_OPTIONS=-javaagent:<mount>/javaagent.jar` |
| Node | `autoinstrumentation-nodejs:0.53.0` | `/otel-auto-instrumentation-nodejs` | `cp -r /autoinstrumentation/. <mount>` | `NODE_OPTIONS=--require <mount>/autoinstrumentation.js` |
| Python | `autoinstrumentation-python:0.59b0` | `/otel-auto-instrumentation-python` | `cp -r /autoinstrumentation/. <mount>` | `PYTHONPATH=<mount>/opentelemetry/instrumentation/auto_instrumentation:<mount>` |

All images sourced from `ghcr.io/open-telemetry/opentelemetry-operator/`.

For Python with musl libc, mount path becomes `/otel-auto-instrumentation-python-musl` (appends the `MuslSuffix` constant).

## MW Agent Sidecar Variants

| | Fargate | EC2 |
|---|---|---|
| CPU | 256 | 100 |
| Memory | — | 512 |
| Essential | true | true |
| Privileged | — | true |
| Ports | 8006 (http), 9320 | 9319 (grpc), 8006, 9320 |
| Mount Points | none | docker-sock, proc, dev, cgroup (al1+al2), docker-containers-root, var-log-host |
| Env Vars | MW_API_KEY, MW_TARGET, OTEL_PROTOCOL=grpc | MW_API_KEY, MW_TARGET, OTEL_PROTOCOL=grpc |

## Project Structure

```
.
├── main.go                          # entrypoint
├── cmd/
│   ├── root.go                      # cobra root command
│   ├── config.go                    # config file reader (/etc/mw-agent/mw-ecs.conf)
│   ├── instrument.go                # instrument subcommand (single/multi/batch)
│   ├── detect.go                    # detect subcommand (test auto-detection)
│   ├── discover.go                  # list ECS task families
│   ├── register.go                  # register task definition revision
│   ├── rollback.go                  # revert to previous revision
│   └── run.go                       # run a task from a definition
├── internal/
│   ├── aws/
│   │   └── client.go                # ECS + ECR SDK wrapper
│   ├── instrument/
│   │   ├── constants.go             # images, languages, mount paths, types
│   │   ├── containers.go            # container/env-var builders (Fargate + EC2)
│   │   ├── detect.go                # language + libc auto-detection
│   │   ├── detect_test.go           # detection unit tests
│   │   ├── patch.go                 # task definition patching logic
│   │   └── serialize.go             # JSON serialization
│   └── prompt/
│       └── prompt.go                # interactive terminal prompts
└── aws-ecs-auto-instrumentation/    # Dockerfiles for init container images
    ├── java/
    ├── node/
    └── python/
```

## Configuration Precedence

```
┌─────────────────────────────────┐
│  CLI flags (--mw-api-key, etc.) │  ◄── highest priority
└──────────────┬──────────────────┘
               │ if empty
               ▼
┌─────────────────────────────────┐
│  /etc/mw-agent/mw-ecs.conf     │  ◄── written by install script
│                                 │
│  MW_API_KEY=...                 │
│  MW_TARGET=...                  │
└──────────────┬──────────────────┘
               │ if empty
               ▼
         error: flag required
```

The install script (`mw-ecs-tool-install.sh`) writes the config file when `MW_API_KEY` and `MW_TARGET` are passed as environment variables during installation.

## CLI Modes

```
mw-ecs instrument
    │
    ├── --task-definition <single>     ──► runSingleInstrument
    │     unified flow: flags → auto-detect → prompt for rest
    │
    ├── --task-definition <a>,<b>,<c>  ──► runMultiInstrument
    │     global APM/logs prompt, per-task language auto-detect
    │
    └── --all                          ──► runBatchInstrument
          discovers all families, per-task language auto-detect
```

All modes use the same flow: use provided flags, auto-detect what's missing, prompt for the rest. There is no separate interactive/non-interactive path.
