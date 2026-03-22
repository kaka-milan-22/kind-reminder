package model

type SMTPDiagnosticStage struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
	Error  string `json:"error,omitempty"`
}

type SMTPDiagnostic struct {
	Host                string                `json:"host"`
	Port                int                   `json:"port"`
	From                string                `json:"from,omitempty"`
	To                  string                `json:"to,omitempty"`
	ResolvedAddresses   []string              `json:"resolved_addresses,omitempty"`
	SuspectedFakeIP     bool                  `json:"suspected_fake_ip"`
	Hint                string                `json:"hint,omitempty"`
	Success             bool                  `json:"success"`
	LastSuccessfulStage string                `json:"last_successful_stage,omitempty"`
	FailedStage         string                `json:"failed_stage,omitempty"`
	Error               string                `json:"error,omitempty"`
	Stages              []SMTPDiagnosticStage `json:"stages"`
}
