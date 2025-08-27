# --- İNŞA AŞAMASI (DEBIAN TABANLI) ---
FROM golang:1.24-bullseye AS builder

# Git'i kur (gerekirse, özel modüller için)
RUN apt-get update && apt-get install -y --no-install-recommends git

WORKDIR /app

# Önce bağımlılıkları çekmek için mod dosyalarını kopyala
COPY go.mod go.sum ./
RUN go mod download

# Tüm proje kaynak kodunu kopyala
COPY . .

# === DÜZELTME ===
# Derleme komutuna main paketinin tam yolunu veriyoruz.
# Eski Hali: RUN CGO_ENABLED=0 GOOS=linux go build -o /app/bin/sentiric-dialplan-service .
# Yeni Hali:
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/bin/sentiric-dialplan-service ./cmd/dialplan-service

# --- ÇALIŞTIRMA AŞAMASI (ALPINE) ---
FROM alpine:latest

# Güvenlik sertifikalarını ekle
RUN apk add --no-cache ca-certificates

WORKDIR /app

# Sadece derlenmiş binary dosyasını kopyala
COPY --from=builder /app/bin/sentiric-dialplan-service .

# Servisi başlat
ENTRYPOINT ["./sentiric-dialplan-service"]