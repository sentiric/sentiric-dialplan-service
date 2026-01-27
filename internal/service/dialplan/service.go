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

	"github.com/rs/zerolog"
	dialplanv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/dialplan/v1"
	userv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/user/v1"
	"github.com/sentiric/sentiric-dialplan-service/internal/cache"
	"github.com/sentiric/sentiric-dialplan-service/internal/config"
	grpchelper "github.com/sentiric/sentiric-dialplan-service/internal/grpc" // Imports package grpchelper
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

type Service struct {
	repo       Repository
	userClient userv1.UserServiceClient
	userCache  *cache.UserCache // ✅ EKLENDİ
	log        zerolog.Logger
}

// ✅ GÜNCELLEME: NewService artık UserCache kabul ediyor
func NewService(repo Repository, userClient userv1.UserServiceClient, userCache *cache.UserCache, log zerolog.Logger) *Service {
	return &Service{repo: repo, userClient: userClient, userCache: userCache, log: log}
}

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

func (s *Service) ResolveDialplan(ctx context.Context, caller, destination string) (*dialplanv1.ResolveDialplanResponse, error) {
	route, err := s.repo.FindInboundRouteByPhone(ctx, destination)
	// ... (route error handling aynı)
	if err != nil {
		if errors.Is(err, ErrTableMissing) {
			s.log.Error().Msg("Kritik Altyapı Hatası: Tablolar eksik.")
			failsafeRoute := &dialplanv1.InboundRoute{TenantId: "system"}
			return s.buildFailsafeResponse(ctx, DialplanSystemFailsafe, nil, nil, failsafeRoute)
		}
		if errors.Is(err, ErrNotFound) {
			s.log.Info().Str("destination", destination).Msg("Route bulunamadı. Misafir planı geçici olarak döndürülüyor.")
			guestRoute := &dialplanv1.InboundRoute{
				PhoneNumber:         destination,
				TenantId:            "system",
				DefaultLanguageCode: "tr",
			}
			return s.buildFailsafeResponse(ctx, DialplanSystemWelcomeGuest, nil, nil, guestRoute)
		}
		s.log.Error().Err(err).Msg("Inbound route sorgusu başarısız")
		return nil, status.Errorf(codes.Internal, "Route sorgusu başarısız: %v", err)
	}

	if route.IsMaintenanceMode {
		s.log.Info().Str("destination", destination).Msg("Hat bakım modunda, failsafe planına yönlendiriliyor.")
		return s.buildFailsafeResponse(ctx, safeString(route.FailsafeDialplanId), nil, nil, route)
	}

	// Trace ID Propagation
	md, _ := metadata.FromIncomingContext(ctx)
	traceIDValues := md.Get("x-trace-id")
	traceID := "unknown"
	if len(traceIDValues) > 0 {
		traceID = traceIDValues[0]
	}
	userReqCtx := metadata.AppendToOutgoingContext(ctx, "x-trace-id", traceID)

	// User Identification
	// ✅ EKLENDİ: Cache Kontrolü
	var matchedUser *userv1.User
	var userErr error

	// 1. Önce Cache'e bak
	if s.userCache != nil {
		matchedUser, userErr = s.userCache.GetUser(ctx, caller)
		if userErr != nil {
			s.log.Warn().Err(userErr).Msg("UserCache okuma hatası (ihmal ediliyor)")
		}
	}

	// 2. Cache miss ise Service'e git
	if matchedUser == nil {
		// ✅ GÜNCELLEME: Timeout Helper Kullanımı
		findUserFunc := func(c context.Context, opts ...grpc.CallOption) (*userv1.FindUserByContactResponse, error) {
			return s.userClient.FindUserByContact(c, &userv1.FindUserByContactRequest{
				ContactType:  "phone",
				ContactValue: caller,
			}, opts...)
		}

		userRes, err := grpchelper.CallWithTimeout(userReqCtx, findUserFunc)
		if err != nil {
			st, _ := status.FromError(err)
			if st.Code() == codes.NotFound {
				s.log.Info().Str("caller", caller).Msg("Arayan bulunamadı, misafir planına yönlendiriliyor.")
				return s.buildFailsafeResponse(ctx, DialplanSystemWelcomeGuest, nil, nil, route)
			}
			s.log.Error().Err(err).Msg("User service ile iletişim kurulamadı, failsafe planına yönlendiriliyor.")
			return s.buildFailsafeResponse(ctx, safeString(route.FailsafeDialplanId), nil, nil, route)
		}
		matchedUser = userRes.GetUser()

		// 3. Cache'e yaz
		if s.userCache != nil && matchedUser != nil {
			if err := s.userCache.SetUser(ctx, caller, matchedUser); err != nil {
				s.log.Warn().Err(err).Msg("UserCache yazma hatası")
			}
		}
	} else {
		s.log.Info().Str("caller", caller).Msg("✅ Kullanıcı cache'den bulundu")
	}

	var matchedContact *userv1.Contact
	if matchedUser != nil {
		for _, c := range matchedUser.Contacts {
			if c.ContactValue == caller {
				matchedContact = c
				break
			}
		}
	}

	s.log.Info().Str("user_id", matchedUser.Id).Msg("Kullanıcı bulundu, aktif plana yönlendiriliyor.")
	plan, err := s.repo.FindDialplanByID(ctx, safeString(route.ActiveDialplanId))
	if err != nil {
		s.log.Error().Err(err).Str("plan_id", safeString(route.ActiveDialplanId)).Msg("Aktif plan bulunamadı, failsafe tetikleniyor.")
		return s.buildFailsafeResponse(ctx, safeString(route.FailsafeDialplanId), matchedUser, matchedContact, route)
	}

	return &dialplanv1.ResolveDialplanResponse{
		DialplanId:     plan.Id,
		TenantId:       plan.TenantId,
		Action:         plan.Action,
		MatchedUser:    matchedUser,
		MatchedContact: matchedContact,
		InboundRoute:   route,
	}, nil
}

