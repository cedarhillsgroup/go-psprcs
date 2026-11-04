package psprcs

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// In-memory mock driver for testing database/sql interactions
type mockDriver struct {
	mu      sync.Mutex
	queries []string
	args    [][]driver.Value
}

var testDriver = &mockDriver{}

func init() {
	sql.Register("psprcs_mock", testDriver)
}

func (d *mockDriver) Open(name string) (driver.Conn, error) {
	return &mockConn{driver: d}, nil
}

type mockConn struct {
	driver *mockDriver
}

func (c *mockConn) Prepare(query string) (driver.Stmt, error) {
	return &mockStmt{conn: c, query: query}, nil
}

func (c *mockConn) Close() error { return nil }
func (c *mockConn) Begin() (driver.Tx, error) {
	return &mockTx{}, nil
}

type mockTx struct{}

func (t *mockTx) Commit() error   { return nil }
func (t *mockTx) Rollback() error { return nil }

type mockStmt struct {
	conn  *mockConn
	query string
}

func (s *mockStmt) Close() error { return nil }
func (s *mockStmt) NumInput() int { return -1 }

func (s *mockStmt) Exec(args []driver.Value) (driver.Result, error) {
	s.conn.driver.mu.Lock()
	defer s.conn.driver.mu.Unlock()
	s.conn.driver.queries = append(s.conn.driver.queries, s.query)
	s.conn.driver.args = append(s.conn.driver.args, args)
	return driver.RowsAffected(1), nil
}

func (s *mockStmt) Query(args []driver.Value) (driver.Rows, error) {
	s.conn.driver.mu.Lock()
	defer s.conn.driver.mu.Unlock()
	s.conn.driver.queries = append(s.conn.driver.queries, s.query)
	s.conn.driver.args = append(s.conn.driver.args, args)

	if strings.Contains(s.query, "MAX(MESSAGE_SEQ)") {
		return &mockRows{
			columns: []string{"MAX(MESSAGE_SEQ)"},
			data:    [][]driver.Value{{int64(0)}},
		}, nil
	}
	if strings.Contains(s.query, "FROM PSPRCSRQST") {
		return &mockRows{
			columns: []string{"JOBINSTANCE", "PRCSJOBSEQ", "OUTDESTTYPE", "RUNSTATUS"},
			data:    [][]driver.Value{{int64(10), int64(1), "5", "7"}},
		}, nil
	}
	if strings.Contains(s.query, "FROM PS_CDM_LIST") {
		return &mockRows{
			columns: []string{"PRCSOUTPUTDIR"},
			data:    [][]driver.Value{{"C:\\psft\\log_output\\OTH_OPRAUDIT_12345"}},
		}, nil
	}
	return &mockRows{columns: []string{"col"}, data: nil}, nil
}

type mockRows struct {
	columns []string
	data    [][]driver.Value
	idx     int
}

func (r *mockRows) Columns() []string { return r.columns }
func (r *mockRows) Close() error      { return nil }
func (r *mockRows) Next(dest []driver.Value) error {
	if r.idx >= len(r.data) {
		return io.EOF
	}
	row := r.data[r.idx]
	r.idx++
	for i, val := range row {
		dest[i] = val
	}
	return nil
}

