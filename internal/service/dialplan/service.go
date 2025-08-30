// DOSYANIN TAM VE DOĞRU HALİ
package dialplan

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rs/zerolog"
	dialplanv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/dialplan/v1"
	userv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/user/v1"
	"github.com/sentiric/sentiric-dialplan-service/internal/config"
	platformgrpc "github.com/sentiric/sentiric-dialplan-service/internal/platform/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Arayüz (interface) orijinal, basit haline geri döndü.
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
	log        zerolog.Logger
}

func NewService(repo Repository, userClient userv1.UserServiceClient, log zerolog.Logger) *Service {
	return &Service{repo: repo, userClient: userClient, log: log}
}

func NewUserServiceClient(targetURL string, cfg config.Config) (userv1.UserServiceClient, *grpc.ClientConn, error) {
	conn, err := platformgrpc.NewClientConnection(targetURL, "user-service", cfg)
	if err != nil {
		return nil, nil, err
	}
	return userv1.NewUserServiceClient(conn), conn, nil
}

// --- Ana İş Mantığı
func (s *Service) ResolveDialplan(ctx context.Context, caller, destination string) (*dialplanv1.ResolveDialplanResponse, error) {

	route, err := s.repo.FindInboundRouteByPhone(ctx, destination)
	if err != nil {
		if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.Code == "42P01" {
			s.log.Error().Err(err).Msg("Kritik 'inbound_routes' tablosu bulunamadı.")
			failsafeRoute := &dialplanv1.InboundRoute{TenantId: "system"}
			return s.buildFailsafeResponse(ctx, "DP_SYSTEM_FAILSAFE", nil, nil, failsafeRoute)
		}
		if errors.Is(err, pgx.ErrNoRows) {
			s.log.Info().Str("destination", destination).Msg("Route bulunamadı, auto-provisioning tetikleniyor.")
			newRoute, provisionErr := s.autoProvisionInboundRoute(ctx, destination)
			if provisionErr != nil {
				return nil, status.Errorf(codes.Internal, "Auto-provisioning başarısız: %v", provisionErr)
			}
			return s.buildFailsafeResponse(ctx, "DP_GUEST_ENTRY", nil, nil, newRoute)
		}
		s.log.Error().Err(err).Msg("Inbound route sorgusu başarısız")
		return nil, status.Errorf(codes.Internal, "Route sorgusu başarısız: %v", err)
	}

	if route.IsMaintenanceMode {
		s.log.Info().Str("destination", destination).Msg("Hat bakım modunda, failsafe planına yönlendiriliyor.")
		return s.buildFailsafeResponse(ctx, safeString(route.FailsafeDialplanId), nil, nil, route)
	}

	md, _ := metadata.FromIncomingContext(ctx)
	traceIDValues := md.Get("x-trace-id")
	traceID := "unknown"
	if len(traceIDValues) > 0 {
		traceID = traceIDValues[0]
	}
	userReqCtx := metadata.AppendToOutgoingContext(ctx, "x-trace-id", traceID)

	userRes, err := s.userClient.FindUserByContact(userReqCtx, &userv1.FindUserByContactRequest{
		ContactType: "phone", ContactValue: caller,
	})
	if err != nil {
		st, _ := status.FromError(err)
		if st.Code() == codes.NotFound {
			s.log.Info().Str("caller", caller).Msg("Arayan bulunamadı, misafir planına yönlendiriliyor.")
			return s.buildFailsafeResponse(ctx, "DP_GUEST_ENTRY", nil, nil, route)
		}
		s.log.Error().Err(err).Msg("User service ile iletişim kurulamadı, failsafe planına yönlendiriliyor.")
		return s.buildFailsafeResponse(ctx, safeString(route.FailsafeDialplanId), nil, nil, route)
	}

	matchedUser := userRes.GetUser()
	var matchedContact *userv1.Contact
	for _, c := range matchedUser.Contacts {
		if c.ContactValue == caller {
			matchedContact = c
			break
		}
	}

	s.log.Info().Str("user_id", matchedUser.Id).Msg("Kullanıcı bulundu, aktif plana yönlendiriliyor.")
	plan, err := s.repo.FindDialplanByID(ctx, safeString(route.ActiveDialplanId))
	if err != nil {
		s.log.Error().Err(err).Str("plan_id", safeString(route.ActiveDialplanId)).Msg("Aktif plan bulunamadı, failsafe tetikleniyor.")
		return s.buildFailsafeResponse(ctx, safeString(route.FailsafeDialplanId), matchedUser, matchedContact, route)
	}

	return &dialplanv1.ResolveDialplanResponse{
		DialplanId: plan.Id, TenantId: plan.TenantId, Action: plan.Action,
		MatchedUser: matchedUser, MatchedContact: matchedContact, InboundRoute: route,
	}, nil
}

