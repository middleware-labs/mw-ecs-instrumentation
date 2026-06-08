FROM node:22-alpine as builder

WORKDIR /autoinstrumentation

# Install node apm package for auto instrumentation
RUN npm init -y && \
    npm install \
        @middleware.io/node-apm \
        dotenv

# Copy in instrumentation entry point
COPY instrument.js /autoinstrumentation/instrument.js

FROM node:22-alpine
COPY --from=builder /autoinstrumentation /autoinstrumentation

# Init container will copy the instrumentation dependencies to the shared volume and exit
CMD ["sh", "-c", "cp -a /autoinstrumentation/. /mnt/mw-agent/instrumentation/node/"]
