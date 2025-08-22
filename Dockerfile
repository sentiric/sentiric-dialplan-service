# --- İNŞA AŞAMASI (DEBIAN TABANLI) ---
FROM golang:1.24-bullseye AS builder

RUN apt-get update && apt-get install -y --no-install-recommends git

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

ARG SERVICE_NAME

RUN CGO_ENABLED=0 GOOS=linux go build -o /app/bin/sentiric-dialplan-service .

# --- ÇALIŞTIRMA AŞAMASI (ALPINE) ---
FROM alpine:latest

RUN apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=builder /app/bin/sentiric-dialplan-service .

ENTRYPOINT ["./sentiric-dialplan-service"]