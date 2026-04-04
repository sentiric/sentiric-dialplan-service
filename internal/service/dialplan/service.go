// Dosya: sentiric-dialplan-service/internal/service/dialplan/service.go
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
	"github.com/sentiric/sentiric-dialplan-service/internal/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	DialplanSystemFailsafe     = "DP_SYSTEM_FAILSAFE"
	DialplanSystemWelcomeGuest = "DP_SYSTEM_AI_GUEST"
	ActionPlayAnnouncement     = "PLAY_ANNOUNCEMENT"
	AnnouncementSystemError    = "ANNOUNCE_SYSTEM_ERROR"
	NilUUID                    = "00000000-0000-0000-0000-000000000000"
)

type Service struct {
	repo       Repository
	userClient userv1.UserServiceClient
	userCache  *cache.UserCache
	baseLog    zerolog.Logger
}

func NewService(repo Repository, userClient userv1.UserServiceClient, userCache *cache.UserCache, log zerolog.Logger) *Service {
	return &Service{repo: repo, userClient: userClient, userCache: userCache, baseLog: log}
}

func (s *Service) ResolveDialplan(ctx context.Context, caller, destination string) (*dialplanv1.ResolveDialplanResponse, error) {
	l := logger.ContextLogger(ctx, s.baseLog)
	traceID := logger.ExtractTraceIDFromContext(ctx)

	cleanDestination := normalizePhoneNumber(extractUserPart(destination))
	cleanCaller := normalizePhoneNumber(extractUserPart(caller))

	//[ARCH-COMPLIANCE] Dinamik Kontak Tipi Belirleme (Extension vs Phone)
	contactType := "phone"
	if len(cleanCaller) <= 5 && cleanCaller != "anonymous" {
		contactType = "extension"
	}

	l.Info().
		Str("event", logger.EventDialplanResolveStart).
		Dict("attributes", zerolog.Dict().
			Str("sip.caller", cleanCaller).
			Str("contact_type", contactType).
			Str("sip.destination", cleanDestination)).
		Msg("📞 ResolveDialplan İsteği İşleniyor")

	route, err := s.repo.FindInboundRouteByPhone(ctx, cleanDestination)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			l.Warn().
				Str("event", logger.EventRouteNotFound).
				Dict("attributes", zerolog.Dict().
					Str("sip.destination", cleanDestination)).
				Msg("🚫 Route bulunamadı. Misafir akışına yönlendiriliyor.")

			guestRoute := &dialplanv1.InboundRoute{
				PhoneNumber:         cleanDestination,
				TenantId:            "system",
				DefaultLanguageCode: "tr",
			}
			return s.buildFailsafeResponse(ctx, l, DialplanSystemWelcomeGuest, nil, nil, guestRoute)
		}

		l.Error().Err(err).
			Str("event", logger.EventRouteQueryFailed).
			Msg("Route sorgusu başarısız")
		return nil, status.Errorf(codes.Internal, "Route sorgusu başarısız: %v", err)
	}

	if route.BlockAnonymous && (cleanCaller == "" || cleanCaller == "anonymous") {
		l.Warn().Str("event", logger.EventAnonymousBlocked).Msg("🚫 Gizli numara engellendi.")
		return nil, status.Errorf(codes.PermissionDenied, "Anonymous calls blocked")
	}

	if route.IsMaintenanceMode {
		l.Warn().Str("event", logger.EventMaintenanceMode).Msg("🔧 Hat bakım modunda.")
		return s.buildFailsafeResponse(ctx, l, safeString(route.FailsafeDialplanId), nil, nil, route)
	}

	targetDialplanID := route.ActiveDialplanId

	if route.ScheduleId != nil && *route.ScheduleId != "" {
		schedule, err := s.repo.GetSchedule(ctx, *route.ScheduleId)
		if err == nil {
			//[ARCH-COMPLIANCE] Context Logger geçirildi
			isOpen := IsWorkingHour(schedule.ScheduleJson, l)

			if !isOpen {
				l.Info().
					Str("event", logger.EventOffHoursActive).
					Str("schedule", schedule.Name).
					Msg("🌙 Mesai dışı (Off-Hours) kuralı devrede.")
				if route.OffHoursDialplanId != nil && *route.OffHoursDialplanId != "" {
					targetDialplanID = route.OffHoursDialplanId
				}
			} else {
				l.Debug().
					Str("event", logger.EventWorkingHoursActive).
					Str("schedule", schedule.Name).
					Msg("☀️ Mesai içi (Working-Hours) kuralı devrede.")
			}
		} else {
			l.Warn().Err(err).
				Str("event", logger.EventScheduleLoadFailed).
				Str("schedule_id", *route.ScheduleId).
				Msg("Zamanlama planı yüklenemedi, varsayılan akışa devam ediliyor.")
		}
	}

	var activePlan *dialplanv1.Dialplan
	if targetDialplanID != nil {
		if p, err := s.repo.FindDialplanByID(ctx, *targetDialplanID); err == nil {
			activePlan = p
		}
	}

	userReqCtx := metadata.AppendToOutgoingContext(ctx, "x-trace-id", traceID)
	var matchedUser *userv1.User
	var matchedContact *userv1.Contact

	if s.userCache != nil {
		matchedUser, _ = s.userCache.GetUser(ctx, cleanCaller, l)
	}

	if matchedUser == nil {
		l.Debug().Str("event", logger.EventUserCacheMiss).Msg("Cache miss, User Service sorgulanıyor")
		findUserFunc := func(c context.Context, opts ...grpc.CallOption) (*userv1.FindUserByContactResponse, error) {
			return s.userClient.FindUserByContact(c, &userv1.FindUserByContactRequest{
				ContactType:  contactType,
				ContactValue: cleanCaller,
			}, opts...)
		}
		userRes, err := grpchelper.CallWithTimeout(userReqCtx, findUserFunc)
		if err == nil && userRes.GetUser() != nil {
			matchedUser = userRes.GetUser()
			for _, contact := range matchedUser.Contacts {
				if normalizePhoneNumber(contact.ContactValue) == cleanCaller {
					matchedContact = contact
					break
				}
			}
			if s.userCache != nil {
				_ = s.userCache.SetUser(ctx, cleanCaller, matchedUser, l)
			}
		} else {
			l.Info().
				Str("event", logger.EventAutoProvisionStart).
				Str("phone", cleanCaller).
				Str("tenant", route.TenantId).
				Msg("👤 Kayıtlı olmayan numara tespit edildi. Sistemde otomatik (Gölge) profili oluşturuluyor...")

			createFunc := func(c context.Context, opts ...grpc.CallOption) (*userv1.CreateUserResponse, error) {
				return s.userClient.CreateUser(c, &userv1.CreateUserRequest{
					TenantId: route.TenantId,
					UserType: "guest",
					Name:     toPtr("Guest_" + cleanCaller),
					InitialContact: &userv1.CreateUserRequest_InitialContact{
						ContactType:  contactType,
						ContactValue: cleanCaller,
					},
					PreferredLanguageCode: toPtr(route.DefaultLanguageCode),
				}, opts...)
			}

			createRes, createErr := grpchelper.CallWithTimeout(userReqCtx, createFunc)

			if createErr == nil && createRes.GetUser() != nil {
				matchedUser = createRes.GetUser()
				l.Info().
					Str("event", logger.EventAutoProvisionSuccess).
					Str("user_id", matchedUser.Id).
					Msg("✅ Otomatik profil başarıyla oluşturuldu.")

				for _, contact := range matchedUser.Contacts {
					if normalizePhoneNumber(contact.ContactValue) == cleanCaller {
						matchedContact = contact
						break
					}
				}
				if s.userCache != nil {
					_ = s.userCache.SetUser(ctx, cleanCaller, matchedUser, l)
				}
			} else {
				l.Error().Err(createErr).
					Str("event", logger.EventAutoProvisionFail).
					Msg("❌ Misafir kullanıcı DB'ye yazılamadı! Ghost profille devam ediliyor.")

				matchedUser = &userv1.User{
					Id:       NilUUID,
					Name:     toPtr("Ghost Misafir"),
					TenantId: route.TenantId,
					UserType: "guest",
				}
			}
		}
	} else {
		for _, contact := range matchedUser.Contacts {
			if normalizePhoneNumber(contact.ContactValue) == cleanCaller {
				matchedContact = contact
				break
			}
		}
	}

	if activePlan != nil {
		l.Info().
			Str("event", logger.EventDialplanResolveDone).
			Str("dialplan.id", activePlan.Id).
			Str("dialplan.action", activePlan.Action.Action).
			Msg("✅ Dialplan başarıyla çözüldü")

		return &dialplanv1.ResolveDialplanResponse{
			DialplanId:     activePlan.Id,
			TenantId:       activePlan.TenantId,
			Action:         activePlan.Action,
			MatchedUser:    matchedUser,
			MatchedContact: matchedContact,
			InboundRoute:   route,
		}, nil
	}

	return s.buildFailsafeResponse(ctx, l, DialplanSystemWelcomeGuest, matchedUser, matchedContact, route)
}

