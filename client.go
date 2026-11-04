package psprcs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DB represents the minimal SQL interface required by Client.
// It is implemented by *sql.DB, *sql.Tx, and *sql.Conn.
type DB interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// JobInfo holds runtime scheduling context queried from PSPRCSRQST.
type JobInfo struct {
	ProcessInstance int64  `json:"process_instance"`
	JobInstance     int64  `json:"job_instance"`
	JobSequence     int    `json:"job_sequence"`
	OutDestType     string `json:"out_dest_type"`
	RunStatus       string `json:"run_status"`
}

// Client coordinates API-aware status reporting and message logging
// back to PeopleSoft Process Scheduler database tables.
type Client struct {
	db          DB
	params      *Params
	programName string
	jobID       string

	mu        sync.Mutex
	lastSeq   int
	outputDir string
}

// NewClient constructs an API-aware Process Scheduler client.
func NewClient(db DB, params *Params, programName string) *Client {
	prog := strings.TrimSpace(programName)
	if prog == "" {
		prog = "PSPRCS"
	}
	return &Client{
		db:          db,
		params:      params,
		programName: prog,
		jobID:       prog,
	}
}

// SetJobID overrides the Job ID recorded in PS_MESSAGE_LOG (defaults to programName).
func (c *Client) SetJobID(jobID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.jobID = jobID
}

// Start marks the process as actively executing (RUNSTATUS = '7' / Processing)
// and updates the begin timestamp in both PSPRCSRQST and PSPRCSQUE.
func (c *Client) Start(ctx context.Context) error {
	if c.params.Instance <= 0 {
		return errors.New("cannot start process without a valid positive process instance")
	}

	now := time.Now().UTC()

	// Update PSPRCSRQST
	queryRqst := `UPDATE PSPRCSRQST SET RUNSTATUS = '7', BEGINDTTM = :1 WHERE PRCSINSTANCE = :2`
	if _, err := c.db.ExecContext(ctx, queryRqst, now, c.params.Instance); err != nil {
		// Fallback for drivers using '?' placeholders (e.g. SQLite / MySQL)
		altQuery := `UPDATE PSPRCSRQST SET RUNSTATUS = '7', BEGINDTTM = ? WHERE PRCSINSTANCE = ?`
		if _, err2 := c.db.ExecContext(ctx, altQuery, now, c.params.Instance); err2 != nil {
			return fmt.Errorf("failed to update PSPRCSRQST to Processing: %w", err)
		}
	}

	// Update PSPRCSQUE (may have 0 rows if already transitioned, so ignore sql.ErrNoRows)
	queryQue := `UPDATE PSPRCSQUE SET RUNSTATUS = '7' WHERE PRCSINSTANCE = :1`
	if _, err := c.db.ExecContext(ctx, queryQue, c.params.Instance); err != nil {
		altQue := `UPDATE PSPRCSQUE SET RUNSTATUS = '7' WHERE PRCSINSTANCE = ?`
		_ = c.execIgnoreErr(ctx, altQue, c.params.Instance)
	}

	return nil
}

// GetJobInfo queries the execution context (Job Instance, Job Sequence, Output Type) from PSPRCSRQST.
func (c *Client) GetJobInfo(ctx context.Context) (*JobInfo, error) {
	if c.params.Instance <= 0 {
		return nil, errors.New("cannot get job info without a valid positive process instance")
	}

	info := &JobInfo{ProcessInstance: c.params.Instance}
	query := `SELECT COALESCE(JOBINSTANCE, 0), COALESCE(PRCSJOBSEQ, 0), COALESCE(OUTDESTTYPE, ''), COALESCE(RUNSTATUS, '') FROM PSPRCSRQST WHERE PRCSINSTANCE = :1`

	err := c.db.QueryRowContext(ctx, query, c.params.Instance).Scan(&info.JobInstance, &info.JobSequence, &info.OutDestType, &info.RunStatus)
	if err != nil {
		altQuery := `SELECT COALESCE(JOBINSTANCE, 0), COALESCE(PRCSJOBSEQ, 0), COALESCE(OUTDESTTYPE, ''), COALESCE(RUNSTATUS, '') FROM PSPRCSRQST WHERE PRCSINSTANCE = ?`
		if err2 := c.db.QueryRowContext(ctx, altQuery, c.params.Instance).Scan(&info.JobInstance, &info.JobSequence, &info.OutDestType, &info.RunStatus); err2 != nil {
			return nil, fmt.Errorf("failed to query PSPRCSRQST for instance %d: %w", c.params.Instance, err)
		}
	}

	return info, nil
}

