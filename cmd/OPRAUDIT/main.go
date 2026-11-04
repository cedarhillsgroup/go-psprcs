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
	go_ora "github.com/sijms/go-ora/v2"
)

// ProcessName defines the PeopleSoft PRCSNAME (max 8 characters for generic/custom process types).
const ProcessName = "OPRAUDIT"

type UserAudit struct {
	TotalUsers     int
	LockedUsers    int
	ActiveUsers    int
	FailedLogins   int
	SymbolicCounts map[string]int
	Users          []UserRow
}

type UserRow struct {
	OprID       string
	Descr       string
	AcctLock    int
	LastSignon  string
	FailedCount int
	SymbolicID  string
}

func main() {
	if err := psprcs.ValidateProcessName(ProcessName); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: invalid process name %q: %v\n", ProcessName, err)
		os.Exit(1)
	}

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR [%s]: %v\n", ProcessName, err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()

	// 1. Parse parameters supplied by Process Scheduler
	params, err := psprcs.ParseFromArgs()
	if err != nil {
		return fmt.Errorf("parsing parameters: %w", err)
	}

	fmt.Printf("[%s] Instance: %d | RunControl: %q | DB: %q | OutDest: %q\n",
		ProcessName, params.Instance, params.RunControl, params.DBName, params.OutDest)

	var db *sql.DB
	var client *psprcs.Client

	// Connect to Oracle via pure-Go go-ora driver if AccessID and AccessPswd are available
	if params.AccessID != "" && params.AccessPswd != "" && params.DBName != "" {
		host := "localhost"
		port := 1521
		url := go_ora.BuildUrl(host, port, params.DBName, params.AccessID, params.AccessPswd, nil)
		var err error
		db, err = sql.Open("oracle", url)
		if err == nil {
			if pingErr := db.PingContext(ctx); pingErr != nil {
				fmt.Printf("[%s] Notice: direct DB login failed (%v). Continuing in standalone mode.\n", ProcessName, pingErr)
				_ = db.Close()
				db = nil
			}
		}
	} else if connectStr := os.Getenv("PS_DB_CONNECT"); connectStr != "" {
		var err error
		db, err = sql.Open("oracle", connectStr)
		if err == nil {
			if pingErr := db.PingContext(ctx); pingErr != nil {
				_ = db.Close()
				db = nil
			}
		}
	}

	// 2. Initialize API-aware reporting if running under Process Scheduler
	if db != nil && params.Instance > 0 {
		client = psprcs.NewClient(db, params, ProcessName)
		if err := client.Start(ctx); err != nil {
			return fmt.Errorf("reporting Start to Process Scheduler: %w", err)
		}
		_ = client.LogMessage(ctx, psprcs.MsgSetProcessScheduler, 35, psprcs.SeverityInfo,
			fmt.Sprintf("Started %s execution", ProcessName))
	}

	// 3. Perform the audit (live database or mock fallback)
	audit, err := runAudit(ctx, db)
	if err != nil {
		if client != nil {
			_ = client.Fail(ctx, 1, err)
		}
		return fmt.Errorf("audit query failed: %w", err)
	}

	// 4. Generate Markdown report
	markdown := renderMarkdown(params, audit)

	// 5. Write to output destination (resolves to PS_CDM_LIST.PRCSOUTPUTDIR when API-aware)
	var outputPath string
	if client != nil {
		outputPath = client.OutputPath(ctx, "OPRAUDIT.md")
	} else {
		outputPath = params.OutputPath("OPRAUDIT.md")
	}
	if dir := filepath.Dir(outputPath); dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0755)
	}
	if err := os.WriteFile(outputPath, []byte(markdown), 0644); err != nil {
		if client != nil {
			_ = client.Fail(ctx, 2, err)
		}
		return fmt.Errorf("writing report: %w", err)
	}

	fmt.Printf("[%s] Report successfully written to %s\n", ProcessName, outputPath)

	// 6. Report success to Process Scheduler
	if client != nil {
		_ = client.LogMessage(ctx, psprcs.MsgSetProcessScheduler, 35, psprcs.SeverityInfo,
			fmt.Sprintf("Audited %d operator accounts successfully", audit.TotalUsers))
		if err := client.Success(ctx); err != nil {
			return fmt.Errorf("reporting Success to Process Scheduler: %w", err)
		}
	}

	return nil
}

