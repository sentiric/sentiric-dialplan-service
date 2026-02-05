// sentiric-dialplan-service/internal/service/dialplan/service.go
package dialplan

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode"

	"github.com/rs/zerolog"
	dialplanv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/dialplan/v1"
	userv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/user/v1"
	"github.com/sentiric/sentiric-dialplan-service/internal/cache"
	"github.com/sentiric/sentiric-dialplan-service/internal/config"
	grpchelper "github.com/sentiric/sentiric-dialplan-service/internal/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Consts for Magic Strings
const (
	DialplanSystemFailsafe     = "DP_SYSTEM_FAILSAFE"
	DialplanSystemWelcomeGuest = "DP_SYSTEM_WELCOME_GUEST"
	ActionPlayAnnouncement     = "PLAY_ANNOUNCEMENT"
	AnnouncementSystemError    = "ANNOUNCE_SYSTEM_ERROR"
)

// Repository Interface
type Repository interface {
	FindInboundRouteByPhone(ctx context.Context, phoneNumber string) (*dialplanv1.InboundRoute, error)
	CreateInboundRoute(ctx context.Context, route *dialplanv1.InboundRoute) error
	UpdateInboundRoute(ctx context.Context, route *dialplanv1.InboundRoute) (int64, error)
	DeleteInboundRoute(ctx context.Context, phoneNumber string) (int64, error)
	ListInboundRoutes(ctx context.Context, tenantID string, pageSize, offset int32) ([]*dialplanv1.InboundRoute, error)
	CountInboundRoutes(ctx context.Context, tenantID string) (int32, error)

	FindDialplanByID(ctx context.Context, id string) (*dialplanv1.Dialplan, error)
	CreateDialplan(ctx context.Context, dp *dialplanv1.Dialplan, actionDataBytes []byte) error
	UpdateDialplan(ctx context.Context, dp *dialplanv1.Dialplan, actionDataBytes []byte) (int64, error)
	DeleteDialplan(ctx context.Context, id string) (int64, error)
	ListDialplans(ctx context.Context, tenantID string, pageSize, offset int32) ([]*dialplanv1.Dialplan, error)
	CountDialplans(ctx context.Context, tenantID string) (int32, error)
}

// Service Struct
type Service struct {
	repo       Repository
	userClient userv1.UserServiceClient
	userCache  *cache.UserCache
	log        zerolog.Logger
}

// NewService Constructor
func NewService(repo Repository, userClient userv1.UserServiceClient, userCache *cache.UserCache, log zerolog.Logger) *Service {
	return &Service{repo: repo, userClient: userClient, userCache: userCache, log: log}
}

// Helper: NewUserServiceClient
func NewUserServiceClient(targetURL string, cfg config.Config) (userv1.UserServiceClient, *grpc.ClientConn, error) {
	clientCert, err := tls.LoadX509KeyPair(cfg.TLS.CertPath, cfg.TLS.KeyPath)
	if err != nil {
		return nil, nil, fmt.Errorf("istemci sertifikası yüklenemedi: %w", err)
	}
	caCert, err := os.ReadFile(cfg.TLS.CaPath)
	if err != nil {
		return nil, nil, fmt.Errorf("CA sertifikası okunamadı: %w", err)
	}
	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, nil, fmt.Errorf("CA sertifikası havuza eklenemedi")
	}

	cleanTarget := targetURL
	if strings.Contains(targetURL, "://") {
		parts := strings.Split(targetURL, "://")
		if len(parts) > 1 {
			cleanTarget = parts[1]
		}
	}

	serverName := strings.Split(cleanTarget, ":")[0]

	creds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      caCertPool,
		ServerName:   serverName,
	})

	conn, err := grpc.NewClient(cleanTarget, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, nil, fmt.Errorf("user-service'e bağlanılamadı: %w", err)
	}
	return userv1.NewUserServiceClient(conn), conn, nil
}

// --- CORE LOGIC: RESOLVE DIALPLAN ---

