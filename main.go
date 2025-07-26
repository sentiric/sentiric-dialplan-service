package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	_ "github.com/jackc/pgx/v5/stdlib"

	dialplanv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/dialplan/v1"
	userv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/user/v1"
)

// server struct'ına bir veritabanı bağlantısı (DB pool) ekliyoruz.
type server struct {
	dialplanv1.UnimplementedDialplanServiceServer
	db *sql.DB
}

// GetDialplanForUser RPC'si artık mock veri yerine veritabanından okuma yapacak.
func (s *server) GetDialplanForUser(ctx context.Context, req *dialplanv1.GetDialplanForUserRequest) (*dialplanv1.GetDialplanForUserResponse, error) {
	userId := req.GetUserId()
	log.Printf("GetDialplanForUser request received for user ID: %s", userId)

	// Veritabanından dialplan'i ve ilişkili kullanıcıyı (owner) sorgula.
	// Bir kullanıcının birden fazla dialplan'i olabilir, şimdilik ilk bulduğumuzu alıyoruz.
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
	var owner userv1.User // owner bilgisini doldurmak için geçici bir struct

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

	response.Owner = &owner // owner bilgisini yanıta ekle
	log.Printf("Dialplan found: %s for user %s", response.DialplanId, owner.Name)
	return &response, nil
}

func main() {
	// Veritabanı bağlantı bilgisini ortam değişkeninden al.
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL environment variable is not set")
	}

	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("failed to ping database: %v", err)
	}
	log.Println("Successfully connected to the database")

	// gRPC sunucusunu başlat
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

	log.Printf("gRPC server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
