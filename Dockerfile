#### Build Step ####
FROM golang:1.10-alpine as builder
RUN apk add --update make git
WORKDIR /go/src/git.containerum.net/ch/api-gateway
COPY . .
RUN VERSION=$(git describe --abbrev=0 --tags) make build-for-docker

#### Generate Cert Step ####
FROM alpine:3.7 as generator

RUN apk update && \
  apk add --no-cache openssl && \
  rm -rf /var/cache/apk/*

WORKDIR /cert

RUN openssl req -subj '/CN=containerum.io/O=Containerum/C=LV' -new -newkey rsa:2048 -sha256 -days 365 -nodes -x509 -keyout key.pem -out cert.pem

#### Run Step ####
FROM alpine:3.7

# Copy bin and migrations
COPY --from=builder /go/src/git.containerum.net/ch/api-gateway/charts/api-gateway/env/config.toml /
COPY --from=builder /go/src/git.containerum.net/ch/api-gateway/charts/api-gateway/env/routes /routes
COPY --from=builder /tmp/api-gateway /

# Copy certs
COPY --from=generator /cert /cert

# Set envs
ENV GATEWAY_DEBUG=false \
    GRPC_AUTH_ADDRESS="127.0.0.1:1112" \
    CONFIG_FILE="config.toml" \
    ROUTES_FILE="routes/routes.toml" \
    TLS_CERT="cert/cert.pem" \
    TLS_KEY="cert/key.pem" \
    SERVICE_HOST_PREFIX=""

EXPOSE 8082 8282

# run app
WORKDIR "/"
CMD "./api-gateway"