func (s *Service) ResolveDialplan(ctx context.Context, caller, destination string) (*dialplanv1.ResolveDialplanResponse, error) {
	// [ARCHITECTURAL DECISION: UES 1.0] EDGE SANITIZATION
	// 1. Hedef Numarayı Temizle (Route bulmak için)
	rawDestination := extractUserPart(destination)
	cleanDestination := normalizePhoneNumber(rawDestination)

	// 2. Arayan Numarayı Temizle (User bulmak için)
	rawCaller := extractUserPart(caller)
	cleanCaller := normalizePhoneNumber(rawCaller)

	s.log.Info().
		Str("raw_dest", destination).
		Str("clean_dest", cleanDestination).
		Str("raw_caller", caller).
		Str("clean_caller", cleanCaller).
		Msg("📞 ResolveDialplan İsteği İşleniyor (Sanitized)")

	// 3. Veritabanından Rotayı Bul
	route, err := s.repo.FindInboundRouteByPhone(ctx, cleanDestination)
	if err != nil {
		if errors.Is(err, ErrTableMissing) {
			s.log.Error().Msg("🚨 Kritik Altyapı Hatası: Tablolar eksik.")
			failsafeRoute := &dialplanv1.InboundRoute{TenantId: "system"}
			return s.buildFailsafeResponse(ctx, DialplanSystemFailsafe, nil, nil, failsafeRoute)
		}
		if errors.Is(err, ErrNotFound) {
			s.log.Warn().Str("destination", cleanDestination).Msg("🚫 Route bulunamadı. Varsayılan Misafir (Guest) akışına yönlendiriliyor.")

			// Bilinmeyen numaralar için varsayılan bir route oluştur (Sanal)
			guestRoute := &dialplanv1.InboundRoute{
				PhoneNumber:         cleanDestination,
				TenantId:            "system",
				DefaultLanguageCode: "tr",
			}
			// Route yoksa doğrudan Misafir Karşılamaya git
			return s.buildFailsafeResponse(ctx, DialplanSystemWelcomeGuest, nil, nil, guestRoute)
		}
		s.log.Error().Err(err).Msg("Inbound route sorgusu başarısız")
		return nil, status.Errorf(codes.Internal, "Route sorgusu başarısız: %v", err)
	}

	// 4. Bakım Modu Kontrolü
	if route.IsMaintenanceMode {
		s.log.Info().Str("destination", cleanDestination).Msg("🚧 Hat bakım modunda. Failsafe planı devreye giriyor.")
		return s.buildFailsafeResponse(ctx, safeString(route.FailsafeDialplanId), nil, nil, route)
	}

	// 5. Öncelikli Plan Kontrolü (Public Service Check)
	// Eğer numaraya atanmış açık bir "Aktif Plan" varsa (örn: Echo Test, IVR),
	// kullanıcıyı tanımamıza gerek yoktur. Önce planı yükleyelim.
	var activePlan *dialplanv1.Dialplan
	if route.ActiveDialplanId != nil {
		p, err := s.repo.FindDialplanByID(ctx, *route.ActiveDialplanId)
		if err == nil {
			activePlan = p
			s.log.Info().Str("plan_id", p.Id).Str("action", p.Action.Action).Msg("✅ Aktif Plan Bulundu.")
		}
	}

	// 6. Kullanıcı Tanıma (User Identification)
	// Trace ID'yi taşı
	md, _ := metadata.FromIncomingContext(ctx)
	traceIDValues := md.Get("x-trace-id")
	traceID := "unknown"
	if len(traceIDValues) > 0 {
		traceID = traceIDValues[0]
	}
	userReqCtx := metadata.AppendToOutgoingContext(ctx, "x-trace-id", traceID)

	var matchedUser *userv1.User
	var matchedContact *userv1.Contact
	var userErr error

	// A. Cache Kontrolü (Temizlenmiş numara ile)
	if s.userCache != nil {
		matchedUser, userErr = s.userCache.GetUser(ctx, cleanCaller)
		if userErr != nil {
			s.log.Warn().Err(userErr).Msg("UserCache okuma hatası (ihmal ediliyor)")
		}
	}

	// B. User Service Çağrısı (Cache Miss ise)
	if matchedUser == nil {
		findUserFunc := func(c context.Context, opts ...grpc.CallOption) (*userv1.FindUserByContactResponse, error) {
			return s.userClient.FindUserByContact(c, &userv1.FindUserByContactRequest{
				ContactType: "phone",
				// BURASI KRİTİK: Temizlenmiş 'cleanCaller' gönderiyoruz.
				ContactValue: cleanCaller,
			}, opts...)
		}

		userRes, err := grpchelper.CallWithTimeout(userReqCtx, findUserFunc)
		if err != nil {
			st, _ := status.FromError(err)
			if st.Code() == codes.NotFound {
				s.log.Info().Str("caller", cleanCaller).Msg("👤 Arayan sistemde kayıtlı değil (Anonymous).")
			} else {
				s.log.Error().Err(err).Msg("User service erişim hatası.")
			}
		} else {
			matchedUser = userRes.GetUser()
			// Cache'e yaz
			if s.userCache != nil && matchedUser != nil {
				_ = s.userCache.SetUser(ctx, cleanCaller, matchedUser)
			}
		}
	} else {
		s.log.Info().Str("caller", cleanCaller).Msg("✅ Kullanıcı cache'den bulundu")
	}

	// 7. Karar Mantığı (Decision Matrix)

	// DURUM A: Hedef numara için özel bir plan (Active Plan) VAR.
	// Örn: 9999 (Echo), 8001 (Demo Bot).
	if activePlan != nil {
		if matchedUser == nil {
			// Kullanıcı yoksa sahte bir "Misafir" kullanıcı oluştur ki akış bozulmasın.
			matchedUser = &userv1.User{
				Id:       "anonymous",
				Name:     toPtr("Misafir Kullanıcı"),
				TenantId: route.TenantId,
				UserType: "caller",
			}
			s.log.Info().Msg("Genel servis (Public Service) için geçici kullanıcı atandı.")
		}

		return &dialplanv1.ResolveDialplanResponse{
			DialplanId:     activePlan.Id,
			TenantId:       activePlan.TenantId,
			Action:         activePlan.Action,
			MatchedUser:    matchedUser,
			MatchedContact: matchedContact,
			InboundRoute:   route,
		}, nil
	}

	// DURUM B: Hedef numara için özel plan YOK ama kullanıcı TANINIYOR.
	if matchedUser != nil {
		s.log.Info().Str("user_id", matchedUser.Id).Msg("Kullanıcı tanındı ama özel rota yok, varsayılan AI sohbetine yönlendiriliyor.")
		// Şimdilik sistem varsayılanına yönlendiriyoruz.
		return s.buildFailsafeResponse(ctx, "DP_DEMO_MAIN_ENTRY", matchedUser, matchedContact, route)
	}

	// DURUM C: Ne plan var ne de kullanıcı tanınıyor.
	s.log.Info().Msg("Ne rota ne de kullanıcı eşleşti. Misafir akışı başlatılıyor.")
	return s.buildFailsafeResponse(ctx, DialplanSystemWelcomeGuest, nil, nil, route)
}

