# --- İNŞA AŞAMASI (DEBIAN TABANLI) ---
FROM golang:1.24-bullseye AS builder

# YENİ: Build argümanlarını build aşamasında kullanılabilir yap
ARG GIT_COMMIT="unknown"
ARG BUILD_DATE="unknown"
ARG SERVICE_VERSION="0.0.0"

# Git'i kur (gerekirse, özel modüller için)
RUN apt-get update && apt-get install -y --no-install-recommends git

WORKDIR /app

# Önce bağımlılıkları çekmek için mod dosyalarını kopyala
COPY go.mod go.sum ./
RUN go mod download

# Tüm proje kaynak kodunu kopyala
COPY . .

# GÜNCELLEME: ldflags ile build-time değişkenlerini Go binary'sine göm
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-X main.GitCommit=${GIT_COMMIT} -X main.BuildDate=${BUILD_DATE} -X main.ServiceVersion=${SERVICE_VERSION} -w -s" \
    -o /app/bin/sentiric-dialplan-service ./cmd/dialplan-service

# --- ÇALIŞTIRMA AŞAMASI (ALPINE) ---
FROM alpine:latest

# Güvenlik sertifikalarını ekle
RUN apk add --no-cache ca-certificates

WORKDIR /app

# Sadece derlenmiş binary dosyasını kopyala
COPY --from=builder /app/bin/sentiric-dialplan-service .

# Servisi başlat
ENTRYPOINT ["./sentiric-dialplan-service"]