package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"time"

	"github.com/sentiric/sentiric-dialplan-service/internal/logger"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
	dialplanv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/dialplan/v1"
	userv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/user/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

const serviceName = "dialplan-service"

var log zerolog.Logger

type server struct {
	dialplanv1.UnimplementedDialplanServiceServer
	db         *sql.DB
	userClient userv1.UserServiceClient
}

func getLoggerWithTraceID(ctx context.Context, baseLogger zerolog.Logger) (zerolog.Logger, string) {
	md, ok := metadata.FromIncomingContext(ctx)
	traceID := "unknown"
	if !ok {
		return baseLogger.With().Str("trace_id", traceID).Logger(), traceID
	}
	traceIDValues := md.Get("x-trace-id")
	if len(traceIDValues) > 0 {
		traceID = traceIDValues[0]
	}
	return baseLogger.With().Str("trace_id", traceID).Logger(), traceID
}

func main() {
	godotenv.Load()
	log = logger.New(serviceName)
	log.Info().Msg("Dialplan Service başlatılıyor...")

	dbURL := getEnvOrFail("POSTGRES_URL")

	db := connectToDBWithRetry(dbURL, 10)
	defer db.Close()

	userClient := createUserServiceClient()

	port := getEnv("DIALPLAN_SERVICE_GRPC_PORT", "50054")
	listenAddr := fmt.Sprintf(":%s", port)
	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatal().Err(err).Msg("TCP port dinlenemedi")
	}

	creds := loadServerTLS(
		getEnvOrFail("DIALPLAN_SERVICE_CERT_PATH"),
		getEnvOrFail("DIALPLAN_SERVICE_KEY_PATH"),
		getEnvOrFail("GRPC_TLS_CA_PATH"),
	)

	s := grpc.NewServer(grpc.Creds(creds))
	dialplanv1.RegisterDialplanServiceServer(s, &server{db: db, userClient: userClient})
	reflection.Register(s)

	log.Info().Str("port", port).Msg("gRPC sunucusu dinleniyor...")
	if err := s.Serve(lis); err != nil {
		log.Fatal().Err(err).Msg("gRPC sunucusu başlatılamadı")
	}
}

// --- Ana Karar Mekanizması ---