// OutputDir queries the dedicated instance directory from PS_CDM_LIST.PRCSOUTPUTDIR.
// In PeopleSoft Process Scheduler, each process instance is provisioned with a temporary
// directory (e.g. %PS_SERVDIR%\log_output\OTH_<PRCSNAME>_<INSTANCE>). Any files written
// to this directory are automatically transferred by PSDSTSRV to the Report Repository
// and displayed under "View Log/Trace" in Process Monitor.
func (c *Client) OutputDir(ctx context.Context) (string, error) {
	c.mu.Lock()
	if c.outputDir != "" {
		dir := c.outputDir
		c.mu.Unlock()
		return dir, nil
	}
	c.mu.Unlock()

	if c.params.Instance <= 0 {
		return "", errors.New("cannot determine output dir without a valid positive process instance")
	}

	query := `SELECT PRCSOUTPUTDIR FROM PS_CDM_LIST WHERE PRCSINSTANCE = :1`
	var dir string
	err := c.db.QueryRowContext(ctx, query, c.params.Instance).Scan(&dir)
	if err != nil {
		altQuery := `SELECT PRCSOUTPUTDIR FROM PS_CDM_LIST WHERE PRCSINSTANCE = ?`
		if err2 := c.db.QueryRowContext(ctx, altQuery, c.params.Instance).Scan(&dir); err2 != nil {
			return "", fmt.Errorf("failed to query PS_CDM_LIST for instance %d: %w", c.params.Instance, err)
		}
	}

	dir = strings.TrimSpace(dir)
	c.mu.Lock()
	c.outputDir = dir
	c.mu.Unlock()
	return dir, nil
}

// OutputPath resolves a relative file name into the process instance's designated output directory
// (from PS_CDM_LIST.PRCSOUTPUTDIR). If PS_CDM_LIST is not available or has an empty directory,
// it falls back to Params.OutputPath(elem...).
func (c *Client) OutputPath(ctx context.Context, elem ...string) string {
	if outDir, err := c.OutputDir(ctx); err == nil && outDir != "" {
		return filepath.Join(outDir, filepath.Join(elem...))
	}
	return c.params.OutputPath(elem...)
}

