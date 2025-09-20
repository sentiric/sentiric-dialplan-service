// sentiric-dialplan-service/internal/server/grpc/handler.go
package grpc

import (
	"context"

	"github.com/rs/zerolog"
	dialplanv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/dialplan/v1"
	"google.golang.org/grpc/metadata"
)

// Service arayüzü, handler'ın service katmanından ne beklediğini tanımlar.
type Service interface {
	ResolveDialplan(ctx context.Context, caller, destination string) (*dialplanv1.ResolveDialplanResponse, error)
	CreateInboundRoute(ctx context.Context, route *dialplanv1.InboundRoute) error
	GetInboundRoute(ctx context.Context, phoneNumber string) (*dialplanv1.InboundRoute, error)
	UpdateInboundRoute(ctx context.Context, route *dialplanv1.InboundRoute) error
	DeleteInboundRoute(ctx context.Context, phoneNumber string) error
	ListInboundRoutes(ctx context.Context, req *dialplanv1.ListInboundRoutesRequest) (*dialplanv1.ListInboundRoutesResponse, error)

	CreateDialplan(ctx context.Context, req *dialplanv1.CreateDialplanRequest) error
	GetDialplan(ctx context.Context, id string) (*dialplanv1.Dialplan, error)
	UpdateDialplan(ctx context.Context, req *dialplanv1.UpdateDialplanRequest) error

	DeleteDialplan(ctx context.Context, id string) error
	ListDialplans(ctx context.Context, req *dialplanv1.ListDialplansRequest) (*dialplanv1.ListDialplansResponse, error)
}

type Handler struct {
	dialplanv1.UnimplementedDialplanServiceServer
	svc Service
	log zerolog.Logger
}

func NewHandler(svc Service, log zerolog.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// ResolveDialplan ve InboundRoute handler'ları aynı kalacak
func (h *Handler) ResolveDialplan(ctx context.Context, req *dialplanv1.ResolveDialplanRequest) (*dialplanv1.ResolveDialplanResponse, error) {
	ctx = h.propagateTrace(ctx)
	h.log.Info().Str("method", "ResolveDialplan").Msg("gRPC isteği alındı.")
	return h.svc.ResolveDialplan(ctx, req.GetCallerContactValue(), req.GetDestinationNumber())
}
func (h *Handler) CreateInboundRoute(ctx context.Context, req *dialplanv1.CreateInboundRouteRequest) (*dialplanv1.CreateInboundRouteResponse, error) {
	ctx = h.propagateTrace(ctx)
	err := h.svc.CreateInboundRoute(ctx, req.GetRoute())
	if err != nil {
		return nil, err
	}
	return &dialplanv1.CreateInboundRouteResponse{Route: req.GetRoute()}, nil
}
func (h *Handler) GetInboundRoute(ctx context.Context, req *dialplanv1.GetInboundRouteRequest) (*dialplanv1.GetInboundRouteResponse, error) {
	ctx = h.propagateTrace(ctx)
	route, err := h.svc.GetInboundRoute(ctx, req.GetPhoneNumber())
	if err != nil {
		return nil, err
	}
	return &dialplanv1.GetInboundRouteResponse{Route: route}, nil
}
func (h *Handler) UpdateInboundRoute(ctx context.Context, req *dialplanv1.UpdateInboundRouteRequest) (*dialplanv1.UpdateInboundRouteResponse, error) {
	ctx = h.propagateTrace(ctx)
	err := h.svc.UpdateInboundRoute(ctx, req.GetRoute())
	if err != nil {
		return nil, err
	}
	return &dialplanv1.UpdateInboundRouteResponse{Route: req.GetRoute()}, nil
}
func (h *Handler) DeleteInboundRoute(ctx context.Context, req *dialplanv1.DeleteInboundRouteRequest) (*dialplanv1.DeleteInboundRouteResponse, error) {
	ctx = h.propagateTrace(ctx)
	err := h.svc.DeleteInboundRoute(ctx, req.GetPhoneNumber())
	if err != nil {
		return nil, err
	}
	return &dialplanv1.DeleteInboundRouteResponse{Success: true}, nil
}
func (h *Handler) ListInboundRoutes(ctx context.Context, req *dialplanv1.ListInboundRoutesRequest) (*dialplanv1.ListInboundRoutesResponse, error) {
	ctx = h.propagateTrace(ctx)
	return h.svc.ListInboundRoutes(ctx, req)
}

// --- Dialplan Handlers ---
func (h *Handler) CreateDialplan(ctx context.Context, req *dialplanv1.CreateDialplanRequest) (*dialplanv1.CreateDialplanResponse, error) {
	ctx = h.propagateTrace(ctx)
	err := h.svc.CreateDialplan(ctx, req)
	if err != nil {
		return nil, err
	}
	return &dialplanv1.CreateDialplanResponse{Dialplan: req.GetDialplan()}, nil
}

func (h *Handler) GetDialplan(ctx context.Context, req *dialplanv1.GetDialplanRequest) (*dialplanv1.GetDialplanResponse, error) {
	ctx = h.propagateTrace(ctx)
	dp, err := h.svc.GetDialplan(ctx, req.GetId())
	if err != nil {
		return nil, err
	}
	return &dialplanv1.GetDialplanResponse{Dialplan: dp}, nil
}

func (h *Handler) UpdateDialplan(ctx context.Context, req *dialplanv1.UpdateDialplanRequest) (*dialplanv1.UpdateDialplanResponse, error) {
	ctx = h.propagateTrace(ctx)
	err := h.svc.UpdateDialplan(ctx, req)
	if err != nil {
		return nil, err
	}
	return &dialplanv1.UpdateDialplanResponse{Dialplan: req.GetDialplan()}, nil
}

func (h *Handler) DeleteDialplan(ctx context.Context, req *dialplanv1.DeleteDialplanRequest) (*dialplanv1.DeleteDialplanResponse, error) {

	ctx = h.propagateTrace(ctx)
	err := h.svc.DeleteDialplan(ctx, req.GetId())
	if err != nil {
		return nil, err
	}
	return &dialplanv1.DeleteDialplanResponse{Success: true}, nil
}
func (h *Handler) ListDialplans(ctx context.Context, req *dialplanv1.ListDialplansRequest) (*dialplanv1.ListDialplansResponse, error) {

	ctx = h.propagateTrace(ctx)
	return h.svc.ListDialplans(ctx, req)
}
func (h *Handler) propagateTrace(ctx context.Context) context.Context {

	md, ok := metadata.FromIncomingContext(ctx)
	traceID := "unknown"
	if ok {
		traceIDValues := md.Get("x-trace-id")
		if len(traceIDValues) > 0 {
			traceID = traceIDValues[0]
		}
	}
	return metadata.NewIncomingContext(ctx, metadata.Pairs("x-trace-id", traceID))
}