func (s *server) ResolveDialplan(ctx context.Context, req *dialplanv1.ResolveDialplanRequest) (*dialplanv1.ResolveDialplanResponse, error) {
	l, traceID := getLoggerWithTraceID(ctx, log)
	l = l.With().Str("method", "ResolveDialplan").Str("caller", req.GetCallerContactValue()).Str("destination", req.GetDestinationNumber()).Logger()
	l.Info().Msg("İstek alındı")

	var route dialplanv1.InboundRoute
	var activeDP, offHoursDP, failsafeDP sql.NullString

	query := `
		SELECT 
			phone_number, tenant_id, active_dialplan_id, off_hours_dialplan_id, failsafe_dialplan_id, 
			is_maintenance_mode, default_language_code
		FROM inbound_routes WHERE phone_number = $1
	`
	err := s.db.QueryRowContext(ctx, query, req.GetDestinationNumber()).Scan(
		&route.PhoneNumber, &route.TenantId, &activeDP, &offHoursDP, &failsafeDP,
		&route.IsMaintenanceMode, &route.DefaultLanguageCode,
	)

	if err != nil {
		if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.Code == "42P01" {
			l.Error().Err(err).Msg("Kritik 'inbound_routes' tablosu bulunamadı, sistem failsafe planına yönlendiriliyor.")
			failsafeRoute := &dialplanv1.InboundRoute{TenantId: "system", DefaultLanguageCode: "tr"}
			return s.getDialplanByID(ctx, "DP_SYSTEM_FAILSAFE", nil, nil, failsafeRoute)
		}

		if err == sql.ErrNoRows {
			l.Info().Msg("Aranan numara için inbound_route bulunamadı. Otomatik olarak yeni bir route oluşturuluyor (Auto-Provisioning).")
			newRoute, provisionErr := s.autoProvisionInboundRoute(ctx, req.GetDestinationNumber())
			if provisionErr != nil {
				l.Error().Err(provisionErr).Msg("Otomatik route oluşturma başarısız oldu, sistem failsafe planına yönlendiriliyor.")
				failsafeRoute := &dialplanv1.InboundRoute{TenantId: "system", DefaultLanguageCode: "tr"}
				return s.getDialplanByID(ctx, "DP_SYSTEM_FAILSAFE", nil, nil, failsafeRoute)
			}
			l.Info().Msg("Yeni route başarıyla oluşturuldu, çağrı misafir (guest) planına yönlendiriliyor.")
			return s.getDialplanByID(ctx, "DP_GUEST_ENTRY", nil, nil, newRoute)
		}

		l.Error().Err(err).Msg("Inbound route sorgusu başarısız")
		return nil, status.Errorf(codes.Internal, "Route sorgusu başarısız: %v", err)
	}

	if activeDP.Valid {
		route.ActiveDialplanId = &activeDP.String
	}
	if offHoursDP.Valid {
		route.OffHoursDialplanId = &offHoursDP.String
	}
	if failsafeDP.Valid {
		route.FailsafeDialplanId = &failsafeDP.String
	}
	if route.IsMaintenanceMode {
		l.Info().Str("failsafe_dialplan", safeString(route.FailsafeDialplanId)).Msg("Sistem bakım modunda, failsafe planına yönlendiriliyor.")
		return s.getDialplanByID(ctx, safeString(route.FailsafeDialplanId), nil, nil, &route)
	}
	userReqCtx := metadata.AppendToOutgoingContext(ctx, "x-trace-id", traceID)
	userRes, err := s.userClient.FindUserByContact(userReqCtx, &userv1.FindUserByContactRequest{
		ContactType:  "phone",
		ContactValue: req.GetCallerContactValue(),
	})
	if err != nil {
		st, ok := status.FromError(err)
		if ok && st.Code() == codes.NotFound {
			l.Info().Msg("Arayan kullanıcı User Service'de bulunamadı, misafir (guest) planına yönlendiriliyor.")
			return s.getDialplanByID(ctx, "DP_GUEST_ENTRY", nil, nil, &route)
		}
		l.Error().Err(err).Msg("User Service'e yapılan FindUserByContact çağrısı başarısız")
		return s.getDialplanByID(ctx, safeString(route.FailsafeDialplanId), nil, nil, &route)
	}
	matchedUser := userRes.GetUser()
	var matchedContact *userv1.Contact
	for _, c := range matchedUser.Contacts {
		if c.ContactValue == req.GetCallerContactValue() {
			matchedContact = c
			break
		}
	}
	l.Info().Str("active_dialplan", safeString(route.ActiveDialplanId)).Str("user_id", matchedUser.Id).Msg("Kullanıcı bulundu, aktif plana yönlendiriliyor.")
	return s.getDialplanByID(ctx, safeString(route.ActiveDialplanId), matchedUser, matchedContact, &route)
}

// --- Inbound Route Yönetimi (CRUD) ---