// LogMessage inserts a structured entry into PS_MESSAGE_LOG and parameter rows in PS_MESSAGE_LOGPARM.
// Messages appear directly in the Process Monitor "Message Log" viewer.
func (c *Client) LogMessage(ctx context.Context, setNbr, msgNbr, severity int, parms ...string) error {
	if c.params.Instance <= 0 {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Initialize message sequence from database if not yet cached
	if c.lastSeq == 0 {
		var maxSeq sql.NullInt64
		seqQuery := `SELECT MAX(MESSAGE_SEQ) FROM PS_MESSAGE_LOG WHERE PROCESS_INSTANCE = :1`
		err := c.db.QueryRowContext(ctx, seqQuery, c.params.Instance).Scan(&maxSeq)
		if err != nil {
			altSeqQuery := `SELECT MAX(MESSAGE_SEQ) FROM PS_MESSAGE_LOG WHERE PROCESS_INSTANCE = ?`
			_ = c.db.QueryRowContext(ctx, altSeqQuery, c.params.Instance).Scan(&maxSeq)
		}
		if maxSeq.Valid {
			c.lastSeq = int(maxSeq.Int64)
		}
	}

	c.lastSeq++
	seq := c.lastSeq
	now := time.Now().UTC()

	insertLog := `INSERT INTO PS_MESSAGE_LOG (PROCESS_INSTANCE, MESSAGE_SEQ, JOBID, PROGRAM_NAME, MESSAGE_SET_NBR, MESSAGE_NBR, MESSAGE_SEVERITY, DTTM_STAMP_SEC) VALUES (:1, :2, :3, :4, :5, :6, :7, :8)`
	_, err := c.db.ExecContext(ctx, insertLog, c.params.Instance, seq, c.jobID, c.programName, setNbr, msgNbr, severity, now)
	if err != nil {
		altInsert := `INSERT INTO PS_MESSAGE_LOG (PROCESS_INSTANCE, MESSAGE_SEQ, JOBID, PROGRAM_NAME, MESSAGE_SET_NBR, MESSAGE_NBR, MESSAGE_SEVERITY, DTTM_STAMP_SEC) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
		if _, err2 := c.db.ExecContext(ctx, altInsert, c.params.Instance, seq, c.jobID, c.programName, setNbr, msgNbr, severity, now); err2 != nil {
			return fmt.Errorf("failed to insert PS_MESSAGE_LOG: %w", err)
		}
	}

	// Insert parameter rows into PS_MESSAGE_LOGPARM (up to 5 parameters, truncating to 30 chars per standard PeopleSoft convention)
	for i, p := range parms {
		if i >= 5 {
			break
		}
		parmVal := p
		if len(parmVal) > 30 {
			parmVal = parmVal[:30]
		}
		parmSeq := i + 1

		insertParm := `INSERT INTO PS_MESSAGE_LOGPARM (PROCESS_INSTANCE, MESSAGE_SEQ, PARM_SEQ, MESSAGE_PARM) VALUES (:1, :2, :3, :4)`
		if _, pErr := c.db.ExecContext(ctx, insertParm, c.params.Instance, seq, parmSeq, parmVal); pErr != nil {
			altParm := `INSERT INTO PS_MESSAGE_LOGPARM (PROCESS_INSTANCE, MESSAGE_SEQ, PARM_SEQ, MESSAGE_PARM) VALUES (?, ?, ?, ?)`
			_ = c.execIgnoreErr(ctx, altParm, c.params.Instance, seq, parmSeq, parmVal)
		}
	}

	return nil
}

// Success transitions the process to RUNSTATUS = '9' (Success), sets return code = 0,
// allows PSJob chain to continue (CONTINUEJOB = 1), and records the end timestamp.
func (c *Client) Success(ctx context.Context) error {
	_ = c.LogMessage(ctx, MsgSetProcessScheduler, MsgNbrSuccess, SeverityInfo)
	return c.updateTerminalStatus(ctx, StatusSuccess, 0, 1)
}

// Warning transitions the process to RUNSTATUS = '17' (Warning) with an exit return code.
func (c *Client) Warning(ctx context.Context, returnCode int, warningText string) error {
	if warningText != "" {
		_ = c.LogMessage(ctx, MsgSetProcessScheduler, MsgNbrSQLError, SeverityWarning, warningText)
	}
	return c.updateTerminalStatus(ctx, StatusWarning, returnCode, 1)
}

// Fail transitions the process to RUNSTATUS = '10' (Not Successful), halts PSJob continuation,
// logs the error string, and records the return code.
func (c *Client) Fail(ctx context.Context, returnCode int, cause error) error {
	errMsg := "Application execution failed"
	if cause != nil {
		errMsg = cause.Error()
	}
	_ = c.LogMessage(ctx, MsgSetProcessScheduler, MsgNbrNotSuccessful, SeverityError, errMsg)

	rc := returnCode
	if rc == 0 {
		rc = 1
	}
	return c.updateTerminalStatus(ctx, StatusNotSuccessful, rc, 0)
}

// updateTerminalStatus writes final completion attributes to PSPRCSRQST and PSPRCSQUE.
func (c *Client) updateTerminalStatus(ctx context.Context, status, returnCode, continueJob int) error {
	if c.params.Instance <= 0 {
		return nil
	}

	now := time.Now().UTC()
	statusStr := strconv.Itoa(status)

	queryRqst := `UPDATE PSPRCSRQST SET RUNSTATUS = :1, PRCSRTNCD = :2, CONTINUEJOB = :3, ENDDTTM = :4 WHERE PRCSINSTANCE = :5`
	if _, err := c.db.ExecContext(ctx, queryRqst, statusStr, returnCode, continueJob, now, c.params.Instance); err != nil {
		altQuery := `UPDATE PSPRCSRQST SET RUNSTATUS = ?, PRCSRTNCD = ?, CONTINUEJOB = ?, ENDDTTM = ? WHERE PRCSINSTANCE = ?`
		if _, err2 := c.db.ExecContext(ctx, altQuery, statusStr, returnCode, continueJob, now, c.params.Instance); err2 != nil {
			return fmt.Errorf("failed to update terminal status in PSPRCSRQST: %w", err)
		}
	}

	queryQue := `UPDATE PSPRCSQUE SET RUNSTATUS = :1, PRCSRTNCD = :2, CONTINUEJOB = :3 WHERE PRCSINSTANCE = :4`
	if _, err := c.db.ExecContext(ctx, queryQue, statusStr, returnCode, continueJob, c.params.Instance); err != nil {
		altQue := `UPDATE PSPRCSQUE SET RUNSTATUS = ?, PRCSRTNCD = ?, CONTINUEJOB = ? WHERE PRCSINSTANCE = ?`
		_ = c.execIgnoreErr(ctx, altQue, statusStr, returnCode, continueJob, c.params.Instance)
	}

	return nil
}

func (c *Client) execIgnoreErr(ctx context.Context, query string, args ...any) error {
	_, err := c.db.ExecContext(ctx, query, args...)
	return err
}
