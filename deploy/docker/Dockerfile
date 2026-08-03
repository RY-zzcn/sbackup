FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
ARG VERSION=dev
RUN case "$TARGETARCH/$TARGETVARIANT" in amd64/*) goarch=amd64 ;; arm64/*) goarch=arm64 ;; arm/v7) goarch=arm; export GOARM=7 ;; *) goarch="$TARGETARCH" ;; esac && \
    CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$goarch" go build -trimpath -buildvcs=false -ldflags="-s -w -X main.version=${VERSION#v}" -o /out/sbackup-monitor ./cmd/sbackup-monitor

FROM alpine:3.20
RUN addgroup -S sbackup && adduser -S -G sbackup sbackup
COPY --from=build /out/sbackup-monitor /usr/local/bin/sbackup-monitor
USER sbackup
VOLUME ["/var/lib/sbackup-monitor"]
EXPOSE 8788
ENTRYPOINT ["/usr/local/bin/sbackup-monitor"]
