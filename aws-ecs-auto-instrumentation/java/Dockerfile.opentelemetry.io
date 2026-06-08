FROM alpine:latest AS downloader

RUN apk add --no-cache curl

# Download the latest OpenTelemetry java agent jar
RUN curl -L -o /opentelemetry-javaagent.jar \
    https://github.com/open-telemetry/opentelemetry-java-instrumentation/releases/latest/download/opentelemetry-javaagent.jar

FROM alpine:latest
COPY --from=downloader /opentelemetry-javaagent.jar /autoinstrumentation/opentelemetry-javaagent.jar

CMD ["sh", "-c", "mkdir -p /mnt/mw-agent/instrumentation/java && cp -a /autoinstrumentation/. /mnt/mw-agent/instrumentation/java/"]
