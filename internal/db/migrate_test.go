package db

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/UTMIST/Mist/internal/db/dbtest"
)

func TestMain(m *testing.M) { dbtest.Main(m) }

func TestMigrateFS(t *testing.T) {
	ctx := context.Background()
	pool, err := Open(ctx, dbtest.URL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	fsys := fstest.MapFS{
		"alpha/0001_one.sql": {Data: []byte("CREATE TABLE one (id int)")},
		"alpha/0002_two.sql": {Data: []byte("CREATE TABLE two (id int)")},
		"beta/0001_ref.sql":  {Data: []byte("CREATE TABLE ref (id int); INSERT INTO one VALUES (1)")},
	}
	owners := []string{"alpha", "beta", "gamma"} // gamma has no directory yet
	if err := migrateFS(ctx, pool, fsys, owners); err != nil {
		t.Fatal(err)
	}
	// Running again is a no-op.
	if err := migrateFS(ctx, pool, fsys, owners); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM one").Scan(&n); err != nil || n != 1 {
		t.Fatalf("beta ran %d times, err=%v", n, err)
	}

	// An applied migration cannot change.
	fsys["alpha/0002_two.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE two (id bigint)")}
	if err := migrateFS(ctx, pool, fsys, owners); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("want changed-migration error, got %v", err)
	}
}

func TestMigrateFSFailureRollsBack(t *testing.T) {
	ctx := context.Background()
	pool, err := Open(ctx, dbtest.URL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	fsys := fstest.MapFS{"alpha/0001_bad.sql": {Data: []byte("CREATE TABLE half (id int); SELECT nope")}}
	if err := migrateFS(ctx, pool, fsys, []string{"alpha"}); err == nil {
		t.Fatal("want error")
	}
	var exists bool
	pool.QueryRow(ctx, "SELECT to_regclass('half') IS NOT NULL").Scan(&exists)
	if exists {
		t.Fatal("failed migration left a table behind")
	}
}

func TestLoadOwnerRejectsBadNames(t *testing.T) {
	for _, name := range []string{"alpha/1_short.sql", "alpha/0001_Upper.sql", "alpha/notes.txt"} {
		fsys := fstest.MapFS{name: {Data: []byte("SELECT 1")}}
		if _, err := loadOwner(fsys, "alpha"); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	dup := fstest.MapFS{
		"alpha/0001_a.sql": {Data: []byte("SELECT 1")},
		"alpha/0001_b.sql": {Data: []byte("SELECT 1")},
	}
	if _, err := loadOwner(dup, "alpha"); err == nil {
		t.Error("duplicate version: want error")
	}
}

func TestEmbeddedMigrationsApply(t *testing.T) {
	ctx := context.Background()
	pool, err := Open(ctx, dbtest.URL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
}
