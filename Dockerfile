FROM golang:1.23-alpine AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY *.go ./

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -trimpath -o /Freebuff2API .

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S app && adduser -S app -G app

COPY --from=builder /Freebuff2API /usr/local/bin/Freebuff2API

USER app

# Expose proxy port
EXPOSE 8080

ENTRYPOINT ["Freebuff2API"]
