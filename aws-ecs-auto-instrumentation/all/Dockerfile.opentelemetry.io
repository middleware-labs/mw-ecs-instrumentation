# ── Java ──────────────────────────────────────────────
FROM alpine:latest AS java-builder
RUN apk add --no-cache curl
RUN curl -L -o /opentelemetry-javaagent.jar \
    https://github.com/open-telemetry/opentelemetry-java-instrumentation/releases/latest/download/opentelemetry-javaagent.jar

# ── Node ──────────────────────────────────────────────
FROM node:22-alpine AS node-builder
WORKDIR /autoinstrumentation
RUN npm init -y && \
    npm install \
        @opentelemetry/sdk-node \
        @opentelemetry/auto-instrumentations-node \
        @opentelemetry/exporter-trace-otlp-grpc \
        @opentelemetry/exporter-metrics-otlp-grpc \
        @opentelemetry/sdk-metrics
COPY instrument.otel.js /autoinstrumentation/instrument.js

# ── Python ────────────────────────────────────────────
FROM python:3.12-slim AS python-builder
WORKDIR /autoinstrumentation
COPY requirements.txt /tmp/requirements.txt
RUN pip install --target=/autoinstrumentation/packages -r /tmp/requirements.txt

# ── Final image ──────────────────────────────────────
FROM alpine:latest

COPY --from=java-builder /opentelemetry-javaagent.jar /autoinstrumentation/java/opentelemetry-javaagent.jar
COPY --from=node-builder /autoinstrumentation /autoinstrumentation/node
COPY --from=python-builder /autoinstrumentation /autoinstrumentation/python

CMD ["sh", "-c", "\
    mkdir -p /mnt/mw-agent/instrumentation/java \
             /mnt/mw-agent/instrumentation/node \
             /mnt/mw-agent/instrumentation/python && \
    cp -a /autoinstrumentation/java/. /mnt/mw-agent/instrumentation/java/ && \
    cp -a /autoinstrumentation/node/. /mnt/mw-agent/instrumentation/node/ && \
    cp -a /autoinstrumentation/python/. /mnt/mw-agent/instrumentation/python/ \
"]
