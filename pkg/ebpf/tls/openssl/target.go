package openssl

import (
	"context"
	"debug/elf"
	"errors"
	"fmt"

	"github.com/cilium/ebpf/link"
	"github.com/qpoint-io/qtap/pkg/binutils"
	"github.com/qpoint-io/qtap/pkg/ebpf/common"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
)

// enum for target type
type TargetType int

const (
	TargetTypeShared TargetType = iota
	TargetTypeStatic
)

type OpenSSLTarget struct {
	// name
	name string

	// container
	containerID string

	// type
	type_ TargetType

	// logger
	logger *zap.Logger

	// absolute path to the target location
	location string

	// cache entry
	cacheEntry *ScanResult

	// uprobes
	probes []*common.Uprobe

	// elf file
	ef *binutils.Elf
}

func NewOpenSSLTarget(logger *zap.Logger, name, containerID, location string, ef *binutils.Elf, type_ TargetType, probes []*common.Uprobe, cacheEntry *ScanResult) *OpenSSLTarget {
	return &OpenSSLTarget{
		logger:      logger,
		name:        name,
		containerID: containerID,
		location:    location,
		ef:          ef,
		type_:       type_,
		probes:      probes,
		cacheEntry:  cacheEntry,
	}
}

func (t *OpenSSLTarget) Start(ctx context.Context) error {
	ctx, span := tracer.WithoutCancel(ctx, "OpenSSLTarget.Start")
	defer span.End()
	span.SetAttributes(
		attribute.String("name", t.name),
		attribute.String("container_id", t.containerID),
		attribute.String("location", t.location),
	)

	// create a link to the executable
	ex, err := link.OpenExecutable(t.location)
	if err != nil {
		return fmt.Errorf("opening executable: %w", err)
	}

	// searched symbols to use
	var syms []elf.Symbol

	// if we have a cache entry, use it
	if t.cacheEntry != nil && t.cacheEntry.Symbols != nil {
		syms = t.cacheEntry.Symbols
	}

	if syms == nil {
		// create a symbol search from the probes
		search := []binutils.SymbolSearch{}
		for _, p := range t.probes {
			search = append(search, binutils.SymbolSearch{
				Name:          p.Function,
				MatchStrategy: binutils.MatchStrategyExact,
			})
		}

		// open the ELF file if we don't have one
		ef := t.ef
		if ef == nil {
			ef, err = binutils.NewElf(ctx, t.location, "/", false)
			if err != nil {
				return err
			}

			defer ef.Close()
		}

		// find the symbols from the binary
		syms, err = ef.SearchSymbols(ctx, search, elf.SHT_SYMTAB, elf.SHT_DYNSYM)
		if err != nil && !errors.Is(err, binutils.ErrNoSymbols) {
			t.logger.Debug("Failed to search for symbols", zap.Error(err))
		}

		// calculate the addresses of the symbols
		syms = ef.CalculateUprobeAddresses(ctx, syms)

		// cache the result
		if t.cacheEntry != nil {
			t.cacheEntry.Symbols = syms
		}
	}

	// attach all of the probes
	for _, probe := range t.probes {
		var err error

		// ensure the symbol exists
		for _, sym := range syms {
			if sym.Name == probe.Function {
				// if this is a static target, let's ensure the symbol is embedded
				if t.type_ == TargetTypeStatic {
					if sym.Value == 0 || sym.Size == 0 {
						continue
					}
				}

				// debug
				if !probe.IsRet {
					t.logger.Debug("Attaching OpenSSL probe",
						zap.String("target", t.name),
						zap.String("container_id", t.containerID),
						zap.String("function (symbol)", probe.Function),
						zap.Uint64("address", sym.Value),
					)
				}

				err = probe.Attach(ctx, ex, sym.Value)
				if err != nil {
					return fmt.Errorf("attaching probe to %s:%w", probe.Function, err)
				}

				break
			}
		}
	}

	return nil
}

func (t *OpenSSLTarget) Stop() error {
	_, span := tracer.Start(context.TODO(), "OpenSSLTarget.Stop")
	defer span.End()
	// disconnect all of the probes
	for _, ln := range t.probes {
		if err := ln.Detach(); err != nil {
			return fmt.Errorf("closing probe link: %w", err)
		}
	}

	return nil
}
