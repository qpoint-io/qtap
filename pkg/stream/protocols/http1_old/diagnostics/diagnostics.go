// Package diagnostics provides HTTP/1.x protocol handling.
// This file contains diagnostic tooling for debug builds.
// Build with -tags=debug to include this functionality.
//
// DiagnosticWriter Usage:
//
//  1. Create a new writer with a target directory and unique suffix:
//     writer := NewDiagnosticWriter("/path/to/output", "conn123", logger)
//
//  2. Write raw HTTP traffic as it's captured:
//     writer.Write(rawData)
//
//  3. Close the writer when done (e.g., connection closes):
//     writer.Close()
//
// The writer generates two files per session:
//   - {timestamp}_{suffix}.log: Human-readable hex dumps with timestamps
//   - {timestamp}_{suffix}.json: Machine-parseable base64 payloads
//
// Example output location:
//
//	/path/to/output/
//	  ├── dump_20240423_170800_conn123.log
//	  └── dump_20240423_170800_conn123.json
package diagnostics

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.uber.org/zap"
)

// PayloadEntry represents a captured HTTP payload with ordering metadata
type PayloadEntry struct {
	Sequence int    `json:"sequence"`
	Data     string `json:"data"` // base64 encoded
}

// DiagnosticWriter captures raw HTTP traffic for debugging purposes.
// It writes hex dumps to a .log file and base64-encoded payloads to a .json file.
type DiagnosticWriter struct {
	enabled       bool   // whether writer is active
	directory     string // output directory for diagnostic files
	baseFilename  string // shared prefix for .log and .json files
	logFilePath   string // path to detailed hex dump log
	jsonFilePath  string // path to payload JSON array
	rawFilePath   string // path to raw payloads
	logger        *zap.Logger
	payloads      [][]byte   // ordered list of captured payloads
	payloadsMutex sync.Mutex // guards payloads slice
	sequence      int        // monotonic counter for ordering
}

// NewDiagnosticWriter creates a writer that outputs to timestamp-based files in the given directory.
// Returns a disabled writer if directory creation fails.
func NewDiagnosticWriter(directory string, suffix string, logger *zap.Logger) *DiagnosticWriter {
	if logger == nil {
		logger = zap.NewNop() // Use a no-op logger if none provided
	}

	// Create timestamp-based base filename (e.g., dump_20250423_170800)
	timestamp := time.Now().Format("20060102_150405")
	baseFilename := fmt.Sprintf("dump_%s_%s", timestamp, suffix)

	// Ensure the diagnostics directory exists
	if err := os.MkdirAll(directory, 0755); err != nil {
		logger.Error("Failed to create diagnostics directory, disabling writer",
			zap.String("directory", directory),
			zap.Error(err))
		// Return a disabled writer if directory creation fails
		return &DiagnosticWriter{enabled: false, logger: logger}
	}

	// Construct full paths for both log and JSON files
	logFilePath := filepath.Join(directory, baseFilename+".log")
	jsonFilePath := filepath.Join(directory, baseFilename+".json")
	rawFilePath := filepath.Join(directory, baseFilename+".raw")
	logger.Info("Diagnostic writer initialized",
		zap.String("logFile", logFilePath),
		zap.String("jsonFile", jsonFilePath))

	return &DiagnosticWriter{
		enabled:       true,
		directory:     directory,
		baseFilename:  baseFilename,
		logFilePath:   logFilePath,
		jsonFilePath:  jsonFilePath,
		rawFilePath:   rawFilePath,
		logger:        logger,
		payloads:      make([][]byte, 0, 100), // Initialize slice with some capacity
		payloadsMutex: sync.Mutex{},           // Initialize the mutex
		sequence:      0,                      // Initialize sequence counter
	}
}

// Write captures and stores a copy of the raw data, writing hex dumps to the log file.
func (d *DiagnosticWriter) Write(data []byte) {
	if !d.enabled {
		return // Do nothing if the writer is disabled
	}

	// --- Store payload for JSON ---
	// We must make a copy because the input slice 'data' might be reused
	// by the caller after this function returns.
	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)

	// Lock the mutex before accessing the shared payloads slice
	d.payloadsMutex.Lock()
	d.payloads = append(d.payloads, dataCopy)
	d.sequence++
	currentSeq := d.sequence
	d.payloadsMutex.Unlock() // Unlock immediately after appending
	// -----------------------------

	// --- Append to log file (existing functionality) ---
	// This writes the hex dump and raw string to the .log file for each chunk
	d.appendToLogFile(d.logFilePath, data, currentSeq)
	d.appendToRawFile(d.rawFilePath, data)
	// -------------------------------------------------
}

