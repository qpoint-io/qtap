package kafka

import (
	"fmt"
	"time"

	"github.com/qpoint-io/qtap/pkg/plugins"
)

// ApiKey constants for Kafka protocol
const (
	ApiKeyProduce         int16 = 0
	ApiKeyFetch           int16 = 1
	ApiKeyListOffsets     int16 = 2
	ApiKeyMetadata        int16 = 3
	ApiKeyLeaderAndIsr    int16 = 4
	ApiKeyStopReplica     int16 = 5
	ApiKeyOffsetCommit    int16 = 8
	ApiKeyOffsetFetch     int16 = 9
	ApiKeyFindCoordinator int16 = 10
	ApiKeyJoinGroup       int16 = 11
	ApiKeyHeartbeat       int16 = 12
	ApiKeyLeaveGroup      int16 = 13
	ApiKeySyncGroup       int16 = 14
	ApiKeyDescribeGroups  int16 = 15
	ApiKeyListGroups      int16 = 16
	ApiKeySaslHandshake   int16 = 17
	ApiKeyApiVersions     int16 = 18
	ApiKeyCreateTopics    int16 = 19
	ApiKeyDeleteTopics    int16 = 20
	ApiKeyDeleteRecords   int16 = 21
	ApiKeySaslAuthenticate int16 = 36
	ApiKeyCreatePartitions int16 = 37
	ApiKeyDescribeConfigs  int16 = 32
	ApiKeyAlterConfigs     int16 = 33
)

// apiKeyNames maps ApiKey values to human-readable names
var apiKeyNames = map[int16]string{
	0:  "Produce",
	1:  "Fetch",
	2:  "ListOffsets",
	3:  "Metadata",
	4:  "LeaderAndIsr",
	5:  "StopReplica",
	6:  "UpdateMetadata",
	7:  "ControlledShutdown",
	8:  "OffsetCommit",
	9:  "OffsetFetch",
	10: "FindCoordinator",
	11: "JoinGroup",
	12: "Heartbeat",
	13: "LeaveGroup",
	14: "SyncGroup",
	15: "DescribeGroups",
	16: "ListGroups",
	17: "SaslHandshake",
	18: "ApiVersions",
	19: "CreateTopics",
	20: "DeleteTopics",
	21: "DeleteRecords",
	22: "InitProducerId",
	23: "OffsetForLeaderEpoch",
	24: "AddPartitionsToTxn",
	25: "AddOffsetsToTxn",
	26: "EndTxn",
	27: "WriteTxnMarkers",
	28: "TxnOffsetCommit",
	29: "DescribeAcls",
	30: "CreateAcls",
	31: "DeleteAcls",
	32: "DescribeConfigs",
	33: "AlterConfigs",
	34: "AlterReplicaLogDirs",
	35: "DescribeLogDirs",
	36: "SaslAuthenticate",
	37: "CreatePartitions",
	38: "CreateDelegationToken",
	39: "RenewDelegationToken",
	40: "ExpireDelegationToken",
	41: "DescribeDelegationToken",
	42: "DeleteGroups",
	43: "ElectLeaders",
	44: "IncrementalAlterConfigs",
	45: "AlterPartitionReassignments",
	46: "ListPartitionReassignments",
	47: "OffsetDelete",
	48: "DescribeClientQuotas",
	49: "AlterClientQuotas",
	50: "DescribeUserScramCredentials",
	51: "AlterUserScramCredentials",
	56: "AlterPartition",
	57: "UpdateFeatures",
	60: "DescribeCluster",
	61: "DescribeProducers",
	65: "DescribeTransactions",
	66: "ListTransactions",
	67: "AllocateProducerIds",
}

// ApiKeyName returns a human-readable name for the given ApiKey
func ApiKeyName(apiKey int16) string {
	if name, ok := apiKeyNames[apiKey]; ok {
		return name
	}
	return fmt.Sprintf("Unknown(%d)", apiKey)
}

