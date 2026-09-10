# The service image. Configuration is environment only, so the image carries no
# command line, and migrations are embedded in the binary, so it carries nothing
# but the binary, the healthcheck binary and the certificate bundle.
FROM golang:1.27.1-alpine3.24 AS build
ENV GOTOOLCHAIN=local
# sqlc's Postgres parser is cgo.
RUN apk add --no-cache build-base
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY buf.yaml buf.lock buf.gen.go.yaml sqlc.yaml ./
COPY proto/ proto/
RUN go tool buf generate --template buf.gen.go.yaml
COPY server/ server/
RUN go tool sqlc generate
# The build context has no .git, so the revision arrives as a build argument.
ARG BUILD_REV=unknown
RUN CGO_ENABLED=0 go build -buildvcs=false -trimpath \
    -ldflags "-s -w -X main.buildRevision=$BUILD_REV" -o /stonks ./server/cmd/stonks
RUN CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags "-s -w" \
    -o /healthcheck ./server/cmd/healthcheck

FROM alpine:3.24.1
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /stonks /healthcheck /
USER 65534:65534
EXPOSE 8090
ENTRYPOINT ["/stonks"]
