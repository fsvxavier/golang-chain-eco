// Package pgtest runs the PostgreSQL integrations (pgvector, sqlsaver,
// sqlstore) against a real server. It lives in its own module so the main
// module keeps zero dependencies.
//
//	docker run -d -e POSTGRES_PASSWORD=pw -p 5432:5432 pgvector/pgvector:pg16
//	PG_DSN=postgres://postgres:pw@localhost:5432/postgres go test ./...
package pgtest

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/fsvxavier/golang-chain-eco/integrations/vectorstore"
	"github.com/fsvxavier/golang-chain-eco/internal/conformance"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint/sqlsaver"
	"github.com/fsvxavier/golang-chain-eco/langgraph/store/sqlstore"
)

func db(t *testing.T) *sql.DB {
	dsn := os.Getenv("PG_DSN")
	if dsn == "" {
		t.Skip("PG_DSN not set")
	}
	d, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestPGVector(t *testing.T) {
	d := db(t)
	d.Exec(`DROP TABLE IF EXISTS lcg_vec_test`)
	conformance.Vector(t, &vectorstore.PGVector{DB: d, Table: "lcg_vec_test"})
}

func TestSaver(t *testing.T) {
	s, err := sqlsaver.New(t.Context(), db(t), sqlsaver.Postgres)
	if err != nil {
		t.Fatal(err)
	}
	conformance.Saver(t, s)
}

func TestStore(t *testing.T) {
	s, err := sqlstore.New(t.Context(), db(t), sqlstore.Postgres)
	if err != nil {
		t.Fatal(err)
	}
	conformance.Store(t, s)
}

// COCKROACH_DSN=postgres://root@localhost:26257/defaultdb?sslmode=disable
func TestCockroachDB(t *testing.T) {
	dsn := os.Getenv("COCKROACH_DSN")
	if dsn == "" {
		t.Skip("COCKROACH_DSN not set")
	}
	d, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.Exec(`DROP TABLE IF EXISTS lcg_crdb_test`)
	conformance.Vector(t, &vectorstore.CockroachDB{DB: d, Table: "lcg_crdb_test"})
}
