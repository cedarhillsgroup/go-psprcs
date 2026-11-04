package psprcs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Common output type constants as defined by PeopleSoft Process Scheduler.
const (
	OutTypeNone    = "1"
	OutTypeFile    = "2"
	OutTypePrinter = "3"
	OutTypeEmail   = "4"
	OutTypeWeb     = "5"
	OutTypeWindow  = "6"
)

// Common output format constants.
const (
	OutFormatTXT = "TXT"
	OutFormatCSV = "CSV"
	OutFormatPDF = "PDF"
	OutFormatHTM = "HTM"
	OutFormatXLS = "XLS"
	OutFormatSPF = "SPF"
)

// Params holds runtime parameters provided by PeopleSoft Process Scheduler.
type Params struct {
	DBType     string   `json:"db_type"`     // -ct / -CT (%%DBTYPE%%)
	Server     string   `json:"server"`      // -cs / -CS (%%SERVER%%)
	DBName     string   `json:"db_name"`     // -cd / -CD (%%DBNAME%%)
	AccessID   string   `json:"access_id"`   // -ca / -CA (%%ACCESSID%%)
	AccessPswd string   `json:"access_pswd"` // -cap / -CAP (%%ACCESSPSWD%%)
	PrcsName   string   `json:"prcs_name"`   // -rp / -RP / -ai / -AI (%%PRCSNAME%%)
	Instance   int64    `json:"instance"`    // -i  / -I (%%INSTANCE%%)
	RunControl string   `json:"run_control"` // -r  / -R (%%RUNCNTLID%%)
	OprID      string   `json:"oprid"`       // -co / -CO (%%OPRID%%)
	OutDest    string   `json:"out_dest"`    // -fp / -FP / -op / -OP (%%OUTDEST%%)
	OutFormat  string   `json:"out_format"`  // -of / -OF (%%OUTDESTFORMAT%%)
	OutType    string   `json:"out_type"`    // -ot / -OT (%%OUTDESTTYPE%%)
	LanguageCd string   `json:"language_cd"` // -lg / -LG (%%LANGUAGECD%%)
	ExtraArgs  []string `json:"extra_args"`  // Remaining unrecognized or positional arguments
}

// Parse extracts Process Scheduler parameters from a command-line argument slice.
func Parse(args []string) (*Params, error) {
	p := &Params{}
	n := len(args)

	for i := 0; i < n; i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			p.ExtraArgs = append(p.ExtraArgs, arg)
			continue
		}

		rawFlag := strings.TrimLeft(arg, "-")
		key := rawFlag
		val := ""
		hasInlineVal := false

		if eqIdx := strings.IndexByte(rawFlag, '='); eqIdx >= 0 {
			key = rawFlag[:eqIdx]
			val = rawFlag[eqIdx+1:]
			hasInlineVal = true
		}

		normKey := strings.ToLower(key)

		// consumeVal handles inline values (-flag=val) or separated values (-flag val).
		// If the value was omitted or the next argument is another flag, it safely returns ""
		// rather than erroring, because Process Scheduler often expands optional meta-strings
		// (like %%OUTDEST%% or %%SERVER%%) into empty strings ("").
		consumeVal := func() string {
			if hasInlineVal {
				return val
			}
			if i+1 < n && !strings.HasPrefix(args[i+1], "-") {
				i++
				return args[i]
			}
			return ""
		}

		switch normKey {
		case "ct", "dbtype":
			p.DBType = strings.ToUpper(consumeVal())

		case "cs", "server":
			p.Server = consumeVal()

		case "cd", "dbname":
			p.DBName = consumeVal()

		case "ca", "accessid":
			p.AccessID = consumeVal()

		case "cap", "accesspswd":
			p.AccessPswd = consumeVal()

		case "rp", "ai", "prcsname", "processname":
			p.PrcsName = strings.ToUpper(consumeVal())

		case "i", "instance", "prcsinstance":
			v := consumeVal()
			if v != "" {
				inst, err := strconv.ParseInt(v, 10, 64)
				if err != nil {
					return nil, fmt.Errorf("invalid process instance %q: %w", v, err)
				}
				p.Instance = inst
			}

		case "r", "runcontrol", "runcntlid":
			p.RunControl = consumeVal()

		case "co", "oprid":
			p.OprID = consumeVal()

		case "fp", "op", "outdest", "outdestination":
			p.OutDest = consumeVal()

		case "of", "outformat", "destformat":
			p.OutFormat = strings.ToUpper(consumeVal())

		case "ot", "outtype", "desttype":
			p.OutType = consumeVal()

		case "lg", "lang", "languagecd":
			p.LanguageCd = strings.ToUpper(consumeVal())

		default:
			// Check for compact attached flags
			if !hasInlineVal && len(rawFlag) > 3 && strings.EqualFold(rawFlag[:3], "cap") {
				p.AccessPswd = rawFlag[3:]
				continue
			}
			if !hasInlineVal && len(rawFlag) > 2 {
				prefix2 := strings.ToLower(rawFlag[:2])
				rest := rawFlag[2:]
				switch prefix2 {
				case "ct":
					p.DBType = strings.ToUpper(rest)
					continue
				case "cs":
					p.Server = rest
					continue
				case "cd":
					p.DBName = rest
					continue
				case "ca":
					p.AccessID = rest
					continue
				case "rp":
					p.PrcsName = strings.ToUpper(rest)
					continue
				case "co":
					p.OprID = rest
					continue
				case "fp", "op":
					p.OutDest = rest
					continue
				case "of":
					p.OutFormat = strings.ToUpper(rest)
					continue
				case "ot":
					p.OutType = rest
					continue
				case "lg":
					p.LanguageCd = strings.ToUpper(rest)
					continue
				}
			}
			if !hasInlineVal && len(rawFlag) > 1 {
				prefix1 := strings.ToLower(rawFlag[:1])
				rest := rawFlag[1:]
				switch prefix1 {
				case "i":
					inst, err := strconv.ParseInt(rest, 10, 64)
					if err == nil {
						p.Instance = inst
						continue
					}
				case "r":
					p.RunControl = rest
					continue
				}
			}
			p.ExtraArgs = append(p.ExtraArgs, arg)
		}
	}

	return p, nil
}