func (s *server) CreateInboundRoute(ctx context.Context, req *dialplanv1.CreateInboundRouteRequest) (*dialplanv1.CreateInboundRouteResponse, error) {
	l, _ := getLoggerWithTraceID(ctx, log)
	route := req.GetRoute()
	if route == nil {
		return nil, status.Error(codes.InvalidArgument, "Route nesnesi boş olamaz")
	}
	l = l.With().Str("method", "CreateInboundRoute").Str("phone_number", route.GetPhoneNumber()).Logger()
	l.Info().Msg("Yeni inbound route oluşturma isteği alındı.")

	query := `
		INSERT INTO inbound_routes 
		(phone_number, tenant_id, active_dialplan_id, off_hours_dialplan_id, failsafe_dialplan_id, is_maintenance_mode, default_language_code)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err := s.db.ExecContext(ctx, query,
		route.PhoneNumber, route.TenantId, route.ActiveDialplanId,
		route.OffHoursDialplanId, route.FailsafeDialplanId,
		route.IsMaintenanceMode, route.DefaultLanguageCode,
	)

	if err != nil {
		l.Error().Err(err).Msg("Inbound route oluşturulamadı.")
		if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.Code == "23505" { // unique_violation
			return nil, status.Errorf(codes.AlreadyExists, "Bu telefon numarası zaten kayıtlı: %s", route.PhoneNumber)
		}
		return nil, status.Errorf(codes.Internal, "Inbound route oluşturulurken bir hata oluştu: %v", err)
	}

	l.Info().Msg("Inbound route başarıyla oluşturuldu.")
	return &dialplanv1.CreateInboundRouteResponse{Route: route}, nil
}

func (s *server) GetInboundRoute(ctx context.Context, req *dialplanv1.GetInboundRouteRequest) (*dialplanv1.GetInboundRouteResponse, error) {
	l, _ := getLoggerWithTraceID(ctx, log)
	l = l.With().Str("method", "GetInboundRoute").Str("phone_number", req.GetPhoneNumber()).Logger()
	l.Info().Msg("Inbound route getirme isteği alındı.")

	var route dialplanv1.InboundRoute
	var activeDP, offHoursDP, failsafeDP sql.NullString

	query := `SELECT phone_number, tenant_id, active_dialplan_id, off_hours_dialplan_id, failsafe_dialplan_id, is_maintenance_mode, default_language_code FROM inbound_routes WHERE phone_number = $1`
	err := s.db.QueryRowContext(ctx, query, req.GetPhoneNumber()).Scan(
		&route.PhoneNumber, &route.TenantId, &activeDP, &offHoursDP, &failsafeDP,
		&route.IsMaintenanceMode, &route.DefaultLanguageCode,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			l.Warn().Msg("İstenen inbound_route bulunamadı.")
			return nil, status.Errorf(codes.NotFound, "Inbound route bulunamadı: %s", req.GetPhoneNumber())
		}
		l.Error().Err(err).Msg("Inbound route sorgusu başarısız.")
		return nil, status.Errorf(codes.Internal, "Inbound route sorgulanırken bir hata oluştu: %v", err)
	}

	if activeDP.Valid {
		route.ActiveDialplanId = &activeDP.String
	}
	if offHoursDP.Valid {
		route.OffHoursDialplanId = &offHoursDP.String
	}
	if failsafeDP.Valid {
		route.FailsafeDialplanId = &failsafeDP.String
	}

	l.Info().Msg("Inbound route başarıyla bulundu.")
	return &dialplanv1.GetInboundRouteResponse{Route: &route}, nil
}

func (s *server) UpdateInboundRoute(ctx context.Context, req *dialplanv1.UpdateInboundRouteRequest) (*dialplanv1.UpdateInboundRouteResponse, error) {
	l, _ := getLoggerWithTraceID(ctx, log)
	route := req.GetRoute()
	if route == nil {
		return nil, status.Error(codes.InvalidArgument, "Route nesnesi boş olamaz")
	}
	l = l.With().Str("method", "UpdateInboundRoute").Str("phone_number", route.GetPhoneNumber()).Logger()
	l.Info().Msg("Inbound route güncelleme isteği alındı.")

	query := `
		UPDATE inbound_routes SET
		tenant_id = $2, active_dialplan_id = $3, off_hours_dialplan_id = $4, failsafe_dialplan_id = $5,
		is_maintenance_mode = $6, default_language_code = $7
		WHERE phone_number = $1
	`
	res, err := s.db.ExecContext(ctx, query,
		route.PhoneNumber, route.TenantId, route.ActiveDialplanId,
		route.OffHoursDialplanId, route.FailsafeDialplanId,
		route.IsMaintenanceMode, route.DefaultLanguageCode,
	)
	if err != nil {
		l.Error().Err(err).Msg("Inbound route güncellenemedi.")
		return nil, status.Errorf(codes.Internal, "Inbound route güncellenirken bir hata oluştu: %v", err)
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return nil, status.Errorf(codes.NotFound, "Güncellenecek inbound route bulunamadı: %s", route.PhoneNumber)
	}

	l.Info().Msg("Inbound route başarıyla güncellendi.")
	return &dialplanv1.UpdateInboundRouteResponse{Route: route}, nil
}

func (s *server) DeleteInboundRoute(ctx context.Context, req *dialplanv1.DeleteInboundRouteRequest) (*dialplanv1.DeleteInboundRouteResponse, error) {
	l, _ := getLoggerWithTraceID(ctx, log)
	l = l.With().Str("method", "DeleteInboundRoute").Str("phone_number", req.GetPhoneNumber()).Logger()
	l.Info().Msg("Inbound route silme isteği alındı.")

	res, err := s.db.ExecContext(ctx, "DELETE FROM inbound_routes WHERE phone_number = $1", req.GetPhoneNumber())
	if err != nil {
		l.Error().Err(err).Msg("Inbound route silinemedi.")
		return nil, status.Errorf(codes.Internal, "Inbound route silinirken bir hata oluştu: %v", err)
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		l.Warn().Msg("Silinecek inbound route bulunamadı.")
	}

	l.Info().Msg("Inbound route başarıyla silindi.")
	return &dialplanv1.DeleteInboundRouteResponse{Success: true}, nil
}

func (s *server) ListInboundRoutes(ctx context.Context, req *dialplanv1.ListInboundRoutesRequest) (*dialplanv1.ListInboundRoutesResponse, error) {
	l, _ := getLoggerWithTraceID(ctx, log)
	l = l.With().Str("method", "ListInboundRoutes").Str("tenant_id", req.GetTenantId()).Logger()
	l.Info().Msg("Inbound route listeleme isteği alındı.")

	page := req.GetPage()
	if page < 1 {
		page = 1
	}
	pageSize := req.GetPageSize()
	if pageSize < 1 {
		pageSize = 10
	}
	offset := (page - 1) * pageSize

	baseQuery := "FROM inbound_routes"
	args := []interface{}{}
	if req.GetTenantId() != "" {
		baseQuery += " WHERE tenant_id = $1"
		args = append(args, req.GetTenantId())
	}

	var totalCount int32
	countQuery := "SELECT count(*) " + baseQuery
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&totalCount); err != nil {
		l.Error().Err(err).Msg("Toplam route sayısı alınamadı.")
		return nil, status.Error(codes.Internal, "Route'lar listelenemedi.")
	}

	dataQuery := "SELECT phone_number, tenant_id, active_dialplan_id, off_hours_dialplan_id, failsafe_dialplan_id, is_maintenance_mode, default_language_code " + baseQuery + fmt.Sprintf(" ORDER BY phone_number LIMIT %d OFFSET %d", pageSize, offset)
	rows, err := s.db.QueryContext(ctx, dataQuery, args...)
	if err != nil {
		l.Error().Err(err).Msg("Route'lar sorgulanamadı.")
		return nil, status.Error(codes.Internal, "Route'lar listelenemedi.")
	}
	defer rows.Close()

	routes := []*dialplanv1.InboundRoute{}
	for rows.Next() {
		var route dialplanv1.InboundRoute
		var activeDP, offHoursDP, failsafeDP sql.NullString
		if err := rows.Scan(&route.PhoneNumber, &route.TenantId, &activeDP, &offHoursDP, &failsafeDP, &route.IsMaintenanceMode, &route.DefaultLanguageCode); err != nil {
			l.Error().Err(err).Msg("Route verisi okunamadı.")
			continue
		}
		if activeDP.Valid {
			route.ActiveDialplanId = &activeDP.String
		}
		if offHoursDP.Valid {
			route.OffHoursDialplanId = &offHoursDP.String
		}
		if failsafeDP.Valid {
			route.FailsafeDialplanId = &failsafeDP.String
		}
		routes = append(routes, &route)
	}

	l.Info().Int32("count", int32(len(routes))).Msg("Inbound route'lar başarıyla listelendi.")
	return &dialplanv1.ListInboundRoutesResponse{Routes: routes, TotalCount: totalCount}, nil
}

// --- Dialplan Yönetimi (CRUD) ---

func (s *server) CreateDialplan(ctx context.Context, req *dialplanv1.CreateDialplanRequest) (*dialplanv1.CreateDialplanResponse, error) {
	l, _ := getLoggerWithTraceID(ctx, log)
	dp := req.GetDialplan()
	if dp == nil {
		return nil, status.Error(codes.InvalidArgument, "Dialplan nesnesi boş olamaz.")
	}
	l = l.With().Str("method", "CreateDialplan").Str("dialplan_id", dp.GetId()).Logger()
	l.Info().Msg("Yeni dialplan oluşturma isteği alındı.")

	actionDataBytes, err := json.Marshal(dp.GetAction().GetActionData().GetData())
	if err != nil {
		l.Error().Err(err).Msg("ActionData JSON'a çevrilemedi.")
		return nil, status.Errorf(codes.InvalidArgument, "Geçersiz action_data: %v", err)
	}

	query := `INSERT INTO dialplans (id, tenant_id, description, action, action_data) VALUES ($1, $2, $3, $4, $5)`
	_, err = s.db.ExecContext(ctx, query, dp.Id, dp.TenantId, dp.Description, dp.GetAction().GetAction(), actionDataBytes)

	if err != nil {
		l.Error().Err(err).Msg("Dialplan oluşturulamadı.")
		if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.Code == "23505" {
			return nil, status.Errorf(codes.AlreadyExists, "Bu dialplan ID zaten kayıtlı: %s", dp.Id)
		}
		return nil, status.Errorf(codes.Internal, "Dialplan oluşturulurken bir hata oluştu: %v", err)
	}

	l.Info().Msg("Dialplan başarıyla oluşturuldu.")
	return &dialplanv1.CreateDialplanResponse{Dialplan: dp}, nil
}

func (s *server) GetDialplan(ctx context.Context, req *dialplanv1.GetDialplanRequest) (*dialplanv1.GetDialplanResponse, error) {
	l, _ := getLoggerWithTraceID(ctx, log)
	l = l.With().Str("method", "GetDialplan").Str("dialplan_id", req.GetId()).Logger()
	l.Info().Msg("Dialplan getirme isteği alındı.")

	var dp dialplanv1.Dialplan
	var action dialplanv1.DialplanAction
	var actionData dialplanv1.ActionData
	var actionStr sql.NullString
	var actionDataBytes []byte

	query := `SELECT id, tenant_id, description, action, action_data FROM dialplans WHERE id = $1`
	err := s.db.QueryRowContext(ctx, query, req.GetId()).Scan(&dp.Id, &dp.TenantId, &dp.Description, &actionStr, &actionDataBytes)

	if err != nil {
		if err == sql.ErrNoRows {
			l.Warn().Msg("İstenen dialplan bulunamadı.")
			return nil, status.Errorf(codes.NotFound, "Dialplan bulunamadı: %s", req.GetId())
		}
		l.Error().Err(err).Msg("Dialplan sorgusu başarısız.")
		return nil, status.Errorf(codes.Internal, "Dialplan sorgulanırken bir hata oluştu: %v", err)
	}

	if actionStr.Valid {
		action.Action = actionStr.String
	}
	if actionDataBytes != nil {
		var dataMap map[string]string
		if err := json.Unmarshal(actionDataBytes, &dataMap); err == nil {
			actionData.Data = dataMap
		}
	}
	action.ActionData = &actionData
	dp.Action = &action

	l.Info().Msg("Dialplan başarıyla bulundu.")
	return &dialplanv1.GetDialplanResponse{Dialplan: &dp}, nil
}

func (s *server) UpdateDialplan(ctx context.Context, req *dialplanv1.UpdateDialplanRequest) (*dialplanv1.UpdateDialplanResponse, error) {
	l, _ := getLoggerWithTraceID(ctx, log)
	dp := req.GetDialplan()
	if dp == nil {
		return nil, status.Error(codes.InvalidArgument, "Dialplan nesnesi boş olamaz.")
	}
	l = l.With().Str("method", "UpdateDialplan").Str("dialplan_id", dp.GetId()).Logger()
	l.Info().Msg("Dialplan güncelleme isteği alındı.")

	actionDataBytes, err := json.Marshal(dp.GetAction().GetActionData().GetData())
	if err != nil {
		l.Error().Err(err).Msg("ActionData JSON'a çevrilemedi.")
		return nil, status.Errorf(codes.InvalidArgument, "Geçersiz action_data: %v", err)
	}

	query := `UPDATE dialplans SET tenant_id = $2, description = $3, action = $4, action_data = $5 WHERE id = $1`
	res, err := s.db.ExecContext(ctx, query, dp.Id, dp.TenantId, dp.Description, dp.GetAction().GetAction(), actionDataBytes)
	if err != nil {
		l.Error().Err(err).Msg("Dialplan güncellenemedi.")
		return nil, status.Errorf(codes.Internal, "Dialplan güncellenirken bir hata oluştu: %v", err)
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return nil, status.Errorf(codes.NotFound, "Güncellenecek dialplan bulunamadı: %s", dp.Id)
	}

	l.Info().Msg("Dialplan başarıyla güncellendi.")
	return &dialplanv1.UpdateDialplanResponse{Dialplan: dp}, nil
}

func (s *server) DeleteDialplan(ctx context.Context, req *dialplanv1.DeleteDialplanRequest) (*dialplanv1.DeleteDialplanResponse, error) {
	l, _ := getLoggerWithTraceID(ctx, log)
	l = l.With().Str("method", "DeleteDialplan").Str("dialplan_id", req.GetId()).Logger()
	l.Info().Msg("Dialplan silme isteği alındı.")

	res, err := s.db.ExecContext(ctx, "DELETE FROM dialplans WHERE id = $1", req.GetId())
	if err != nil {
		l.Error().Err(err).Msg("Dialplan silinemedi.")
		return nil, status.Errorf(codes.Internal, "Dialplan silinirken bir hata oluştu: %v", err)
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		l.Warn().Msg("Silinecek dialplan bulunamadı.")
	}

	l.Info().Msg("Dialplan başarıyla silindi.")
	return &dialplanv1.DeleteDialplanResponse{Success: true}, nil
}

func (s *server) ListDialplans(ctx context.Context, req *dialplanv1.ListDialplansRequest) (*dialplanv1.ListDialplansResponse, error) {
	l, _ := getLoggerWithTraceID(ctx, log)
	l = l.With().Str("method", "ListDialplans").Str("tenant_id", req.GetTenantId()).Logger()
	l.Info().Msg("Dialplan listeleme isteği alındı.")

	page := req.GetPage()
	if page < 1 {
		page = 1
	}
	pageSize := req.GetPageSize()
	if pageSize < 1 {
		pageSize = 10
	}
	offset := (page - 1) * pageSize

	baseQuery := "FROM dialplans"
	args := []interface{}{}
	if req.GetTenantId() != "" {
		baseQuery += " WHERE tenant_id = $1"
		args = append(args, req.GetTenantId())
	}

	var totalCount int32
	countQuery := "SELECT count(*) " + baseQuery
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&totalCount); err != nil {
		l.Error().Err(err).Msg("Toplam dialplan sayısı alınamadı.")
		return nil, status.Error(codes.Internal, "Dialplan'lar listelenemedi.")
	}

	dataQuery := "SELECT id, tenant_id, description, action, action_data " + baseQuery + fmt.Sprintf(" ORDER BY id LIMIT %d OFFSET %d", pageSize, offset)
	rows, err := s.db.QueryContext(ctx, dataQuery, args...)
	if err != nil {
		l.Error().Err(err).Msg("Dialplan'lar sorgulanamadı.")
		return nil, status.Error(codes.Internal, "Dialplan'lar listelenemedi.")
	}
	defer rows.Close()

	dialplans := []*dialplanv1.Dialplan{}
	for rows.Next() {
		var dp dialplanv1.Dialplan
		var action dialplanv1.DialplanAction
		var actionData dialplanv1.ActionData
		var actionStr sql.NullString
		var actionDataBytes []byte
		if err := rows.Scan(&dp.Id, &dp.TenantId, &dp.Description, &actionStr, &actionDataBytes); err != nil {
			l.Error().Err(err).Msg("Dialplan verisi okunamadı.")
			continue
		}
		if actionStr.Valid {
			action.Action = actionStr.String
		}
		if actionDataBytes != nil {
			var dataMap map[string]string
			if err := json.Unmarshal(actionDataBytes, &dataMap); err == nil {
				actionData.Data = dataMap
			}
		}
		action.ActionData = &actionData
		dp.Action = &action
		dialplans = append(dialplans, &dp)
	}

	l.Info().Int32("count", int32(len(dialplans))).Msg("Dialplan'lar başarıyla listelendi.")
	return &dialplanv1.ListDialplansResponse{Dialplans: dialplans, TotalCount: totalCount}, nil
}

// --- Yardımcı Fonksiyonlar ---

func (s *server) autoProvisionInboundRoute(ctx context.Context, phoneNumber string) (*dialplanv1.InboundRoute, error) {
	l, _ := getLoggerWithTraceID(ctx, log)
	l = l.With().Str("method", "autoProvisionInboundRoute").Str("phone_number", phoneNumber).Logger()

	defaultGuestPlan := "DP_GUEST_ENTRY"
	defaultSystemTenant := "system"
	defaultLangCode := "tr"

	query := `
		INSERT INTO inbound_routes (phone_number, tenant_id, active_dialplan_id, default_language_code)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (phone_number) DO NOTHING
	`

	_, err := s.db.ExecContext(ctx, query, phoneNumber, defaultSystemTenant, defaultGuestPlan, defaultLangCode)
	if err != nil {
		l.Error().Err(err).Msg("Yeni inbound route veritabanına eklenemedi.")
		return nil, err
	}

	newRoute := &dialplanv1.InboundRoute{
		PhoneNumber:         phoneNumber,
		TenantId:            defaultSystemTenant,
		ActiveDialplanId:    &defaultGuestPlan,
		DefaultLanguageCode: defaultLangCode,
		IsMaintenanceMode:   false,
	}

	l.Info().Msg("Yeni inbound route başarıyla veritabanına eklendi.")
	return newRoute, nil
}

func (s *server) getDialplanByID(ctx context.Context, id string, user *userv1.User, contact *userv1.Contact, route *dialplanv1.InboundRoute) (*dialplanv1.ResolveDialplanResponse, error) {
	l, _ := getLoggerWithTraceID(ctx, log)
	l = l.With().Str("method", "getDialplanByID").Str("dialplan_id", id).Logger()
	l.Info().Msg("Dialplan detayları alınıyor")

	var description, action, tenantID sql.NullString
	var actionBytes []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT description, action, action_data, tenant_id FROM dialplans WHERE id = $1`, id).
		Scan(&description, &action, &actionBytes, &tenantID)

	if err != nil {
		l.Error().Err(err).Str("failed_dialplan_id", id).Msg("Dialplan ID sorgusu başarısız, failsafe tetikleniyor.")

		if id == "DP_SYSTEM_FAILSAFE" {
			l.Error().Msg("KRİTİK HATA: Sistem failsafe dialplan (`DP_SYSTEM_FAILSAFE`) dahi bulunamadı! Servis çökmemek için hardcoded bir yanıt üretiyor.")
			return &dialplanv1.ResolveDialplanResponse{
				DialplanId: "ULTIMATE_FAILSAFE",
				TenantId:   "system",
				Action: &dialplanv1.DialplanAction{
					Action: "PLAY_ANNOUNCEMENT",
					ActionData: &dialplanv1.ActionData{
						Data: map[string]string{"announcement_id": "ANNOUNCE_SYSTEM_ERROR"},
					},
				},
				InboundRoute: route,
			}, nil
		}
		return s.getDialplanByID(ctx, "DP_SYSTEM_FAILSAFE", user, contact, route)
	}

	var dataMap map[string]string
	if actionBytes != nil {
		if err := json.Unmarshal(actionBytes, &dataMap); err != nil {
			l.Warn().Err(err).Msg("action_data JSON parse edilemedi")
			dataMap = make(map[string]string)
		}
	}
	resp := &dialplanv1.ResolveDialplanResponse{
		DialplanId:     id,
		TenantId:       tenantID.String,
		Action:         &dialplanv1.DialplanAction{Action: action.String, ActionData: &dialplanv1.ActionData{Data: dataMap}},
		MatchedUser:    user,
		MatchedContact: contact,
		InboundRoute:   route,
	}
	l.Info().Str("action", resp.Action.Action).Msg("Dialplan başarıyla çözümlendi")
	return resp, nil
}

