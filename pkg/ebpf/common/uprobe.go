package common

import (
	"errors"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
)

type Uprobe struct {
	// meta
	Function string
	Prog     *ebpf.Program
	IsRet    bool

	// state
	conn link.Link
}

func NewUprobe(function string, prog *ebpf.Program) *Uprobe {
	return &Uprobe{
		Function: function,
		Prog:     prog,
	}
}

func NewUretprobe(function string, prog *ebpf.Program) *Uprobe {
	return &Uprobe{
		Function: function,
		Prog:     prog,
		IsRet:    true,
	}
}

func (k *Uprobe) Attach(exe *link.Executable, addr uint64) error {
	if exe == nil {
		return errors.New("executable is nil")
	}

	var conn link.Link
	var err error

	// establish the link
	if k.IsRet {
		conn, err = exe.Uretprobe(k.Function, k.Prog, &link.UprobeOptions{Address: addr})
	} else {
		conn, err = exe.Uprobe(k.Function, k.Prog, &link.UprobeOptions{Address: addr})
	}

	// set the state
	k.conn = conn

	// return the error
	return err
}

func (k *Uprobe) Detach() error {
	if k.conn == nil {
		return nil
	}

	return k.conn.Close()
}

func (k *Uprobe) ID() string {
	return "uprobe/" + k.Function
}
