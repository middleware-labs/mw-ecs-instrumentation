FROM python:3.12-slim AS builder
WORKDIR /autoinstrumentation

# everything will be installed to /autoinstrumentation/packages, which will be copied to the final image
RUN pip install --target=/autoinstrumentation/packages middleware-io setuptools==81.0.0

FROM python:3.12-slim
COPY --from=builder /autoinstrumentation /autoinstrumentation
CMD ["sh", "-c", "mkdir -p /mnt/mw-agent/instrumentation/python && cp -a /autoinstrumentation/. /mnt/mw-agent/instrumentation/python/"]

# destination path is /mnt/mw-agent/instrumentation/python/, 
# so  PYTHONPATH is /mnt/mw-agent/instrumentation/python/packages/opentelemetry/instrumentation/auto_instrumentation:/mnt/mw-agent/instrumentation/python/packages 