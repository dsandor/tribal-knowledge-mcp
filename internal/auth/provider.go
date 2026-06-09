package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/dsandor/memory/internal/storage"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/oauth2"
)

// UserInfo holds the resolved identity from either auth path.
type UserInfo struct {
	Email      string
	Name       string
	ExternalID string // OIDC subject claim; empty for local auth
}

// Provider abstracts over OIDC and local auth paths.
type Provider interface {
	// AuthURL returns the OIDC redirect URL. Empty for LocalProvider.
	AuthURL(state string) string
	// Exchange exchanges an OIDC auth code for a UserInfo.
	Exchange(ctx context.Context, code string) (*UserInfo, error)
	// VerifyPassword verifies email+password for local auth.
	VerifyPassword(ctx context.Context, email, password string) (*UserInfo, error)
}

// HashPassword bcrypts a plaintext password for storage.
func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// --- LocalProvider ---

type localUserLookup interface {
	GetUserByEmail(ctx context.Context, email string) (*storage.User, error)
}

// LocalProvider implements Provider using bcrypt local auth.
type LocalProvider struct{ store localUserLookup }

// NewLocalProvider creates a LocalProvider backed by the given store.
func NewLocalProvider(store localUserLookup) *LocalProvider {
	return &LocalProvider{store: store}
}

func (p *LocalProvider) AuthURL(_ string) string { return "" }

func (p *LocalProvider) Exchange(_ context.Context, _ string) (*UserInfo, error) {
	return nil, errors.New("local provider does not support OIDC exchange")
}

func (p *LocalProvider) VerifyPassword(ctx context.Context, email, password string) (*UserInfo, error) {
	u, err := p.store.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, errors.New("invalid credentials")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, errors.New("invalid credentials")
	}
	return &UserInfo{Email: u.Email, Name: u.Name}, nil
}

// --- OIDCProvider ---

// OIDCProvider implements Provider using OIDC authorization code flow.
type OIDCProvider struct {
	provider *oidc.Provider
	config   oauth2.Config
	verifier *oidc.IDTokenVerifier
}

// NewOIDCProvider creates an OIDCProvider by discovering the OIDC provider at issuer.
func NewOIDCProvider(ctx context.Context, issuer, clientID, clientSecret, redirectURL string) (*OIDCProvider, error) {
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc provider: %w", err)
	}
	cfg := oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}
	return &OIDCProvider{
		provider: provider,
		config:   cfg,
		verifier: provider.Verifier(&oidc.Config{ClientID: clientID}),
	}, nil
}

func (p *OIDCProvider) AuthURL(state string) string {
	return p.config.AuthCodeURL(state)
}

func (p *OIDCProvider) Exchange(ctx context.Context, code string) (*UserInfo, error) {
	token, err := p.config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("exchange code: %w", err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return nil, errors.New("no id_token in response")
	}
	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("verify id_token: %w", err)
	}
	var claims struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("parse claims: %w", err)
	}
	return &UserInfo{
		Email:      claims.Email,
		Name:       claims.Name,
		ExternalID: idToken.Subject,
	}, nil
}

func (p *OIDCProvider) VerifyPassword(_ context.Context, _, _ string) (*UserInfo, error) {
	return nil, errors.New("OIDC provider does not support local password auth")
}
