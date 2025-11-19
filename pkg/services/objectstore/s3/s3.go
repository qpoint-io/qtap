package s3

import (
	"context"
	"fmt"
	"strings"

	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/qtap/pkg/services/objectstore"
	"go.uber.org/zap"
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
	put        func(logger *zap.Logger, artifact *eventstore.Artifact)
	eventStore eventstore.EventStore
	endpoint   string
}

func (s *ObjectStore) Put(ctx context.Context, artifact *eventstore.Artifact) {
	logger := s.Log().With(s.LogFields(artifact)...)
	go s.put(logger, artifact)
}

func (s *ObjectStore) ServiceEndpoints() []string {
	if s.endpoint == "" {
		return []string{}
	}
	return []string{s.endpoint}
}
