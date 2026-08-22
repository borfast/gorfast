package db

import "database/sql"

// Pool wraps the application's database handle.
type Pool struct {
	DB *sql.DB
}

// Open connects to the database.
func Open() (*Pool, error) {
	handle, err := sql.Open("postgres", "postgres://localhost:5432/shop?sslmode=disable")
	if err != nil {
		return nil, err
	}
	handle.SetMaxOpenConns(10)
	return &Pool{DB: handle}, nil
}

// Close releases the pool.
func (p *Pool) Close() error { return p.DB.Close() }
