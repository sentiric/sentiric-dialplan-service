package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"

	// DEĞİŞİKLİK: Artık yerel 'gen' klasörü yerine Go modülü olarak indirilen
	// merkezi kontrat reposundan import ediyoruz. Bu, projenin en önemli standardizasyon adımıdır.
	dialplanv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/dialplan/v1"
	userv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/user/v1"

	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// --- Veri Yapıları ve Mock Veritabanı ---

// Gerçek bir veritabanı yerine geçecek olan hafızadaki yönlendirme kurallarımız
var mockDialplan = map[string][]*dialplanv1.GetDialplanForUserResponse{
	// "1001" user_id'si için bu kural çalışır
	"1001": {
		{
			DialplanId: "dp-internal-default",
			Content:    "<extension name='internal'><condition><action application='bridge' data='user/1002'/></condition></extension>",
			Owner: &userv1.User{
				Id:    "1001",
				Name:  "Alice",
				Email: "alice@sentiric.com",
			},
		},
	},
	// "902124548590" user_id'si için bu kural çalışır
	"902124548590": {
		{
			DialplanId: "dp-main-ivr",
			Content:    "<extension name='main_ivr'><condition><action application='answer'/><action application='playback' data='sounds/welcome.wav'/></condition></extension>",
			Owner: &userv1.User{
				Id:    "902124548590",
				Name:  "Main IVR",
				Email: "ivr@sentiric.com",
			},
		},
	},
}

// --- gRPC Sunucu Implementasyonu ---

// dialplanv1.DialplanServiceServer arayüzünü implemente eden struct
type server struct {
	dialplanv1.UnimplementedDialplanServiceServer
	logger *zap.Logger
}

// GetDialplanForUser RPC'sini implemente eden fonksiyon
func (s *server) GetDialplanForUser(ctx context.Context, req *dialplanv1.GetDialplanForUserRequest) (*dialplanv1.GetDialplanForUserResponse, error) {
	userId := req.GetUserId()
	s.logger.Info("GetDialplanForUser isteği alındı",
		zap.String("user_id", userId),
	)

	// Mock dialplan'de hedefi ara
	// Not: Gerçekte, birden fazla plan olabilir, şimdilik ilkini döndürüyoruz.
	if plans, ok := mockDialplan[userId]; ok && len(plans) > 0 {
		s.logger.Info("Yönlendirme planı bulundu", zap.String("dialplan_id", plans[0].DialplanId))
		return plans[0], nil
	}

	s.logger.Warn("Kullanıcı için yönlendirme planı bulunamadı", zap.String("user_id", userId))
	// Hata yerine boş bir yanıt döndürmek, gRPC'de "Not Found" durumunu yönetmenin bir yoludur.
	// İstemci, boş DialplanId'yi kontrol edebilir.
	return &dialplanv1.GetDialplanForUserResponse{}, nil
}

// --- Ana Fonksiyon ---

func main() {
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("Logger oluşturulamadı: %v", err)
	}
	defer logger.Sync()

	port := os.Getenv("GRPC_PORT")
	if port == "" {
		port = "50054"
	}
	listenAddr := fmt.Sprintf(":%s", port)

	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		logger.Fatal("TCP dinleme başlatılamadı", zap.String("address", listenAddr), zap.Error(err))
	}

	s := grpc.NewServer()
	dialplanv1.RegisterDialplanServiceServer(s, &server{logger: logger})

	logger.Info("gRPC sunucusu dinlemede", zap.String("address", listenAddr))
	if err := s.Serve(lis); err != nil {
		logger.Fatal("Sunucu başlatılamadı", zap.Error(err))
	}
}
