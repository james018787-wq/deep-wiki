package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"time"

	"ai-code-wiki/internal/model"
	"ai-code-wiki/internal/repo"
	"ai-code-wiki/pkg/common"
	"ai-code-wiki/pkg/logger"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// tokenTTL 登录令牌有效期（默认 7 天，AUTH_TOKEN_TTL_DAYS 覆盖）。
const tokenTTLDays = 7

// AuthService 用户登录鉴权服务。
type AuthService struct {
	db       *gorm.DB
	userRepo *repo.UserRepo
}

// NewAuthService 构建鉴权服务，并自动创建默认管理员（user 表为空时）。
func NewAuthService(db *gorm.DB) *AuthService {
	s := &AuthService{db: db, userRepo: repo.NewUserRepo(db)}
	s.ensureAdmin(context.Background())
	go s.cleanupLoop(context.Background())
	return s
}

// ensureAdmin 初始化默认管理员（admin / AUTH_ADMIN_PASSWORD 或 admin123）。
func (s *AuthService) ensureAdmin(ctx context.Context) {
	var count int64
	if err := s.db.Model(&model.User{}).Count(&count).Error; err != nil || count > 0 {
		return
	}
	password := os.Getenv("AUTH_ADMIN_PASSWORD")
	if password == "" {
		password = "admin123"
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		logger.Error(ctx, "[auth] 默认管理员密码哈希失败: %v", err)
		return
	}
	if err := s.userRepo.CreateUser(&model.User{
		Username:     "admin",
		PasswordHash: string(hash),
		Nickname:     "系统管理员",
		Status:       1,
	}); err != nil {
		logger.Error(ctx, "[auth] 创建默认管理员失败: %v", err)
		return
	}
	logger.Warn(ctx, "[auth] 已创建默认管理员 admin（默认密码 admin123，请登录后尽快修改）")
}

// cleanupLoop 定时清理过期令牌。
func (s *AuthService) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n, err := s.userRepo.CleanupExpiredTokens(1000); err == nil && n > 0 {
				logger.Info(context.Background(), "[auth] 清理过期令牌 %d 条", n)
			}
		}
	}
}

// LoginReq 登录入参。
type LoginReq struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// LoginResult 登录返回。
type LoginResult struct {
	Token    string `json:"token"`
	ExpireAt int64  `json:"expire_at"`
	Username string `json:"username"`
	Nickname string `json:"nickname"`
	IsAdmin  bool   `json:"is_admin"`
}

// Login 用户名密码登录：校验 bcrypt 哈希，签发随机令牌并落库。
func (s *AuthService) Login(ctx context.Context, req *LoginReq) (*LoginResult, error) {
	u, err := s.userRepo.GetByUsername(req.Username)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NewError(common.CodeUnauthorized, "用户名或密码错误")
		}
		return nil, common.WrapError(common.CodeInternalError, "查询用户失败", err)
	}
	if u.Status != 1 {
		return nil, common.NewError(common.CodeForbidden, "账号已停用")
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)) != nil {
		return nil, common.NewError(common.CodeUnauthorized, "用户名或密码错误")
	}

	token, err := newTokenHex()
	if err != nil {
		return nil, common.WrapError(common.CodeInternalError, "生成令牌失败", err)
	}
	expireAt := time.Now().Add(tokenTTLDays * 24 * time.Hour)
	if err := s.userRepo.CreateToken(&model.UserToken{Token: token, UserID: u.ID, ExpireAt: expireAt}); err != nil {
		return nil, common.WrapError(common.CodeInternalError, "保存令牌失败", err)
	}

	logger.Info(ctx, "[auth] 登录成功 username=%s user_id=%d", u.Username, u.ID)
	return &LoginResult{
		Token:    token,
		ExpireAt: expireAt.Unix(),
		Username: u.Username,
		Nickname: u.Nickname,
		IsAdmin:  u.Username == "admin",
	}, nil
}

// Logout 登出：使令牌失效。
func (s *AuthService) Logout(ctx context.Context, token string) {
	if token == "" {
		return
	}
	if err := s.userRepo.DeleteToken(token); err != nil {
		logger.Error(ctx, "[auth] 登出失败: %v", err)
	}
}

// Validate 校验令牌，返回当前用户；无效返回错误。
func (s *AuthService) Validate(ctx context.Context, token string) (*model.User, error) {
	if token == "" {
		return nil, common.NewError(common.CodeUnauthorized, "未登录")
	}
	_, u, err := s.userRepo.GetTokenByValue(token)
	if err != nil {
		return nil, common.NewError(common.CodeUnauthorized, "登录已失效，请重新登录")
	}
	return u, nil
}

// newTokenHex 生成 32 字节随机令牌（64 hex 字符）。
func newTokenHex() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
