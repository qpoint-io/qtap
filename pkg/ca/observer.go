package ca

import "github.com/qpoint-io/qtap/pkg/process"

type Observer interface {
	CertInjected(p *process.Process, path string, rootID uint64) error
	CertRemoved(p *process.Process, path string, rootID uint64) error
	CertRead(p *process.Process, path string) error
}

type DefaultObserver struct{}

func (d *DefaultObserver) CertInjected(p *process.Process, path string, rootID uint64) error {
	return nil
}

func (d *DefaultObserver) CertRemoved(p *process.Process, path string, rootID uint64) error {
	return nil
}

func (d *DefaultObserver) CertRead(p *process.Process, path string) error {
	return nil
}
