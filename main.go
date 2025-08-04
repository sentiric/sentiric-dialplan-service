package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"time"

	"github.com/sentiric/sentiric-dialplan-service/internal/logger" // YENİ

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"
	"github.com/rs/zerolog" // YENİ
	dialplanv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/dialplan/v1"
	userv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/user/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

// Sabitler ve Global Değişkenler
const serviceName = "dialplan-service"

var log zerolog.Logger

type server struct {
	dialplanv1.UnimplementedDialplanServiceServer
	db *sql.DB
}

func main() {
	godotenv.Load()
	log = logger.New(serviceName)

	log.Info().Msg("Dialplan Service başlatılıyor...")

	db := connectToDBWithRetry(getEnvOrFail("POSTGRES_URL"), 10)
	defer db.Close()

	port := getEnv("DIALPLAN_SERVICE_GRPC_PORT", "50054")
	listenAddr := fmt.Sprintf(":%s", port)
	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatal().Err(err).Msg("TCP port dinlenemedi")
	}

	creds := loadServerTLS(
		getEnvOrFail("DIALPLAN_SERVICE_CERT_PATH"),
		getEnvOrFail("DIALPLAN_SERVICE_KEY_PATH"),
		getEnvOrFail("GRPC_TLS_CA_PATH"),
	)

	s := grpc.NewServer(grpc.Creds(creds))
	dialplanv1.RegisterDialplanServiceServer(s, &server{db: db})
	reflection.Register(s)

	log.Info().Str("port", port).Msg("gRPC sunucusu dinleniyor...")
	if err := s.Serve(lis); err != nil {
		log.Fatal().Err(err).Msg("gRPC sunucusu başlatılamadı")
	}
}

// ... (Database ve TLS fonksiyonları aynı, sadece loglama çağrıları güncellendi) ...

func connectToDBWithRetry(dsn string, maxRetries int) *sql.DB {
	var db *sql.DB
	var err error
	for i := 1; i <= maxRetries; i++ {
		db, err = sql.Open("pgx", dsn)
		if err == nil {
			if pingErr := db.Ping(); pingErr == nil {
				log.Info().Msg("Veritabanı bağlantısı başarılı.")
				return db
			} else {
				err = pingErr
			}
		}
		log.Warn().Err(err).Int("attempt", i).Int("max_attempts", maxRetries).Msg("Veritabanına bağlanılamadı, 5 saniye sonra tekrar denenecek...")
		time.Sleep(5 * time.Second)
	}
	log.Fatal().Err(err).Msg("Veritabanına bağlanılamadı")
	return nil
}

func loadServerTLS(certPath, keyPath, caPath string) credentials.TransportCredentials {
	serverCert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		log.Fatal().Err(err).Msg("Sertifika yüklenemedi")
	}
	caPEM, err := ioutil.ReadFile(caPath)
	if err != nil {
		log.Fatal().Err(err).Msg("CA sertifikası okunamadı")
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caPEM) {
		log.Fatal().Msg("CA sertifikası geçersiz.")
	}
	return credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    caPool,
	})
}

