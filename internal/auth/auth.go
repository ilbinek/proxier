// Package auth implements the single-admin login: argon2id password hashing,
// a one-time setup token for first run, DB-backed sessions and login throttling.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"

	"proxier/internal/store"
)

var (
	ErrBadCredentials = errors.New("invalid password")
	ErrBadSetupToken  = errors.New("invalid setup token")
	ErrAlreadySetup   = errors.New("admin already configured")
	ErrRateLimited    = errors.New("too many attempts, try again later")
	ErrWeakPassword   = errors.New("password must be at least 10 characters")
)

const (
	SessionTTL      = 7 * 24 * time.Hour
	MinPasswordLen  = 10
	sessionTokenLen = 32
)

type argonParams struct {
	memory  uint32
	time    uint32
	threads uint8
	keyLen  uint32
}

var defaultParams = argonParams{memory: 64 * 1024, time: 3, threads: 2, keyLen: 32}

// HashPassword returns an encoded argon2id hash.
func HashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	p := defaultParams
	key := argon2.IDKey([]byte(pw), salt, p.time, p.memory, p.threads, p.keyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, p.memory, p.time, p.threads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword checks pw against an encoded argon2id hash.
func VerifyPassword(pw, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var p argonParams
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memory, &p.time, &p.threads); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, p.time, p.memory, p.threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// RandomToken returns a URL-safe random token of n bytes.
func RandomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashToken(tok string) string {
	h := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(h[:])
}

type Service struct {
	store *store.Store

	mu         sync.Mutex
	setupToken string
	attempts   map[string]*attempt
}

type attempt struct {
	failures    int
	first       time.Time
	lockedUntil time.Time
}

func NewService(s *store.Store) *Service {
	return &Service{store: s, attempts: map[string]*attempt{}}
}

// NeedsSetup reports whether no admin password has been set.
func (s *Service) NeedsSetup(ctx context.Context) (bool, error) {
	_, err := s.store.AdminPasswordHash(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return true, nil
	}
	return false, err
}

// SetupToken returns the current one-time setup token, creating one if needed.
// It returns "" when setup is already complete.
func (s *Service) SetupToken(ctx context.Context) (string, error) {
	need, err := s.NeedsSetup(ctx)
	if err != nil || !need {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.setupToken == "" {
		// Short enough to type from a terminal, long enough to not be guessable
		// given the login throttling.
		b := make([]byte, 8) // 64 bits, shown as 16 hex chars
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		s.setupToken = strings.ToUpper(hex.EncodeToString(b))
	}
	return s.setupToken, nil
}

// Setup sets the first admin password. ip is used for throttling.
func (s *Service) Setup(ctx context.Context, token, password, ip string) error {
	if err := s.checkThrottle(ip); err != nil {
		return err
	}
	s.mu.Lock()
	want := s.setupToken
	s.mu.Unlock()
	tok := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(token), "-", ""))
	if want == "" || subtle.ConstantTimeCompare([]byte(tok), []byte(want)) != 1 {
		s.recordFailure(ip)
		return ErrBadSetupToken
	}
	if len(password) < MinPasswordLen {
		return ErrWeakPassword
	}
	h, err := HashPassword(password)
	if err != nil {
		return err
	}
	created, err := s.store.CreateAdminIfMissing(ctx, h)
	if err != nil {
		return err
	}
	if !created {
		return ErrAlreadySetup
	}
	s.mu.Lock()
	s.setupToken = ""
	s.mu.Unlock()
	return nil
}

// Login verifies the password and creates a session, returning its token.
func (s *Service) Login(ctx context.Context, password, ip, userAgent string) (string, error) {
	if err := s.checkThrottle(ip); err != nil {
		return "", err
	}
	h, err := s.store.AdminPasswordHash(ctx)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "", ErrBadCredentials
		}
		return "", err
	}
	if !VerifyPassword(password, h) {
		s.recordFailure(ip)
		return "", ErrBadCredentials
	}
	s.clearFailures(ip)
	return s.NewSession(ctx, ip, userAgent)
}

