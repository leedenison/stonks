# The provider stand-in for the e2e stack: a reverse proxy over go-vcr, built
# from the same module as the service. The cassettes are mounted at run time,
# so a recording changes nothing in the image.
FROM golang:1.27.1-alpine3.24 AS build
ENV GOTOOLCHAIN=local
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY server/cmd/vcrproxy/ server/cmd/vcrproxy/
COPY server/cmd/healthcheck/ server/cmd/healthcheck/
COPY server/internal/testutil/vcr/ server/internal/testutil/vcr/
RUN CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags "-s -w" \
    -o /vcrproxy ./server/cmd/vcrproxy
RUN CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags "-s -w" \
    -o /healthcheck ./server/cmd/healthcheck

FROM alpine:3.24.2
RUN apk add --no-cache ca-certificates
COPY --from=build /vcrproxy /healthcheck /
USER 65534:65534
EXPOSE 8080
ENTRYPOINT ["/vcrproxy"]
