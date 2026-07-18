package qscan

// HTTPTransactionRequest represents the main request for scanning HTTP transactions
type HTTPTransactionRequest struct {
	URL       string            `json:"url,omitempty"`
	Method    string            `json:"method,omitempty"`
	RequestID string            `json:"request_id,omitempty"`
	Request   *HTTPRequestData  `json:"request,omitempty"`
	Response  *HTTPResponseData `json:"response,omitempty"`
}

// HTTPRequestData represents HTTP request components
type HTTPRequestData struct {
	Headers map[string]string `json:"headers,omitempty"`
	Body    *string           `json:"body,omitempty"`
}

// HTTPResponseData represents HTTP response components
type HTTPResponseData struct {
	Status  *int              `json:"status,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    *string           `json:"body,omitempty"`
}

// HTTPTransactionScanResponse represents the response from scanning an HTTP transaction
type HTTPTransactionScanResponse struct {
	Success                bool                          `json:"success,omitempty"`
	RequestID              string                        `json:"request_id,omitempty"`
	Method                 string                        `json:"method,omitempty"`
	URL                    string                        `json:"url,omitempty"`
	SectionResults         map[string]SectionScanSummary `json:"section_results,omitempty"`
	Summary                ScanSummary                   `json:"summary,omitzero"`
	HighConfidenceFindings []HighConfidenceFinding       `json:"high_confidence_findings,omitempty,omitzero"`
	ScanMetadata           ScanMetadata                  `json:"scan_metadata,omitzero"`
	ProcessingInfo         ProcessingInfo                `json:"processing_info,omitzero"`
}

// SectionScanSummary represents scanning results for a specific HTTP transaction section
type SectionScanSummary struct {
	Section                string   `json:"section,omitempty"`
	ContentFormat          string   `json:"content_format,omitempty"`
	OriginalContentLength  int      `json:"original_content_length,omitempty"`
	ScannableContentLength int      `json:"scannable_content_length,omitempty"`
	RawEntitiesCount       int      `json:"raw_entities_count,omitempty"`
	ValidatedEntitiesCount int      `json:"validated_entities_count,omitempty"`
	ConfirmedEntities      int      `json:"confirmed_entities,omitempty"`
	SuspectedEntities      int      `json:"suspected_entities,omitempty"`
	RejectedEntities       int      `json:"rejected_entities,omitempty"`
	ScanRulesApplied       []string `json:"scan_rules_applied,omitempty"`
}

// ScanSummary represents overall summary statistics for the HTTP transaction scan
type ScanSummary struct {
	HighConfidenceEntities int                       `json:"high_confidence_entities,omitempty"`
	SuspectedEntities      int                       `json:"suspected_entities,omitempty"`
	RejectedEntities       int                       `json:"rejected_entities,omitempty"`
	EntitiesBySection      map[string]map[string]int `json:"entities_by_section,omitempty"`
	EntitiesByType         map[string]int            `json:"entities_by_type,omitempty"`
	ValidationStatistics   map[string]int            `json:"validation_statistics,omitempty"`
	ConfidenceLevel        string                    `json:"confidence_level,omitempty"`
}

// HighConfidenceFinding represents a high-confidence PII finding from HTTP transaction scanning
type HighConfidenceFinding struct {
	Section             string            `json:"section,omitempty"`
	EntityType          string            `json:"entity_type,omitempty"`
	Value               string            `json:"value,omitempty"`
	Confidence          float64           `json:"confidence,omitempty"`
	StartPosition       int               `json:"start_position,omitempty"`
	EndPosition         int               `json:"end_position,omitempty"`
	SupportingDetectors []string          `json:"supporting_detectors,omitempty"`
	ContentFormat       string            `json:"content_format,omitempty"`
	ValidationNotes     []string          `json:"validation_notes,omitempty"`
	DetectorConsensus   bool              `json:"detector_consensus,omitempty"`
	Metadata            map[string]string `json:"metadata,omitempty"`
}

// ScanMetadata represents metadata about the scanning approach and configuration
type ScanMetadata struct {
	ValidationApproach     string `json:"validation_approach,omitempty"`
	FalsePositiveFiltering string `json:"false_positive_filtering,omitempty"`
	MultiDetectorConsensus string `json:"multi_detector_consensus,omitempty"`
}

// ProcessingInfo represents information about the processing that was performed
type ProcessingInfo struct {
	AnalysisApproach       string   `json:"analysis_approach,omitempty"`
	SectionsAnalyzed       int      `json:"sections_analyzed,omitempty"`
	DetectorsUsed          []string `json:"detectors_used,omitempty"`
	ValidationLayers       []string `json:"validation_layers,omitempty"`
	ContentFormatsDetected []string `json:"content_formats_detected,omitempty"`
}

// DBTransactionRequest represents a database transaction for qscan scanning.
// It is protocol-agnostic; the db_type field distinguishes MySQL, Redis, and Kafka.
type DBTransactionRequest struct {
	TransactionType string           `json:"transaction_type"`
	DBType          string           `json:"db_type"`
	Query           string           `json:"query"`
	Params          map[string]any   `json:"params,omitempty"`
	ResultSet       []map[string]any `json:"result_set,omitempty"`
	RequestID       string           `json:"request_id,omitempty"`
	Error           *DBError         `json:"error,omitempty"`
	Truncated       bool             `json:"truncated,omitempty"`
}

// DBError captures error details from the database response.
type DBError struct {
	Code    int    `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}
