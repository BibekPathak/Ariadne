package store

import (
	"context"
	"time"
)

type Organization struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type User struct {
	ID        string    `json:"id"`
	OrgID     string    `json:"org_id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

type APIKey struct {
	ID        string    `json:"id"`
	OrgID     string    `json:"org_id"`
	Name      string    `json:"name"`
	KeyHash   string    `json:"-"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

type OrgsRepo struct{ pool pool }
type UsersRepo struct{ pool pool }
type APIKeysRepo struct{ pool pool }

func (r *OrgsRepo) Create(ctx context.Context, o *Organization) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO organizations (id, name) VALUES ($1,$2)`, o.ID, o.Name)
	return err
}

func (r *OrgsRepo) Get(ctx context.Context, id string) (*Organization, error) {
	row := r.pool.QueryRow(ctx, `SELECT id, name, created_at FROM organizations WHERE id=$1`, id)
	var o Organization
	if err := row.Scan(&o.ID, &o.Name, &o.CreatedAt); err != nil {
		return nil, err
	}
	return &o, nil
}

func (r *OrgsRepo) Exists(ctx context.Context, id string) (bool, error) {
	var n int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM organizations WHERE id=$1`, id).Scan(&n)
	return n > 0, err
}

func (r *UsersRepo) Create(ctx context.Context, u *User) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO users (id, org_id, email) VALUES ($1,$2,$3)`, u.ID, u.OrgID, u.Email)
	return err
}

func (r *APIKeysRepo) Create(ctx context.Context, k *APIKey) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO api_keys (id, org_id, name, key_hash, role) VALUES ($1,$2,$3,$4,$5)`,
		k.ID, k.OrgID, k.Name, k.KeyHash, k.Role)
	return err
}

func (r *APIKeysRepo) GetByHash(ctx context.Context, hash string) (*APIKey, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, org_id, name, key_hash, role, created_at FROM api_keys WHERE key_hash=$1`, hash)
	var k APIKey
	if err := row.Scan(&k.ID, &k.OrgID, &k.Name, &k.KeyHash, &k.Role, &k.CreatedAt); err != nil {
		return nil, err
	}
	return &k, nil
}

func (r *APIKeysRepo) ListByOrg(ctx context.Context, orgID string) ([]*APIKey, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, org_id, name, key_hash, role, created_at FROM api_keys WHERE org_id=$1 ORDER BY created_at`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*APIKey
	for rows.Next() {
		var k APIKey
		if err := rows.Scan(&k.ID, &k.OrgID, &k.Name, &k.KeyHash, &k.Role, &k.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &k)
	}
	return out, rows.Err()
}

func (r *APIKeysRepo) Count(ctx context.Context) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM api_keys`).Scan(&n)
	return n, err
}
