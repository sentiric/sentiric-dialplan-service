// sentiric-dialplan-service/internal/database/database.go
package database

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewConnection, veritabanı havuzunu oluşturur.
// [ARCH-COMPLIANCE FIX] FATAL Crash Engellendi.
// Eğer PostgreSQL'e anında ulaşılamazsa (Ping hata verirse), havuz (Pool)
// yine de oluşturulur ve arka planda Lazy olarak tekrar bağlanmaya çalışır.
// Ancak ping'in başarısız olduğunu bildirmek için hem Pool hem de Ping Error döneriz.
func NewConnection(url string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}

	config.MaxConns = 10
	config.MinConns = 2
	config.MaxConnLifetime = time.Hour
	config.MaxConnIdleTime = time.Minute * 30

	// Pool oluşturma işlemi için zaman aşımı
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// NewWithConfig, veritabanına hemen bağlanmaz, sadece konfigürasyonla pool yaratır.
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}

	// Eğer Ping başarısız olursa, havuzu kapatmıyoruz! (Ghost Mode devamlılığı)
	// Havuzu (pool) geçerli olarak dönüyoruz ama ping hatasını da yukarı taşıyoruz.
	pingErr := pool.Ping(ctx)

	return pool, pingErr
}
