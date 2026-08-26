# Shared build for every binary in cmd/. Pass BINARY=ingester or BINARY=queryapi.
FROM golang:1.25-alpine AS build

ARG BINARY
ARG VERSION=dev

WORKDIR /src

# Dependencies as their own layer so source edits do not re-download the module
# graph on every build.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN test -n "$BINARY" || (echo "build arg BINARY is required" >&2; exit 1) \
 && CGO_ENABLED=0 go build \
      -trimpath \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/service ./cmd/${BINARY}

# alpine rather than scratch: busybox wget makes compose healthchecks possible
# without baking a healthcheck binary into the image.
FROM alpine:3.21

RUN adduser -D -u 10001 sap
COPY --from=build /out/service /usr/local/bin/service
USER sap

ENTRYPOINT ["/usr/local/bin/service"]