// ParseFromArgs parses parameters directly from os.Args[1:].
func ParseFromArgs() (*Params, error) {
	return Parse(os.Args[1:])
}

// OutputPath resolves a relative file name against OutDest.
// If OutDest is empty, invalid, or represents a PeopleSoft distribution target
// (such as "User:PS", "Role:Admin", or "None"), it safely falls back to returning
// the file path in the current working directory.
func (p *Params) OutputPath(elem ...string) string {
	target := filepath.Join(elem...)

	// Check if OutDest is empty or represents a PeopleSoft distribution string
	if p.OutDest == "" || strings.HasPrefix(p.OutDest, "User:") || strings.HasPrefix(p.OutDest, "Role:") || strings.EqualFold(p.OutDest, "None") {
		return target
	}

	// Check for colons that are not Windows drive letters (e.g., "User:PS" or custom URI distribution)
	if strings.Contains(p.OutDest, ":") {
		if len(p.OutDest) < 2 || p.OutDest[1] != ':' {
			return target
		}
	}

	if fi, err := os.Stat(p.OutDest); (err == nil && fi.IsDir()) || strings.HasSuffix(p.OutDest, string(filepath.Separator)) {
		return filepath.Join(p.OutDest, target)
	}
	if filepath.Ext(p.OutDest) != "" && len(elem) == 0 {
		return p.OutDest
	}
	return filepath.Join(p.OutDest, target)
}

// ConnectString returns an Oracle connection string if AccessID, AccessPswd, and DBName are available.
func (p *Params) ConnectString() string {
	if p.AccessID != "" && p.AccessPswd != "" && p.DBName != "" {
		return fmt.Sprintf("%s/%s@%s", p.AccessID, p.AccessPswd, p.DBName)
	}
	return ""
}

// IsWeb returns true if the requested output type is Web (Report Repository).
func (p *Params) IsWeb() bool {
	return p.OutType == OutTypeWeb || strings.EqualFold(p.OutType, "Web")
}

// IsFile returns true if the requested output type is File.
func (p *Params) IsFile() bool {
	return p.OutType == OutTypeFile || strings.EqualFold(p.OutType, "File")
}

// Validate checks whether essential Process Scheduler parameters are populated.
func (p *Params) Validate() error {
	var errs []error
	if p.Instance <= 0 {
		errs = append(errs, errors.New("missing or invalid process instance (must be > 0)"))
	}
	if p.RunControl == "" {
		errs = append(errs, errors.New("missing run control ID"))
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}
