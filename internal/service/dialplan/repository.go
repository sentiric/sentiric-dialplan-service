// sentiric-dialplan-service/internal/service/dialplan/repository.go
package dialplan

import (
	"context"

	dialplanv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/dialplan/v1"
)

// Repository, dialplan servisinin veritabanı ile etkileşimini tanımlayan arayüzdür.
// YENİLİK: Queue (Kuyruk) ve Schedule (Zamanlama) yönetim metodları eklendi.
type Repository interface {
	// --- Inbound Routes ---
	FindInboundRouteByPhone(ctx context.Context, phoneNumber string) (*dialplanv1.InboundRoute, error)
	CreateInboundRoute(ctx context.Context, route *dialplanv1.InboundRoute) error
	UpdateInboundRoute(ctx context.Context, route *dialplanv1.InboundRoute) (int64, error)
	DeleteInboundRoute(ctx context.Context, phoneNumber string) (int64, error)
	ListInboundRoutes(ctx context.Context, tenantID string, pageSize, offset int32) ([]*dialplanv1.InboundRoute, error)
	CountInboundRoutes(ctx context.Context, tenantID string) (int32, error)

	// --- Dialplans ---
	FindDialplanByID(ctx context.Context, id string) (*dialplanv1.Dialplan, error)
	CreateDialplan(ctx context.Context, dp *dialplanv1.Dialplan, actionDataBytes []byte) error
	UpdateDialplan(ctx context.Context, dp *dialplanv1.Dialplan, actionDataBytes []byte) (int64, error)
	DeleteDialplan(ctx context.Context, id string) (int64, error)
	ListDialplans(ctx context.Context, tenantID string, pageSize, offset int32) ([]*dialplanv1.Dialplan, error)
	CountDialplans(ctx context.Context, tenantID string) (int32, error)

	// --- [YENİ] Queues (ACD Kuyrukları) ---
	CreateQueue(ctx context.Context, q *dialplanv1.Queue) error
	GetQueue(ctx context.Context, id string) (*dialplanv1.Queue, error)
	UpdateQueue(ctx context.Context, q *dialplanv1.Queue) (int64, error)
	DeleteQueue(ctx context.Context, id string) (int64, error)
	ListQueues(ctx context.Context, tenantID string, pageSize, offset int32) ([]*dialplanv1.Queue, error)
	CountQueues(ctx context.Context, tenantID string) (int32, error)

	// --- [YENİ] Schedules (Mesai Saatleri) ---
	CreateSchedule(ctx context.Context, s *dialplanv1.Schedule) error
	GetSchedule(ctx context.Context, id string) (*dialplanv1.Schedule, error)
}
