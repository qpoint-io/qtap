package s3

import (
	"context"
	"fmt"
	"strings"

	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/qtap/pkg/services/objectstore"
)

// templateURL takes a URL template string containing {{KEY}} placeholders
// and replaces them with corresponding values from the provided map.
// If a key in the template is not found in the values map, it remains unchanged.
func templateURL(template string, values map[string]string) string {
	result := template
	for key, value := range values {
		placeholder := fmt.Sprintf("{{%s}}", key)
		result = strings.ReplaceAll(result, placeholder, value)
	}
	return result
}

type ObjectStore struct {
	services.LogHelper
	objectstore.BaseObjectStore
	putFn     func(ctx context.Context, digest string, contentType string, data []byte) (map[string]string, error)
	accessURL string
}

func (s *ObjectStore) Put(artifact eventstore.Artifact) (*eventstore.ArtifactRecord, error) {
	ctx := context.Background()
	m, err := s.putFn(ctx, artifact.Digest(), artifact.ContentType, artifact.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to put artifact: %w", err)
	}

	// build the URL
	url := fmt.Sprintf("%s://%s/%s/%s", m["SCHEME"], m["ENDPOINT"], m["BUCKET"], m["DIGEST"])
	if s.accessURL != "" {
		url = templateURL(s.accessURL, m)
	}

	return artifact.Record(url), nil
}