func TestClientLifecycle(t *testing.T) {
	db, err := sql.Open("psprcs_mock", "testdb")
	if err != nil {
		t.Fatalf("failed to open mock db: %v", err)
	}
	defer db.Close()

	testDriver.mu.Lock()
	testDriver.queries = nil
	testDriver.args = nil
	testDriver.mu.Unlock()

	params := &Params{
		Instance:   12345,
		RunControl: "AUDIT_RUN",
		OprID:      "PS",
		DBName:     "CS92DEV",
	}

	client := NewClient(db, params, "GOAUDIT")
	ctx := context.Background()

	// 1. Test Start()
	if err := client.Start(ctx); err != nil {
		t.Fatalf("client.Start() error = %v", err)
	}

	// 2. Test GetJobInfo()
	info, err := client.GetJobInfo(ctx)
	if err != nil {
		t.Fatalf("client.GetJobInfo() error = %v", err)
	}
	if info.JobInstance != 10 || info.JobSequence != 1 {
		t.Errorf("GetJobInfo() = %+v, want JobInstance=10, JobSequence=1", info)
	}

	// 3. Test LogMessage()
	if err := client.LogMessage(ctx, 65, 35, SeverityInfo, "Processed 100 rows", "Batch 1"); err != nil {
		t.Fatalf("client.LogMessage() error = %v", err)
	}

	// 4. Test Success()
	if err := client.Success(ctx); err != nil {
		t.Fatalf("client.Success() error = %v", err)
	}

	testDriver.mu.Lock()
	defer testDriver.mu.Unlock()

	foundStart := false
	foundMsgLog := false
	foundSuccess := false

	for _, q := range testDriver.queries {
		if strings.Contains(q, "UPDATE PSPRCSRQST SET RUNSTATUS = '7'") {
			foundStart = true
		}
		if strings.Contains(q, "INSERT INTO PS_MESSAGE_LOG") {
			foundMsgLog = true
		}
		if strings.Contains(q, "UPDATE PSPRCSRQST SET RUNSTATUS = :1, PRCSRTNCD = :2, CONTINUEJOB = :3") ||
			strings.Contains(q, "UPDATE PSPRCSRQST SET RUNSTATUS = ?, PRCSRTNCD = ?, CONTINUEJOB = ?") {
			foundSuccess = true
		}
	}

	if !foundStart {
		t.Errorf("expected Start query in: %v", testDriver.queries)
	}
	if !foundMsgLog {
		t.Errorf("expected MessageLog query in: %v", testDriver.queries)
	}
	if !foundSuccess {
		t.Errorf("expected Success query in: %v", testDriver.queries)
	}
}

func TestClientFail(t *testing.T) {
	db, err := sql.Open("psprcs_mock", "testdb")
	if err != nil {
		t.Fatalf("failed to open mock db: %v", err)
	}
	defer db.Close()

	testDriver.mu.Lock()
	testDriver.queries = nil
	testDriver.args = nil
	testDriver.mu.Unlock()

	params := &Params{Instance: 99999}
	client := NewClient(db, params, "GOFAIL")

	ctx := context.Background()
	testErr := errors.New("table not found")
	if err := client.Fail(ctx, 10, testErr); err != nil {
		t.Fatalf("client.Fail() error = %v", err)
	}

	testDriver.mu.Lock()
	defer testDriver.mu.Unlock()

	foundFail := false
	for _, args := range testDriver.args {
		for _, a := range args {
			if a == "10" || a == "table not found" {
				foundFail = true
				break
			}
		}
	}
	if !foundFail {
		t.Errorf("expected error status 10 or error message in executed args")
	}
}

func TestClientOutputDir(t *testing.T) {
	db, err := sql.Open("psprcs_mock", "testdb")
	if err != nil {
		t.Fatalf("failed to open mock db: %v", err)
	}
	defer db.Close()

	params := &Params{
		Instance: 12345,
		OutDest:  "User:PS",
		OutType:  OutTypeWeb,
	}
	client := NewClient(db, params, "OPRAUDIT")

	ctx := context.Background()
	dir, err := client.OutputDir(ctx)
	if err != nil {
		t.Fatalf("OutputDir() error = %v", err)
	}
	expectedDir := "C:\\psft\\log_output\\OTH_OPRAUDIT_12345"
	if dir != expectedDir {
		t.Errorf("OutputDir() = %q; want %q", dir, expectedDir)
	}

	// OutputPath should resolve relative to OutputDir
	resolved := client.OutputPath(ctx, "report.md")
	expectedPath := filepath.Join(expectedDir, "report.md")
	if resolved != expectedPath {
		t.Errorf("OutputPath() = %q; want %q", resolved, expectedPath)
	}
}
