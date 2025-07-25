# --- AŞAMA 1: Derleme (Builder) ---
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /dialplan-service .

# --- AŞAMA 2: Çalıştırma (Runtime) ---
FROM scratch
WORKDIR /
COPY --from=builder /dialplan-service .
EXPOSE 50054
ENTRYPOINT ["/dialplan-service"]