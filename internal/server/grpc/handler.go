// sentiric-dialplan-service/internal/server/grpc/handler.go
package grpc

import (
	"context"

	"github.com/rs/zerolog"
	dialplanv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/dialplan/v1"
)

type Service interface {
	ResolveDialplan(ctx context.Context, caller, destination string) (*dialplanv1.ResolveDialplanResponse, error)

	// Routes
	CreateInboundRoute(ctx context.Context, route *dialplanv1.InboundRoute) error
	GetInboundRoute(ctx context.Context, phoneNumber string) (*dialplanv1.InboundRoute, error)
	UpdateInboundRoute(ctx context.Context, route *dialplanv1.InboundRoute) error
	DeleteInboundRoute(ctx context.Context, phoneNumber string) error
	ListInboundRoutes(ctx context.Context, req *dialplanv1.ListInboundRoutesRequest) (*dialplanv1.ListInboundRoutesResponse, error)

	// Dialplans
	CreateDialplan(ctx context.Context, req *dialplanv1.CreateDialplanRequest) error
	GetDialplan(ctx context.Context, id string) (*dialplanv1.Dialplan, error)
	UpdateDialplan(ctx context.Context, req *dialplanv1.UpdateDialplanRequest) error
	DeleteDialplan(ctx context.Context, id string) error
	ListDialplans(ctx context.Context, req *dialplanv1.ListDialplansRequest) (*dialplanv1.ListDialplansResponse, error)

	// [YENİ] Queues
	CreateQueue(ctx context.Context, req *dialplanv1.CreateQueueRequest) error
	GetQueue(ctx context.Context, id string) (*dialplanv1.Queue, error)
	UpdateQueue(ctx context.Context, req *dialplanv1.UpdateQueueRequest) error
	DeleteQueue(ctx context.Context, id string) error
	ListQueues(ctx context.Context, req *dialplanv1.ListQueuesRequest) (*dialplanv1.ListQueuesResponse, error)

	// [YENİ] Schedules
	CreateSchedule(ctx context.Context, req *dialplanv1.CreateScheduleRequest) error
	GetSchedule(ctx context.Context, id string) (*dialplanv1.Schedule, error)
}

type Handler struct {
	dialplanv1.UnimplementedDialplanServiceServer
	svc Service
	log zerolog.Logger
}

func NewHandler(svc Service, log zerolog.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// --- Mevcut Metodlar (Değişiklik Yok) ---
func (h *Handler) ResolveDialplan(ctx context.Context, req *dialplanv1.ResolveDialplanRequest) (*dialplanv1.ResolveDialplanResponse, error) {
	return h.svc.ResolveDialplan(ctx, req.GetCallerContactValue(), req.GetDestinationNumber())
}
func (h *Handler) CreateInboundRoute(ctx context.Context, req *dialplanv1.CreateInboundRouteRequest) (*dialplanv1.CreateInboundRouteResponse, error) {
	if err := h.svc.CreateInboundRoute(ctx, req.GetRoute()); err != nil {
		return nil, err
	}
	return &dialplanv1.CreateInboundRouteResponse{Route: req.GetRoute()}, nil
}

// ... (Diğer InboundRoute metodları aynı) ...
// ... (Diğer Dialplan metodları aynı) ...

// --- [YENİ] Queue Handlers ---
func (h *Handler) CreateQueue(ctx context.Context, req *dialplanv1.CreateQueueRequest) (*dialplanv1.CreateQueueResponse, error) {
	if err := h.svc.CreateQueue(ctx, req); err != nil {
		return nil, err
	}
	return &dialplanv1.CreateQueueResponse{Queue: req.GetQueue()}, nil
}
func (h *Handler) GetQueue(ctx context.Context, req *dialplanv1.GetQueueRequest) (*dialplanv1.GetQueueResponse, error) {
	q, err := h.svc.GetQueue(ctx, req.GetId())
	if err != nil {
		return nil, err
	}
	return &dialplanv1.GetQueueResponse{Queue: q}, nil
}
func (h *Handler) UpdateQueue(ctx context.Context, req *dialplanv1.UpdateQueueRequest) (*dialplanv1.UpdateQueueResponse, error) {
	if err := h.svc.UpdateQueue(ctx, req); err != nil {
		return nil, err
	}
	return &dialplanv1.UpdateQueueResponse{Queue: req.GetQueue()}, nil
}
func (h *Handler) DeleteQueue(ctx context.Context, req *dialplanv1.DeleteQueueRequest) (*dialplanv1.DeleteQueueResponse, error) {
	if err := h.svc.DeleteQueue(ctx, req.GetId()); err != nil {
		return nil, err
	}
	return &dialplanv1.DeleteQueueResponse{Success: true}, nil
}
func (h *Handler) ListQueues(ctx context.Context, req *dialplanv1.ListQueuesRequest) (*dialplanv1.ListQueuesResponse, error) {
	return h.svc.ListQueues(ctx, req)
}

// --- [YENİ] Schedule Handlers ---
func (h *Handler) CreateSchedule(ctx context.Context, req *dialplanv1.CreateScheduleRequest) (*dialplanv1.CreateScheduleResponse, error) {
	if err := h.svc.CreateSchedule(ctx, req); err != nil {
		return nil, err
	}
	return &dialplanv1.CreateScheduleResponse{Schedule: req.GetSchedule()}, nil
}
func (h *Handler) GetSchedule(ctx context.Context, req *dialplanv1.GetScheduleRequest) (*dialplanv1.GetScheduleResponse, error) {
	s, err := h.svc.GetSchedule(ctx, req.GetId())
	if err != nil {
		return nil, err
	}
	return &dialplanv1.GetScheduleResponse{Schedule: s}, nil
}
