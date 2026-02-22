// sentiric-dialplan-service/internal/logger/events.go
package logger

// SUTS v4.0 Standard Event IDs for dialplan-service
const (
	EventSystemStartup        = "SYSTEM_STARTUP"
	EventGrpcRequest          = "GRPC_REQUEST_RECEIVED"
	EventDialplanResolveStart = "DIALPLAN_RESOLUTION_START"
	EventDialplanResolveDone  = "DIALPLAN_RESOLUTION_SUCCESS"
	EventRouteNotFound        = "ROUTE_NOT_FOUND"
	EventMaintenanceMode      = "MAINTENANCE_MODE_ACTIVE"
	EventUserCacheHit         = "USER_CACHE_HIT"
	EventUserCacheMiss        = "USER_CACHE_MISS"
	EventUserLookupFailed     = "USER_LOOKUP_FAILED"
	EventCacheReadError       = "CACHE_READ_ERROR"
	EventCacheWriteError      = "CACHE_WRITE_ERROR"
	EventCacheParseError      = "CACHE_PARSE_ERROR"
	EventUserCached           = "USER_CACHED"
)
