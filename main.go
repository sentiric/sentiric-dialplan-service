// ========== FILE: sentiric-dialplan-service/main.go ==========
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

	"github.com/sentiric/sentiric-dialplan-service/internal/logger"

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

	creds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      caCertPool,
		ServerName:   "user-service",
	})

	conn, err := grpc.NewClient(userServiceURL, grpc.WithTransportCredentials(creds))
	if err != nil {
		log.Fatal().Err(err).Msg("User Service'e gRPC bağlantısı kurulamadı")
	}

	log.Info().Str("url", userServiceURL).Msg("User Service'e gRPC istemci bağlantısı başarılı")
	return userv1.NewUserServiceClient(conn)
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

	db := connectToDBWithRetry(getEnvOrFail("POSTGRES_URL"), 10)
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

func (s *server) ResolveDialplan(ctx context.Context, req *dialplanv1.ResolveDialplanRequest) (*dialplanv1.ResolveDialplanResponse, error) {
	l, traceID := getLoggerWithTraceID(ctx, log)
	l = l.With().Str("method", "ResolveDialplan").Str("caller", req.GetCallerContactValue()).Str("destination", req.GetDestinationNumber()).Logger()
	l.Info().Msg("İstek alındı")

	var tenantID, activeDP, failsafeDP sql.NullString
	var maintenance sql.NullBool
	err := s.db.QueryRowContext(ctx,
		`SELECT tenant_id, active_dialplan_id, failsafe_dialplan_id, is_maintenance_mode
		 FROM inbound_routes WHERE phone_number = $1`, req.GetDestinationNumber()).
		Scan(&tenantID, &activeDP, &failsafeDP, &maintenance)

	if err != nil {
		if err == sql.ErrNoRows {
			l.Warn().Msg("Aranan numara için inbound_route bulunamadı, sistem failsafe planına yönlendiriliyor.")
			return s.getDialplanByID(ctx, "DP_SYSTEM_FAILSAFE", nil, nil)
		}
		l.Error().Err(err).Msg("Inbound route sorgusu başarısız")
		return nil, status.Errorf(codes.Internal, "Route sorgusu başarısız: %v", err)
	}

	if maintenance.Valid && maintenance.Bool {
		l.Info().Str("failsafe_dialplan", failsafeDP.String).Msg("Sistem bakım modunda, failsafe planına yönlendiriliyor.")
		return s.getDialplanByID(ctx, failsafeDP.String, nil, nil)
	}

	// DÜZELTME: Giden context'e trace_id'yi manuel olarak ekliyoruz.
	userReqCtx := metadata.AppendToOutgoingContext(ctx, "x-trace-id", traceID)

	userRes, err := s.userClient.FindUserByContact(userReqCtx, &userv1.FindUserByContactRequest{
		ContactType:  "phone",
		ContactValue: req.GetCallerContactValue(),
	})

	if err != nil {
		st, ok := status.FromError(err)
		if ok && st.Code() == codes.NotFound {
			l.Info().Msg("Arayan kullanıcı User Service'de bulunamadı, misafir (guest) planına yönlendiriliyor.")
			return s.getDialplanByID(ctx, "DP_GUEST_ENTRY", nil, nil)
		}

		l.Error().Err(err).Msg("User Service'e yapılan FindUserByContact çağrısı başarısız")
		return s.getDialplanByID(ctx, failsafeDP.String, nil, nil)
	}

	matchedUser := userRes.GetUser()
	var matchedContact *userv1.Contact
	for _, c := range matchedUser.Contacts {
		if c.ContactValue == req.GetCallerContactValue() {
			matchedContact = c
			break
		}
	}

	l.Info().Str("active_dialplan", activeDP.String).Str("user_id", matchedUser.Id).Msg("Kullanıcı bulundu, aktif plana yönlendiriliyor.")
	return s.getDialplanByID(ctx, activeDP.String, matchedUser, matchedContact)
}

func (s *server) getDialplanByID(ctx context.Context, id string, user *userv1.User, contact *userv1.Contact) (*dialplanv1.ResolveDialplanResponse, error) {
	l, _ := getLoggerWithTraceID(ctx, log)
	l = l.With().Str("method", "getDialplanByID").Str("dialplan_id", id).Logger()
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
			return s.getDialplanByID(ctx, "DP_SYSTEM_FAILSAFE", user, contact)
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
		DialplanId: id,
		TenantId:   tenantID.String,
		Action: &dialplanv1.DialplanAction{
			Action:     action.String,
			ActionData: &dialplanv1.ActionData{Data: dataMap},
		},
		MatchedUser:    user,
		MatchedContact: contact,
	}

	l.Info().Str("action", resp.Action.Action).Msg("Dialplan başarıyla çözümlendi")
	return resp, nil
}

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
