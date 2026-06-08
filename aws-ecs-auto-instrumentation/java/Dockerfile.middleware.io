FROM alpine:latest AS downloader

RUN apk add --no-cache curl

# Download the latest middleware java agent jar
RUN curl -L -o /middleware-javaagent.jar \
    https://github.com/middleware-labs/opentelemetry-java-instrumentation/releases/latest/download/middleware-javaagent.jar

FROM alpine:latest
COPY --from=downloader /middleware-javaagent.jar /autoinstrumentation/middleware-javaagent.jar

CMD ["sh", "-c", "mkdir -p /mnt/mw-agent/instrumentation/java && cp -a /autoinstrumentation/. /mnt/mw-agent/instrumentation/java/"]