func safeString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func createUserServiceClient() userv1.UserServiceClient {
	userServiceURL := getEnvOrFail("USER_SERVICE_GRPC_URL")
	certPath := getEnvOrFail("DIALPLAN_SERVICE_CERT_PATH")
	keyPath := getEnvOrFail("DIALPLAN_SERVICE_KEY_PATH")
	caPath := getEnvOrFail("GRPC_TLS_CA_PATH")
	clientCert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		log.Fatal().Err(err).Msg("İstemci sertifikası yüklenemedi")
	}
	caCert, err := os.ReadFile(caPath)
	if err != nil {
		log.Fatal().Err(err).Msg("CA sertifikası okunamadı")
	}
	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		log.Fatal().Msg("CA sertifikası havuza eklenemedi")
	}
	creds := credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{clientCert}, RootCAs: caCertPool, ServerName: "user-service"})
	conn, err := grpc.NewClient(userServiceURL, grpc.WithTransportCredentials(creds))
	if err != nil {
		log.Fatal().Err(err).Msg("User Service'e gRPC bağlantısı kurulamadı")
	}
	log.Info().Str("url", userServiceURL).Msg("User Service'e gRPC istemci bağlantısı başarılı")
	return userv1.NewUserServiceClient(conn)
}

func connectToDBWithRetry(url string, maxRetries int) *sql.DB {
	var db *sql.DB
	var err error

	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		log.Fatal().Err(err).Msg("PostgreSQL URL parse edilemedi")
	}

	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	finalURL := stdlib.RegisterConnConfig(config.ConnConfig)

	for i := 0; i < maxRetries; i++ {
		db, err = sql.Open("pgx", finalURL)
		if err == nil {
			db.SetConnMaxLifetime(time.Minute * 3)
			db.SetMaxIdleConns(2)
			db.SetMaxOpenConns(5)
			if pingErr := db.Ping(); pingErr == nil {
				log.Info().Msg("Veritabanına bağlantı başarılı (Simple Protocol Mode).")
				return db
			} else {
				err = pingErr
			}
		}
		log.Warn().Err(err).Int("attempt", i+1).Int("max_attempts", maxRetries).Msg("Veritabanına bağlanılamadı, 5 saniye sonra tekrar denenecek...")
		time.Sleep(5 * time.Second)
	}
	log.Fatal().Err(err).Msgf("Veritabanına bağlanılamadı (%d deneme)", maxRetries)
	return nil
}

func loadServerTLS(certPath, keyPath, caPath string) credentials.TransportCredentials {
	certificate, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		log.Fatal().Err(err).Msg("Sunucu sertifikası yüklenemedi")
	}
	caCert, err := ioutil.ReadFile(caPath)
	if err != nil {
		log.Fatal().Err(err).Msg("CA sertifikası okunamadı")
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCert) {
		log.Fatal().Msg("CA sertifikası havuza eklenemedi.")
	}
	tlsConfig := &tls.Config{Certificates: []tls.Certificate{certificate}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: caPool}
	return credentials.NewTLS(tlsConfig)
}

func getEnv(key string, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func getEnvOrFail(key string) string {
	val := os.Getenv(key)
	if val == "" {
		log.Fatal().Str("variable", key).Msg("Gerekli ortam değişkeni tanımlı değil")
	}
	return val
}
