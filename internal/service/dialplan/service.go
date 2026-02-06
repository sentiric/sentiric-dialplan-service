// sentiric-dialplan-service/internal/service/dialplan/service.go
package dialplan

import (
	// ... (imports aynı) ...
	"context"
	"encoding/json"
	"errors"

	"github.com/rs/zerolog"
	dialplanv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/dialplan/v1"
	userv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/user/v1"
	"github.com/sentiric/sentiric-dialplan-service/internal/cache"
	grpchelper "github.com/sentiric/sentiric-dialplan-service/internal/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
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

// Hata değişkenleri errors.go'dan otomatik alınır.

// Service struct'ı, tüm bağımlılıkları tutar (Hata 2'nin çözümü).
type Service struct {
	// Repository Interface'i (Veri Erişimi)
	repo Repository
	// Harici gRPC İstemcileri
	userClient userv1.UserServiceClient
	// Cache
	userCache *cache.UserCache
	// Logger
	log zerolog.Logger
}

func NewService(repo Repository, userClient userv1.UserServiceClient, userCache *cache.UserCache, log zerolog.Logger) *Service {
	// NewService'deki struct literal hatası çözüldü (Hata 2)
	return &Service{repo: repo, userClient: userClient, userCache: userCache, log: log}
}

// -----------------------------------------------------------
// CORE LOGIC: RESOLVE DIALPLAN (s.repo, s.log, s.userCache kullanımları artık geçerli)
// -----------------------------------------------------------

func (s *Service) ResolveDialplan(ctx context.Context, caller, destination string) (*dialplanv1.ResolveDialplanResponse, error) {
	cleanDestination := normalizePhoneNumber(extractUserPart(destination))
	cleanCaller := normalizePhoneNumber(extractUserPart(caller))

	s.log.Info().
		Str("clean_dest", cleanDestination).
		Str("clean_caller", cleanCaller).
		Msg("📞 ResolveDialplan İsteği İşleniyor")

	route, err := s.repo.FindInboundRouteByPhone(ctx, cleanDestination)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			s.log.Warn().Str("destination", cleanDestination).Msg("🚫 Route bulunamadı. Misafir akışına yönlendiriliyor.")
			guestRoute := &dialplanv1.InboundRoute{PhoneNumber: cleanDestination, TenantId: "system", DefaultLanguageCode: "tr"}
			return s.buildFailsafeResponse(ctx, DialplanSystemWelcomeGuest, nil, nil, guestRoute)
		}
		s.log.Error().Err(err).Msg("Inbound route sorgusu başarısız")
		return nil, status.Errorf(codes.Internal, "Route sorgusu başarısız: %v", err)
	}

	if route.IsMaintenanceMode {
		s.log.Info().Str("destination", cleanDestination).Msg("🚧 Hat bakım modunda.")
		return s.buildFailsafeResponse(ctx, safeString(route.FailsafeDialplanId), nil, nil, route)
	}

	var activePlan *dialplanv1.Dialplan
	if route.ActiveDialplanId != nil {
		p, err := s.repo.FindDialplanByID(ctx, *route.ActiveDialplanId)
		if err == nil {
			activePlan = p
			s.log.Info().Str("plan_id", p.Id).Str("action", p.Action.Action).Msg("✅ Aktif Plan Bulundu.")
		}
	}

	// Trace ID Propagation (User Service çağrısı için)
	md, _ := metadata.FromIncomingContext(ctx)
	traceID := "unknown"
	if vals := md.Get("x-trace-id"); len(vals) > 0 {
		traceID = vals[0]
	}
	userReqCtx := metadata.AppendToOutgoingContext(ctx, "x-trace-id", traceID)

	var matchedUser *userv1.User
	var matchedContact *userv1.Contact

	if s.userCache != nil {
		matchedUser, _ = s.userCache.GetUser(ctx, cleanCaller)
	}

	if matchedUser == nil {
		findUserFunc := func(c context.Context, opts ...grpc.CallOption) (*userv1.FindUserByContactResponse, error) {
			return s.userClient.FindUserByContact(c, &userv1.FindUserByContactRequest{
				ContactType: "phone", ContactValue: cleanCaller,
			}, opts...)
		}
		userRes, err := grpchelper.CallWithTimeout(userReqCtx, findUserFunc)
		if err == nil {
			matchedUser = userRes.GetUser()
			if s.userCache != nil && matchedUser != nil {
				_ = s.userCache.SetUser(ctx, cleanCaller, matchedUser)
			}
		} else {
			s.log.Info().Str("caller", cleanCaller).Msg("👤 Arayan sistemde kayıtlı değil (Anonymous).")
		}
	} else {
		s.log.Info().Str("caller", cleanCaller).Msg("✅ Kullanıcı cache'den bulundu")
	}

	if activePlan != nil {
		if matchedUser == nil {
			matchedUser = &userv1.User{Id: "anonymous", Name: toPtr("Misafir Kullanıcı"), TenantId: route.TenantId, UserType: "caller"}
		}

		return &dialplanv1.ResolveDialplanResponse{
			DialplanId: activePlan.Id, TenantId: activePlan.TenantId, Action: activePlan.Action,
			MatchedUser: matchedUser, MatchedContact: matchedContact, InboundRoute: route,
		}, nil
	}

	return s.buildFailsafeResponse(ctx, DialplanSystemWelcomeGuest, matchedUser, matchedContact, route)
}

