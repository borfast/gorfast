// Command todo is a small web application that shows how the gorfast pieces
// fit together: Sulis sign-in, encrypted profiles, and transactions.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/borfast/sulis"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/pgdriver"
	_ "modernc.org/sqlite"

	"github.com/borfast/gorfast/auth/bunstore"
	"github.com/borfast/gorfast/crypt"
	"github.com/borfast/gorfast/data"
	"github.com/borfast/gorfast/examples/todo/internal/config"
	"github.com/borfast/gorfast/examples/todo/internal/storage"
	"github.com/borfast/gorfast/examples/todo/internal/todo"
	"github.com/borfast/gorfast/examples/todo/internal/web"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, ".env", os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "todo:", err)
		stop()
		os.Exit(1)
	}
}

func run(ctx context.Context, envFile string, logOut io.Writer) error {
	cfg, err := config.Load(envFile)
	if err != nil {
		return err
	}

	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.Log.Level)); err != nil {
		return fmt.Errorf("log level: %w", err)
	}
	logger := slog.New(slog.NewTextHandler(logOut, &slog.HandlerOptions{Level: level}))

	db, dialect, err := openDatabase(cfg.Database.URL)
	if err != nil {
		return err
	}
	defer db.Close()
	logger.Info("database opened", "dialect", dialect)

	if err := storage.Apply(ctx, db); err != nil {
		return fmt.Errorf("applying migrations: %w", err)
	}

	keyring, err := crypt.Load(ctx, crypt.StaticKeys(cfg.KeySpecs()...))
	if err != nil {
		return fmt.Errorf("loading the encryption keys: %w", err)
	}

	users, sessions, tokens := bunstore.NewUserStore(db), bunstore.NewSessionStore(db), bunstore.NewTokenStore(db)
	auth, err := sulis.New(users, sessions, tokens, sulis.NoSecondFactors{},
		sulis.WithCookieName(web.SessionCookieName),
		sulis.WithEventSink(sulis.NewSlogSink(logger)),
		// Sign-in is password only, so nothing verifies an email address.
		// Real applications should require email verification.
		sulis.WithRequireVerifiedEmail(false),
		// Failure lockout stays off: it lets anyone lock a person out of their
		// account. Turn it on only for a clear reason.
	)
	if err != nil {
		return fmt.Errorf("creating Sulis: %w", err)
	}

	runner := data.NewBunRunner(db, storage.Binder(auth, keyring))
	handler, err := web.New(todo.NewService(runner), auth, logger)
	if err != nil {
		return err
	}

	ln, err := net.Listen("tcp", cfg.Server.Addr)
	if err != nil {
		return fmt.Errorf("listening: %w", err)
	}
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	logger.Info("listening", "addr", ln.Addr().String())

	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()

	select {
	case err := <-served:
		return fmt.Errorf("serving: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutting down: %w", err)
	}
	if err := <-served; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serving: %w", err)
	}
	return nil
}

// openDatabase opens PostgreSQL for a postgres:// URL and a SQLite file otherwise.
// It adds no query hook: Bun inlines arguments, so one would log ciphertext.
func openDatabase(url string) (*bun.DB, string, error) {
	if strings.HasPrefix(url, "postgres://") || strings.HasPrefix(url, "postgresql://") {
		db := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(url))), pgdialect.New())
		return db, "postgres", nil
	}
	sqldb, err := sql.Open("sqlite", url+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, "", fmt.Errorf("opening SQLite: %w", err)
	}
	// Sign-in reads before it writes, so a second connection could fail as busy.
	sqldb.SetMaxOpenConns(1)
	return bun.NewDB(sqldb, sqlitedialect.New()), "sqlite", nil
}
