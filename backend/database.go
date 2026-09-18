package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// The one-row JSON document keeps the file and Postgres storage modes on the
// same schema. The server runs as a single instance and serializes writes.
func newDatabaseServer(databaseURL string) (*server, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(2)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	_, err = db.Exec("CREATE TABLE IF NOT EXISTS sequencer_state (id integer PRIMARY KEY, payload jsonb NOT NULL)")
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("create state table: %w", err)
	}
	s := &server{db: db, clients: make(map[chan struct{}]struct{})}
	var raw []byte
	err = db.QueryRow("SELECT payload FROM sequencer_state WHERE id = 1").Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		s.data = seed(time.Now().UnixMilli())
		if err := s.saveLocked(); err != nil {
			db.Close()
			return nil, err
		}
		return s, nil
	}
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("read database state: %w", err)
	}
	if err := json.Unmarshal(raw, &s.data); err != nil {
		db.Close()
		return nil, fmt.Errorf("decode database state: %w", err)
	}
	if s.data.CycleStartedAt == 0 {
		db.Close()
		return nil, errors.New("database state has no cycle start")
	}
	s.finishSyncLocked(time.Now().UnixMilli())
	return s, nil
}
