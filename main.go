package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net"
	"os"
	"time" // Tekrar deneme mantığı için eklendi

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	_ "github.com/jackc/pgx/v5/stdlib"

	dialplanv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/dialplan/v1"
	userv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/user/v1"
)

type server struct {
	dialplanv1.UnimplementedDialplanServiceServer
	db *sql.DB
}

func (s *server) GetDialplanForUser(ctx context.Context, req *dialplanv1.GetDialplanForUserRequest) (*dialplanv1.GetDialplanForUserResponse, error) {
	userId := req.GetUserId()
	log.Printf("GetDialplanForUser request received for user ID: %s", userId)

	query := `
		SELECT
			d.dialplan_id, d.content,
			u.id, u.name, u.email, u.tenant_id
		FROM dialplans d
		JOIN users u ON d.user_id = u.id
		WHERE d.user_id = $1
		LIMIT 1
	`
	row := s.db.QueryRowContext(ctx, query, userId)

	var response dialplanv1.GetDialplanForUserResponse
	var owner userv1.User

	err := row.Scan(
		&response.DialplanId, &response.Content,
		&owner.Id, &owner.Name, &owner.Email, &owner.TenantId,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Printf("Dialplan not found for user ID: %s", userId)
			return nil, status.Errorf(codes.NotFound, "dialplan for user ID '%s' not found", userId)
		}
		log.Printf("Database query failed: %v", err)
		return nil, status.Errorf(codes.Internal, "database query failed: %v", err)
	}

	response.Owner = &owner
	log.Printf("Dialplan found: %s for user %s", response.DialplanId, owner.Name)
	return &response, nil
}

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
	// Bu satır, 'server' struct'ını ve metodlarını 'kullanılır' hale getirir.
	dialplanv1.RegisterDialplanServiceServer(s, &server{db: db})
	reflection.Register(s)

	log.Printf("gRPC server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
