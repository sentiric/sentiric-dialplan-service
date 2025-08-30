// File: sentiric-dialplan-service/internal/repository/postgres/repository.go
package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	dialplanv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/dialplan/v1"
)

type Repository struct {
	db  *pgxpool.Pool
	log zerolog.Logger
}

func NewRepository(db *pgxpool.Pool, log zerolog.Logger) *Repository {
	return &Repository{db: db, log: log}
}

// --- InboundRoute Metodları ---

func (r *Repository) FindInboundRouteByPhone(ctx context.Context, phoneNumber string) (*dialplanv1.InboundRoute, error) {
	var route dialplanv1.InboundRoute
	var activeDP, offHoursDP, failsafeDP sql.NullString

	query := `SELECT phone_number, tenant_id, active_dialplan_id, off_hours_dialplan_id, failsafe_dialplan_id, is_maintenance_mode, default_language_code FROM inbound_routes WHERE phone_number = $1`
	err := r.db.QueryRow(ctx, query, phoneNumber).Scan(
		&route.PhoneNumber, &route.TenantId, &activeDP, &offHoursDP, &failsafeDP,
		&route.IsMaintenanceMode, &route.DefaultLanguageCode,
	)
	if err != nil {
		return nil, err
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

	return &route, nil
}

func (r *Repository) CreateInboundRoute(ctx context.Context, route *dialplanv1.InboundRoute) error {
	// Sorgudan sip_trunk_id'yi çıkardık, veritabanı varsayılan değeri kullanacak.
	query := `INSERT INTO inbound_routes (phone_number, tenant_id, active_dialplan_id, off_hours_dialplan_id, failsafe_dialplan_id, is_maintenance_mode, default_language_code) VALUES ($1, $2, $3, $4, $5, $6, $7)`
	_, err := r.db.Exec(ctx, query,
		route.PhoneNumber,
		route.TenantId,
		route.ActiveDialplanId,
		route.OffHoursDialplanId,
		route.FailsafeDialplanId,
		route.IsMaintenanceMode,
		route.DefaultLanguageCode,
	)
	return err
}

func (r *Repository) UpdateInboundRoute(ctx context.Context, route *dialplanv1.InboundRoute) (int64, error) {
	query := `UPDATE inbound_routes SET tenant_id = $2, active_dialplan_id = $3, off_hours_dialplan_id = $4, failsafe_dialplan_id = $5, is_maintenance_mode = $6, default_language_code = $7 WHERE phone_number = $1`
	cmdTag, err := r.db.Exec(ctx, query,
		route.PhoneNumber, route.TenantId, route.ActiveDialplanId,
		route.OffHoursDialplanId, route.FailsafeDialplanId,
		route.IsMaintenanceMode, route.DefaultLanguageCode,
	)
	return cmdTag.RowsAffected(), err
}

func (r *Repository) DeleteInboundRoute(ctx context.Context, phoneNumber string) (int64, error) {
	cmdTag, err := r.db.Exec(ctx, "DELETE FROM inbound_routes WHERE phone_number = $1", phoneNumber)
	return cmdTag.RowsAffected(), err
}

func (r *Repository) ListInboundRoutes(ctx context.Context, tenantID string, pageSize, offset int32) ([]*dialplanv1.InboundRoute, error) {
	baseQuery := "SELECT phone_number, tenant_id, active_dialplan_id, off_hours_dialplan_id, failsafe_dialplan_id, is_maintenance_mode, default_language_code FROM inbound_routes"
	args := []interface{}{}
	if tenantID != "" {
		baseQuery += " WHERE tenant_id = $1"
		args = append(args, tenantID)
	}
	dataQuery := baseQuery + fmt.Sprintf(" ORDER BY phone_number LIMIT %d OFFSET %d", pageSize, offset)

	rows, err := r.db.Query(ctx, dataQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var routes []*dialplanv1.InboundRoute
	for rows.Next() {
		var route dialplanv1.InboundRoute
		var activeDP, offHoursDP, failsafeDP sql.NullString
		if err := rows.Scan(&route.PhoneNumber, &route.TenantId, &activeDP, &offHoursDP, &failsafeDP, &route.IsMaintenanceMode, &route.DefaultLanguageCode); err != nil {
			return nil, err
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
	return totalCount, err
}

// --- Dialplan Metodları ---

func (r *Repository) FindDialplanByID(ctx context.Context, id string) (*dialplanv1.Dialplan, error) {
	var dp dialplanv1.Dialplan
	var action dialplanv1.DialplanAction
	var actionData dialplanv1.ActionData
	var actionStr, description, tenantID sql.NullString
	var actionDataBytes []byte

	query := `SELECT id, tenant_id, description, action, action_data FROM dialplans WHERE id = $1`
	err := r.db.QueryRow(ctx, query, id).Scan(&dp.Id, &tenantID, &description, &actionStr, &actionDataBytes)
	if err != nil {
		return nil, err
	}
	dp.TenantId = tenantID.String
	dp.Description = description.String

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
	return &dp, nil
}

func (r *Repository) CreateDialplan(ctx context.Context, dp *dialplanv1.Dialplan, actionDataBytes []byte) error {
	query := `INSERT INTO dialplans (id, tenant_id, description, action, action_data) VALUES ($1, $2, $3, $4, $5)`
	_, err := r.db.Exec(ctx, query, dp.Id, dp.TenantId, dp.Description, dp.GetAction().GetAction(), actionDataBytes)
	return err
}

func (r *Repository) UpdateDialplan(ctx context.Context, dp *dialplanv1.Dialplan, actionDataBytes []byte) (int64, error) {
	query := `UPDATE dialplans SET tenant_id = $2, description = $3, action = $4, action_data = $5 WHERE id = $1`
	cmdTag, err := r.db.Exec(ctx, query, dp.Id, dp.TenantId, dp.Description, dp.GetAction().GetAction(), actionDataBytes)
	return cmdTag.RowsAffected(), err
}

func (r *Repository) DeleteDialplan(ctx context.Context, id string) (int64, error) {
	cmdTag, err := r.db.Exec(ctx, "DELETE FROM dialplans WHERE id = $1", id)
	return cmdTag.RowsAffected(), err
}

func (r *Repository) ListDialplans(ctx context.Context, tenantID string, pageSize, offset int32) ([]*dialplanv1.Dialplan, error) {
	baseQuery := "SELECT id, tenant_id, description, action, action_data FROM dialplans"
	args := []interface{}{}
	if tenantID != "" {
		baseQuery += " WHERE tenant_id = $1"
		args = append(args, tenantID)
	}
	dataQuery := baseQuery + fmt.Sprintf(" ORDER BY id LIMIT %d OFFSET %d", pageSize, offset)

	rows, err := r.db.Query(ctx, dataQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dialplans []*dialplanv1.Dialplan
	for rows.Next() {
		dp, err := r.scanDialplan(rows)
		if err != nil {
			return nil, err
		}
		dialplans = append(dialplans, dp)
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
	return totalCount, err
}

// scanDialplan, bir satırdan Dialplan nesnesi oluşturmak için yardımcı bir fonksiyondur.
func (r *Repository) scanDialplan(row pgx.Row) (*dialplanv1.Dialplan, error) {
	var dp dialplanv1.Dialplan
	var action dialplanv1.DialplanAction
	var actionData dialplanv1.ActionData
	var actionStr, description, tenantID sql.NullString
	var actionDataBytes []byte

	if err := row.Scan(&dp.Id, &tenantID, &description, &actionStr, &actionDataBytes); err != nil {
		return nil, err
	}

	dp.TenantId = tenantID.String
	dp.Description = description.String
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
	return &dp, nil
}
