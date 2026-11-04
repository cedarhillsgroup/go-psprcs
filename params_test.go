package psprcs

import (
	"path/filepath"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantInst   int64
		wantRunCtl string
		wantOprID  string
		wantDBName string
		wantDBType string
		wantOutDst string
		wantOutFmt string
		wantOutTyp string
		wantServer string
		wantAccID  string
		wantAccPwd string
		wantPrcsNm string
		wantLang   string
		wantErr    bool
	}{
		{
			name: "Standard SQR flag parameter list",
			args: []string{
				"-CT", "ORACLE",
				"-CS", "PRCS6646",
				"-CD", "CS92DEV",
				"-CA", "SYSADM",
				"-CAP", "secret_pass",
				"-RP", "PSOPRAUDIT",
				"-I", "248901",
				"-R", "MY_RUN_CNTL",
				"-CO", "PS",
				"-OT", "2",
				"-OP", "/opt/psoft/output",
				"-OF", "TXT",
				"-LG", "ENG",
			},
			wantInst:   248901,
			wantRunCtl: "MY_RUN_CNTL",
			wantOprID:  "PS",
			wantDBName: "CS92DEV",
			wantDBType: "ORACLE",
			wantOutDst: "/opt/psoft/output",
			wantOutFmt: "TXT",
			wantOutTyp: "2",
			wantServer: "PRCS6646",
			wantAccID:  "SYSADM",
			wantAccPwd: "secret_pass",
			wantPrcsNm: "PSOPRAUDIT",
			wantLang:   "ENG",
		},
		{
			name: "Case-insensitive flags with equals",
			args: []string{
				"-CT=oracle",
				"-CD=HR92",
				"-I=55102",
				"-R=BENEFITS_01",
				"-CO=HRADMIN",
				"-FP=/var/reports",
				"-OF=pdf",
				"-OT=5",
			},
			wantInst:   55102,
			wantRunCtl: "BENEFITS_01",
			wantOprID:  "HRADMIN",
			wantDBName: "HR92",
			wantDBType: "ORACLE",
			wantOutDst: "/var/reports",
			wantOutFmt: "PDF",
			wantOutTyp: "5",
		},
		{
			name: "Verbose long flags",
			args: []string{
				"--dbtype", "MICROSFT",
				"--dbname", "FSCM92",
				"--instance", "998231",
				"--runcontrol", "PAYROLL_RUN",
				"--oprid", "VP1",
				"--outdest", "C:\\temp\\prcs",
				"--outformat", "CSV",
				"--outtype", "Web",
			},
			wantInst:   998231,
			wantRunCtl: "PAYROLL_RUN",
			wantOprID:  "VP1",
			wantDBName: "FSCM92",
			wantDBType: "MICROSFT",
			wantOutDst: "C:\\temp\\prcs",
			wantOutFmt: "CSV",
			wantOutTyp: "Web",
		},
		{
			name: "Compact attached flags like SQR",
			args: []string{
				"-ctoracle",
				"-cdCS92DEV",
				"-casysadm",
				"-cappassword123",
				"-i100200",
				"-rQUICK_RUN",
				"-coPS",
			},
			wantInst:   100200,
			wantRunCtl: "QUICK_RUN",
			wantOprID:  "PS",
			wantDBName: "CS92DEV",
			wantDBType: "ORACLE",
			wantAccID:  "sysadm",
			wantAccPwd: "password123",
		},
		{
			name:       "Empty trailing flag or empty string",
			args:       []string{"-ct", "ORACLE", "-cd", "CS92DEV", "-fp", "", "-of", "1"},
			wantDBType: "ORACLE",
			wantDBName: "CS92DEV",
			wantOutDst: "",
			wantOutFmt: "1",
			wantErr:    false,
		},
		{
			name:    "Invalid instance integer",
			args:    []string{"-i", "not_an_int"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := Parse(tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Parse() error = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if p.Instance != tt.wantInst {
				t.Errorf("Instance = %v, want %v", p.Instance, tt.wantInst)
			}
			if p.RunControl != tt.wantRunCtl {
				t.Errorf("RunControl = %v, want %v", p.RunControl, tt.wantRunCtl)
			}
			if p.OprID != tt.wantOprID {
				t.Errorf("OprID = %v, want %v", p.OprID, tt.wantOprID)
			}
			if p.DBName != tt.wantDBName {
				t.Errorf("DBName = %v, want %v", p.DBName, tt.wantDBName)
			}
			if p.DBType != tt.wantDBType {
				t.Errorf("DBType = %v, want %v", p.DBType, tt.wantDBType)
			}
			if p.OutDest != tt.wantOutDst {
				t.Errorf("OutDest = %v, want %v", p.OutDest, tt.wantOutDst)
			}
			if p.OutFormat != tt.wantOutFmt {
				t.Errorf("OutFormat = %v, want %v", p.OutFormat, tt.wantOutFmt)
			}
			if p.OutType != tt.wantOutTyp {
				t.Errorf("OutType = %v, want %v", p.OutType, tt.wantOutTyp)
			}
			if tt.wantServer != "" && p.Server != tt.wantServer {
				t.Errorf("Server = %v, want %v", p.Server, tt.wantServer)
			}
			if tt.wantAccID != "" && p.AccessID != tt.wantAccID {
				t.Errorf("AccessID = %v, want %v", p.AccessID, tt.wantAccID)
			}
			if tt.wantAccPwd != "" && p.AccessPswd != tt.wantAccPwd {
				t.Errorf("AccessPswd = %v, want %v", p.AccessPswd, tt.wantAccPwd)
			}
			if tt.wantPrcsNm != "" && p.PrcsName != tt.wantPrcsNm {
				t.Errorf("PrcsName = %v, want %v", p.PrcsName, tt.wantPrcsNm)
			}
			if tt.wantLang != "" && p.LanguageCd != tt.wantLang {
				t.Errorf("LanguageCd = %v, want %v", p.LanguageCd, tt.wantLang)
			}
		})
	}
}

