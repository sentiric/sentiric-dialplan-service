package grpchelper

import (
	"context"
	"time"

	"google.golang.org/grpc"
)

const DefaultTimeout = 3 * time.Second

// CallWithTimeout executes a gRPC call with a default timeout
func CallWithTimeout[T any](ctx context.Context, fn func(ctx context.Context, opts ...grpc.CallOption) (T, error)) (T, error) {
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()
	return fn(ctx)
}