// --- CRUD: INBOUND ROUTES ---

func (s *Service) CreateInboundRoute(ctx context.Context, route *dialplanv1.InboundRoute) error {
	// Normalizasyon (Write Path)
	route.PhoneNumber = normalizePhoneNumber(route.PhoneNumber)
	err := s.repo.CreateInboundRoute(ctx, route)
	if err != nil {
		if errors.Is(err, ErrConflict) {
			return status.Errorf(codes.AlreadyExists, "Bu telefon numarası zaten kayıtlı: %s", route.PhoneNumber)
		}
		return status.Errorf(codes.Internal, "Inbound route oluşturulamadı: %v", err)
	}
	return nil
}

func (s *Service) GetInboundRoute(ctx context.Context, phoneNumber string) (*dialplanv1.InboundRoute, error) {
	normPhone := normalizePhoneNumber(phoneNumber)
	route, err := s.repo.FindInboundRouteByPhone(ctx, normPhone)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "Inbound route bulunamadı: %s", normPhone)
		}
		return nil, status.Errorf(codes.Internal, "Inbound route alınamadı: %v", err)
	}
	return route, nil
}

func (s *Service) UpdateInboundRoute(ctx context.Context, route *dialplanv1.InboundRoute) error {
	route.PhoneNumber = normalizePhoneNumber(route.PhoneNumber)
	rowsAffected, err := s.repo.UpdateInboundRoute(ctx, route)
	if err != nil {
		return status.Errorf(codes.Internal, "Inbound route güncellenemedi: %v", err)
	}
	if rowsAffected == 0 {
		return status.Errorf(codes.NotFound, "Güncellenecek inbound route bulunamadı: %s", route.PhoneNumber)
	}
	return nil
}

