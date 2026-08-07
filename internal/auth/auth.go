// Package auth implements organization-scoped API-key authentication and RBAC.
// Keys are shown once at mint time; only their SHA-256 hash is stored.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"adriane/internal/store"
)

type Role string

const (
	RoleAdmin  Role = "admin"  // all orgs
	RoleOwner  Role = "owner"  // full access to their org
	RoleReader Role = "reader" // read-only in their org
)

// Principal is the authenticated caller.
type Principal struct {
	KeyID   string
	OrgID   string
	Role    Role
	KeyName string
}

func (p *Principal) CanManage() bool {
	return p.Role == RoleAdmin || p.Role == RoleOwner
}

type ctxKey struct{}

func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// PrincipalFromContext returns the caller principal, or a default dev
// principal when none is set (in-process callers such as the eval runner).
func PrincipalFromContext(ctx context.Context) *Principal {
	if p, ok := ctx.Value(ctxKey{}).(*Principal); ok {
		return p
	}
	return &Principal{OrgID: "default", Role: RoleOwner}
}

// Authenticator verifies bearer keys and mints new ones.
type Authenticator struct {
	keys *store.APIKeysRepo
	orgs *store.OrgsRepo
}

func New(keys *store.APIKeysRepo, orgs *store.OrgsRepo) *Authenticator {
	return &Authenticator{keys: keys, orgs: orgs}
}

func hashKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// Authenticate resolves a raw bearer token to a principal.
func (a *Authenticator) Authenticate(ctx context.Context, bearer string) (*Principal, error) {
	if bearer == "" {
		return nil, fmt.Errorf("missing API key")
	}
	k, err := a.keys.GetByHash(ctx, hashKey(bearer))
	if err != nil {
		return nil, fmt.Errorf("invalid API key")
	}
	return &Principal{KeyID: k.ID, OrgID: k.OrgID, Role: Role(k.Role), KeyName: k.Name}, nil
}

// MintKey creates a new key for an org and returns the raw token (shown once).
func (a *Authenticator) MintKey(ctx context.Context, orgID, name string, role Role) (string, error) {
	if role == "" {
		role = RoleOwner
	}
	exists, err := a.orgs.Exists(ctx, orgID)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("organization %s does not exist", orgID)
	}
	raw := "adr_" + randHex(32)
	if err := a.keys.Create(ctx, &store.APIKey{
		ID: newID("key"), OrgID: orgID, Name: name, KeyHash: hashKey(raw), Role: string(role),
	}); err != nil {
		return "", err
	}
	return raw, nil
}

// MintKeyForOrg creates a key for a specific org, creating the org when
// autoCreate is set. Only callers with cross-org privileges (admin) use this.
func (a *Authenticator) MintKeyForOrg(ctx context.Context, orgID, name string, role Role, autoCreate bool) (string, error) {
	if autoCreate {
		exists, err := a.orgs.Exists(ctx, orgID)
		if err != nil {
			return "", err
		}
		if !exists {
			if err := a.orgs.Create(ctx, &store.Organization{ID: orgID, Name: orgID}); err != nil {
				return "", err
			}
		}
	}
	return a.MintKey(ctx, orgID, name, role)
}

// EnsureSeed ensures the default organization and admin key exist. It is
// idempotent: it creates only what is missing, so it is safe to call on every
// control-plane start.
func (a *Authenticator) EnsureSeed(ctx context.Context, adminKey string) error {
	if adminKey == "" {
		adminKey = "adr-dev-admin"
	}
	exists, err := a.orgs.Exists(ctx, "default")
	if err != nil {
		return err
	}
	if !exists {
		if err := a.orgs.Create(ctx, &store.Organization{ID: "default", Name: "Default"}); err != nil {
			return err
		}
	}
	_, err = a.keys.GetByHash(ctx, hashKey(adminKey))
	if err != nil {
		return a.keys.Create(ctx, &store.APIKey{
			ID: newID("key"), OrgID: "default", Name: "admin", KeyHash: hashKey(adminKey), Role: string(RoleAdmin),
		})
	}
	return nil
}

func randHex(n int) string {
	b := make([]byte, n/2+1)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)[:n]
}

func newID(prefix string) string {
	return prefix + "_" + randHex(8)
}
