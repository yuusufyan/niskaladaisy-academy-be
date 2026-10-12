package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/yuusufyan/go-common/pkg/apperror"
	"github.com/yuusufyan/go-common/pkg/utils"
	"github.com/yuusufyan/go-common/response"

	"github.com/yuusufyan/niskaladaisy-academy-be/internal/config"
	"github.com/yuusufyan/niskaladaisy-academy-be/internal/model"
	"github.com/yuusufyan/niskaladaisy-academy-be/internal/repository"
)

// TokenTypeAccess is the "type" claim of access tokens.
const TokenTypeAccess = "access"

type LoginInput struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

type RefreshInput struct {
	RefreshToken string `json:"refresh_token" validate:"required"`
}

type ChangePasswordInput struct {
	CurrentPassword string `json:"current_password" validate:"required"`
	NewPassword     string `json:"new_password" validate:"required,min=8,max=72"`
}

// ClientMeta is stored with refresh tokens to help users recognise their sessions.
type ClientMeta struct {
	UserAgent string
	IPAddress string
}

type LoginResult struct {
	Tokens *response.Tokens `json:"tokens"`
	User   *model.User      `json:"user"`
}

type AuthService struct {
	cfg    config.JWTConfig
	users  *repository.UserRepository
	tokens *repository.TokenRepository
	access *AccessService
	// dummyHash keeps login timing the same whether or not the email exists.
	dummyHash string
}

func NewAuthService(cfg config.JWTConfig, users *repository.UserRepository, tokens *repository.TokenRepository, access *AccessService) (*AuthService, error) {
	dummy, err := utils.HashPassword(uuid.NewString())
	if err != nil {
		return nil, err
	}
	return &AuthService{cfg: cfg, users: users, tokens: tokens, access: access, dummyHash: dummy}, nil
}

func (s *AuthService) Login(ctx context.Context, in LoginInput, meta ClientMeta) (*LoginResult, error) {
	invalid := apperror.Unauthorized("invalid email or password")

	user, err := s.users.FindByEmail(ctx, strings.TrimSpace(in.Email))
	if err != nil {
		if isNotFound(err) {
			utils.CheckPasswordHash(in.Password, s.dummyHash)
			return nil, invalid
		}
		return nil, err
	}
	if !utils.CheckPasswordHash(in.Password, user.PasswordHash) {
		return nil, invalid
	}
	if !user.IsActive {
		return nil, apperror.Forbidden("user is inactive")
	}

	tokens, err := s.issueTokens(ctx, user.ID, meta)
	if err != nil {
		return nil, err
	}
	if err := s.users.TouchLastLogin(ctx, user.ID, time.Now()); err != nil {
		return nil, err
	}

	user, err = s.users.FindByID(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	return &LoginResult{Tokens: tokens, User: user}, nil
}

// Refresh rotates a refresh token. Reusing a revoked token revokes every session of the user,
// since it means the token was likely stolen.
func (s *AuthService) Refresh(ctx context.Context, in RefreshInput, meta ClientMeta) (*response.Tokens, error) {
	invalid := apperror.Unauthorized("invalid refresh token")
	hash := hashToken(in.RefreshToken)

	session, err := s.tokens.Consume(ctx, hash)
	if errors.Is(err, repository.ErrTokenNotFound) {
		userID, used, err := s.tokens.UsedBy(ctx, hash)
		if err != nil {
			return nil, err
		}
		if used {
			if err := s.tokens.RevokeAllForUser(ctx, userID); err != nil {
				return nil, err
			}
		}
		return nil, invalid
	}
	if err != nil {
		return nil, err
	}

	if _, err := s.access.Get(ctx, session.UserID.String()); err != nil {
		return nil, err
	}
	return s.issueTokens(ctx, session.UserID, meta)
}

// Logout deletes the refresh session. Unknown tokens are ignored so logout always succeeds.
func (s *AuthService) Logout(ctx context.Context, in RefreshInput) error {
	return s.tokens.Delete(ctx, hashToken(in.RefreshToken))
}

// ChangePassword updates the caller's password and signs out every other session.
func (s *AuthService) ChangePassword(ctx context.Context, actor *Access, in ChangePasswordInput) error {
	id := uuid.MustParse(actor.UserID)
	user, err := s.users.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if !utils.CheckPasswordHash(in.CurrentPassword, user.PasswordHash) {
		return apperror.BadRequest("current password is incorrect")
	}
	hash, err := utils.HashPassword(in.NewPassword)
	if err != nil {
		return err
	}
	if err := s.users.Update(ctx, id, map[string]any{"password_hash": hash}); err != nil {
		return err
	}
	return s.tokens.RevokeAllForUser(ctx, id)
}

func (s *AuthService) issueTokens(ctx context.Context, userID uuid.UUID, meta ClientMeta) (*response.Tokens, error) {
	now := time.Now()
	accessExp := now.Add(s.cfg.AccessTTL)
	access, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":  userID.String(),
		"type": TokenTypeAccess,
		"iat":  now.Unix(),
		"exp":  accessExp.Unix(),
		"jti":  uuid.NewString(),
	}).SignedString([]byte(s.cfg.Secret))
	if err != nil {
		return nil, err
	}

	raw, err := randomToken()
	if err != nil {
		return nil, err
	}
	session := repository.RefreshSession{
		UserID:    userID,
		UserAgent: truncate(meta.UserAgent, 255),
		IPAddress: truncate(meta.IPAddress, 64),
		ExpiresAt: now.Add(s.cfg.RefreshTTL),
	}
	if err := s.tokens.Save(ctx, hashToken(raw), session); err != nil {
		return nil, err
	}

	return &response.Tokens{
		Access:  response.TokenExpires{Token: access, Expires: accessExp},
		Refresh: response.TokenExpires{Token: raw, Expires: session.ExpiresAt},
	}, nil
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// hashToken stores refresh tokens as SHA-256 so a Redis dump does not expose live sessions.
func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
