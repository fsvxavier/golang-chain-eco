package pgtest

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/go-sql-driver/mysql"

	"github.com/fsvxavier/golang-chain-eco/integrations/vectorstore"
	"github.com/fsvxavier/golang-chain-eco/internal/conformance"
)

// MARIADB_DSN=root:pw@tcp(localhost:3306)/app (MariaDB 11.7+ for VECTOR)
func TestMariaDBVector(t *testing.T) {
	dsn := os.Getenv("MARIADB_DSN")
	if dsn == "" {
		t.Skip("MARIADB_DSN not set")
	}
	d, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.Exec(`DROP TABLE IF EXISTS lcg_maria_test`)
	conformance.Vector(t, &vectorstore.MariaDB{DB: d, Table: "lcg_maria_test"})
}