func (s *Service) DeleteInboundRoute(ctx context.Context, phoneNumber string) error {
	normPhone := normalizePhoneNumber(phoneNumber)
	_, err := s.repo.DeleteInboundRoute(ctx, normPhone)
	if err != nil {
		return status.Errorf(codes.Internal, "Inbound route silinemedi: %v", err)
	}
	return nil
}

func (s *Service) ListInboundRoutes(ctx context.Context, req *dialplanv1.ListInboundRoutesRequest) (*dialplanv1.ListInboundRoutesResponse, error) {
	page := req.GetPage()
	if page < 1 {
		page = 1
	}
	pageSize := req.GetPageSize()
	if pageSize < 1 {
		pageSize = 10
	}
	offset := (page - 1) * pageSize
	total, err := s.repo.CountInboundRoutes(ctx, req.GetTenantId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Route sayısı alınamadı: %v", err)
	}
	routes, err := s.repo.ListInboundRoutes(ctx, req.GetTenantId(), pageSize, offset)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Route'lar listelenemedi: %v", err)
	}
	return &dialplanv1.ListInboundRoutesResponse{Routes: routes, TotalCount: total}, nil
}

// --- CRUD: DIALPLANS ---

func (s *Service) CreateDialplan(ctx context.Context, req *dialplanv1.CreateDialplanRequest) error {
	dp := req.GetDialplan()
	if dp == nil {
		return status.Error(codes.InvalidArgument, "Dialplan nesnesi boş olamaz")
	}
	var actionDataBytes []byte
	var err error
	if dp.GetAction() != nil && dp.GetAction().GetActionData() != nil {
		actionDataBytes, err = json.Marshal(dp.GetAction().GetActionData().GetData())
		if err != nil {
			return status.Errorf(codes.InvalidArgument, "Geçersiz action_data formatı: %v", err)
		}
	} else {
		actionDataBytes = []byte("{}")
	}

	err = s.repo.CreateDialplan(ctx, dp, actionDataBytes)
	if err != nil {
		if errors.Is(err, ErrConflict) {
			return status.Errorf(codes.AlreadyExists, "Bu dialplan ID zaten kayıtlı: %s", dp.Id)
		}
		return status.Errorf(codes.Internal, "Dialplan oluşturulamadı: %v", err)
	}
	return nil
}

func (s *Service) GetDialplan(ctx context.Context, id string) (*dialplanv1.Dialplan, error) {
	dp, err := s.repo.FindDialplanByID(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "Dialplan bulunamadı: %s", id)
		}
		return nil, status.Errorf(codes.Internal, "Dialplan alınamadı: %v", err)
	}
	return dp, nil
}

func (s *Service) UpdateDialplan(ctx context.Context, req *dialplanv1.UpdateDialplanRequest) error {
	dp := req.GetDialplan()
	if dp == nil {
		return status.Error(codes.InvalidArgument, "Dialplan nesnesi boş olamaz")
	}

	var actionDataBytes []byte
	var err error
	if dp.GetAction() != nil && dp.GetAction().GetActionData() != nil {
		actionDataBytes, err = json.Marshal(dp.GetAction().GetActionData().GetData())
		if err != nil {
			return status.Errorf(codes.InvalidArgument, "Geçersiz action_data: %v", err)
		}
	} else {
		actionDataBytes = []byte("{}")
	}

	rowsAffected, err := s.repo.UpdateDialplan(ctx, dp, actionDataBytes)
	if err != nil {
		return status.Errorf(codes.Internal, "Dialplan güncellenemedi: %v", err)
	}
	if rowsAffected == 0 {
		return status.Errorf(codes.NotFound, "Güncellenecek dialplan bulunamadı: %s", dp.Id)
	}
	return nil
}

func (s *Service) DeleteDialplan(ctx context.Context, id string) error {
	_, err := s.repo.DeleteDialplan(ctx, id)
	if err != nil {
		return status.Errorf(codes.Internal, "Dialplan silinemedi: %v", err)
	}
	return nil
}