// --- CRUD Metodları ---

func (s *Service) CreateInboundRoute(ctx context.Context, route *dialplanv1.InboundRoute) error {
	err := s.repo.CreateInboundRoute(ctx, route)
	if err != nil {
		if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.Code == "23505" {
			return status.Errorf(codes.AlreadyExists, "Bu telefon numarası zaten kayıtlı: %s", route.PhoneNumber)
		}
		return status.Errorf(codes.Internal, "Inbound route oluşturulamadı: %v", err)
	}
	return nil
}

func (s *Service) GetInboundRoute(ctx context.Context, phoneNumber string) (*dialplanv1.InboundRoute, error) {
	route, err := s.repo.FindInboundRouteByPhone(ctx, phoneNumber)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
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
		if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.Code == "23505" {
			return status.Errorf(codes.AlreadyExists, "Bu dialplan ID zaten kayıtlı: %s", dp.Id)
		}
		return status.Errorf(codes.Internal, "Dialplan oluşturulamadı: %v", err)
	}
	return nil
}

func (s *Service) GetDialplan(ctx context.Context, id string) (*dialplanv1.Dialplan, error) {

	dp, err := s.repo.FindDialplanByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
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

// --- Yardımcı Metodlar ---

// Bu fonksiyon artık çok daha basit. Sadece repository'yi çağırıyor.
func (s *Service) autoProvisionInboundRoute(ctx context.Context, phoneNumber string) (*dialplanv1.InboundRoute, error) {
	guestPlan := "DP_GUEST_ENTRY"
	newRoute := &dialplanv1.InboundRoute{
		PhoneNumber:         phoneNumber,
		TenantId:            "system",
		ActiveDialplanId:    &guestPlan,
		DefaultLanguageCode: "tr",
	}
	// DB şeması artık varsayılan trunk ID'sini kendi atayacak.
	err := s.repo.CreateInboundRoute(ctx, newRoute)
	return newRoute, err
}

func (s *Service) buildFailsafeResponse(ctx context.Context, planID string, user *userv1.User, contact *userv1.Contact, route *dialplanv1.InboundRoute) (*dialplanv1.ResolveDialplanResponse, error) {
	if planID == "" {
		planID = "DP_SYSTEM_FAILSAFE"
	}
	plan, err := s.repo.FindDialplanByID(ctx, planID)
	if err != nil {
		s.log.Error().Err(err).Str("plan_id", planID).Msg("KRİTİK HATA: Failsafe dialplan dahi bulunamadı!")
		return &dialplanv1.ResolveDialplanResponse{
			DialplanId: "ULTIMATE_FAILSAFE", TenantId: "system",
			Action: &dialplanv1.DialplanAction{
				Action:     "PLAY_ANNOUNCEMENT",
				ActionData: &dialplanv1.ActionData{Data: map[string]string{"announcement_id": "ANNOUNCE_SYSTEM_ERROR"}},
			}, InboundRoute: route,
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
