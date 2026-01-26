package dialplan

import "errors"

var (
	// ErrNotFound: İstenen kayıt veritabanında bulunamadı.
	ErrNotFound = errors.New("record not found")

	// ErrConflict: Kayıt zaten mevcut (Unique constraint violation).
	ErrConflict = errors.New("record already exists")

	// ErrDatabase: Beklenmeyen veritabanı hatası.
	ErrDatabase = errors.New("database internal error")
	
	// ErrTableMissing: Kritik altyapı hatası (Tablo yok).
	ErrTableMissing = errors.New("critical: database table missing")
)