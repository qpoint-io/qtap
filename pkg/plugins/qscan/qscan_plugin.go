package qscan

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	classifier "github.com/jonfriesen/trie-url-classifier"
	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/objectstore"
	"github.com/qpoint-io/qtap/pkg/synq"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

const (
	pluginTypeQscan plugins.PluginType = "qscan"
)

const (
	defaultCacheTTL             = time.Hour * 24 // 24 hour TTL
	defaultCacheSize            = 4096           // items before dropping lru
	defaultSampleBaseline       = 100            // 100 samples before we start sampling
	defaultSampleRate           = 0.1            // 10% sample rate
	defaultMinLearningCount     = 0              // minimum learning count for classifier
	defaultCardinalityThreshold = 0.75           // cardinality threshold for classifier
	defaultMinSamples           = 2              // minimum samples for classifier
	defaultMaxValuesPerNode     = 100            // maximum values per node
	defaultPruneHighCardinality = true           // prune high cardinality nodes
)

// QscanConfig represents the configuration for the qscan plugin
//
// Example YAML configuration:
// ```yaml
//
//	cache_ttl: 24h                 # How long to keep URLs in the cache (default: 24h)
//	cache_size: 4096               # Maximum number of URLs to track (default: 4096)
//	sample_baseline: 100           # Always sample first N requests per URL (default: 100)
//	sample_rate: 0.1               # Sample rate after baseline (default: 0.1 or 10%)
//	record_document: true          # Save a single JSON document; if false, nothing is saved (default: false)
//	verbose: true                  # Print verbose output to the console (default: false)
//	qscan_cloud: true              # Use the qscan cloud service (default: false)
//	classifier:
//	  min_learning_count: 5        # Minimum learning count for classifier (default: 5)
//	  cardinality_threshold: 0.75  # Cardinality threshold for classifier (default: 0.75)
//	  min_samples: 2               # Minimum samples for classifier (default: 2)
//	  max_values_per_node: 100     # Maximum values per node (default: 100)
//	  prune_high_cardinality: true # Prune high cardinality nodes (default: true)
//	monitors:
//	  - type: CREDIT_CARD          # Entity type to monitor
//	    record_value: true         # Whether to record the value in the document
//	  - type: EMAIL_ADDRESS
//	    record_value: false
//
// ```
type QscanConfig struct {
	CacheTTL          time.Duration    `json:"cacheTTL" yaml:"cache_ttl"`
	CacheSize         int              `json:"cacheSize" yaml:"cache_size"`
	SampleBaseline    uint32           `json:"sampleBaseline" yaml:"sample_baseline"`
	SampleRate        float64          `json:"sampleRate" yaml:"sample_rate"`
	Classifier        ClassifierConfig `json:"classifier" yaml:"classifier"`
	Monitors          []Monitor        `json:"monitors" yaml:"monitors"`
	RecordDocument    bool             `json:"recordDocument" yaml:"record_document"` // Save a single JSON document; if false, nothing is saved
	Verbose           bool             `json:"verbose" yaml:"verbose"`
	QscanCloudEnabled bool             `json:"qscanCloud" yaml:"qscan_cloud"`
	ObjectStoreID     string           `json:"objectStoreID" yaml:"object_store_id"`
}

// ClassifierConfig represents the configuration for the URL classifier
type ClassifierConfig struct {
	MinLearningCount     int     `json:"minLearningCount" yaml:"min_learning_count"`
	CardinalityThreshold float64 `json:"cardinalityThreshold" yaml:"cardinality_threshold"`
	MinSamples           int     `json:"minSamples" yaml:"min_samples"`
	MaxValuesPerNode     int     `json:"maxValuesPerNode" yaml:"max_values_per_node"`
	PruneHighCardinality *bool   `json:"pruneHighCardinality" yaml:"prune_high_cardinality"`
}

type Monitor struct {
	Type        string `json:"type" yaml:"type"`                // Entity type
	RecordValue bool   `json:"recordValue" yaml:"record_value"` // Copy the value to an artifact repository
}

