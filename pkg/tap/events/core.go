package events

import "github.com/qpoint-io/qtap/pkg/process"

type ProcessStartedEvent struct {
	Process *process.Process
}

type ProcessReplacedEvent struct {
	Process *process.Process
}

type ProcessStoppedEvent struct {
	Process *process.Process
}
