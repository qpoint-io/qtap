package dlp

type DLPConfig struct {
	ScanHeaders bool   `json:"scanHeaders" yaml:"scan_headers"`
	Rules       []Rule `json:"rules" yaml:"rules"`
}

type Rule struct {
	Label        string `json:"label" yaml:"label"`                 // Replacement value for scrubbing
	Expression   string `json:"expression" yaml:"expression"`       // Regular expression
	Scrub        bool   `json:"scrub" yaml:"scrub"`                 // Replace value with the label
	Report       bool   `json:"report" yaml:"report"`               // Log detection of a rule
	Record       bool   `json:"record" yaml:"record"`               // Copy the value to an artifact repository
	FailOnDetect bool   `json:"failOnDetect" yaml:"fail_on_detect"` // Fail a request if the pattern is matched
}
