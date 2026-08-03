FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/sbackup-monitor ./cmd/sbackup-monitor

FROM alpine:3.20
RUN addgroup -S sbackup && adduser -S -G sbackup sbackup
COPY --from=build /out/sbackup-monitor /usr/local/bin/sbackup-monitor
USER sbackup
VOLUME ["/var/lib/sbackup-monitor"]
EXPOSE 8788
ENTRYPOINT ["/usr/local/bin/sbackup-monitor"]
