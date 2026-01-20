package tap

import (
	"github.com/qpoint-io/qtap/pkg/broker"
	"github.com/qpoint-io/qtap/pkg/container"
	"github.com/qpoint-io/qtap/pkg/process"
	"github.com/qpoint-io/qtap/pkg/synq"
)

type Tap struct {
	Core    *broker.Broker
	Plugins *broker.Broker

	processes  *synq.Map[int, *process.Process]
	containers *synq.Map[string, *container.Container]
}

type TapOpt func(*Tap)

func NewTap(opts ...TapOpt) *Tap {
	t := &Tap{
		Core:       broker.NewBroker(),
		Plugins:    broker.NewBroker(),
		processes:  synq.NewMap[int, *process.Process](),
		containers: synq.NewMap[string, *container.Container](),
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

func (t *Tap) Start() error {
	return nil
}

func (t *Tap) Stop() error {
	return nil
}
