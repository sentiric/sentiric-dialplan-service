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
	"strings"
	"time"

	"github.com/sentiric/sentiric-dialplan-service/internal/logger"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn" // Hata tipini kontrol etmek için GEREKLİ
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
	dialplanv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/dialplan/v1"
	userv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/user/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

const serviceName = "dialplan-service"

var log zerolog.Logger

type server struct {
	dialplanv1.UnimplementedDialplanServiceServer
	db         *sql.DB
	userClient userv1.UserServiceClient
}

func getLoggerWithTraceID(ctx context.Context, baseLogger zerolog.Logger) (zerolog.Logger, string) {
	md, ok := metadata.FromIncomingContext(ctx)
	traceID := "unknown"
	if !ok {
		return baseLogger.With().Str("trace_id", traceID).Logger(), traceID
	}
	traceIDValues := md.Get("x-trace-id")
	if len(traceIDValues) > 0 {
		traceID = traceIDValues[0]
	}
	return baseLogger.With().Str("trace_id", traceID).Logger(), traceID
}

func main() {
	godotenv.Load()
	log = logger.New(serviceName)
	log.Info().Msg("Dialplan Service başlatılıyor...")

	dbURL := getEnvOrFail("POSTGRES_URL")

	db := connectToDBWithRetry(dbURL, 10)
	defer db.Close()

	userClient := createUserServiceClient()

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
	dialplanv1.RegisterDialplanServiceServer(s, &server{db: db, userClient: userClient})
	reflection.Register(s)

	log.Info().Str("port", port).Msg("gRPC sunucusu dinleniyor...")
	if err := s.Serve(lis); err != nil {
		log.Fatal().Err(err).Msg("gRPC sunucusu başlatılamadı")
	}
}

// ======================================================================
// ===                RESOLVE DIALPLAN (GÜNCELLENMİŞ HALİ)            ===
// ======================================================================
func (s *server) ResolveDialplan(ctx context.Context, req *dialplanv1.ResolveDialplanRequest) (*dialplanv1.ResolveDialplanResponse, error) {
	l, traceID := getLoggerWithTraceID(ctx, log)
	l = l.With().Str("method", "ResolveDialplan").Str("caller", req.GetCallerContactValue()).Str("destination", req.GetDestinationNumber()).Logger()
	l.Info().Msg("İstek alındı")

	var route dialplanv1.InboundRoute
	var activeDP, offHoursDP, failsafeDP sql.NullString

	query := `
		SELECT 
			phone_number, tenant_id, active_dialplan_id, off_hours_dialplan_id, failsafe_dialplan_id, 
			is_maintenance_mode, default_language_code
		FROM inbound_routes WHERE phone_number = $1
	`
	err := s.db.QueryRowContext(ctx, query, req.GetDestinationNumber()).Scan(
		&route.PhoneNumber, &route.TenantId, &activeDP, &offHoursDP, &failsafeDP,
		&route.IsMaintenanceMode, &route.DefaultLanguageCode,
	)

	// --- YENİ VE DAYANIKLI HATA YÖNETİMİ ---
	if err != nil {
		// Hatanın tipini kontrol et
		if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.Code == "42P01" { // 42P01 = undefined_table
			l.Warn().Err(err).Msg("Kritik 'inbound_routes' tablosu bulunamadı, sistem failsafe planına yönlendiriliyor.")
			return s.getDialplanByID(ctx, "DP_SYSTEM_FAILSAFE_TR", nil, nil, nil)
		}

		if err == sql.ErrNoRows {
			l.Warn().Msg("Aranan numara için inbound_route bulunamadı, sistem failsafe planına yönlendiriliyor.")
			return s.getDialplanByID(ctx, "DP_SYSTEM_FAILSAFE_TR", nil, nil, nil)
		}

		// Diğer tüm veritabanı hataları
		l.Error().Err(err).Msg("Inbound route sorgusu başarısız")
		return nil, status.Errorf(codes.Internal, "Route sorgusu başarısız: %v", err)
	}

	// Null string'leri optional proto alanlarına güvenli bir şekilde ata
	if activeDP.Valid {
		route.ActiveDialplanId = &activeDP.String
	}
	if offHoursDP.Valid {
		route.OffHoursDialplanId = &offHoursDP.String
	}
	if failsafeDP.Valid {
		route.FailsafeDialplanId = &failsafeDP.String
	}

	if route.IsMaintenanceMode {
		l.Info().Str("failsafe_dialplan", safeString(route.FailsafeDialplanId)).Msg("Sistem bakım modunda, failsafe planına yönlendiriliyor.")
		return s.getDialplanByID(ctx, safeString(route.FailsafeDialplanId), nil, nil, &route)
	}

	userReqCtx := metadata.AppendToOutgoingContext(ctx, "x-trace-id", traceID)
	userRes, err := s.userClient.FindUserByContact(userReqCtx, &userv1.FindUserByContactRequest{
		ContactType:  "phone",
		ContactValue: req.GetCallerContactValue(),
	})

	if err != nil {
		st, ok := status.FromError(err)
		if ok && st.Code() == codes.NotFound {
			l.Info().Msg("Arayan kullanıcı User Service'de bulunamadı, misafir (guest) planına yönlendiriliyor.")
			return s.getDialplanByID(ctx, "DP_GUEST_ENTRY", nil, nil, &route)
		}
		l.Error().Err(err).Msg("User Service'e yapılan FindUserByContact çağrısı başarısız")
		return s.getDialplanByID(ctx, safeString(route.FailsafeDialplanId), nil, nil, &route)
	}

	matchedUser := userRes.GetUser()
	var matchedContact *userv1.Contact
	for _, c := range matchedUser.Contacts {
		if c.ContactValue == req.GetCallerContactValue() {
			matchedContact = c
			break
		}
	}

	l.Info().Str("active_dialplan", safeString(route.ActiveDialplanId)).Str("user_id", matchedUser.Id).Msg("Kullanıcı bulundu, aktif plana yönlendiriliyor.")
	return s.getDialplanByID(ctx, safeString(route.ActiveDialplanId), matchedUser, matchedContact, &route)
}

