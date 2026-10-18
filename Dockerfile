# One image, two programs: sleipnir (the panel) and huginn (the exit check that runs inside a
# tunnel). They ship together so the panel and its probe never drift apart. Compose picks one with
# `command`. The module has no requires, so the compile downloads nothing.
FROM golang:1.26-alpine AS build

WORKDIR /src
# The release tag, logged at start so what is deployed is readable from the logs.
ARG VERSION=local
COPY go.mod ./
COPY cmd/ ./cmd/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.buildVersion=${VERSION}" -o /out/sleipnir ./cmd/sleipnir && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.buildVersion=${VERSION}" -o /out/huginn ./cmd/huginn

FROM alpine:3.21

# Gluetun's API is plain HTTP inside the network, but huginn asks an IP-info service over HTTPS.
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/sleipnir /out/huginn /usr/local/bin/

ENTRYPOINT []
CMD ["sleipnir"]
