FROM --platform=$BUILDPLATFORM golang:1.25-bookworm AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -ldflags="-s -w" -o /out/tailgrant-server ./cmd/tailgrant-server && \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -ldflags="-s -w" -o /out/tailgrant-worker ./cmd/tailgrant-worker

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/tailgrant-server /usr/local/bin/tailgrant-server
COPY --from=builder /out/tailgrant-worker /usr/local/bin/tailgrant-worker

USER nonroot:nonroot
