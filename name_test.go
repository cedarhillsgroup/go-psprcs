package psprcs

import (
	"testing"
)

func TestValidateProcessName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		maxLen  int
		wantErr bool
	}{
		{name: "Valid 8-char name OPRAUDIT (default limit 8)", input: "OPRAUDIT", wantErr: false},
		{name: "Valid 6-char name XRFWIN", input: "XRFWIN", wantErr: false},
		{name: "Valid with underscore", input: "OPR_AUD", wantErr: false},
		{name: "10-char name exceeds default 8-char limit", input: "PSOPRAUDIT", wantErr: true},
		{name: "10-char name valid with 12-char AE limit", input: "PSOPRAUDIT", maxLen: 12, wantErr: false},
		{name: "Exceeds 12 chars (13 chars)", input: "PSOPRAUDITING", maxLen: 12, wantErr: true},
		{name: "Empty name", input: "", wantErr: true},
		{name: "Lowercase letters", input: "opraudit", wantErr: true},
		{name: "Mixed case", input: "OprAudit", wantErr: true},
		{name: "Invalid hyphen character", input: "OPR-AUD", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			if tt.maxLen > 0 {
				err = ValidateProcessName(tt.input, tt.maxLen)
			} else {
				err = ValidateProcessName(tt.input)
			}
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateProcessName(%q) error = %v, wantErr = %v", tt.input, err, tt.wantErr)
			}
		})
	}
}
