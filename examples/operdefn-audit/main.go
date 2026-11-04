package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cedarhillsgroup/go-psprcs"
)

// UserSummary stores aggregated metrics from PSOPRDEFN.
type UserSummary struct {
	TotalUsers     int
	LockedUsers    int
	ActiveUsers    int
	FailedLogins   int
	SymbolicCounts map[string]int
	SampleUsers    []UserRecord
}

// UserRecord represents an individual user row in PSOPRDEFN.
type UserRecord struct {
	OprID       string
	Descr       string
	AcctLock    int
	LastSignon  string
	FailedCount int
	SymbolicID  string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()

	// 1. Parse standard Process Scheduler command line arguments
	params, err := psprcs.ParseFromArgs()
	if err != nil {
		return fmt.Errorf("failed to parse process parameters: %w", err)
	}

	fmt.Printf("go-psprcs: Process Instance: %d, Run Control: %q, DB: %q, Dest: %q\n",
		params.Instance, params.RunControl, params.DBName, params.OutDest)

	var db *sql.DB
	var client *psprcs.Client

	// If running under Process Scheduler with database connection string
	if params.DBName != "" && os.Getenv("PS_DB_CONNECT") != "" {
		driverName := "godror"
		if strings.EqualFold(params.DBType, "MICROSFT") {
			driverName = "sqlserver"
		}
		var err error
		db, err = sql.Open(driverName, os.Getenv("PS_DB_CONNECT"))
		if err != nil {
			return fmt.Errorf("failed to connect to database: %w", err)
		}
		defer db.Close()
	}

	// 2. Initialize API-aware status reporting if database is available and instance > 0
	if db != nil && params.Instance > 0 {
		client = psprcs.NewClient(db, params, "PSOPRAUDIT")
		if err := client.Start(ctx); err != nil {
			return fmt.Errorf("failed to report Start to Process Scheduler: %w", err)
		}
		_ = client.LogMessage(ctx, psprcs.MsgSetProcessScheduler, 35, psprcs.SeverityInfo, "Starting PSOPRDEFN security audit")
	}

	// 3. Query PSOPRDEFN or load demo data if running in standalone/demo mode
	summary, err := fetchUserSummary(ctx, db)
	if err != nil {
		if client != nil {
			_ = client.Fail(ctx, 1, err)
		}
		return fmt.Errorf("audit query failed: %w", err)
	}

	// 4. Generate Markdown report
	markdownContent := generateMarkdownReport(params, summary)

	// 5. Write to output destination (%%OUTDEST%%)
	outputPath := params.OutputPath("operdefn_audit.md")
	if dir := filepath.Dir(outputPath); dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0755)
	}
	if err := os.WriteFile(outputPath, []byte(markdownContent), 0644); err != nil {
		if client != nil {
			_ = client.Fail(ctx, 2, err)
		}
		return fmt.Errorf("failed to write output markdown: %w", err)
	}

	fmt.Printf("Audit report generated successfully at: %s\n", outputPath)

	// 6. Report success to Process Scheduler
	if client != nil {
		_ = client.LogMessage(ctx, psprcs.MsgSetProcessScheduler, 35, psprcs.SeverityInfo,
			fmt.Sprintf("Audited %d operator accounts", summary.TotalUsers))
		if err := client.Success(ctx); err != nil {
			return fmt.Errorf("failed to report Success to Process Scheduler: %w", err)
		}
	}

	return nil
}

// fetchUserSummary queries PSOPRDEFN if a live DB connection is present,
// or returns representative PeopleSoft demo data for testing and offline execution.
func fetchUserSummary(ctx context.Context, db *sql.DB) (*UserSummary, error) {
	if db == nil {
		return loadDemoData(), nil
	}

	query := `
		SELECT OPRID, OPRDEFNDESC, ACCTLOCK, COALESCE(TO_CHAR(LASTSIGNONDTTM, 'YYYY-MM-DD HH24:MI:SS'), 'NEVER'),
		       FAILEDLOGINS, SYMBOLICID
		FROM PSOPRDEFN
		ORDER BY OPRID
	`

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	summary := &UserSummary{
		SymbolicCounts: make(map[string]int),
	}

	for rows.Next() {
		var rec UserRecord
		if err := rows.Scan(&rec.OprID, &rec.Descr, &rec.AcctLock, &rec.LastSignon, &rec.FailedCount, &rec.SymbolicID); err != nil {
			return nil, err
		}

		summary.TotalUsers++
		if rec.AcctLock == 1 {
			summary.LockedUsers++
		} else {
			summary.ActiveUsers++
		}
		if rec.FailedCount > 0 {
			summary.FailedLogins++
		}
		summary.SymbolicCounts[rec.SymbolicID]++

		if len(summary.SampleUsers) < 25 {
			summary.SampleUsers = append(summary.SampleUsers, rec)
		}
	}

	return summary, rows.Err()
}

