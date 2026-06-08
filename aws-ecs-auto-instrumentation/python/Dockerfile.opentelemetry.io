FROM python:3.12-slim AS builder
WORKDIR /autoinstrumentation

COPY requirements.txt /tmp/requirements.txt

RUN pip install --target=/autoinstrumentation/packages -r /tmp/requirements.txt

FROM alpine:latest
COPY --from=builder /autoinstrumentation /autoinstrumentation
CMD ["sh", "-c", "mkdir -p /mnt/mw-agent/instrumentation/python && cp -a /autoinstrumentation/. /mnt/mw-agent/instrumentation/python/"]
