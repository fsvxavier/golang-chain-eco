package pgtest

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/sijms/go-ora/v2"

	"github.com/fsvxavier/golang-chain-eco/integrations/vectorstore"
	"github.com/fsvxavier/golang-chain-eco/internal/conformance"
)

// ORACLE_DSN=oracle://system:pw@localhost:1521/FREEPDB1 (gvenzl/oracle-free)
func TestOracleVector(t *testing.T) {
	dsn := os.Getenv("ORACLE_DSN")
	if dsn == "" {
		t.Skip("ORACLE_DSN not set")
	}
	d, err := sql.Open("oracle", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.Exec(`DROP TABLE lcg_ora_test PURGE`)
	conformance.Vector(t, &vectorstore.Oracle{DB: d, Table: "lcg_ora_test"})
}
