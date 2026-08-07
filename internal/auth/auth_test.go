package auth

import (
	"context"
	"os"
	"testing"

	"adriane/internal/store"
)

// These tests need a reachable Postgres. Run with TEST_DATABASE_URL set, e.g.
//
//	TEST_DATABASE_URL=postgres://kubeai:kubeai@localhost:5432/kubeai?sslmode=disable go test ./internal/auth/ -v
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to run auth integration tests")
	}
	st, err := store.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	return st
}

func TestMintAndAuthenticate(t *testing.T) {
	st := newTestStore(t)
	a := New(st.APIKeys, st.Orgs)
	ctx := context.Background()

	_ = st.Orgs.Create(ctx, &store.Organization{ID: "acme", Name: "Acme"})
	raw, err := a.MintKey(ctx, "acme", "ci", RoleOwner)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 10 || raw[:4] != "adr_" {
		t.Fatalf("unexpected key format %q", raw)
	}
	p, err := a.Authenticate(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	if p.OrgID != "acme" || p.Role != RoleOwner {
		t.Fatalf("unexpected principal %+v", p)
	}
	if _, err := a.Authenticate(ctx, "adr_wrong"); err == nil {
		t.Fatal("expected failure for unknown key")
	}
}

func TestEnsureSeedCreatesAdminKey(t *testing.T) {
	st := newTestStore(t)
	a := New(st.APIKeys, st.Orgs)
	ctx := context.Background()

	if _, err := st.Pool().Exec(ctx, `TRUNCATE api_keys, users, organizations CASCADE`); err != nil {
		t.Fatal(err)
	}
	if err := a.EnsureSeed(ctx, "adr-test-admin"); err != nil {
		t.Fatal(err)
	}
	p, err := a.Authenticate(ctx, "adr-test-admin")
	if err != nil {
		t.Fatal(err)
	}
	if p.Role != RoleAdmin {
		t.Fatalf("expected admin role, got %s", p.Role)
	}
}

func TestPrincipalRBAC(t *testing.T) {
	if !(&Principal{Role: RoleAdmin}).CanManage() {
		t.Fatal("admin should manage")
	}
	if !(&Principal{Role: RoleOwner}).CanManage() {
		t.Fatal("owner should manage")
	}
	if (&Principal{Role: RoleReader}).CanManage() {
		t.Fatal("reader should not manage")
	}
}