// ======================================================================
// ===                      (FONKSİYON SONU)                          ===
// ======================================================================

func (s *server) getDialplanByID(ctx context.Context, id string, user *userv1.User, contact *userv1.Contact, route *dialplanv1.InboundRoute) (*dialplanv1.ResolveDialplanResponse, error) {
	l, _ := getLoggerWithTraceID(ctx, log)
	l = l.With().Str("method", "getDialplanByID").Str("dialplan_id", id).Logger()
	l.Info().Msg("Dialplan detayları alınıyor")

	var description, action, tenantID sql.NullString
	var actionBytes []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT description, action, action_data, tenant_id FROM dialplans WHERE id = $1`, id).
		Scan(&description, &action, &actionBytes, &tenantID)
	if err != nil {
		// --- YENİ: Failsafe planı bile bulunamazsa kritik hata ver ---
		if err == sql.ErrNoRows {
			l.Error().Msg("Dialplan ID bulunamadı, sistem failsafe planına yönlendiriliyor.")
			if strings.HasPrefix(id, "DP_SYSTEM_FAILSAFE") {
				l.Fatal().Msg("KRİTİK HATA: Sistem failsafe dialplan dahi bulunamadı! '02_core_data.sql' script'inin çalıştığından emin olun.")
				// Fatal, programı sonlandıracağı için return'e gerek yok.
			}
			// Hangi dilde failsafe'e gidileceğini belirle
			lang := "TR"
			if route != nil && route.DefaultLanguageCode != "" {
				lang = strings.ToUpper(route.DefaultLanguageCode)
			}
			return s.getDialplanByID(ctx, fmt.Sprintf("DP_SYSTEM_FAILSAFE_%s", lang), user, contact, route)
		}
		l.Error().Err(err).Msg("Dialplan sorgusu başarısız")
		return nil, status.Errorf(codes.Internal, "Dialplan sorgusu başarısız: %v", err)
	}
	var dataMap map[string]string
	if actionBytes != nil {
		if err := json.Unmarshal(actionBytes, &dataMap); err != nil {
			l.Warn().Err(err).Msg("action_data JSON parse edilemedi")
			dataMap = make(map[string]string)
		}
	}
	resp := &dialplanv1.ResolveDialplanResponse{
		DialplanId:     id,
		TenantId:       tenantID.String,
		Action:         &dialplanv1.DialplanAction{Action: action.String, ActionData: &dialplanv1.ActionData{Data: dataMap}},
		MatchedUser:    user,
		MatchedContact: contact,
		InboundRoute:   route,
	}
	l.Info().Str("action", resp.Action.Action).Msg("Dialplan başarıyla çözümlendi")
	return resp, nil
}

func safeString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func createUserServiceClient() userv1.UserServiceClient {
	userServiceURL := getEnvOrFail("USER_SERVICE_GRPC_URL")
	certPath := getEnvOrFail("DIALPLAN_SERVICE_CERT_PATH")
	keyPath := getEnvOrFail("DIALPLAN_SERVICE_KEY_PATH")
	caPath := getEnvOrFail("GRPC_TLS_CA_PATH")
	clientCert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		log.Fatal().Err(err).Msg("İstemci sertifikası yüklenemedi")
	}
	caCert, err := os.ReadFile(caPath)
	if err != nil {
		log.Fatal().Err(err).Msg("CA sertifikası okunamadı")
	}
	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		log.Fatal().Msg("CA sertifikası havuza eklenemedi")
	}
	creds := credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{clientCert}, RootCAs: caCertPool, ServerName: "user-service"})
	conn, err := grpc.NewClient(userServiceURL, grpc.WithTransportCredentials(creds))
	if err != nil {
		log.Fatal().Err(err).Msg("User Service'e gRPC bağlantısı kurulamadı")
	}
	log.Info().Str("url", userServiceURL).Msg("User Service'e gRPC istemci bağlantısı başarılı")
	return userv1.NewUserServiceClient(conn)
}

func connectToDBWithRetry(url string, maxRetries int) *sql.DB {
	var db *sql.DB
	var err error

	// 1. URL'yi parse et
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		log.Fatal().Err(err).Msg("PostgreSQL URL parse edilemedi")
	}

	// 2. Connection Pooler ile uyumluluk için prepared statement'ları devre dışı bırak
	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	// 3. Yeni, yapılandırılmış URL ile bağlantıyı yeniden dene
	finalURL := stdlib.RegisterConnConfig(config.ConnConfig)

	for i := 0; i < maxRetries; i++ {
		db, err = sql.Open("pgx", finalURL)
		if err == nil {
			db.SetConnMaxLifetime(time.Minute * 3)
			db.SetMaxIdleConns(2)
			db.SetMaxOpenConns(5)
			if pingErr := db.Ping(); pingErr == nil {
				log.Info().Msg("Veritabanına bağlantı başarılı (Simple Protocol Mode).")
				return db
			} else {
				err = pingErr
			}
		}
		log.Warn().Err(err).Int("attempt", i+1).Int("max_attempts", maxRetries).Msg("Veritabanına bağlanılamadı, 5 saniye sonra tekrar denenecek...")
		time.Sleep(5 * time.Second)
	}
	log.Fatal().Err(err).Msgf("Veritabanına bağlanılamadı (%d deneme)", maxRetries)
	return nil
}

func loadServerTLS(certPath, keyPath, caPath string) credentials.TransportCredentials {
	certificate, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		log.Fatal().Err(err).Msg("Sunucu sertifikası yüklenemedi")
	}
	caCert, err := ioutil.ReadFile(caPath)
	if err != nil {
		log.Fatal().Err(err).Msg("CA sertifikası okunamadı")
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCert) {
		log.Fatal().Msg("CA sertifikası havuza eklenemedi.")
	}
	tlsConfig := &tls.Config{Certificates: []tls.Certificate{certificate}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: caPool}
	return credentials.NewTLS(tlsConfig)
}

func getEnv(key string, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func getEnvOrFail(key string) string {
	val := os.Getenv(key)
	if val == "" {
		log.Fatal().Str("variable", key).Msg("Gerekli ortam değişkeni tanımlı değil")
	}
	return val
}
