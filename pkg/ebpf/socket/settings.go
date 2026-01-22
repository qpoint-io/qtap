package socket

import (
	"fmt"

	"github.com/cilium/ebpf"
	"github.com/qpoint-io/qtap/pkg/config"
	"go.uber.org/zap"
)

// socket settings (maps to SOCKET_SETTINGS enum)
type socketSetting uint32

const (
	sockSettingIgnoreLoopback socketSetting = iota
	sockSettingDirection
	sockSettingStreamProtocols
)

// protocol stream flags (maps to STREAM_*_FLAG in settings.bpf.h)
const (
	streamProtocolHTTP  uint32 = 1 << 0
	streamProtocolRedis uint32 = 1 << 1
)

// possible traffic directions (maps to DIRECTION enum)
type trafficDirection uint32

const (
	trafficDirection_INGRESS trafficDirection = iota
	trafficDirection_EGRESS
	trafficDirection_EGRESS_INTERNAL
	trafficDirection_EGRESS_EXTERNAL
	trafficDirection_ALL
)

// socket setting value (maps to SocketSettingValue union)
type socketSettingValue struct {
	IgnoreLoopback  bool
	Direction       trafficDirection
	StreamProtocols uint32
}

type SettingsManager struct {
	// logger
	logger *zap.Logger

	// app config
	config *config.Config

	// socket settings map
	socketSettingsMap *ebpf.Map
}

func NewSocketSettingsManager(logger *zap.Logger, socketSettingsMap *ebpf.Map) *SettingsManager {
	m := &SettingsManager{
		logger:            logger,
		socketSettingsMap: socketSettingsMap,
	}

	return m
}

func (m *SettingsManager) SetConfig(conf *config.Config) {
	m.config = conf

	m.updateConfig()
}

func (m *SettingsManager) updateConfig() {
	// ensure we have a config
	if m.config == nil {
		return
	}

	// update ignore loopback
	if err := m.updateSocketSettingIgnoreLoopback(m.config.Tap.IgnoreLoopback); err != nil {
		m.logger.Error("persisting socket setting 'ignore_loopback'", zap.Error(err))
	}

	// update direction
	if err := m.updateSocketSettingDirection(m.config.Tap.Direction); err != nil {
		m.logger.Error("persisting socket setting 'direction'", zap.Error(err))
	}

	// update stream protocols
	if err := m.updateSocketSettingStreamProtocols(); err != nil {
		m.logger.Error("persisting socket setting 'stream_protocols'", zap.Error(err))
	}
}

func (m *SettingsManager) updateSocketSettingStreamProtocols() error {
	var protocols uint32

	// check top-level and endpoint-level HTTP configs
	if m.config.Tap.Http.HasStack() {
		protocols |= streamProtocolHTTP
	} else {
		for _, e := range m.config.Tap.Endpoints {
			if e.Http.HasStack() {
				protocols |= streamProtocolHTTP
				break
			}
		}
	}

	// check top-level and endpoint-level Redis configs
	if m.config.Tap.Redis.HasStack() {
		protocols |= streamProtocolRedis
	} else {
		for _, e := range m.config.Tap.Endpoints {
			if e.Redis.HasStack() {
				protocols |= streamProtocolRedis
				break
			}
		}
	}

	// create the value
	value := &socketSettingValue{
		StreamProtocols: protocols,
	}

	// set
	return m.updateSocketSetting(sockSettingStreamProtocols, value)
}

func (m *SettingsManager) updateSocketSettingIgnoreLoopback(ignore bool) error {
	// create the value
	value := &socketSettingValue{
		IgnoreLoopback: ignore,
	}

	// set
	return m.updateSocketSetting(sockSettingIgnoreLoopback, value)
}

func (m *SettingsManager) updateSocketSettingDirection(direction config.TrafficDirection) error {
	// determine the value
	value := &socketSettingValue{}

	// map the direction
	switch direction {
	case config.TrafficDirection_INGRESS:
		value.Direction = trafficDirection_INGRESS
	case config.TrafficDirection_EGRESS:
		value.Direction = trafficDirection_EGRESS
	case config.TrafficDirection_EGRESS_INTERNAL:
		value.Direction = trafficDirection_EGRESS_INTERNAL
	case config.TrafficDirection_EGRESS_EXTERNAL:
		value.Direction = trafficDirection_EGRESS_EXTERNAL
	default:
		value.Direction = trafficDirection_ALL
	}

	// set
	return m.updateSocketSetting(sockSettingDirection, value)
}

func (m *SettingsManager) updateSocketSetting(key socketSetting, value *socketSettingValue) error {
	// assuming the union size is 8 bytes max
	var rawValue uint32

	// prepare the value
	switch key {
	case sockSettingIgnoreLoopback:
		if value.IgnoreLoopback {
			rawValue = 1
		} else {
			rawValue = 0
		}
	case sockSettingDirection:
		rawValue = uint32(value.Direction)
	case sockSettingStreamProtocols:
		rawValue = value.StreamProtocols
	default:
		return fmt.Errorf("unknown socket setting: %d", key)
	}

	// update
	return m.socketSettingsMap.Update(uint32(key), rawValue, ebpf.UpdateAny)
}