func (m *QscanConfig) Summary() map[string]any {
	summary := make(map[string]any)

	summary["verbose"] = strconv.FormatBool(m.Verbose)
	summary["record_document"] = strconv.FormatBool(m.RecordDocument)
	summary["qscan_cloud"] = strconv.FormatBool(m.QscanCloudEnabled)
	for _, monitor := range m.Monitors {
		summary["entity."+monitor.Type] = strconv.FormatBool(monitor.RecordValue)
	}
	return summary
}

func (m *QscanConfig) RecordValue(entity string) bool {
	for _, monitor := range m.Monitors {
		if monitor.Type == entity {
			return monitor.RecordValue
		}
	}
	return false
}

type Factory struct {
	logger *zap.Logger
	config *QscanConfig

	sampler     *Sampler
	classifiers *synq.Map[string, *classifier.Classifier] // keyed by domain
}

func (f *Factory) Init(logger *zap.Logger, config yaml.Node) {
	f.logger = logger

	// parse
	var cfg QscanConfig
	if err := config.Decode(&cfg); err != nil {
		logger.Error("error decoding config", zap.Error(err))
		return
	}

	var cacheTTL time.Duration
	if cfg.CacheTTL > 0 {
		cacheTTL = cfg.CacheTTL
	} else {
		cacheTTL = defaultCacheTTL
	}

	var cacheSize int
	if cfg.CacheSize > 0 {
		cacheSize = cfg.CacheSize
	} else {
		cacheSize = defaultCacheSize
	}
	cache := expirable.NewLRU[string, uint32](cacheSize, nil, cacheTTL)
	f.classifiers = synq.NewMap[string, *classifier.Classifier]()

	var baseline uint32
	if cfg.SampleBaseline > 0 {
		baseline = cfg.SampleBaseline
	} else {
		baseline = defaultSampleBaseline
	}

	var rate float64
	if cfg.SampleRate > 0 {
		if cfg.SampleRate > 1 {
			// If provided as a percentage (e.g., 10 for 10%)
			rate = cfg.SampleRate / 100
		} else {
			// If provided as a decimal (e.g., 0.1 for 10%)
			rate = cfg.SampleRate
		}
	} else {
		rate = defaultSampleRate
	}

	f.sampler = NewSampler(cache, baseline, rate)
	f.config = &cfg
}

func (f *Factory) NewHttpInstance(ctx plugins.PluginContext, svcs *services.ServiceRegistry) plugins.HttpPluginInstance {
	f.logger.Debug("new plugin instance created")
	fi := &filterInstance{
		ctx:     ctx,
		logger:  f.logger,
		config:  f.config,
		factory: f,
	}

	if os, err := services.GetService[objectstore.ObjectStore](ctx.Context(), svcs, objectstore.TypeObjectStore, f.config.ObjectStoreID); err != nil {
		f.logger.Error("failed to get object store", zap.Error(err))
	} else {
		fi.objectstore = os
	}

	return fi
}

func (f *Factory) NewMySQLInstance(ctx plugins.PluginContext, svcs *services.ServiceRegistry) plugins.MySQLPluginInstance {
	f.logger.Debug("new MySQL plugin instance created")
	mi := &mysqlFilterInstance{
		ctx:     ctx,
		logger:  f.logger,
		config:  f.config,
		factory: f,
	}

	if os, err := services.GetService[objectstore.ObjectStore](ctx.Context(), svcs, objectstore.TypeObjectStore, f.config.ObjectStoreID); err != nil {
		f.logger.Error("failed to get object store", zap.Error(err))
	} else {
		mi.objectstore = os
	}

	return mi
}

