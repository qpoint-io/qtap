package common

import (
	"fmt"
	"strings"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
)

type Kprobe struct {
	// meta
	Functions []string
	Prog      *ebpf.Program
	IsRet     bool

	// state
	conn link.Link
}

func NewKprobe(prog *ebpf.Program, functions ...string) *Kprobe {
	return &Kprobe{
		Functions: functions,
		Prog:      prog,
	}
}

func NewKretprobe(prog *ebpf.Program, functions ...string) *Kprobe {
	return &Kprobe{
		Functions: functions,
		Prog:      prog,
		IsRet:     true,
	}
}

func (k *Kprobe) Attach() error {
	var conn link.Link
	var err error

	// establish the link
	if k.IsRet {
		conn, err = tryAttach(link.Kretprobe, k.Prog, k.Functions)
	} else {
		conn, err = tryAttach(link.Kprobe, k.Prog, k.Functions)
	}

	// set the state
	k.conn = conn

	// return the error
	return err
}

func (k *Kprobe) Detach() error {
	if k.conn == nil {
		return nil
	}

	return k.conn.Close()
}

func (k *Kprobe) ID() string {
	return "kprobe/" + strings.Join(k.Functions, ", ")
}

type kprobeLinkerFn func(symbol string, prog *ebpf.Program, opts *link.KprobeOptions) (link.Link, error)

func tryAttach(fn kprobeLinkerFn, prog *ebpf.Program, functions []string) (link.Link, error) {
	var conn link.Link
	var err error

	for _, function := range functions {
		conn, err = fn(function, prog, nil)

		if err == nil {
			return conn, nil
		}
	}

	return nil, fmt.Errorf("failed to attach to any of the functions: %s, error: %w", strings.Join(functions, ", "), err)
}
