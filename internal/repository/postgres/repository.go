// sentiric-dialplan-service/internal/repository/postgres/repository.go
package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	dialplanv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/dialplan/v1"
	"github.com/sentiric/sentiric-dialplan-service/internal/service/dialplan"
)

type Repository struct {
	db  *pgxpool.Pool
	log zerolog.Logger
}

func NewRepository(db *pgxpool.Pool, log zerolog.Logger) *Repository {
	return &Repository{db: db, log: log}
}

func (r *Repository) handleError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return dialplan.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return dialplan.ErrConflict
		}
	}
	return fmt.Errorf("%w: %v", dialplan.ErrDatabase, err)
}

// --- INBOUND ROUTES ---

func (r *Repository) FindInboundRouteByPhone(ctx context.Context, phoneNumber string) (*dialplanv1.InboundRoute, error) {
	var route dialplanv1.InboundRoute
	var activeDP, offHoursDP, failsafeDP, scheduleID sql.NullString
	// TrunkID'yi alıyoruz ama Contracts'ta henüz yoksa kullanamayız.
	// Ancak DB'den çekmek iyi pratiktir.
	var trunkID sql.NullInt32

	query := `
		SELECT 
			phone_number, tenant_id, 
			active_dialplan_id, off_hours_dialplan_id, failsafe_dialplan_id, schedule_id,
			is_maintenance_mode, block_anonymous, default_language_code, sip_trunk_id 
		FROM inbound_routes 
		WHERE phone_number = $1`

	err := r.db.QueryRow(ctx, query, phoneNumber).Scan(
		&route.PhoneNumber, &route.TenantId,
		&activeDP, &offHoursDP, &failsafeDP, &scheduleID,
		&route.IsMaintenanceMode, &route.BlockAnonymous, &route.DefaultLanguageCode, &trunkID,
	)
	if err != nil {
		return nil, r.handleError(err)
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
	if scheduleID.Valid {
		route.ScheduleId = &scheduleID.String
	}

	// [FIX]: Trunk ID ataması yapıldı (Proto definition'da mevcut değilse compile hatası verir,
	// contracts güncellendiği için bu alanın olduğunu varsayıyoruz.
	// Eğer v1.18.0'da InboundRoute içinde sip_trunk_id yoksa bu satırı yoruma alınız).
	// Kontrol ettiğimde InboundRoute mesajında henüz sip_trunk_id yok.
	// Bu yüzden şimdilik atamayı yapmıyoruz, sadece DB'den çekiyoruz.
	// if trunkID.Valid {
	//    route.SipTrunkId = trunkID.Int32
	// }

	return &route, nil
}

func (r *Repository) CreateInboundRoute(ctx context.Context, route *dialplanv1.InboundRoute) error {
	query := `
		INSERT INTO inbound_routes (
			phone_number, tenant_id, active_dialplan_id, off_hours_dialplan_id, failsafe_dialplan_id, schedule_id,
			is_maintenance_mode, block_anonymous, default_language_code, sip_trunk_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 99)` // Default Trunk 99 (Dev)

	_, err := r.db.Exec(ctx, query,
		route.PhoneNumber, route.TenantId, route.ActiveDialplanId, route.OffHoursDialplanId, route.FailsafeDialplanId, route.ScheduleId,
		route.IsMaintenanceMode, route.BlockAnonymous, route.DefaultLanguageCode,
	)
	return r.handleError(err)
}

func (r *Repository) UpdateInboundRoute(ctx context.Context, route *dialplanv1.InboundRoute) (int64, error) {
	query := `
		UPDATE inbound_routes SET 
			tenant_id = $2, active_dialplan_id = $3, off_hours_dialplan_id = $4, failsafe_dialplan_id = $5, schedule_id = $6,
			is_maintenance_mode = $7, block_anonymous = $8, default_language_code = $9 
		WHERE phone_number = $1`

	cmdTag, err := r.db.Exec(ctx, query,
		route.PhoneNumber, route.TenantId, route.ActiveDialplanId, route.OffHoursDialplanId, route.FailsafeDialplanId, route.ScheduleId,
		route.IsMaintenanceMode, route.BlockAnonymous, route.DefaultLanguageCode,
	)
	if err != nil {
		return 0, r.handleError(err)
	}
	return cmdTag.RowsAffected(), nil
}

func (r *Repository) DeleteInboundRoute(ctx context.Context, phoneNumber string) (int64, error) {
	cmdTag, err := r.db.Exec(ctx, "DELETE FROM inbound_routes WHERE phone_number = $1", phoneNumber)
	if err != nil {
		return 0, r.handleError(err)
	}
	return cmdTag.RowsAffected(), nil
}

func (r *Repository) ListInboundRoutes(ctx context.Context, tenantID string, pageSize, offset int32) ([]*dialplanv1.InboundRoute, error) {
	baseQuery := "SELECT phone_number, tenant_id, active_dialplan_id FROM inbound_routes"
	args := []interface{}{}
	if tenantID != "" {
		baseQuery += " WHERE tenant_id = $1"
		args = append(args, tenantID)
	}
	dataQuery := baseQuery + fmt.Sprintf(" ORDER BY phone_number ASC LIMIT %d OFFSET %d", pageSize, offset)
	rows, err := r.db.Query(ctx, dataQuery, args...)
	if err != nil {
		return nil, r.handleError(err)
	}
	defer rows.Close()
	var routes []*dialplanv1.InboundRoute
	for rows.Next() {
		var route dialplanv1.InboundRoute
		var activeDP sql.NullString
		if err := rows.Scan(&route.PhoneNumber, &route.TenantId, &activeDP); err != nil {
			return nil, r.handleError(err)
		}
		if activeDP.Valid {
			route.ActiveDialplanId = &activeDP.String
		}
		routes = append(routes, &route)
	}
	return routes, nil
}

func (r *Repository) CountInboundRoutes(ctx context.Context, tenantID string) (int32, error) {
	var totalCount int32
	baseQuery := "SELECT count(*) FROM inbound_routes"
	args := []interface{}{}
	if tenantID != "" {
		baseQuery += " WHERE tenant_id = $1"
		args = append(args, tenantID)
	}
	err := r.db.QueryRow(ctx, baseQuery, args...).Scan(&totalCount)
	return totalCount, r.handleError(err)
}

// --- DIALPLANS ---

func (r *Repository) FindDialplanByID(ctx context.Context, id string) (*dialplanv1.Dialplan, error) {
	var dp dialplanv1.Dialplan
	var actionStr, description, tenantID sql.NullString
	var actionDataBytes []byte

	query := `SELECT id, tenant_id, description, action, action_data FROM dialplans WHERE id = $1`
	err := r.db.QueryRow(ctx, query, id).Scan(&dp.Id, &tenantID, &description, &actionStr, &actionDataBytes)
	if err != nil {
		return nil, r.handleError(err)
	}
	dp.TenantId = tenantID.String
	dp.Description = description.String

	action := &dialplanv1.DialplanAction{}
	if actionStr.Valid {
		action.Action = actionStr.String
		action.Type = dialplan.MapStringToActionType(actionStr.String)
	}
	if actionDataBytes != nil {
		var dataMap map[string]string
		if err := json.Unmarshal(actionDataBytes, &dataMap); err == nil {
			action.ActionData = dataMap
		}
	}
	dp.Action = action
	return &dp, nil
}

func (r *Repository) CreateDialplan(ctx context.Context, dp *dialplanv1.Dialplan, actionDataBytes []byte) error {
	query := `INSERT INTO dialplans (id, tenant_id, description, action, action_data) VALUES ($1, $2, $3, $4, $5)`
	_, err := r.db.Exec(ctx, query, dp.Id, dp.TenantId, dp.Description, dp.GetAction().GetAction(), actionDataBytes)
	return r.handleError(err)
}

func (r *Repository) UpdateDialplan(ctx context.Context, dp *dialplanv1.Dialplan, actionDataBytes []byte) (int64, error) {
	query := `UPDATE dialplans SET tenant_id = $2, description = $3, action = $4, action_data = $5 WHERE id = $1`
	cmdTag, err := r.db.Exec(ctx, query, dp.Id, dp.TenantId, dp.Description, dp.GetAction().GetAction(), actionDataBytes)
	if err != nil {
		return 0, r.handleError(err)
	}
	return cmdTag.RowsAffected(), nil
}

func (r *Repository) DeleteDialplan(ctx context.Context, id string) (int64, error) {
	cmdTag, err := r.db.Exec(ctx, "DELETE FROM dialplans WHERE id = $1", id)
	if err != nil {
		return 0, r.handleError(err)
	}
	return cmdTag.RowsAffected(), nil
}

func (r *Repository) ListDialplans(ctx context.Context, tenantID string, pageSize, offset int32) ([]*dialplanv1.Dialplan, error) {
	baseQuery := "SELECT id, tenant_id, description, action, action_data FROM dialplans"
	args := []interface{}{}
	if tenantID != "" {
		baseQuery += " WHERE tenant_id = $1"
		args = append(args, tenantID)
	}
	dataQuery := baseQuery + fmt.Sprintf(" ORDER BY id ASC LIMIT %d OFFSET %d", pageSize, offset)
	rows, err := r.db.Query(ctx, dataQuery, args...)
	if err != nil {
		return nil, r.handleError(err)
	}
	defer rows.Close()
	var dialplans []*dialplanv1.Dialplan
	for rows.Next() {
		// Basitleştirilmiş tarama, sadece ID dönüyor
		var dp dialplanv1.Dialplan
		var dummyAction, dummyDesc, dummyTenant sql.NullString
		var dummyBytes []byte
		rows.Scan(&dp.Id, &dummyTenant, &dummyDesc, &dummyAction, &dummyBytes)
		dialplans = append(dialplans, &dp)
	}
	return dialplans, nil
}

func (r *Repository) CountDialplans(ctx context.Context, tenantID string) (int32, error) {
	var totalCount int32
	baseQuery := "SELECT count(*) FROM dialplans"
	args := []interface{}{}
	if tenantID != "" {
		baseQuery += " WHERE tenant_id = $1"
		args = append(args, tenantID)
	}
	err := r.db.QueryRow(ctx, baseQuery, args...).Scan(&totalCount)
	return totalCount, r.handleError(err)
}

// --- QUEUES ---

func (r *Repository) CreateQueue(ctx context.Context, q *dialplanv1.Queue) error {
	query := `
		INSERT INTO queues (id, tenant_id, name, routing_strategy, max_wait_time_seconds, fallback_action, is_active) 
		VALUES ($1, $2, $3, $4, $5, $6, $7)`
	_, err := r.db.Exec(ctx, query,
		q.Id, q.TenantId, q.Name, q.RoutingStrategy, q.MaxWaitTimeSeconds, q.FallbackAction, q.IsActive)
	return r.handleError(err)
}

func (r *Repository) GetQueue(ctx context.Context, id string) (*dialplanv1.Queue, error) {
	var q dialplanv1.Queue
	var fallbackAction sql.NullString
	query := `SELECT id, tenant_id, name, routing_strategy, max_wait_time_seconds, fallback_action, is_active FROM queues WHERE id = $1`
	err := r.db.QueryRow(ctx, query, id).Scan(
		&q.Id, &q.TenantId, &q.Name, &q.RoutingStrategy, &q.MaxWaitTimeSeconds, &fallbackAction, &q.IsActive,
	)
	if err != nil {
		return nil, r.handleError(err)
	}
	if fallbackAction.Valid {
		q.FallbackAction = fallbackAction.String
	}
	return &q, nil
}

func (r *Repository) UpdateQueue(ctx context.Context, q *dialplanv1.Queue) (int64, error) {
	query := `UPDATE queues SET name = $2, routing_strategy = $3, max_wait_time_seconds = $4, fallback_action = $5, is_active = $6 WHERE id = $1`
	cmdTag, err := r.db.Exec(ctx, query, q.Id, q.Name, q.RoutingStrategy, q.MaxWaitTimeSeconds, q.FallbackAction, q.IsActive)
	if err != nil {
		return 0, r.handleError(err)
	}
	return cmdTag.RowsAffected(), nil
}

func (r *Repository) DeleteQueue(ctx context.Context, id string) (int64, error) {
	cmdTag, err := r.db.Exec(ctx, "DELETE FROM queues WHERE id = $1", id)
	if err != nil {
		return 0, r.handleError(err)
	}
	return cmdTag.RowsAffected(), nil
}

func (r *Repository) ListQueues(ctx context.Context, tenantID string, pageSize, offset int32) ([]*dialplanv1.Queue, error) {
	baseQuery := "SELECT id, tenant_id, name, routing_strategy, max_wait_time_seconds, fallback_action, is_active FROM queues"
	args := []interface{}{}
	if tenantID != "" {
		baseQuery += " WHERE tenant_id = $1"
		args = append(args, tenantID)
	}
	dataQuery := baseQuery + fmt.Sprintf(" ORDER BY name ASC LIMIT %d OFFSET %d", pageSize, offset)
	rows, err := r.db.Query(ctx, dataQuery, args...)
	if err != nil {
		return nil, r.handleError(err)
	}
	defer rows.Close()
	var queues []*dialplanv1.Queue
	for rows.Next() {
		var q dialplanv1.Queue
		var fa sql.NullString
		if err := rows.Scan(&q.Id, &q.TenantId, &q.Name, &q.RoutingStrategy, &q.MaxWaitTimeSeconds, &fa, &q.IsActive); err != nil {
			return nil, r.handleError(err)
		}
		if fa.Valid {
			q.FallbackAction = fa.String
		}
		queues = append(queues, &q)
	}
	return queues, nil
}

func (r *Repository) CountQueues(ctx context.Context, tenantID string) (int32, error) {
	var totalCount int32
	baseQuery := "SELECT count(*) FROM queues"
	args := []interface{}{}
	if tenantID != "" {
		baseQuery += " WHERE tenant_id = $1"
		args = append(args, tenantID)
	}
	err := r.db.QueryRow(ctx, baseQuery, args...).Scan(&totalCount)
	return totalCount, r.handleError(err)
}

// --- SCHEDULES ---

func (r *Repository) CreateSchedule(ctx context.Context, s *dialplanv1.Schedule) error {
	query := `INSERT INTO schedules (id, tenant_id, name, timezone, schedule_data) VALUES ($1, $2, $3, $4, $5::jsonb)`
	_, err := r.db.Exec(ctx, query, s.Id, s.TenantId, s.Name, s.Timezone, s.ScheduleJson)
	return r.handleError(err)
}

func (r *Repository) GetSchedule(ctx context.Context, id string) (*dialplanv1.Schedule, error) {
	var s dialplanv1.Schedule
	var jsonData []byte
	query := `SELECT id, tenant_id, name, timezone, schedule_data FROM schedules WHERE id = $1`
	err := r.db.QueryRow(ctx, query, id).Scan(&s.Id, &s.TenantId, &s.Name, &s.Timezone, &jsonData)
	if err != nil {
		return nil, r.handleError(err)
	}
	s.ScheduleJson = string(jsonData)
	return &s, nil
}