func (s *Service) CreateInboundRoute(ctx context.Context, route *dialplanv1.InboundRoute) error {
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
	route, err := s.repo.FindInboundRouteByPhone(ctx, phoneNumber)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "Inbound route bulunamadı: %s", phoneNumber)
		}
		return nil, status.Errorf(codes.Internal, "Inbound route alınamadı: %v", err)
	}
	return route, nil
}

func (s *Service) UpdateInboundRoute(ctx context.Context, route *dialplanv1.InboundRoute) error {
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
	_, err := s.repo.DeleteInboundRoute(ctx, phoneNumber)
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

func (s *Service) CreateDialplan(ctx context.Context, req *dialplanv1.CreateDialplanRequest) error {
	dp := req.GetDialplan()
	if dp == nil {
		return status.Error(codes.InvalidArgument, "Dialplan nesnesi boş olamaz")
	}
	actionDataBytes, err := json.Marshal(dp.GetAction().GetActionData().GetData())
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "Geçersiz action_data: %v", err)
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
	actionDataBytes, err := json.Marshal(dp.GetAction().GetActionData().GetData())
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "Geçersiz action_data: %v", err)
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

func (s *Service) buildFailsafeResponse(ctx context.Context, planID string, user *userv1.User, contact *userv1.Contact, route *dialplanv1.InboundRoute) (*dialplanv1.ResolveDialplanResponse, error) {
	if planID == "" {
		planID = DialplanSystemFailsafe
	}
	plan, err := s.repo.FindDialplanByID(ctx, planID)
	if err != nil {
		s.log.Error().Err(err).Str("plan_id", planID).Msg("KRİTİK HATA: Failsafe dialplan veritabanından çekilemedi!")

		// ULTIMATE FAILSAFE: Veritabanı yoksa bile cevap ver.
		// Bu yapı hardcoded olarak bellekte yaşar, DB gerektirmez.
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