// Kafka error codes
var kafkaErrorNames = map[int16]string{
	0:  "NONE",
	-1: "UNKNOWN_SERVER_ERROR",
	1:  "OFFSET_OUT_OF_RANGE",
	2:  "CORRUPT_MESSAGE",
	3:  "UNKNOWN_TOPIC_OR_PARTITION",
	4:  "INVALID_FETCH_SIZE",
	5:  "LEADER_NOT_AVAILABLE",
	6:  "NOT_LEADER_OR_FOLLOWER",
	7:  "REQUEST_TIMED_OUT",
	8:  "BROKER_NOT_AVAILABLE",
	9:  "REPLICA_NOT_AVAILABLE",
	10: "MESSAGE_TOO_LARGE",
	11: "STALE_CONTROLLER_EPOCH",
	12: "OFFSET_METADATA_TOO_LARGE",
	13: "NETWORK_EXCEPTION",
	14: "COORDINATOR_LOAD_IN_PROGRESS",
	15: "COORDINATOR_NOT_AVAILABLE",
	16: "NOT_COORDINATOR",
	17: "INVALID_TOPIC_EXCEPTION",
	18: "RECORD_LIST_TOO_LARGE",
	19: "NOT_ENOUGH_REPLICAS",
	20: "NOT_ENOUGH_REPLICAS_AFTER_APPEND",
	21: "INVALID_REQUIRED_ACKS",
	22: "ILLEGAL_GENERATION",
	23: "INCONSISTENT_GROUP_PROTOCOL",
	24: "INVALID_GROUP_ID",
	25: "UNKNOWN_MEMBER_ID",
	26: "INVALID_SESSION_TIMEOUT",
	27: "REBALANCE_IN_PROGRESS",
	28: "INVALID_COMMIT_OFFSET_SIZE",
	29: "TOPIC_AUTHORIZATION_FAILED",
	30: "GROUP_AUTHORIZATION_FAILED",
	31: "CLUSTER_AUTHORIZATION_FAILED",
	32: "INVALID_TIMESTAMP",
	33: "UNSUPPORTED_SASL_MECHANISM",
	34: "ILLEGAL_SASL_STATE",
	35: "UNSUPPORTED_VERSION",
	36: "TOPIC_ALREADY_EXISTS",
	37: "INVALID_PARTITIONS",
	38: "INVALID_REPLICATION_FACTOR",
	39: "INVALID_REPLICA_ASSIGNMENT",
	40: "INVALID_CONFIG",
	41: "NOT_CONTROLLER",
	42: "INVALID_REQUEST",
	43: "UNSUPPORTED_FOR_MESSAGE_FORMAT",
	44: "POLICY_VIOLATION",
	45: "OUT_OF_ORDER_SEQUENCE_NUMBER",
	46: "DUPLICATE_SEQUENCE_NUMBER",
	47: "INVALID_PRODUCER_EPOCH",
	48: "INVALID_TXN_STATE",
	49: "INVALID_PRODUCER_ID_MAPPING",
	50: "INVALID_TRANSACTION_TIMEOUT",
	51: "CONCURRENT_TRANSACTIONS",
	52: "TRANSACTION_COORDINATOR_FENCED",
	53: "TRANSACTIONAL_ID_AUTHORIZATION_FAILED",
	54: "SECURITY_DISABLED",
	55: "OPERATION_NOT_ATTEMPTED",
	58: "REASSIGNMENT_IN_PROGRESS",
}

// KafkaErrorName returns a human-readable name for the given error code
func KafkaErrorName(errorCode int16) string {
	if name, ok := kafkaErrorNames[errorCode]; ok {
		return name
	}
	return fmt.Sprintf("UNKNOWN_ERROR(%d)", errorCode)
}

// RequestHeader represents a Kafka request header
type RequestHeader struct {
	ApiKey        int16
	ApiVersion    int16
	CorrelationID int32
	ClientID      string
}

// Request represents a parsed Kafka request
type Request struct {
	Header       RequestHeader
	MessageSize  int32
	Topics       []string // Extracted topic names
	GroupID      string   // Consumer group (for JoinGroup, SyncGroup, OffsetCommit, etc.)
	TotalSize    int      // Total bytes consumed including length prefix
}

// ResponseHeader represents a Kafka response header
type ResponseHeader struct {
	CorrelationID int32
}

// Response represents a parsed Kafka response
type Response struct {
	Header      ResponseHeader
	MessageSize int32
	ErrorCode   int16  // Top-level error code (if applicable)
	TotalSize   int    // Total bytes consumed including length prefix
}

// PendingRequest represents a request awaiting its response
type PendingRequest struct {
	Request    *Request
	Timestamp  time.Time
	PluginConn *plugins.Connection
}