func (s *Service) buildFailsafeResponse(ctx context.Context, l zerolog.Logger, planID string, user *userv1.User, contact *userv1.Contact, route *dialplanv1.InboundRoute) (*dialplanv1.ResolveDialplanResponse, error) {
	if planID == "" {
		planID = DialplanSystemFailsafe
	}
	plan, err := s.repo.FindDialplanByID(ctx, planID)
	if err != nil {
		l.Error().Str("event", logger.EventFailsafeMissing).Msg("❌ CRITICAL: Failsafe plan DB'de yok!")
		return nil, status.Errorf(codes.Internal, "System Error: Failsafe plan missing")
	}
	return &dialplanv1.ResolveDialplanResponse{
		DialplanId: plan.Id, TenantId: plan.TenantId, Action: plan.Action,
		MatchedUser: user, MatchedContact: contact, InboundRoute: route,
	}, nil
}

func (s *Service) CreateInboundRoute(ctx context.Context, route *dialplanv1.InboundRoute) error {
	route.PhoneNumber = normalizePhoneNumber(route.PhoneNumber)
	return s.repo.CreateInboundRoute(ctx, route)
}

func (s *Service) GetInboundRoute(ctx context.Context, phoneNumber string) (*dialplanv1.InboundRoute, error) {
	return s.repo.FindInboundRouteByPhone(ctx, normalizePhoneNumber(phoneNumber))
}

