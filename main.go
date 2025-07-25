package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"regexp"

	// Projemizin içine kopyaladığımız üretilmiş gRPC kodunu import ediyoruz
	dialplanv1 "github.com/sentiric/sentiric-dialplan-service/gen/dialplan/v1"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// --- Veri Yapıları ve Mock Veritabanı ---

// Gerçek bir veritabanı yerine geçecek olan hafızadaki yönlendirme kurallarımız
var mockDialplan = map[string][]*dialplanv1.GetDialplanResponse_Action{
	// Aranan numara "902124548590" ise bu kurallar çalışır
	"902124548590": {
		{
			Type: dialplanv1.GetDialplanResponse_Action_ROUTE_TO_AGENT,
			Parameters: map[string]string{
				"initial_prompt": "Merhaba, Sentiric'e hoş geldiniz. Size nasıl yardımcı olabilirim?",
			},
		},
	},
	// Aranan numara "1001" (dahili bir operatör) ise bu kural çalışır
	"1001": {
		{
			Type: dialplanv1.GetDialplanResponse_Action_ROUTE_TO_AGENT,
			Parameters: map[string]string{
				"target_agent_id": "op-alice-456",
			},
		},
	},
	// Aranan numara "911" ise çağrıyı reddet
	"911": {
		{
			Type: dialplanv1.GetDialplanResponse_Action_REJECT,
			Parameters: map[string]string{
				"reason": "Emergency services not supported",
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

// GetDialplan RPC'sini implemente eden fonksiyon
func (s *server) GetDialplan(ctx context.Context, req *dialplanv1.GetDialplanRequest) (*dialplanv1.GetDialplanResponse, error) {
	// SIP URI'sinden aranan numarayı ayıklayalım
	re := regexp.MustCompile(`sip:([^@;]+)`)
	matches := re.FindStringSubmatch(req.GetToUri())

	if len(matches) < 2 {
		s.logger.Warn("Geçersiz To URI formatı", zap.String("uri", req.GetToUri()))
		// Boş bir plan dönebiliriz, bu da arayan için "Not Found" anlamına gelir
		return &dialplanv1.GetDialplanResponse{Actions: []*dialplanv1.GetDialplanResponse_Action{}}, nil
	}
	destination := matches[1]

	s.logger.Info("GetDialplan isteği alındı",
		zap.String("destination", destination),
		zap.String("from", req.GetFromUri()),
	)

	// Mock dialplan'de hedefi ara
	if actions, ok := mockDialplan[destination]; ok {
		s.logger.Info("Yönlendirme planı bulundu", zap.Int("action_count", len(actions)))
		return &dialplanv1.GetDialplanResponse{Actions: actions}, nil
	}

	s.logger.Warn("Hedef için yönlendirme planı bulunamadı", zap.String("destination", destination))
	return &dialplanv1.GetDialplanResponse{Actions: []*dialplanv1.GetDialplanResponse_Action{}}, nil
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
