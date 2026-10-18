// Package sqlsaver is a checkpoint.Saver on database/sql, for SQLite and
// PostgreSQL. It has no driver dependency: import the driver you want
// (modernc.org/sqlite, github.com/jackc/pgx/v5/stdlib, ...) and pass the
// *sql.DB.
//
//	db, _ := sql.Open("sqlite", "checkpoints.db")
//	saver, _ := sqlsaver.New(ctx, db, sqlsaver.SQLite)
//
// Values are stored as JSON; register custom state types with
// checkpoint.Register so they survive the round trip.
package sqlsaver

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
)

// Dialect selects SQL placeholder style.
type Dialect int

const (
	SQLite   Dialect = iota // ? placeholders
	Postgres                // $1 placeholders
)

// Saver implements checkpoint.Saver.
type Saver struct {
	db    *sql.DB
	d     Dialect
	table string

	mu   sync.Mutex
	last int64
}

var _ checkpoint.Saver = (*Saver)(nil)

// New creates the saver and its table (checkpoints_go) if missing.
func New(ctx context.Context, db *sql.DB, d Dialect) (*Saver, error) {
	s := &Saver{db: db, d: d, table: "checkpoints_go"}
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS `+s.table+` (
		thread_id TEXT NOT NULL,
		checkpoint_id TEXT NOT NULL,
		seq BIGINT NOT NULL,
		data TEXT NOT NULL,
		PRIMARY KEY (thread_id, checkpoint_id))`)
	if err != nil {
		return nil, fmt.Errorf("sqlsaver: create table: %w", err)
	}
	return s, nil
}

// q rewrites ? placeholders for Postgres.
func (s *Saver) q(query string) string {
	if s.d != Postgres {
		return query
	}
	var b strings.Builder
	n := 0
	for _, r := range query {
		if r == '?' {
			n++
			fmt.Fprintf(&b, "$%d", n)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// nextSeq returns a strictly increasing sequence number (nanosecond clock).
func (s *Saver) nextSeq() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := time.Now().UnixNano()
	if n <= s.last {
		n = s.last + 1
	}
	s.last = n
	return n
}

func (s *Saver) Put(ctx context.Context, cp checkpoint.Checkpoint) (string, error) {
	if cp.ThreadID == "" {
		return "", errors.New("checkpoint: empty thread id")
	}
	if cp.ID == "" {
		cp.ID = messages.NewID()
	}
	b, err := checkpoint.Marshal(cp)
	if err != nil {
		return "", err
	}
	_, err = s.db.ExecContext(ctx, s.q(`INSERT INTO `+s.table+` (thread_id, checkpoint_id, seq, data) VALUES (?, ?, ?, ?)`),
		cp.ThreadID, cp.ID, s.nextSeq(), string(b))
	return cp.ID, err
}

func (s *Saver) Get(ctx context.Context, cfg checkpoint.Config) (checkpoint.Checkpoint, bool, error) {
	var row *sql.Row
	if cfg.CheckpointID == "" {
		row = s.db.QueryRowContext(ctx, s.q(`SELECT data FROM `+s.table+` WHERE thread_id = ? ORDER BY seq DESC LIMIT 1`), cfg.ThreadID)
	} else {
		row = s.db.QueryRowContext(ctx, s.q(`SELECT data FROM `+s.table+` WHERE thread_id = ? AND checkpoint_id = ?`), cfg.ThreadID, cfg.CheckpointID)
	}
	var data string
	switch err := row.Scan(&data); {
	case errors.Is(err, sql.ErrNoRows):
		return checkpoint.Checkpoint{}, false, nil
	case err != nil:
		return checkpoint.Checkpoint{}, false, err
	}
	cp, err := checkpoint.Unmarshal([]byte(data))
	return cp, err == nil, err
}

func (s *Saver) List(ctx context.Context, threadID string, limit int) ([]checkpoint.Checkpoint, error) {
	query := `SELECT data FROM ` + s.table + ` WHERE thread_id = ? ORDER BY seq DESC`
	args := []any{threadID}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, s.q(query), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []checkpoint.Checkpoint
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		cp, err := checkpoint.Unmarshal([]byte(data))
		if err != nil {
			return nil, err
		}
		out = append(out, cp)
	}
	return out, rows.Err()
}

func (s *Saver) DeleteThread(ctx context.Context, threadID string) error {
	_, err := s.db.ExecContext(ctx, s.q(`DELETE FROM `+s.table+` WHERE thread_id = ?`), threadID)
	return err
}
