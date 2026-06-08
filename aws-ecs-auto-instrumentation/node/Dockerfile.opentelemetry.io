FROM node:22-alpine as builder

WORKDIR /autoinstrumentation

# Install OpenTelemetry packages for auto instrumentation
RUN npm init -y && \
    npm install \
        @opentelemetry/sdk-node \
        @opentelemetry/auto-instrumentations-node \
        @opentelemetry/exporter-trace-otlp-grpc \
        @opentelemetry/exporter-metrics-otlp-grpc \
        @opentelemetry/sdk-metrics

# Copy in instrumentation entry point
COPY instrument.otel.js /autoinstrumentation/instrument.js

FROM node:22-alpine
COPY --from=builder /autoinstrumentation /autoinstrumentation

# Init container will copy the instrumentation dependencies to the shared volume and exit
CMD ["sh", "-c", "cp -a /autoinstrumentation/. /mnt/mw-agent/instrumentation/node/"]
