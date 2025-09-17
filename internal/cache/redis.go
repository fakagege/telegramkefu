package cache

import (
	"context"
	"fmt"
	"strconv"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/redis/go-redis/v9"
)

const (
	UsersSetKey     = "telegram_bot_users"
	BlockedUsersSet = "blocked_users" // 新增：用于存储黑名单的 Redis Set Key redis.go 我怎么新增个查看main.go可以查看拉黑的用户列表
)

// RedisClient 封装了 Redis 客户端
type RedisClient struct {
	rdb *redis.Client
}

// NewRedisClient 创建并返回一个新的 RedisClient 实例
func NewRedisClient(addr, password string, db int) (*RedisClient, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		return nil, err
	}

	return &RedisClient{rdb: rdb}, nil
}

// CheckAndAddUser 检查用户是否存在，如果不存在则添加
func (rc *RedisClient) CheckAndAddUser(ctx context.Context, key string, userID int64) {
	rc.rdb.SAdd(ctx, key, strconv.FormatInt(userID, 10))
}

// GetAllUserIDs 获取所有用户ID
func (rc *RedisClient) GetAllUserIDs(ctx context.Context, key string) ([]string, error) {
	return rc.rdb.SMembers(ctx, key).Result()
}

// SetConfigValue 设置配置值
func (rc *RedisClient) SetConfigValue(ctx context.Context, key, value string) error {
	return rc.rdb.Set(ctx, key, value, 0).Err()
}

// GetConfigValue 获取配置值
func (rc *RedisClient) GetConfigValue(ctx context.Context, key string) (string, error) {
	val, err := rc.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", nil // Key 不存在，返回空字符串
	}
	return val, err
}

// AddBlockedUser 将用户添加到黑名单
func (rc *RedisClient) AddBlockedUser(ctx context.Context, userID int64) error {
	return rc.rdb.SAdd(ctx, BlockedUsersSet, strconv.FormatInt(userID, 10)).Err()
}

// RemoveBlockedUser 将用户从黑名单中移除
func (rc *RedisClient) RemoveBlockedUser(ctx context.Context, userID int64) error {
	return rc.rdb.SRem(ctx, BlockedUsersSet, strconv.FormatInt(userID, 10)).Err()
}

// IsUserBlocked 检查用户是否在黑名单中
func (rc *RedisClient) IsUserBlocked(ctx context.Context, userID int64) (bool, error) {
	return rc.rdb.SIsMember(ctx, BlockedUsersSet, strconv.FormatInt(userID, 10)).Result()
}

// GetBlockedUserIDs 获取所有被拉黑的用户ID列表（作为字符串返回，与 GetAllUserIDs 一致）
func (rc *RedisClient) GetBlockedUserIDs(ctx context.Context) ([]string, error) {
	return rc.rdb.SMembers(ctx, BlockedUsersSet).Result()
}

// StoreUserInfo 存储用户的用户名和昵称到 Redis Hash（key: "user:<userID>"）
func (rc *RedisClient) StoreUserInfo(ctx context.Context, user *tgbotapi.User) error {
	if user == nil {
		return nil // 无用户对象，不存储
	}
	key := fmt.Sprintf("user:%d", user.ID)

	// 使用多次 HSet 调用来兼容旧版 Redis
	err := rc.rdb.HSet(ctx, key, "first_name", user.FirstName).Err()
	if err != nil {
		return err
	}
	err = rc.rdb.HSet(ctx, key, "last_name", user.LastName).Err()
	if err != nil {
		return err
	}
	err = rc.rdb.HSet(ctx, key, "username", user.UserName).Err()
	if err != nil {
		return err
	}
	return nil
}

// GetUserInfo 从 Redis Hash 获取用户的用户名和昵称
func (rc *RedisClient) GetUserInfo(ctx context.Context, userID int64) (firstName, lastName, username string, err error) {
	key := fmt.Sprintf("user:%d", userID)
	vals, err := rc.rdb.HMGet(ctx, key, "first_name", "last_name", "username").Result()
	if err != nil {
		return "", "", "", err
	}
	if len(vals) > 0 && vals[0] != nil {
		firstName = vals[0].(string)
	}
	if len(vals) > 1 && vals[1] != nil {
		lastName = vals[1].(string)
	}
	if len(vals) > 2 && vals[2] != nil {
		username = vals[2].(string)
	}
	return firstName, lastName, username, nil
}
