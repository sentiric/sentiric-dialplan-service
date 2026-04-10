package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	userv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/user/v1"
	"github.com/sentiric/sentiric-dialplan-service/internal/logger"
)

const UserCacheTTL = 5 * time.Minute

type UserCache struct {
	redis *redis.Client
}

func NewUserCache(redisClient *redis.Client) *UserCache {
	return &UserCache{redis: redisClient}
}

func (c *UserCache) GetUser(ctx context.Context, phoneNumber string, log zerolog.Logger) (*userv1.User, error) {
	key := fmt.Sprintf("user:phone:%s", phoneNumber)

	val, err := c.redis.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		log.Error().Err(err).
			Str("event", "CACHE_READ_ERROR").
			Dict("attributes", zerolog.Dict().Str("phone", phoneNumber)).
			Msg("Redis read error")
		return nil, err
	}

	var user userv1.User
	if err := json.Unmarshal([]byte(val), &user); err != nil {
		log.Error().Err(err).
			Str("event", "CACHE_PARSE_ERROR").
			Dict("attributes", zerolog.Dict()).
			Msg("Failed to unmarshal cached user")
		return nil, err
	}

	log.Debug(). // [ARCH-COMPLIANCE] info -> debug
			Str("event", logger.EventUserCacheHit).
			Dict("attributes", zerolog.Dict().Str("phone", phoneNumber)).
			Msg("✅ User Cache HIT")

	return &user, nil
}

func (c *UserCache) SetUser(ctx context.Context, phoneNumber string, user *userv1.User, log zerolog.Logger) error {
	key := fmt.Sprintf("user:phone:%s", phoneNumber)

	data, err := json.Marshal(user)
	if err != nil {
		return fmt.Errorf("failed to marshal user: %w", err)
	}

	if err := c.redis.Set(ctx, key, data, UserCacheTTL).Err(); err != nil {
		log.Error().Err(err).
			Str("event", "CACHE_WRITE_ERROR").
			Dict("attributes", zerolog.Dict()).
			Msg("Failed to write to cache")
		return err
	}

	log.Debug().
		Str("event", "USER_CACHED").
		Dict("attributes", zerolog.Dict().Str("phone", phoneNumber)).
		Msg("User cached successfully")

	return nil
}

func (c *UserCache) InvalidateUser(ctx context.Context, phoneNumber string) error {
	key := fmt.Sprintf("user:phone:%s", phoneNumber)
	return c.redis.Del(ctx, key).Err()
}
