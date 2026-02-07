// sentiric-dialplan-service/internal/service/dialplan/service.go
package dialplan

import (
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

const (
	DialplanSystemFailsafe     = "DP_SYSTEM_FAILSAFE"
	DialplanSystemWelcomeGuest = "DP_SYSTEM_WELCOME_GUEST"
	ActionPlayAnnouncement     = "PLAY_ANNOUNCEMENT"
	AnnouncementSystemError    = "ANNOUNCE_SYSTEM_ERROR"
)

type Service struct {
	repo       Repository
	userClient userv1.UserServiceClient
	userCache  *cache.UserCache
	log        zerolog.Logger
}

func NewService(repo Repository, userClient userv1.UserServiceClient, userCache *cache.UserCache, log zerolog.Logger) *Service {
	return &Service{repo: repo, userClient: userClient, userCache: userCache, log: log}
}

func (s *Service) ResolveDialplan(ctx context.Context, caller, destination string) (*dialplanv1.ResolveDialplanResponse, error) {
	cleanDestination := normalizePhoneNumber(extractUserPart(destination))
	cleanCaller := normalizePhoneNumber(extractUserPart(caller))

	s.log.Info().Str("clean_dest", cleanDestination).Str("clean_caller", cleanCaller).Msg("📞 ResolveDialplan İsteği İşleniyor")

	route, err := s.repo.FindInboundRouteByPhone(ctx, cleanDestination)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			s.log.Warn().Str("destination", cleanDestination).Msg("🚫 Route bulunamadı. Misafir akışına yönlendiriliyor.")
			guestRoute := &dialplanv1.InboundRoute{PhoneNumber: cleanDestination, TenantId: "system", DefaultLanguageCode: "tr"}
			return s.buildFailsafeResponse(ctx, DialplanSystemWelcomeGuest, nil, nil, guestRoute)
		}
		return nil, status.Errorf(codes.Internal, "Route sorgusu başarısız: %v", err)
	}

	if route.IsMaintenanceMode {
		return s.buildFailsafeResponse(ctx, safeString(route.FailsafeDialplanId), nil, nil, route)
	}

	var activePlan *dialplanv1.Dialplan
	if route.ActiveDialplanId != nil {
		p, err := s.repo.FindDialplanByID(ctx, *route.ActiveDialplanId)
		if err == nil {
			activePlan = p
		}
	}

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
		}
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

func (s *Service) CreateInboundRoute(ctx context.Context, route *dialplanv1.InboundRoute) error {
	route.PhoneNumber = normalizePhoneNumber(route.PhoneNumber)
	err := s.repo.CreateInboundRoute(ctx, route)
	if err != nil {
		if errors.Is(err, ErrConflict) {
			return status.Errorf(codes.AlreadyExists, "Bu telefon numarası zaten kayıtlı")
		}
		return status.Errorf(codes.Internal, "Hata: %v", err)
	}
	return nil
}

func (s *Service) GetInboundRoute(ctx context.Context, phoneNumber string) (*dialplanv1.InboundRoute, error) {
	normPhone := normalizePhoneNumber(phoneNumber)
	route, err := s.repo.FindInboundRouteByPhone(ctx, normPhone)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "Route yok")
		}
		return nil, status.Errorf(codes.Internal, "Hata: %v", err)
	}
	return route, nil
}

func (s *Service) UpdateInboundRoute(ctx context.Context, route *dialplanv1.InboundRoute) error {
	route.PhoneNumber = normalizePhoneNumber(route.PhoneNumber)
	rows, err := s.repo.UpdateInboundRoute(ctx, route)
	if err != nil || rows == 0 {
		return status.Errorf(codes.NotFound, "Güncellenemedi")
	}
	return nil
}

func (s *Service) DeleteInboundRoute(ctx context.Context, phoneNumber string) error {
	normPhone := normalizePhoneNumber(phoneNumber)
	rows, err := s.repo.DeleteInboundRoute(ctx, normPhone)
	if err != nil || rows == 0 {
		return status.Errorf(codes.NotFound, "Silinemedi")
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
	total, _ := s.repo.CountInboundRoutes(ctx, req.GetTenantId())
	routes, err := s.repo.ListInboundRoutes(ctx, req.GetTenantId(), pageSize, offset)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Hata")
	}
	return &dialplanv1.ListInboundRoutesResponse{Routes: routes, TotalCount: total}, nil
}

func (s *Service) CreateDialplan(ctx context.Context, req *dialplanv1.CreateDialplanRequest) error {
	dp := req.GetDialplan()
	actionDataBytes, err := s.serializeActionData(dp)
	if err != nil {
		return err
	}
	err = s.repo.CreateDialplan(ctx, dp, actionDataBytes)
	if err != nil {
		return status.Errorf(codes.Internal, "Hata")
	}
	return nil
}

func (s *Service) GetDialplan(ctx context.Context, id string) (*dialplanv1.Dialplan, error) {
	dp, err := s.repo.FindDialplanByID(ctx, id)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "Yok")
	}
	return dp, nil
}

func (s *Service) UpdateDialplan(ctx context.Context, req *dialplanv1.UpdateDialplanRequest) error {
	dp := req.GetDialplan()
	actionDataBytes, err := s.serializeActionData(dp)
	if err != nil {
		return err
	}
	rows, err := s.repo.UpdateDialplan(ctx, dp, actionDataBytes)
	if err != nil || rows == 0 {
		return status.Errorf(codes.NotFound, "Hata")
	}
	return nil
}

func (s *Service) DeleteDialplan(ctx context.Context, id string) error {
	rows, err := s.repo.DeleteDialplan(ctx, id)
	if err != nil || rows == 0 {
		return status.Errorf(codes.NotFound, "Hata")
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
	total, _ := s.repo.CountDialplans(ctx, req.GetTenantId())
	dialplans, err := s.repo.ListDialplans(ctx, req.GetTenantId(), pageSize, offset)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Hata")
	}
	return &dialplanv1.ListDialplansResponse{Dialplans: dialplans, TotalCount: total}, nil
}

// [v1.15.0 FIX]: ActionData artık doğrudan bir haritadır.
func (s *Service) serializeActionData(dp *dialplanv1.Dialplan) ([]byte, error) {
	if dp.GetAction() != nil && dp.GetAction().GetActionData() != nil {
		// GetData() silindi, doğrudan map dönüyor.
		data := dp.GetAction().GetActionData()
		bytes, err := json.Marshal(data)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "JSON hatası")
		}
		return bytes, nil
	}
	return []byte("{}"), nil
}

func (s *Service) buildFailsafeResponse(ctx context.Context, planID string, user *userv1.User, contact *userv1.Contact, route *dialplanv1.InboundRoute) (*dialplanv1.ResolveDialplanResponse, error) {
	if planID == "" {
		planID = DialplanSystemFailsafe
	}
	plan, err := s.repo.FindDialplanByID(ctx, planID)
	if err != nil {
		// Emergency plan with v1.15.0 map structure
		emergencyPlan := &dialplanv1.DialplanAction{
			Action:     ActionPlayAnnouncement,
			ActionData: map[string]string{"announcement_id": AnnouncementSystemError},
		}
		return &dialplanv1.ResolveDialplanResponse{
			DialplanId: "EMERGENCY_MODE", TenantId: "system", Action: emergencyPlan,
			MatchedUser: user, MatchedContact: contact, InboundRoute: route,
		}, nil
	}
	return &dialplanv1.ResolveDialplanResponse{
		DialplanId: plan.Id, TenantId: plan.TenantId, Action: plan.Action,
		MatchedUser: user, MatchedContact: contact, InboundRoute: route,
	}, nil
}