func (f *Factory) NewRedisInstance(ctx plugins.PluginContext, svcs *services.ServiceRegistry) plugins.RedisPluginInstance {
	f.logger.Debug("new Redis plugin instance created")
	ri := &redisFilterInstance{
		ctx:     ctx,
		logger:  f.logger,
		config:  f.config,
		factory: f,
	}

	if os, err := services.GetService[objectstore.ObjectStore](ctx.Context(), svcs, objectstore.TypeObjectStore, f.config.ObjectStoreID); err != nil {
		f.logger.Error("failed to get object store", zap.Error(err))
	} else {
		ri.objectstore = os
	}

	return ri
}

func (f *Factory) NewKafkaInstance(ctx plugins.PluginContext, svcs *services.ServiceRegistry) plugins.KafkaPluginInstance {
	f.logger.Debug("new Kafka plugin instance created")
	ki := &kafkaFilterInstance{
		ctx:     ctx,
		logger:  f.logger,
		config:  f.config,
		factory: f,
	}

	if os, err := services.GetService[objectstore.ObjectStore](ctx.Context(), svcs, objectstore.TypeObjectStore, f.config.ObjectStoreID); err != nil {
		f.logger.Error("failed to get object store", zap.Error(err))
	} else {
		ki.objectstore = os
	}

	return ki
}

func (f *Factory) shouldSampleDB(key string) (bool, string) {
	return f.sampler.ShouldSample(key)
}

func (f *Factory) Destroy() {
	f.logger.Debug("plugin destroyed")
}

func (f *Factory) shouldSample(fullURL string) (bool, string) {
	// Parse the URL to extract domain and path
	parsed, err := url.Parse(fullURL)
	if err != nil {
		return false, "url_parse_error"
	}

	domain := parsed.Host
	path := parsed.Path

	// Get or create a classifier for this domain (uses RWMutex internally via synq.Map)
	clf := f.getOrCreateClassifier(domain)

	// Classify the path to normalize variable segments (e.g., /users/123 -> /users/{id})
	normalized, err := clf.Classify(path)
	if insuffErr, ok := errors.AsType[*classifier.InsufficientDataError](err); ok {
		return false, fmt.Sprintf("insufficient_data: %d", insuffErr.Count)
	}
	if normalized == "" {
		return false, "normalized_empty"
	}

	// Use domain + normalized path for per-endpoint sampling
	return f.sampler.ShouldSample(domain + normalized)
}

// getOrCreateClassifier returns the classifier for a domain, creating one if needed
func (f *Factory) getOrCreateClassifier(domain string) *classifier.Classifier {
	// Try to load existing classifier (fast path with read lock)
	if clf, ok := f.classifiers.Load(domain); ok {
		return clf
	}

	// Build classifier configuration
	cfg := f.config.Classifier
	minLearningCount := cfg.MinLearningCount
	if minLearningCount == 0 {
		minLearningCount = defaultMinLearningCount
	}

	cardinalityThreshold := cfg.CardinalityThreshold
	if cardinalityThreshold == 0 {
		cardinalityThreshold = defaultCardinalityThreshold
	}

	minSamples := cfg.MinSamples
	if minSamples == 0 {
		minSamples = defaultMinSamples
	}

	maxValuesPerNode := cfg.MaxValuesPerNode
	if maxValuesPerNode == 0 {
		maxValuesPerNode = defaultMaxValuesPerNode
	}

	pruneHighCardinality := defaultPruneHighCardinality
	if cfg.PruneHighCardinality != nil {
		pruneHighCardinality = *cfg.PruneHighCardinality
	}

	// Create new classifier
	newClf := classifier.NewClassifier(
		classifier.WithMinLearningCount(minLearningCount),
		classifier.WithCardinalityThreshold(cardinalityThreshold),
		classifier.WithMinSamples(minSamples),
		classifier.WithMaxValuesPerNode(maxValuesPerNode),
		classifier.WithPruneHighCardinality(pruneHighCardinality),
	)

	// LoadOrInsert: returns existing if another goroutine created it first
	actual, _ := f.classifiers.LoadOrInsert(domain, newClf)
	return actual
}

func (f *Factory) PluginType() plugins.PluginType {
	return pluginTypeQscan
}