func (s *server) ResolveDialplan(ctx context.Context, req *dialplanv1.ResolveDialplanRequest) (*dialplanv1.ResolveDialplanResponse, error) {
	l := log.With().Str("method", "ResolveDialplan").Str("caller_id", req.CallerId).Str("destination", req.DestinationNumber).Logger()
	l.Info().Msg("İstek alındı")

	var tenantID, activeDP, failsafeDP sql.NullString
	var maintenance sql.NullBool
	err := s.db.QueryRowContext(ctx,
		`SELECT tenant_id, active_dialplan_id, failsafe_dialplan_id, is_maintenance_mode
		 FROM inbound_routes WHERE phone_number = $1`, req.DestinationNumber).
		Scan(&tenantID, &activeDP, &failsafeDP, &maintenance)

	if err != nil {
		if err == sql.ErrNoRows {
			l.Warn().Msg("Aranan numara için inbound_route bulunamadı, sistem failsafe planına yönlendiriliyor.")
			return s.getDialplanByID(ctx, "DP_SYSTEM_FAILSAFE", nil)
		}
		l.Error().Err(err).Msg("Inbound route sorgusu başarısız")
		return nil, status.Errorf(codes.Internal, "Route sorgusu başarısız: %v", err)
	}

	if maintenance.Valid && maintenance.Bool {
		l.Info().Str("failsafe_dialplan", failsafeDP.String).Msg("Sistem bakım modunda, failsafe planına yönlendiriliyor.")
		return s.getDialplanByID(ctx, failsafeDP.String, nil)
	}

	var user userv1.User
	var name sql.NullString
	err = s.db.QueryRowContext(ctx,
		`SELECT id, name, tenant_id, user_type FROM users WHERE id = $1 AND tenant_id = $2`,
		req.CallerId, tenantID.String).
		Scan(&user.Id, &name, &user.TenantId, &user.UserType)

	if err != nil {
		if err == sql.ErrNoRows {
			l.Info().Msg("Arayan kullanıcı veritabanında bulunamadı, misafir (guest) planına yönlendiriliyor.")
			return s.getDialplanByID(ctx, "DP_GUEST_ENTRY", nil)
		}
		l.Error().Err(err).Msg("Kullanıcı sorgusu başarısız")
		return nil, status.Errorf(codes.Internal, "User sorgusu başarısız: %v", err)
	}

	if name.Valid {
		user.Name = name.String
	}
	l.Info().Str("active_dialplan", activeDP.String).Msg("Kullanıcı bulundu, aktif plana yönlendiriliyor.")
	return s.getDialplanByID(ctx, activeDP.String, &user)
}

func (s *server) getDialplanByID(ctx context.Context, id string, user *userv1.User) (*dialplanv1.ResolveDialplanResponse, error) {
	l := log.With().Str("method", "getDialplanByID").Str("dialplan_id", id).Logger()
	l.Info().Msg("Dialplan detayları alınıyor")

	var description, action, tenantID sql.NullString
	var actionBytes []byte

	err := s.db.QueryRowContext(ctx,
		`SELECT description, action, action_data, tenant_id FROM dialplans WHERE id = $1`, id).
		Scan(&description, &action, &actionBytes, &tenantID)
	if err != nil {
		if err == sql.ErrNoRows {
			l.Error().Msg("Dialplan ID bulunamadı, sistem failsafe planına yönlendiriliyor.")
			if id == "DP_SYSTEM_FAILSAFE" {
				l.Fatal().Msg("KRİTİK HATA: Sistem failsafe dialplan (DP_SYSTEM_FAILSAFE) dahi bulunamadı!")
				return nil, status.Error(codes.Internal, "Sistem dialplan eksik: DP_SYSTEM_FAILSAFE.")
			}
			return s.getDialplanByID(ctx, "DP_SYSTEM_FAILSAFE", user)
		}
		l.Error().Err(err).Msg("Dialplan sorgusu başarısız")
		return nil, status.Errorf(codes.Internal, "Dialplan sorgusu başarısız: %v", err)
	}

	dataMap := map[string]string{}
	if actionBytes != nil {
		if err := json.Unmarshal(actionBytes, &dataMap); err != nil {
			l.Warn().Err(err).Msg("action_data JSON parse edilemedi")
		}
	}

	resp := &dialplanv1.ResolveDialplanResponse{
		DialplanId: id,
		TenantId:   tenantID.String,
		Action: &dialplanv1.DialplanAction{
			Action:     action.String,
			ActionData: &dialplanv1.ActionData{Data: dataMap},
		},
	}
	if user != nil {
		resp.MatchedUser = user
	}

	l.Info().Str("action", resp.Action.Action).Msg("Dialplan başarıyla çözümlendi")
	return resp, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func getEnvOrFail(key string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	log.Fatal().Str("variable", key).Msg("Gerekli ortam değişkeni tanımlı değil")
	return ""
}