func (s *Service) ListDialplans(ctx context.Context, req *dialplanv1.ListDialplansRequest) (*dialplanv1.ListDialplansResponse, error) {
	page := req.GetPage()
	if page < 1 {
		page = 1
	}
	pageSize := req.GetPageSize()
	if pageSize < 1 {
		pageSize = 10
	}
	offset := (page - 1) * pageSize
	total, err := s.repo.CountDialplans(ctx, req.GetTenantId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Dialplan sayısı alınamadı: %v", err)
	}
	dialplans, err := s.repo.ListDialplans(ctx, req.GetTenantId(), pageSize, offset)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Dialplan'lar listelenemedi: %v", err)
	}
	return &dialplanv1.ListDialplansResponse{Dialplans: dialplans, TotalCount: total}, nil
}

// --- HELPER FUNCTIONS ---

func (s *Service) buildFailsafeResponse(ctx context.Context, planID string, user *userv1.User, contact *userv1.Contact, route *dialplanv1.InboundRoute) (*dialplanv1.ResolveDialplanResponse, error) {
	if planID == "" {
		planID = DialplanSystemFailsafe
	}
	plan, err := s.repo.FindDialplanByID(ctx, planID)
	if err != nil {
		s.log.Error().Err(err).Str("plan_id", planID).Msg("KRİTİK HATA: Failsafe dialplan veritabanından çekilemedi!")

		// Veritabanı bile çöktüyse statik bir acil durum planı dön
		emergencyPlan := &dialplanv1.DialplanAction{
			Action: ActionPlayAnnouncement,
			ActionData: &dialplanv1.ActionData{
				Data: map[string]string{"announcement_id": AnnouncementSystemError},
			},
		}

		return &dialplanv1.ResolveDialplanResponse{
			DialplanId:   "EMERGENCY_MODE",
			TenantId:     "system",
			Action:       emergencyPlan,
			InboundRoute: route,
		}, nil
	}
	return &dialplanv1.ResolveDialplanResponse{
		DialplanId: plan.Id, TenantId: plan.TenantId, Action: plan.Action,
		MatchedUser: user, MatchedContact: contact, InboundRoute: route,
	}, nil
}

func safeString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func toPtr(s string) *string {
	return &s
}

// extractUserPart: SIP URI'den kullanıcı kısmını çıkarır (örn: sip:1001@domain -> 1001)
func extractUserPart(uri string) string {
	clean := uri
	// "sip:" veya "sips:" öneki varsa kaldır
	if strings.HasPrefix(clean, "sip:") {
		clean = clean[4:]
	} else if strings.HasPrefix(clean, "sips:") {
		clean = clean[5:]
	}

	// "@" varsa öncesini al
	if idx := strings.Index(clean, "@"); idx != -1 {
		clean = clean[:idx]
	}

	// ":" varsa (port) ve @ yoksa, temizle (örn: 1001:5060)
	// Dikkat: @'den sonraki port zaten yukarıda atıldı.
	// Bu durum sadece saf "1001:5060" gibi durumlarda geçerli.
	if idx := strings.Index(clean, ":"); idx != -1 {
		clean = clean[:idx]
	}

	return clean
}

// normalizePhoneNumber: Telefon numarasını veritabanı formatına (genellikle 90...) çevirir.
// "+90555..." -> "90555..."
// "0555..." -> "90555..." (Varsayım: Türkiye)
// "555..." -> "90555..." (Varsayım: Türkiye)
func normalizePhoneNumber(phone string) string {
	// Sadece rakamları al
	var sb strings.Builder
	for _, ch := range phone {
		if unicode.IsDigit(ch) {
			sb.WriteRune(ch)
		}
	}
	cleaned := sb.String()

	// Eğer boşsa olduğu gibi dön (Hata üst katmanda yakalanır)
	if cleaned == "" {
		return phone
	}

	// 1. Durum: 90 ile başlıyorsa (12 hane) -> Tamam
	if len(cleaned) == 12 && strings.HasPrefix(cleaned, "90") {
		return cleaned
	}

	// 2. Durum: 0 ile başlıyorsa (11 hane - 0555...) -> 90 ekle, 0'ı at
	if len(cleaned) == 11 && strings.HasPrefix(cleaned, "0") {
		return "90" + cleaned[1:]
	}

	// 3. Durum: 10 hane (555...) -> Başına 90 ekle
	if len(cleaned) == 10 {
		return "90" + cleaned
	}

	// Diğer durumlar (Örn: Kısa numara 9999, 1001) -> Dokunma
	return cleaned
}
