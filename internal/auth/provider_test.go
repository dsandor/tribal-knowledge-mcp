package auth_test

import (
	"context"
	"testing"

	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/storage"
)

type mockUserStore struct {
	user *storage.User
}

func (m *mockUserStore) GetUserByEmail(_ context.Context, email string) (*storage.User, error) {
	if m.user != nil && m.user.Email == email {
		return m.user, nil
	}
	return nil, storage.ErrNotFound
}

func TestLocalProvider_VerifyPassword_WrongPassword(t *testing.T) {
	hash, err := auth.HashPassword("correct-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	store := &mockUserStore{user: &storage.User{
		Email:        "alice@example.com",
		PasswordHash: hash,
	}}
	p := auth.NewLocalProvider(store)
	_, err = p.VerifyPassword(context.Background(), "alice@example.com", "wrong-password")
	if err == nil {
		t.Fatal("expected error for wrong password")
	}
}

func TestLocalProvider_VerifyPassword_Correct(t *testing.T) {
	hash, err := auth.HashPassword("correct-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	store := &mockUserStore{user: &storage.User{
		ID:           "u1",
		Email:        "alice@example.com",
		Name:         "Alice",
		PasswordHash: hash,
	}}
	p := auth.NewLocalProvider(store)
	info, err := p.VerifyPassword(context.Background(), "alice@example.com", "correct-password")
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if info.Email != "alice@example.com" {
		t.Errorf("Email = %q", info.Email)
	}
}

func TestLocalProvider_AuthURL_ReturnsEmpty(t *testing.T) {
	p := auth.NewLocalProvider(nil)
	if url := p.AuthURL("state"); url != "" {
		t.Errorf("LocalProvider.AuthURL should return empty, got %q", url)
	}
}

func TestLocalProvider_Exchange_ReturnsError(t *testing.T) {
	p := auth.NewLocalProvider(nil)
	_, err := p.Exchange(context.Background(), "some-code")
	if err == nil {
		t.Fatal("LocalProvider.Exchange should return error")
	}
}
