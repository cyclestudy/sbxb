FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -tags "with_quic,with_utls" \
    -ldflags "-s -w -X github.com/cyclestudy/sbxb/cmd/sbxb.version=$(git describe --tags --always 2>/dev/null || echo dev)" \
    -o /sbxb .

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /sbxb /usr/local/bin/sbxb

RUN mkdir -p /etc/sbxb

ENTRYPOINT ["sbxb"]
CMD ["server", "-c", "/etc/sbxb/config.json"]
