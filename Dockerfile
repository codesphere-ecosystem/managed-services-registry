FROM golang:1.26.5 AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

ARG TARGETOS=linux
ARG TARGETARCH=amd64

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
	go build -trimpath -ldflags="-s -w" -o /out/managed-services-registry ./cmd/server

FROM dhi.io/debian-base:trixie

COPY --chmod=755 --from=builder /out/managed-services-registry /managed-services-registry

EXPOSE 8080

ENTRYPOINT ["/managed-services-registry"]