func TestConnectString(t *testing.T) {
	p := &Params{
		AccessID:   "sysadm",
		AccessPswd: "pwd",
		DBName:     "CS92DEV",
	}
	got := p.ConnectString()
	want := "sysadm/pwd@CS92DEV"
	if got != want {
		t.Errorf("ConnectString() = %v, want %v", got, want)
	}
}

func TestOutputPath(t *testing.T) {
	p := &Params{OutDest: "/reports/psprcs"}
	got := p.OutputPath("audit.md")
	want := filepath.Join("/reports/psprcs", "audit.md")
	if got != want {
		t.Errorf("OutputPath() = %v, want %v", got, want)
	}

	pEmpty := &Params{}
	gotEmpty := pEmpty.OutputPath("audit.md")
	if gotEmpty != "audit.md" {
		t.Errorf("OutputPath() empty = %v, want %v", gotEmpty, "audit.md")
	}

	pUser := &Params{OutDest: "User:PS"}
	gotUser := pUser.OutputPath("audit.md")
	if gotUser != "audit.md" {
		t.Errorf("OutputPath() User:PS = %v, want audit.md", gotUser)
	}

	pRole := &Params{OutDest: "Role:Administrator"}
	gotRole := pRole.OutputPath("audit.md")
	if gotRole != "audit.md" {
		t.Errorf("OutputPath() Role:Administrator = %v, want audit.md", gotRole)
	}
}

func TestValidate(t *testing.T) {
	invalid := &Params{}
	if err := invalid.Validate(); err == nil {
		t.Error("expected error for empty params, got nil")
	}

	valid := &Params{Instance: 1234, RunControl: "RUN_01"}
	if err := valid.Validate(); err != nil {
		t.Errorf("expected valid params, got error: %v", err)
	}
}