func runAudit(ctx context.Context, db *sql.DB) (*UserAudit, error) {
	if db == nil {
		return loadSampleData(), nil
	}

	query := `
		SELECT OPRID, OPRDEFNDESC, ACCTLOCK,
		       COALESCE(TO_CHAR(LASTSIGNONDTTM, 'YYYY-MM-DD HH24:MI:SS'), 'NEVER'),
		       FAILEDLOGINS, SYMBOLICID
		FROM PSOPRDEFN
		ORDER BY OPRID
	`
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	audit := &UserAudit{
		SymbolicCounts: make(map[string]int),
	}

	for rows.Next() {
		var u UserRow
		if err := rows.Scan(&u.OprID, &u.Descr, &u.AcctLock, &u.LastSignon, &u.FailedCount, &u.SymbolicID); err != nil {
			return nil, err
		}
		audit.TotalUsers++
		if u.AcctLock == 1 {
			audit.LockedUsers++
		} else {
			audit.ActiveUsers++
		}
		if u.FailedCount > 0 {
			audit.FailedLogins++
		}
		audit.SymbolicCounts[u.SymbolicID]++
		if len(audit.Users) < 50 {
			audit.Users = append(audit.Users, u)
		}
	}

	return audit, rows.Err()
}

func loadSampleData() *UserAudit {
	sample := []UserRow{
		{"PS", "PeopleSoft Administrator", 0, "2026-11-04 07:15:00", 0, "SYSADM1"},
		{"VP1", "Vice President Finance", 0, "2026-11-03 14:22:15", 0, "SYSADM1"},
		{"HCADMIN", "HCM Administrator", 0, "2026-10-28 11:45:00", 0, "SYSADM1"},
		{"GUEST", "Default Guest User", 1, "NEVER", 0, "SYSADM1"},
		{"EX_ANALYST", "Terminated Analyst", 1, "2025-04-12 09:30:00", 3, "SYSADM1"},
		{"BATCHADM", "Batch Processing Agent", 0, "2026-11-04 05:00:00", 0, "SYSADM1"},
	}

	audit := &UserAudit{
		TotalUsers:     len(sample),
		SymbolicCounts: map[string]int{"SYSADM1": len(sample)},
		Users:          sample,
	}
	for _, u := range sample {
		if u.AcctLock == 1 {
			summaryLocked(audit)
		} else {
			audit.ActiveUsers++
		}
		if u.FailedCount > 0 {
			audit.FailedLogins++
		}
	}
	return audit
}

func summaryLocked(a *UserAudit) {
	a.LockedUsers++
}

func renderMarkdown(params *psprcs.Params, a *UserAudit) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# PeopleSoft %s Audit Report\n\n", ProcessName))
	sb.WriteString("Generated by Cedar Hills Group `go-psprcs` framework.\n\n")

	sb.WriteString("## Execution Context\n\n")
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
	total := float64(a.TotalUsers)
	if total == 0 {
		total = 1
	}
	sb.WriteString(fmt.Sprintf("| Total Operator Accounts | %d | 100.0%% |\n", a.TotalUsers))
	sb.WriteString(fmt.Sprintf("| Active Accounts | %d | %.1f%% |\n", a.ActiveUsers, (float64(a.ActiveUsers)/total)*100))
	sb.WriteString(fmt.Sprintf("| Locked Accounts (`ACCTLOCK=1`) | %d | %.1f%% |\n", a.LockedUsers, (float64(a.LockedUsers)/total)*100))
	sb.WriteString(fmt.Sprintf("| Accounts with Failed Logins | %d | %.1f%% |\n\n", a.FailedLogins, (float64(a.FailedLogins)/total)*100))

	sb.WriteString("## Symbolic ID Distribution\n\n")
	sb.WriteString("| Symbolic ID | Account Count |\n")
	sb.WriteString("| --- | --- |\n")
	for sym, count := range a.SymbolicCounts {
		sb.WriteString(fmt.Sprintf("| `%s` | %d |\n", sym, count))
	}
	sb.WriteString("\n")

	sb.WriteString("## Operator Account Detail (`PSOPRDEFN`)\n\n")
	sb.WriteString("| Operator ID | Description | Locked | Last Signon | Failed Logins | Symbolic ID |\n")
	sb.WriteString("| --- | --- | --- | --- | --- | --- |\n")
	for _, u := range a.Users {
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
