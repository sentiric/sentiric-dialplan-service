// sentiric-dialplan-service/internal/database/database.go
package database

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewConnection, veritabanı havuzunu oluşturur.
// [ARCH-COMPLIANCE FIX] FATAL Crash Engellendi. (Ghost Mode)
func NewConnection(url string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}

	config.MaxConns = 10
	config.MinConns = 2
	config.MaxConnLifetime = time.Hour
	config.MaxConnIdleTime = time.Minute * 30

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Havuzu yaratıyoruz, hemen bağlanmaya zorlamıyoruz.
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}

	// Ping atıyoruz, başarısızsa havuzu KESİNLİKLE KAPATMIYORUZ.
	// Hata mesajını app.go'ya iletiyoruz ki log basabilsin.
	pingErr := pool.Ping(ctx)

	return pool, pingErr
}
