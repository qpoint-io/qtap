package download

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

type Warehouse struct {
	QpointID string
	Token    string
	DataDir  string
}

func (w *Warehouse) Fetch(version string) (string, error) {
	// display
	fmt.Printf("Downloading version: %s\n", version)

	// generate a uuid for this version as the directory name
	dirName := uuid.New().String()

	// location of the bundle
	archiveURL := fmt.Sprintf("https://warehouse.qpoint.io/assets/qtap-%s-%s", w.QpointID, version)

	// where should we put this?
	outputDirectory := filepath.Join(w.DataDir, dirName)

	// init the request
	req, err := http.NewRequest("GET", archiveURL, nil)
	if err != nil {
		return "", fmt.Errorf("initializing request: %w", err)
	}

	// Set the Authorization header
	req.Header.Set("Authorization", "Bearer "+w.Token)

	// init an http client
	client := &http.Client{}

	// Fetch the remote archive
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching version: %w", err)
	}
	defer resp.Body.Close()

	// ensure the archive exists
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("archive does not exist")
	}

	// display
	fmt.Printf("Extracting archive\n")

	// Create a gzip reader to decompress the archive
	gzipReader, err := gzip.NewReader(resp.Body)
	if err != nil {
		return "", fmt.Errorf("creating gzip reader: %w", err)
	}
	defer gzipReader.Close()

	// Create a tar reader to read the contents of the archive
	tarReader := tar.NewReader(gzipReader)

	// Iterate over each file in the archive
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break // End of archive
		}
		if err != nil {
			return "", fmt.Errorf("reading tar header: %w", err)
		}

		// Extract the file to the output directory
		outputPath := filepath.Join(outputDirectory, header.Name)
		if header.Typeflag == tar.TypeDir {
			// Create directory if it doesn't exist
			if err := os.MkdirAll(outputPath, os.ModePerm); err != nil {
				return "", fmt.Errorf("creating directory: %w", err)
			}
		} else if header.Typeflag == tar.TypeReg {
			// Create file and copy contents
			outputFile, err := os.Create(outputPath)
			if err != nil {
				return "", fmt.Errorf("creating file: %w", err)
			}
			defer outputFile.Close()

			if _, err := io.Copy(outputFile, tarReader); err != nil {
				return "", fmt.Errorf("extracting file: %w", err)
			}
		}
	}

	// display
	fmt.Printf("Extraction complete: %s\n", outputDirectory)

	// return the path to the extraction
	return outputDirectory, nil
}
