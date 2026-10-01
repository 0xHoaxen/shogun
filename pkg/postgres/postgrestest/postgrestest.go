// Package postgrestest starts one postgres:17 testcontainer per test package
// and hands each test its own empty database inside it.
//
// Usage in a package that needs Postgres:
//
//	func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }
//
//	func TestThing(t *testing.T) {
//		url := postgrestest.NewDatabase(t)
//		...
//	}
package postgrestest

import (
	"context"
	"fmt"
	neturl "net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	image          = "postgres:17"
	startTimeout   = 2 * time.Minute
	readyOccurence = 2 // postgres logs "ready" twice: init server, then final server
)

var (
	mu      sync.Mutex
	baseURL string
	counter atomic.Int64
)

// Run starts the shared container, runs the package's tests and terminates the
// container. It returns the exit code for os.Exit. If Docker is unavailable it
// prints the reason and returns 1.
func Run(m *testing.M) int {
	ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
	ctr, err := tcpostgres.Run(ctx, image,
		tcpostgres.WithDatabase("postgres"),
		tcpostgres.WithUsername("shogun"),
		tcpostgres.WithPassword("shogun"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(readyOccurence).WithStartupTimeout(startTimeout)),
	)
	cancel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "postgrestest: start container: %v\n", err)
		return 1
	}
	defer func() {
		if err := testcontainers.TerminateContainer(ctr); err != nil {
			fmt.Fprintf(os.Stderr, "postgrestest: terminate container: %v\n", err)
		}
	}()

	url, err := ctr.ConnectionString(context.Background(), "sslmode=disable")
	if err != nil {
		fmt.Fprintf(os.Stderr, "postgrestest: connection string: %v\n", err)
		return 1
	}
	mu.Lock()
	baseURL = url
	mu.Unlock()

	return m.Run()
}

// NewDatabase creates an empty database in the shared container and returns
// its connection URL. The database is dropped when the test ends. Run must
// have been called from TestMain.
func NewDatabase(t testing.TB) string {
	t.Helper()
	mu.Lock()
	url := baseURL
	mu.Unlock()
	if url == "" {
		t.Fatal("postgrestest: container not started; call postgrestest.Run from TestMain")
	}

	ctx := context.Background()
	admin, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatalf("postgrestest: connect admin: %v", err)
	}
	defer closeConn(t, admin)

	name := fmt.Sprintf("test_%d", counter.Add(1))
	if _, err := admin.Exec(ctx, fmt.Sprintf(`CREATE DATABASE %q`, name)); err != nil {
		t.Fatalf("postgrestest: create database: %v", err)
	}
	t.Cleanup(func() {
		c, err := pgx.Connect(ctx, url)
		if err != nil {
			t.Logf("postgrestest: connect for cleanup: %v", err)
			return
		}
		defer closeConn(t, c)
		if _, err := c.Exec(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS %q WITH (FORCE)`, name)); err != nil {
			t.Logf("postgrestest: drop database: %v", err)
		}
	})

	u, err := neturl.Parse(url)
	if err != nil {
		t.Fatalf("postgrestest: parse url: %v", err)
	}
	u.Path = "/" + name
	return u.String()
}

func closeConn(t testing.TB, c *pgx.Conn) {
	t.Helper()
	if err := c.Close(context.Background()); err != nil {
		t.Logf("postgrestest: close connection: %v", err)
	}
}