func (s *Service) NewSession(ctx context.Context, ip, userAgent string) (string, error) {
	tok := RandomToken(sessionTokenLen)
	if len(userAgent) > 300 {
		userAgent = userAgent[:300]
	}
	now := time.Now()
	err := s.store.CreateSession(ctx, &store.Session{
		TokenHash: hashToken(tok), CreatedAt: now.Unix(), ExpiresAt: now.Add(SessionTTL).Unix(), LastIP: ip, UserAgent: userAgent,
	})
	return tok, err
}

// Validate checks a session token and slides its expiry. It returns false for
// unknown or expired sessions.
func (s *Service) Validate(ctx context.Context, tok, ip string) (bool, error) {
	if tok == "" {
		return false, nil
	}
	h := hashToken(tok)
	sess, err := s.store.GetSession(ctx, h)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	now := time.Now()
	if now.Unix() >= sess.ExpiresAt {
		_ = s.store.DeleteSession(ctx, h)
		return false, nil
	}
	// Only write when the session has aged a bit, to avoid a DB write per request.
	if sess.ExpiresAt-now.Unix() < int64((SessionTTL-time.Hour).Seconds()) || sess.LastIP != ip {
		_ = s.store.TouchSession(ctx, h, now.Add(SessionTTL).Unix(), ip)
	}
	return true, nil
}

func (s *Service) Logout(ctx context.Context, tok string) error {
	return s.store.DeleteSession(ctx, hashToken(tok))
}

// ChangePassword verifies the current password, sets a new one and revokes
// every other session.
func (s *Service) ChangePassword(ctx context.Context, current, next, keepToken, ip string) error {
	if err := s.checkThrottle(ip); err != nil {
		return err
	}
	h, err := s.store.AdminPasswordHash(ctx)
	if err != nil {
		return err
	}
	if !VerifyPassword(current, h) {
		s.recordFailure(ip)
		return ErrBadCredentials
	}
	if len(next) < MinPasswordLen {
		return ErrWeakPassword
	}
	nh, err := HashPassword(next)
	if err != nil {
		return err
	}
	if err := s.store.SetAdminPasswordHash(ctx, nh); err != nil {
		return err
	}
	return s.store.DeleteSessionsExcept(ctx, hashToken(keepToken))
}

// ResetPassword sets the password without verification and revokes all sessions (CLI).
func ResetPassword(ctx context.Context, st *store.Store, pw string) error {
	if len(pw) < MinPasswordLen {
		return ErrWeakPassword
	}
	h, err := HashPassword(pw)
	if err != nil {
		return err
	}
	if err := st.SetAdminPasswordHash(ctx, h); err != nil {
		return err
	}
	return st.DeleteSessionsExcept(ctx, "")
}

// Throttling: 5 failures within 15 minutes lock the IP out; each further
// failure doubles the lockout (30s, 60s, ... capped at 15 minutes).
const (
	maxFailures   = 5
	failureWindow = 15 * time.Minute
	maxLockout    = 15 * time.Minute
)

func (s *Service) checkThrottle(ip string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.attempts[ip]
	if a != nil && time.Now().Before(a.lockedUntil) {
		return ErrRateLimited
	}
	return nil
}

func (s *Service) recordFailure(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if len(s.attempts) > 10000 {
		for k, a := range s.attempts {
			if now.Sub(a.first) > failureWindow && now.After(a.lockedUntil) {
				delete(s.attempts, k)
			}
		}
	}
	a := s.attempts[ip]
	if a == nil || (now.Sub(a.first) > failureWindow && now.After(a.lockedUntil)) {
		a = &attempt{first: now}
		s.attempts[ip] = a
	}
	a.failures++
	if a.failures >= maxFailures {
		lock := 30 * time.Second << min(a.failures-maxFailures, 5)
		a.lockedUntil = now.Add(min(lock, maxLockout))
	}
}

func (s *Service) clearFailures(ip string) {
	s.mu.Lock()
	delete(s.attempts, ip)
	s.mu.Unlock()
}
