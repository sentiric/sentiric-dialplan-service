package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"
	dialplanv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/dialplan/v1"
	userv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/user/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

type server struct {
	dialplanv1.UnimplementedDialplanServiceServer
	db *sql.DB
}

func main() {
	log.Println("Dialplan Service başlatılıyor...")

	_ = godotenv.Load() // .env yüklense veya yüklenmese, Docker’dan gelen env’lere odak

	db := connectToDBWithRetry(getEnvOrFail("POSTGRES_URL"), 10)
	defer db.Close()

	port := getEnv("DIALPLAN_SERVICE_GRPC_PORT", "50054")
	listenAddr := fmt.Sprintf(":%s", port)
	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalf("TCP port dinlenemedi: %v", err)
	}

	creds := loadServerTLS(
		getEnvOrFail("DIALPLAN_SERVICE_CERT_PATH"),
		getEnvOrFail("DIALPLAN_SERVICE_KEY_PATH"),
		getEnvOrFail("GRPC_TLS_CA_PATH"),
	)

	s := grpc.NewServer(grpc.Creds(creds))
	dialplanv1.RegisterDialplanServiceServer(s, &server{db: db})
	reflection.Register(s)

	log.Printf("gRPC Dialplan Service %s portunda dinleniyor...", listenAddr)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("gRPC sunucusu başlatılamadı: %v", err)
	}
}

// ========================
// === DB ve TLS Setup ===
// ========================

func connectToDBWithRetry(dsn string, maxRetries int) *sql.DB {
	var db *sql.DB
	var err error
	for i := 1; i <= maxRetries; i++ {
		db, err = sql.Open("pgx", dsn)
		if err == nil && db.Ping() == nil {
			log.Println("Veritabanı bağlantısı başarılı.")
			return db
		}
		log.Printf("Veritabanına bağlanılamadı (deneme %d/%d): %v", i, maxRetries, err)
		time.Sleep(5 * time.Second)
	}
	log.Fatalf("Veritabanına bağlanılamadı: %v", err)
	return nil
}

func loadServerTLS(certPath, keyPath, caPath string) credentials.TransportCredentials {
	serverCert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		log.Fatalf("Sertifika yüklenemedi: %v", err)
	}
	caPEM, err := ioutil.ReadFile(caPath)
	if err != nil {
		log.Fatalf("CA sertifikası okunamadı: %v", err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caPEM) {
		log.Fatal("CA sertifikası geçersiz.")
	}
	return credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    caPool,
	})
}

// ============================================================
// === gRPC servis metodları — ResolveDialplan & helper =======
// ============================================================

func (s *server) ResolveDialplan(ctx context.Context, req *dialplanv1.ResolveDialplanRequest) (*dialplanv1.ResolveDialplanResponse, error) {
	log.Printf("ResolveDialplan çağrıldı: caller=%s, dest=%s", req.CallerId, req.DestinationNumber)

	var tenantID, activeDP, failsafeDP sql.NullString
	var maintenance sql.NullBool
	err := s.db.QueryRowContext(ctx,
		`SELECT tenant_id, active_dialplan_id, failsafe_dialplan_id, is_maintenance_mode
		 FROM inbound_routes WHERE phone_number = $1`, req.DestinationNumber).
		Scan(&tenantID, &activeDP, &failsafeDP, &maintenance)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Printf("Destination bulunamadı, fallback dialplan DP_SYSTEM_FAILSAFE kullanılacak.")
			return s.getDialplanByID(ctx, "DP_SYSTEM_FAILSAFE", nil)
		}
		return nil, status.Errorf(codes.Internal, "Route sorgusu başarısız: %v", err)
	}

	if maintenance.Valid && maintenance.Bool {
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
			log.Printf("Caller kullanıcı değil → guest dialplan.")
			return s.getDialplanByID(ctx, "DP_GUEST_ENTRY", nil)
		}
		return nil, status.Errorf(codes.Internal, "User sorgusu başarısız: %v", err)
	}

	if name.Valid {
		user.Name = name.String
	}
	return s.getDialplanByID(ctx, activeDP.String, &user)
}

func (s *server) getDialplanByID(ctx context.Context, id string, user *userv1.User) (*dialplanv1.ResolveDialplanResponse, error) {
	log.Printf("Dialplan detayları alınıyor: %s", id)
	var description, action, tenantID sql.NullString
	var actionBytes []byte

	err := s.db.QueryRowContext(ctx,
		`SELECT description, action, action_data, tenant_id FROM dialplans WHERE id = $1`, id).
		Scan(&description, &action, &actionBytes, &tenantID)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Printf("Dialplan ID %s bulunamadı → fallback sistem dialplan.", id)
			if id == "DP_SYSTEM_FAILSAFE" {
				return nil, status.Error(codes.Internal, "Sistem dialplan eksik: DP_SYSTEM_FAILSAFE.")
			}
			return s.getDialplanByID(ctx, "DP_SYSTEM_FAILSAFE", user)
		}
		return nil, status.Errorf(codes.Internal, "Dialplan sorgusu başarısız: %v", err)
	}

	dataMap := map[string]string{}
	if actionBytes != nil {
		if err := json.Unmarshal(actionBytes, &dataMap); err != nil {
			log.Printf("WARN: action_data JSON parse hatalı: %v", err)
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

	log.Printf("Dialplan çözümlendi: id=%s action=%s", resp.DialplanId, resp.Action.Action)
	return resp, nil
}

// =============================
// === Yardımcı Fonksiyonlar ===
// =============================
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
	log.Fatalf("Ortam değişkeni yok: %s", key)
	return ""
}
