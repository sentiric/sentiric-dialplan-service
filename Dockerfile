# --- İNŞA AŞAMASI (DEBIAN TABANLI) ---
FROM golang:1.24-bullseye AS builder

RUN apt-get update && apt-get install -y --no-install-recommends git

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG SERVICE_NAME
# Bu servisin main.go'su kök dizinde olduğu için build komutu farklı
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/bin/${SERVICE_NAME} .

# --- ÇALIŞTIRMA AŞAMASI (ALPINE) ---
FROM alpine:latest

RUN apk add --no-cache ca-certificates

ARG SERVICE_NAME
WORKDIR /app
COPY --from=builder /app/bin/${SERVICE_NAME} .

RUN addgroup -S appgroup && adduser -S appuser -G appgroup
USER appuser

ENTRYPOINT ["./sentiric-dialplan-service"]