// Close writes accumulated payloads to the JSON file and disables the writer.
func (d *DiagnosticWriter) Close() error {
	if !d.enabled {
		return nil // Nothing to do if already closed or disabled initially
	}

	d.logger.Info("Closing diagnostic writer", zap.Int("payload_count", len(d.payloads)))

	// Lock the mutex to safely access and modify shared state (payloads, enabled)
	d.payloadsMutex.Lock()
	defer d.payloadsMutex.Unlock() // Ensure mutex is unlocked even if errors occur

	// Mark as disabled *now* to prevent any writes happening concurrently during the file write.
	d.enabled = false

	// Create ordered payload entries with sequence numbers
	entries := make([]PayloadEntry, len(d.payloads))
	for i, p := range d.payloads {
		entries[i] = PayloadEntry{
			Sequence: i + 1, // Use 1-based sequence numbers
			Data:     base64.StdEncoding.EncodeToString(p),
		}
	}

	// Marshal the array of PayloadEntry structs into JSON format with indentation
	jsonData, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		d.logger.Error("Failed to marshal payloads to JSON", zap.Error(err))
		// Clear slice to release memory even on error, then return
		d.payloads = nil
		return fmt.Errorf("failed to marshal JSON data: %w", err)
	}

	// Write the JSON data to the target file
	err = os.WriteFile(d.jsonFilePath, jsonData, 0644)
	if err != nil {
		d.logger.Error("Failed to write JSON payload file", zap.String("file", d.jsonFilePath), zap.Error(err))
		// Clear slice and return error
		d.payloads = nil
		return fmt.Errorf("failed to write JSON file '%s': %w", d.jsonFilePath, err)
	}

	d.logger.Info("Successfully wrote JSON payload file",
		zap.String("file", d.jsonFilePath),
		zap.Int("payloadCount", len(entries)))

	// Clear the payloads slice to release the stored data from memory after successful write
	d.payloads = nil

	return nil
}

// appendToLogFile writes a timestamped hex dump and raw string representation of the data.
func (d *DiagnosticWriter) appendToRawFile(filepath string, data []byte) {
	// Open the log file in append mode, creating it if it doesn't exist.
	f, err := os.OpenFile(filepath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		d.logger.Error("Failed to open diagnostic log file for appending", zap.String("file", filepath), zap.Error(err))
		return // Cannot proceed if file can't be opened
	}
	defer f.Close() // Ensure file is closed when function exits
	if _, err := f.Write(data); err != nil {
		d.logger.Error("Failed to write diagnostic log file", zap.String("file", filepath), zap.Error(err))
	}
}

// appendToLogFile writes a timestamped hex dump and raw string representation of the data.
func (d *DiagnosticWriter) appendToLogFile(filepath string, data []byte, sequence int) {
	// Open the log file in append mode, creating it if it doesn't exist.
	f, err := os.OpenFile(filepath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		d.logger.Error("Failed to open diagnostic log file for appending", zap.String("file", filepath), zap.Error(err))
		return // Cannot proceed if file can't be opened
	}
	defer f.Close() // Ensure file is closed when function exits

	// Write a separator block including a timestamp, sequence number, and chunk length
	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	separator := fmt.Sprintf("\n\n===== CHUNK #%d AT %s (LENGTH: %d) =====\n", sequence, timestamp, len(data))
	if _, err := f.WriteString(separator); err != nil {
		d.logger.Warn("Failed to write separator to log file", zap.Error(err))
		// Continue trying to write data even if separator fails
	}

	// Generate and write the hex dump of the data
	hexDump := formatHexDump(data)
	if _, err := f.WriteString(hexDump); err != nil {
		d.logger.Warn("Failed to write hex dump to log file", zap.Error(err))
		// Continue trying to write raw string
	}

	// Add the raw string representation (be cautious with binary data)
	// This might produce unreadable output if data is not text.
	strDump := fmt.Sprintf("\n----- RAW STRING -----\n%s\n----- END RAW -----\n", string(data))
	if _, err := f.WriteString(strDump); err != nil {
		d.logger.Warn("Failed to write raw string to log file", zap.Error(err))
	}
}

// formatHexDump creates a standard 16-byte-width hex dump with ASCII representation.
func formatHexDump(data []byte) string {
	var result string
	const bytesPerRow = 16 // Standard width for hex dumps

	for i := 0; i < len(data); i += bytesPerRow {
		// Add the offset at the beginning of the line
		result += fmt.Sprintf("%08x  ", i)

		// Get the slice for the current row (up to bytesPerRow)
		chunk := data[i:]
		if len(chunk) > bytesPerRow {
			chunk = chunk[:bytesPerRow]
		}

		// Add the hex byte representation
		for j := range bytesPerRow {
			if j < len(chunk) {
				result += fmt.Sprintf("%02x ", chunk[j]) // Print byte as 2-digit hex
			} else {
				result += "   " // Pad with spaces if row is shorter
			}
			// Add an extra space halfway through the hex bytes for readability
			if j == bytesPerRow/2-1 {
				result += " "
			}
		}

		// Add the ASCII representation part
		result += " |" // Separator
		for j := range chunk {
			b := chunk[j]
			// Use '.' for non-printable characters, otherwise print the character
			if b >= 32 && b <= 126 {
				result += string(b)
			} else {
				result += "."
			}
		}
		// Pad the ASCII part with spaces if the row is shorter than bytesPerRow
		for j := len(chunk); j < bytesPerRow; j++ {
			result += " "
		}
		result += "|\n" // End of ASCII part and newline
	}

	return result
}
