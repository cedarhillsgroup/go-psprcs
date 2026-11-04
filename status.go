package psprcs

import "strconv"

// Process Scheduler Run Status constants as defined in PSPRCSRQST.RUNSTATUS.
const (
	StatusCancel        = 1  // User requested cancellation before start
	StatusDelete        = 2  // Marked for deletion
	StatusError         = 3  // System error or crashed before job execution
	StatusHold          = 4  // Held by operator or queue constraint
	StatusQueued        = 5  // Queued in Process Scheduler
	StatusInitiated     = 6  // Process Scheduler is spawning the process
	StatusProcessing    = 7  // Program is actively executing
	StatusCancelled     = 8  // Cancelled while running
	StatusSuccess       = 9  // Completed normally
	StatusNotSuccessful = 10 // Application-level failure or unhandled error
	StatusPosting       = 14 // Distribution agent (PSDSTSRV) transferring output
	StatusWarning       = 17 // Completed with non-fatal warnings
)

// Process Message Log severity levels for PS_MESSAGE_LOG.MESSAGE_SEVERITY.
const (
	SeverityWarning = 10 // Warning message
	SeverityError   = 20 // Error message
	SeverityInfo    = 30 // Informational message
)

// Standard PeopleSoft Message Sets.
const (
	MsgSetProcessScheduler = 65 // Process Scheduler delivered message catalog set
	MsgNbrSQLError         = 34 // SQL Error encountered
	MsgNbrSuccess          = 35 // Process completed successfully
	MsgNbrNotSuccessful    = 43 // Process ended with errors
)

// StatusText returns the descriptive English name for a given RUNSTATUS code.
func StatusText(status int) string {
	switch status {
	case StatusCancel:
		return "Cancel"
	case StatusDelete:
		return "Delete"
	case StatusError:
		return "Error"
	case StatusHold:
		return "Hold"
	case StatusQueued:
		return "Queued"
	case StatusInitiated:
		return "Initiated"
	case StatusProcessing:
		return "Processing"
	case StatusCancelled:
		return "Cancelled"
	case StatusSuccess:
		return "Success"
	case StatusNotSuccessful:
		return "Not Successful"
	case StatusPosting:
		return "Posting"
	case StatusWarning:
		return "Warning"
	default:
		return "Unknown (" + strconv.Itoa(status) + ")"
	}
}

// IsTerminal returns true if the status represents a final completed state.
func IsTerminal(status int) bool {
	switch status {
	case StatusCancel, StatusDelete, StatusError, StatusCancelled, StatusSuccess, StatusNotSuccessful, StatusWarning:
		return true
	default:
		return false
	}
}
