# --- AŞAMA 1: Derleme (Builder) ---
# Daha yeni bir Go sürümü kullan (>=1.24.5)
FROM golang:1.24.5-alpine AS builder

WORKDIR /app

# Önce sadece bağımlılık dosyalarını kopyala (önbellekleme için)
COPY go.mod go.sum ./
RUN go mod download

# Şimdi kaynak kodunu ve üretilmiş gRPC kodunu kopyala
COPY . .

# Uygulamayı derle (statik olarak, C kütüphaneleri olmadan)
RUN CGO_ENABLED=0 GOOS=linux go build -o /dialplan-service .

# --- AŞAMA 2: Çalıştırma (Runtime) ---
FROM scratch

WORKDIR /

# Derlenmiş uygulamayı builder aşamasından kopyala
COPY --from=builder /dialplan-service .

EXPOSE 50054

ENTRYPOINT ["/dialplan-service"]
