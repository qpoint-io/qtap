//go:build linux

package cmd

import (
	"fmt"

	"github.com/cilium/ebpf/ringbuf"
	"github.com/qpoint-io/qtap/internal/tap"
	"github.com/qpoint-io/qtap/pkg/ebpf/common"
	"github.com/qpoint-io/qtap/pkg/ebpf/tls/gotls"
	"github.com/qpoint-io/qtap/pkg/ebpf/tls/javassl"
)

func newEbpfNodeTlsProbesCreator(objs *tap.TapObjects) func() []*common.Uprobe {
	return func() []*common.Uprobe {
		return []*common.Uprobe{
			// node < v15.0.0
			common.NewUprobe("_ZN4node7TLSWrapC2E", objs.NodetlsProbeEntryTLSWrapMemfn),
			common.NewUprobe("_ZN4node7TLSWrap7ClearInE", objs.NodetlsProbeEntryTLSWrapMemfn),
			common.NewUprobe("_ZN4node7TLSWrap8ClearOutE", objs.NodetlsProbeEntryTLSWrapMemfn),
			common.NewUprobe("_ZN4node7TLSWrapD1Ev", objs.NodetlsProbeEntryTLSWrapDestructor),

			// node >= v15.0.0
			common.NewUprobe("_ZN4node6crypto7TLSWrapC2E", objs.NodetlsProbeEntryTLSWrapMemfn),
			common.NewUprobe("_ZN4node6crypto7TLSWrap7ClearInE", objs.NodetlsProbeEntryTLSWrapMemfn),
			common.NewUprobe("_ZN4node6crypto7TLSWrap8ClearOutE", objs.NodetlsProbeEntryTLSWrapMemfn),
			common.NewUprobe("_ZN4node6crypto7TLSWrapD1Ev", objs.NodetlsProbeEntryTLSWrapDestructor),
		}
	}
}

func newEbpfGoTlsProbesCreator(objs *tap.TapObjects) func() []*common.Uprobe {
	return func() []*common.Uprobe {
		return []*common.Uprobe{
			// entries
			common.NewUprobe(gotls.SymbolSSLRead, objs.GotlsProbeEntryTlsConnRead),
			common.NewUprobe(gotls.SymbolSSLWrite, objs.GotlsProbeEntryTlsConnWrite),

			// exits
			common.NewUretprobe(gotls.SymbolSSLRead, objs.GotlsProbeRetTlsConnRead),
			common.NewUretprobe(gotls.SymbolSSLWrite, objs.GotlsProbeRetTlsConnWrite),
		}
	}
}

func newEbpfJavaSslEngineBridge(objs *tap.TapObjects) (*javassl.SslEngineBridge, error) {
	rb, err := ringbuf.NewReader(objs.JavaSslEngineEvents)
	if err != nil {
		return nil, fmt.Errorf("failed to create java ssl engine events reader: %w", err)
	}

	return &javassl.SslEngineBridge{
		JavaProcessPidMap:      objs.JavaProcessPidMap,
		SessionIgnoreMap:       objs.JavaSslEngineSessionIgnoreMap,
		SyscallCorrelatedMap:   objs.JavaSslEngineSyscallCorrelatedMap,
		UprobeCorrelatedMap:    objs.JavaSslEngineUprobeCorrelatedMap,
		EventsRingbufferReader: rb,
		SocketProbes: []*common.Tracepoint{
			common.NewTracepoint("syscalls", "sys_enter_write", objs.JavaSslEngineSysEnterWrite),
			common.NewTracepoint("syscalls", "sys_exit_write", objs.JavaSslEngineSysExitWrite),
			common.NewTracepoint("syscalls", "sys_enter_read", objs.JavaSslEngineSysEnterRead),
			common.NewTracepoint("syscalls", "sys_exit_read", objs.JavaSslEngineSysExitRead),
			common.NewTracepoint("syscalls", "sys_enter_writev", objs.JavaSslEngineSysEnterWritev),
			common.NewTracepoint("syscalls", "sys_exit_writev", objs.JavaSslEngineSysExitWritev),
			common.NewTracepoint("syscalls", "sys_enter_readv", objs.JavaSslEngineSysEnterReadv),
			common.NewTracepoint("syscalls", "sys_exit_readv", objs.JavaSslEngineSysExitReadv),
			common.NewTracepoint("syscalls", "sys_enter_sendto", objs.JavaSslEngineSysEnterSendto),
			common.NewTracepoint("syscalls", "sys_exit_sendto", objs.JavaSslEngineSysExitSendto),
			common.NewTracepoint("syscalls", "sys_enter_recvfrom", objs.JavaSslEngineSysEnterRecvfrom),
			common.NewTracepoint("syscalls", "sys_exit_recvfrom", objs.JavaSslEngineSysExitRecvfrom),
			common.NewTracepoint("syscalls", "sys_enter_close", objs.JavaSslEngineSysEnterClose),
		},
	}, nil
}

/*
 * ⚠️ Special case for Java SSL probing:
 *
 * Unlike other languages where we attach uprobes and uretprobes directly to SSL read/write functions,
 * Java uses byte code manipulation to add entry and exit probes to Java functions (normally
 * handled by eBPF). Instead, we:
 *
 * 1. Take the entry args or return params from Java
 * 2. Call a separate C function that can be probed
 * 3. Attach uprobes only to these C functions, which receive both entry and exit args
 *
 * Because of this entry probes are used for both entry and exits, since the exit functions
 * are not actually exits but functions we control.
 */
func newEbpfJavaSslProbesCreator(objs *tap.TapObjects) func() []*common.Uprobe {
	return func() []*common.Uprobe {
		return []*common.Uprobe{
			// SSLSocket (direct SSL) entries and exits
			common.NewUprobe("ssl_read_entry", objs.JavaSslReadEntry),
			common.NewUprobe("ssl_write_entry", objs.JavaSslWriteEntry),
			common.NewUprobe("ssl_read_exit", objs.JavaSslReadExit),
			common.NewUprobe("ssl_write_exit", objs.JavaSslWriteExit),

			// SSLEngine (de-coupled NIO SSL) wrap/unwrap
			common.NewUprobe("ssl_engine_wrap_exit", objs.JavaSslEngineWrapExit),
			common.NewUprobe("ssl_engine_unwrap_exit", objs.JavaSslEngineUnwrapExit),
		}
	}
}