func (s *Service) UpdateInboundRoute(ctx context.Context, route *dialplanv1.InboundRoute) error {
	route.PhoneNumber = normalizePhoneNumber(route.PhoneNumber)
	_, err := s.repo.UpdateInboundRoute(ctx, route)
	return err
}

func (s *Service) DeleteInboundRoute(ctx context.Context, phoneNumber string) error {
	_, err := s.repo.DeleteInboundRoute(ctx, normalizePhoneNumber(phoneNumber))
	return err
}

func (s *Service) ListInboundRoutes(ctx context.Context, req *dialplanv1.ListInboundRoutesRequest) (*dialplanv1.ListInboundRoutesResponse, error) {
	list, err := s.repo.ListInboundRoutes(ctx, req.TenantId, req.PageSize, (req.Page-1)*req.PageSize)
	if err != nil {
		return nil, err
	}
	count, _ := s.repo.CountInboundRoutes(ctx, req.TenantId)
	return &dialplanv1.ListInboundRoutesResponse{Routes: list, TotalCount: count}, nil
}

func (s *Service) CreateDialplan(ctx context.Context, req *dialplanv1.CreateDialplanRequest) error {
	bytes, _ := json.Marshal(req.Dialplan.Action.ActionData)
	return s.repo.CreateDialplan(ctx, req.Dialplan, bytes)
}

func (s *Service) GetDialplan(ctx context.Context, id string) (*dialplanv1.Dialplan, error) {
	return s.repo.FindDialplanByID(ctx, id)
}

func (s *Service) UpdateDialplan(ctx context.Context, req *dialplanv1.UpdateDialplanRequest) error {
	bytes, _ := json.Marshal(req.Dialplan.Action.ActionData)
	_, err := s.repo.UpdateDialplan(ctx, req.Dialplan, bytes)
	return err
}

func (s *Service) DeleteDialplan(ctx context.Context, id string) error {
	_, err := s.repo.DeleteDialplan(ctx, id)
	return err
}

func (s *Service) ListDialplans(ctx context.Context, req *dialplanv1.ListDialplansRequest) (*dialplanv1.ListDialplansResponse, error) {
	list, err := s.repo.ListDialplans(ctx, req.TenantId, req.PageSize, (req.Page-1)*req.PageSize)
	if err != nil {
		return nil, err
	}
	count, _ := s.repo.CountDialplans(ctx, req.TenantId)
	return &dialplanv1.ListDialplansResponse{Dialplans: list, TotalCount: count}, nil
}

func (s *Service) CreateQueue(ctx context.Context, req *dialplanv1.CreateQueueRequest) error {
	return s.repo.CreateQueue(ctx, req.Queue)
}

func (s *Service) GetQueue(ctx context.Context, id string) (*dialplanv1.Queue, error) {
	return s.repo.GetQueue(ctx, id)
}

func (s *Service) UpdateQueue(ctx context.Context, req *dialplanv1.UpdateQueueRequest) error {
	_, err := s.repo.UpdateQueue(ctx, req.Queue)
	return err
}

func (s *Service) DeleteQueue(ctx context.Context, id string) error {
	_, err := s.repo.DeleteQueue(ctx, id)
	return err
}

func (s *Service) ListQueues(ctx context.Context, req *dialplanv1.ListQueuesRequest) (*dialplanv1.ListQueuesResponse, error) {
	list, err := s.repo.ListQueues(ctx, req.TenantId, req.PageSize, (req.Page-1)*req.PageSize)
	if err != nil {
		return nil, err
	}
	count, _ := s.repo.CountQueues(ctx, req.TenantId)
	return &dialplanv1.ListQueuesResponse{Queues: list, TotalCount: count}, nil
}

func (s *Service) CreateSchedule(ctx context.Context, req *dialplanv1.CreateScheduleRequest) error {
	return s.repo.CreateSchedule(ctx, req.Schedule)
}

func (s *Service) GetSchedule(ctx context.Context, id string) (*dialplanv1.Schedule, error) {
	return s.repo.GetSchedule(ctx, id)
}
