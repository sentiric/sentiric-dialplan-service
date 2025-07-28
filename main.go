// DOSYA: sentiric-dialplan-service/main.go (YENİ KONTRAKTLARLA UYUMLU)

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	// structpb import'unu SİLDİK, artık gerek yok.

	_ "github.com/jackc/pgx/v5/stdlib"

	dialplanv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/dialplan/v1"
	userv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/user/v1"
)

type server struct {
	dialplanv1.UnimplementedDialplanServiceServer
	db *sql.DB
}

// ResolveDialplan fonksiyonu aynı kalabilir, değişiklik yok.
func (s *server) ResolveDialplan(ctx context.Context, req *dialplanv1.ResolveDialplanRequest) (*dialplanv1.ResolveDialplanResponse, error) {
	log.Printf("ResolveDialplan request received: caller=%s, destination=%s", req.CallerId, req.DestinationNumber)

	var tenantID, activeDialplanID, failsafeDialplanID sql.NullString
	var isMaintenanceMode sql.NullBool
	routeQuery := "SELECT tenant_id, active_dialplan_id, failsafe_dialplan_id, is_maintenance_mode FROM inbound_routes WHERE phone_number = $1"
	err := s.db.QueryRowContext(ctx, routeQuery, req.DestinationNumber).Scan(&tenantID, &activeDialplanID, &failsafeDialplanID, &isMaintenanceMode)

	if err != nil {
		if err == sql.ErrNoRows {
			log.Printf("Destination number not found in inbound_routes: %s", req.DestinationNumber)
			return s.getDialplanByID(ctx, "DP_SYSTEM_FAILSAFE", nil)
		}
		return nil, status.Errorf(codes.Internal, "database query for route failed: %v", err)
	}

	if isMaintenanceMode.Bool {
		log.Printf("System is in maintenance mode for %s", req.DestinationNumber)
		return s.getDialplanByID(ctx, failsafeDialplanID.String, nil)
	}

	var user userv1.User
	userQuery := "SELECT id, name, tenant_id, user_type FROM users WHERE id = $1 AND tenant_id = $2"
	err = s.db.QueryRowContext(ctx, userQuery, req.CallerId, tenantID.String).Scan(&user.Id, &user.Name, &user.TenantId, &user.UserType)

	if err != nil {
		if err == sql.ErrNoRows {
			log.Printf("Caller %s is not a registered user for tenant %s. Using guest dialplan.", req.CallerId, tenantID.String)
			return s.getDialplanByID(ctx, "DP_GUEST_ENTRY", nil)
		}
		return nil, status.Errorf(codes.Internal, "database query for user failed: %v", err)
	}

	log.Printf("Caller %s identified as registered user %s", req.CallerId, user.Name)
	return s.getDialplanByID(ctx, activeDialplanID.String, &user)
}

// getDialplanByID fonksiyonu YENİ KONTRAKTA GÖRE GÜNCELLENDİ
func (s *server) getDialplanByID(ctx context.Context, dialplanID string, matchedUser *userv1.User) (*dialplanv1.ResolveDialplanResponse, error) {
	log.Printf("Fetching dialplan details for ID: %s", dialplanID)
	var description, action, tenantID sql.NullString
	var actionDataBytes []byte

	query := "SELECT description, action, action_data, tenant_id FROM dialplans WHERE id = $1"
	err := s.db.QueryRowContext(ctx, query, dialplanID).Scan(&description, &action, &actionDataBytes, &tenantID)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Printf("CRITICAL: Dialplan with ID %s not found. Falling back to system failsafe.", dialplanID)
			if dialplanID == "DP_SYSTEM_FAILSAFE" {
				return nil, status.Error(codes.Internal, "CRITICAL: Failsafe dialplan DP_SYSTEM_FAILSAFE is missing.")
			}
			return s.getDialplanByID(ctx, "DP_SYSTEM_FAILSAFE", matchedUser)
		}
		return nil, status.Errorf(codes.Internal, "database query for dialplan failed: %v", err)
	}

	// --- KRİTİK DEĞİŞİKLİK BURADA ---
	// Artık google.protobuf.Struct değil, kendi map'imizi oluşturuyoruz.
	actionDataMap := make(map[string]string)
	if actionDataBytes != nil {
		// Veritabanındaki JSONB'yi doğrudan map[string]string'e unmarshal ediyoruz.
		if err := json.Unmarshal(actionDataBytes, &actionDataMap); err != nil {
			log.Printf("WARN: Could not unmarshal action_data for dialplan %s: %v", dialplanID, err)
			// Hata durumunda boş bir map ile devam et.
			actionDataMap = make(map[string]string)
		}
	}

	response := &dialplanv1.ResolveDialplanResponse{
		DialplanId: dialplanID,
		TenantId:   tenantID.String,
		Action: &dialplanv1.DialplanAction{
			Action: action.String,
			// Yeni ActionData struct'ını oluşturup, map'i içine koyuyoruz.
			ActionData: &dialplanv1.ActionData{
				Data: actionDataMap,
			},
		},
	}

	if matchedUser != nil {
		response.MatchedUser = matchedUser
	}

	log.Printf("Resolved dialplan: id=%s, action=%s", response.DialplanId, response.Action.Action)
	return response, nil
}

// main fonksiyonu aynı kalabilir, değişiklik yok.
func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL environment variable is not set")
	}

	var db *sql.DB
	var err error
	maxRetries := 5
	for i := 0; i < maxRetries; i++ {
		db, err = sql.Open("pgx", dbURL)
		if err == nil {
			err = db.Ping()
			if err == nil {
				log.Println("Successfully connected to the database")
				break
			}
		}
		if i == maxRetries-1 {
			log.Fatalf("Failed to connect to database after %d attempts: %v", maxRetries, err)
		}
		log.Printf("Failed to connect to database (attempt %d/%d): %v. Retrying in 5 seconds...", i+1, maxRetries, err)
		time.Sleep(5 * time.Second)
	}
	defer db.Close()

	port := os.Getenv("GRPC_PORT")
	if port == "" {
		port = "50054"
	}
	listenAddr := fmt.Sprintf(":%s", port)

	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()
	dialplanv1.RegisterDialplanServiceServer(s, &server{db: db})
	reflection.Register(s)

	log.Printf("gRPC dialplan-service listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
