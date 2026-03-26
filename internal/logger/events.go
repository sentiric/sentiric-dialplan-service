// sentiric-dialplan-service/internal/logger/events.go
package logger

// SUTS v4.0 Standard Event IDs for dialplan-service
const (
	EventSystemStartup         = "SYSTEM_STARTUP"
	EventSystemShutdown        = "SYSTEM_SHUTDOWN"
	EventDBConnectionFail      = "DB_CONNECTION_FAILED"
	EventRedisConnected        = "REDIS_CONNECTED"
	EventRedisConnectionFail   = "REDIS_CONNECTION_FAILED"
	EventUserSvcConnectionFail = "USER_SVC_CONNECTION_FAILED"
	EventHTTPServerStart       = "HTTP_SERVER_START"
	EventHTTPServerFail        = "HTTP_SERVER_FAILED"
	EventHTTPServerStop        = "HTTP_SERVER_STOPPED"
	EventGRPCServerStart       = "GRPC_SERVER_START"
	EventGRPCServerFail        = "GRPC_SERVER_FAILED"
	EventGRPCServerStop        = "GRPC_SERVER_STOPPED"

	EventGrpcRequest          = "GRPC_REQUEST_RECEIVED"
	EventGrpcInSuccess        = "GRPC_IN_SUCCESS"
	EventGrpcInFail           = "GRPC_IN_FAIL"
	EventDialplanResolveStart = "DIALPLAN_RESOLUTION_START"
	EventDialplanResolveDone  = "DIALPLAN_RESOLUTION_SUCCESS"

	EventRouteNotFound    = "ROUTE_NOT_FOUND"
	EventRouteQueryFailed = "ROUTE_QUERY_FAILED"
	EventAnonymousBlocked = "ANONYMOUS_BLOCKED"
	EventMaintenanceMode  = "MAINTENANCE_MODE_ACTIVE"
	EventFailsafeMissing  = "FAILSAFE_PLAN_MISSING"

	EventScheduleParseError = "SCHEDULE_PARSE_ERROR"
	EventScheduleLoadFailed = "SCHEDULE_LOAD_FAILED"
	EventWorkingHoursActive = "WORKING_HOURS_ACTIVE"
	EventOffHoursActive     = "OFF_HOURS_ACTIVE"

	EventUserCacheHit     = "USER_CACHE_HIT"
	EventUserCacheMiss    = "USER_CACHE_MISS"
	EventUserLookupFailed = "USER_LOOKUP_FAILED"
	EventCacheReadError   = "CACHE_READ_ERROR"
	EventCacheWriteError  = "CACHE_WRITE_ERROR"
	EventCacheParseError  = "CACHE_PARSE_ERROR"
	EventUserCached       = "USER_CACHED"

	EventAutoProvisionStart   = "AUTO_PROVISIONING_STARTED"
	EventAutoProvisionSuccess = "AUTO_PROVISIONING_SUCCESS"
	EventAutoProvisionFail    = "AUTO_PROVISIONING_FAILED"
)
