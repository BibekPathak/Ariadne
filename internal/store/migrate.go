package store

import (
	"errors"
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"adriane/migrations"

	// register the pgx driver with the migrate framework via init().
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
)

func Migrate(databaseURL string) error {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}
	// The pgx migrate driver is registered under the "pgx5" scheme; swap the
	// standard postgres scheme for it.
	migrateURL := "pgx5" + strings.TrimPrefix(databaseURL, "postgres")
	m, err := migrate.NewWithSourceInstance("iofs", src, migrateURL)
	if err != nil {
		return fmt.Errorf("init migrator: %w", err)
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}
