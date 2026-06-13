package db

import (
	"context"
	"database/sql"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	connStr := path + "?_pragma=foreign_keys(1)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=temp_store(MEMORY)" +
		"&_pragma=mmap_size(67108864)" +
		"&_pragma=cache_size(-32000)" +
		"&_busy_timeout=5000"
	d, err := sql.Open("sqlite", connStr)
	if err != nil {
		return nil, err
	}
	d.SetMaxOpenConns(4)
	d.SetMaxIdleConns(2)
	s := &Store{db: d}
	if err := s.migrate(); err != nil {
		_ = d.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) ExecRaw(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return s.db.ExecContext(ctx, query, args...)
}