func loadDemoData() *UserSummary {
	demoUsers := []UserRecord{
		{"PS", "PeopleSoft Administrator", 0, "2026-11-04 06:12:00", 0, "SYSADM1"},
		{"VP1", "Vice President Finance", 0, "2026-11-03 14:22:15", 0, "SYSADM1"},
		{"PTWEBLIB", "Weblib Integration User", 0, "2026-11-01 08:00:00", 0, "SYSADM1"},
		{"HCADMIN", "HCM System Administrator", 0, "2026-10-28 11:45:00", 0, "SYSADM1"},
		{"GUEST", "Default Guest User", 1, "NEVER", 0, "SYSADM1"},
		{"EX_EMPLOYEE1", "Terminated Analyst", 1, "2025-04-12 09:30:00", 3, "SYSADM1"},
		{"EX_EMPLOYEE2", "Terminated Developer", 1, "2024-11-19 16:20:00", 5, "SYSADM1"},
		{"BATCHADM", "Batch Processing Agent", 0, "2026-11-04 05:00:00", 0, "SYSADM1"},
	}

	summary := &UserSummary{
		TotalUsers:     len(demoUsers),
		SymbolicCounts: map[string]int{"SYSADM1": len(demoUsers)},
		SampleUsers:    demoUsers,
	}

	for _, u := range demoUsers {
		if u.AcctLock == 1 {
			summary.LockedUsers++
		} else {
			summary.ActiveUsers++
		}
		if u.FailedCount > 0 {
			summary.FailedLogins++
		}
	}
	return summary
}

func generateMarkdownReport(params *psprcs.Params, s *UserSummary) string {
	var sb strings.Builder

	sb.WriteString("# PeopleSoft Operator Security Audit Report\n\n")
	sb.WriteString("Generated by Cedar Hills Group `go-psprcs` Batch Processor.\n\n")

	sb.WriteString("## Execution Metadata\n\n")
	sb.WriteString(fmt.Sprintf("- **Generated At**: %s UTC\n", time.Now().UTC().Format(time.RFC3339)))
	if params.Instance > 0 {
		sb.WriteString(fmt.Sprintf("- **Process Instance**: %d\n", params.Instance))
	}
	if params.RunControl != "" {
		sb.WriteString(fmt.Sprintf("- **Run Control ID**: `%s`\n", params.RunControl))
	}
	if params.OprID != "" {
		sb.WriteString(fmt.Sprintf("- **Requested By**: `%s`\n", params.OprID))
	}
	if params.DBName != "" {
		sb.WriteString(fmt.Sprintf("- **Database**: `%s` (%s)\n", params.DBName, params.DBType))
	}
	sb.WriteString("\n")

	sb.WriteString("## Summary Metrics\n\n")
	sb.WriteString("| Metric | Count | Percentage |\n")
	sb.WriteString("| --- | --- | --- |\n")
	total := float64(s.TotalUsers)
	if total == 0 {
		total = 1
	}
	sb.WriteString(fmt.Sprintf("| Total Operator Accounts | %d | 100.0%% |\n", s.TotalUsers))
	sb.WriteString(fmt.Sprintf("| Active Accounts | %d | %.1f%% |\n", s.ActiveUsers, (float64(s.ActiveUsers)/total)*100))
	sb.WriteString(fmt.Sprintf("| Locked Accounts (`ACCTLOCK=1`) | %d | %.1f%% |\n", s.LockedUsers, (float64(s.LockedUsers)/total)*100))
	sb.WriteString(fmt.Sprintf("| Accounts with Failed Logins | %d | %.1f%% |\n\n", s.FailedLogins, (float64(s.FailedLogins)/total)*100))

	sb.WriteString("## Symbolic ID Distribution\n\n")
	sb.WriteString("| Symbolic ID | Account Count |\n")
	sb.WriteString("| --- | --- |\n")
	for sym, count := range s.SymbolicCounts {
		sb.WriteString(fmt.Sprintf("| `%s` | %d |\n", sym, count))
	}
	sb.WriteString("\n")

	sb.WriteString("## Sample Operator Detail (`PSOPRDEFN`)\n\n")
	sb.WriteString("| Operator ID | Description | Locked | Last Signon | Failed Logins | Symbolic ID |\n")
	sb.WriteString("| --- | --- | --- | --- | --- | --- |\n")
	for _, u := range s.SampleUsers {
		lockedStr := "No"
		if u.AcctLock == 1 {
			lockedStr = "**YES**"
		}
		sb.WriteString(fmt.Sprintf("| `%s` | %s | %s | %s | %d | `%s` |\n",
			u.OprID, u.Descr, lockedStr, u.LastSignon, u.FailedCount, u.SymbolicID))
	}
	sb.WriteString("\n")

	return sb.String()
}