// -----------------------------------------------------------
// CRUD: INBOUND ROUTES (s.repo ve hata kullanımı düzeltildi)
// -----------------------------------------------------------

func (s *Service) CreateInboundRoute(ctx context.Context, route *dialplanv1.InboundRoute) error {
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
	rowsAffected, err := s.repo.DeleteInboundRoute(ctx, normPhone)
	if err != nil {
		return status.Errorf(codes.Internal, "Inbound route silinemedi: %v", err)
	}
	if rowsAffected == 0 {
		return status.Errorf(codes.NotFound, "Silinecek inbound route bulunamadı: %s", normPhone)
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

// -----------------------------------------------------------
// CRUD: DIALPLANS
// -----------------------------------------------------------

func (s *Service) CreateDialplan(ctx context.Context, req *dialplanv1.CreateDialplanRequest) error {
	dp := req.GetDialplan()
	if dp == nil {
		return status.Error(codes.InvalidArgument, "Dialplan nesnesi boş olamaz")
	}
	actionDataBytes, err := s.serializeActionData(dp)
	if err != nil {
		return err
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

	actionDataBytes, err := s.serializeActionData(dp)
	if err != nil {
		return err
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
	rowsAffected, err := s.repo.DeleteDialplan(ctx, id)
	if err != nil {
		return status.Errorf(codes.Internal, "Dialplan silinemedi: %v", err)
	}
	if rowsAffected == 0 {
		return status.Errorf(codes.NotFound, "Silinecek dialplan bulunamadı: %s", id)
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

// -----------------------------------------------------------
// HELPER FUNCTIONS
// -----------------------------------------------------------

func (s *Service) serializeActionData(dp *dialplanv1.Dialplan) ([]byte, error) {
	if dp.GetAction() != nil && dp.GetAction().GetActionData() != nil {
		data, err := json.Marshal(dp.GetAction().GetActionData().GetData())
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "Geçersiz action_data formatı: %v", err)
		}
		return data, nil
	}
	return []byte("{}"), nil
}

func (s *Service) buildFailsafeResponse(ctx context.Context, planID string, user *userv1.User, contact *userv1.Contact, route *dialplanv1.InboundRoute) (*dialplanv1.ResolveDialplanResponse, error) {
	if planID == "" {
		planID = DialplanSystemFailsafe
	}
	plan, err := s.repo.FindDialplanByID(ctx, planID)
	if err != nil {
		s.log.Error().Err(err).Str("plan_id", planID).Msg("KRİTİK HATA: Failsafe dialplan veritabanından çekilemedi!")
		emergencyPlan := &dialplanv1.DialplanAction{
			Action: ActionPlayAnnouncement,
			ActionData: &dialplanv1.ActionData{
				Data: map[string]string{"announcement_id": AnnouncementSystemError},
			},
		}
		return &dialplanv1.ResolveDialplanResponse{
			DialplanId:     "EMERGENCY_MODE",
			TenantId:       "system",
			Action:         emergencyPlan,
			MatchedUser:    user,
			MatchedContact: contact,
			InboundRoute:   route,
		}, nil
	}
	return &dialplanv1.ResolveDialplanResponse{
		DialplanId: plan.Id, TenantId: plan.TenantId, Action: plan.Action,
		MatchedUser: user, MatchedContact: contact, InboundRoute: route,
	}, nil
}
