package tls

import (
	"fmt"
	"slices"
	"testing"

	"github.com/qpoint-io/qtap/pkg/process"
	"github.com/stretchr/testify/require"
	gomock "go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"
)

func TestTlsManager(t *testing.T) {
	ctx := t.Context()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	allProbes := []string{"openssl", "gnutls"}
	testProcScanRes := func(proc *process.Process, probes ...string) *ScanResult {
		res := &ScanResult{
			Hash:         "hash(" + proc.Exe + ")",
			ProbeResults: make(map[string]ProbeScanResult),
		}
		for _, probe := range allProbes {
			res.ProbeResults[probe] = &testProbeScanResult{name: probe, detected: slices.Contains(probes, probe)}
		}
		return res
	}

	mockScanner := NewMockTargetScanner(ctrl)
	manager := NewTlsManager(zaptest.NewLogger(t), mockScanner)

	/*
		we will start a new process on the root namespace

		1. the manager should scan the process
		2. it should scan the container
		3. it should attach any detected probes
	*/
	proc2, proc2Closer := testProcess(2, "/bin/ls", "root")
	proc2ScanRes := testProcScanRes(proc2, "openssl")
	mockScanner.EXPECT().Scan(gomock.Any(), &ExeScannable{
		Path:    "/proc/2/exe",
		Cmdline: []string{"/bin/ls"},
		Root:    "/proc/2/root",
	}).Return(proc2ScanRes, nil)
	mockScanner.EXPECT().Attach(gomock.Any(), 2, "/proc/2/exe", proc2ScanRes).Return(proc2Closer, nil)

	ctrRootScanRes := &ContainerScanResult{
		SharedLibraries: map[string][]*SharedLibrary{
			"openssl": {
				{
					Name:  "libssl.so",
					Paths: []string{"/usr/lib/libssl.so"},
				},
			},
		},
	}
	ctrRootCloser := &testCloser{}
	mockScanner.EXPECT().ScanContainer(gomock.Any(), "root", "/proc/2/root").Return(ctrRootScanRes, nil)
	mockScanner.EXPECT().AttachContainer(gomock.Any(), ctrRootScanRes).Return(ctrRootCloser, nil)

	err := manager.ProcessStarted(ctx, proc2)
	require.NoError(t, err)

	require.Equal(t, []string{"openssl"}, proc2.TLSProbeTypesDetected)
	require.Equal(t, 0, proc2Closer.closes)
	require.Equal(t, 0, ctrRootCloser.closes)

	/*
		start two processes on a docker container

		1. the manager should scan the process
		2. it should scan the container ONCE
		3. it should attach any detected probes
	*/

	proc3, proc3Closer := testProcess(3, "/bin/sh", "container-1")
	proc3ScanRes := testProcScanRes(proc3)
	mockScanner.EXPECT().Scan(gomock.Any(), &ExeScannable{
		Path:    "/proc/3/exe",
		Cmdline: []string{"/bin/sh"},
		Root:    "/proc/3/root",
	}).Return(proc3ScanRes, nil)
	mockScanner.EXPECT().Attach(gomock.Any(), 3, "/proc/3/exe", proc3ScanRes).Return(proc3Closer, nil)

	proc4, proc4Closer := testProcess(4, "/bin/sudo", "container-1")
	proc4ScanRes := testProcScanRes(proc4)
	mockScanner.EXPECT().Scan(gomock.Any(), &ExeScannable{
		Path:    "/proc/4/exe",
		Cmdline: []string{"/bin/sudo"},
		Root:    "/proc/4/root",
	}).Return(proc4ScanRes, nil)
	mockScanner.EXPECT().Attach(gomock.Any(), 4, "/proc/4/exe", proc4ScanRes).Return(proc4Closer, nil)

	ctr1ScanRes := &ContainerScanResult{
		SharedLibraries: map[string][]*SharedLibrary{},
	}
	ctr1Closer := &testCloser{}
	mockScanner.EXPECT().ScanContainer(gomock.Any(), "container-1", "/proc/3/root").Return(ctr1ScanRes, nil)
	mockScanner.EXPECT().AttachContainer(gomock.Any(), ctr1ScanRes).Return(ctr1Closer, nil)

	err = manager.ProcessStarted(ctx, proc3)
	require.NoError(t, err)
	err = manager.ProcessStarted(ctx, proc4)
	require.NoError(t, err)

	require.Empty(t, proc3.TLSProbeTypesDetected)
	require.Empty(t, proc4.TLSProbeTypesDetected)
	require.Equal(t, 0, proc3Closer.closes)
	require.Equal(t, 0, proc4Closer.closes)
	require.Equal(t, 0, ctr1Closer.closes)

	/*
		we will now stop the container processes

		1. the manager should detach the processes
		2. other processes should not be affected
		3. the container should remain attached
	*/

	err = manager.ProcessStopped(ctx, proc3)
	require.NoError(t, err)
	err = manager.ProcessStopped(ctx, proc4)
	require.NoError(t, err)

	require.Empty(t, proc3.TLSProbeTypesDetected)
	require.Empty(t, proc4.TLSProbeTypesDetected)
	require.Equal(t, 1, proc3Closer.closes)
	require.Equal(t, 1, proc4Closer.closes)
	require.Equal(t, 0, ctr1Closer.closes)
	// unrelated proc + container should not be affected
	require.Equal(t, 0, proc2Closer.closes)
	require.Equal(t, 0, ctrRootCloser.closes)

	/*
		we will trigger an exec replace on proc2 without changing the executable path

		1. it should not trigger a new scan
	*/
	err = manager.ProcessReplaced(ctx, proc2)
	require.NoError(t, err)

	require.Equal(t, 0, proc2Closer.closes)

	/*
		we will trigger an exec replace on proc2 with a new executable path

		1. it should trigger a new scan
		2. it should detach the existing probes
		3. it should attach any newly detected probes
	*/
	proc2.Exe = "/bin/curl"
	proc2NewScanRes := testProcScanRes(proc2, "gnutls") // openssl -> gnutls
	proc2NewCloser := &testCloser{}
	mockScanner.EXPECT().Scan(gomock.Any(), &ExeScannable{
		Path:    "/proc/2/exe",
		Cmdline: []string{"/bin/curl"},
		Root:    "/proc/2/root",
	}).Return(proc2NewScanRes, nil)
	mockScanner.EXPECT().Attach(gomock.Any(), 2, "/proc/2/exe", proc2NewScanRes).Return(proc2NewCloser, nil)

	err = manager.ProcessReplaced(ctx, proc2)
	require.NoError(t, err)

	require.Equal(t, 1, proc2Closer.closes)
	require.Equal(t, 0, proc2NewCloser.closes)
	require.Equal(t, []string{"gnutls"}, proc2.TLSProbeTypesDetected)
	require.Equal(t, 0, ctrRootCloser.closes)

	/*
		we will stop proc 2

		1. it should detach the existing probes
	*/
	err = manager.ProcessStopped(ctx, proc2)
	require.NoError(t, err)

	require.Equal(t, 1, proc2Closer.closes)
	require.Equal(t, 1, proc2NewCloser.closes)
	require.Equal(t, 0, ctrRootCloser.closes)

	/*
		finally we will close the manager

		1. it should detach all processes and containers
	*/
	mockScanner.EXPECT().Close().Return(nil)

	err = manager.Close()
	require.NoError(t, err)

	require.Equal(t, 1, ctrRootCloser.closes)
	require.Equal(t, 1, ctr1Closer.closes)
	// the following should remain unaffected from before
	require.Equal(t, 1, proc2Closer.closes)
	require.Equal(t, 1, proc2NewCloser.closes)
	require.Equal(t, 1, proc3Closer.closes)
	require.Equal(t, 1, proc4Closer.closes)
}

type testProbeScanResult struct {
	detected bool
	name     string
}

func (t *testProbeScanResult) ProbeName() string {
	return t.name
}

func (t *testProbeScanResult) ProbeDetected() bool {
	return t.detected
}

type testCloser struct {
	closes int
}

func (t *testCloser) Close() error {
	t.closes++
	return nil
}

func testProcess(pid int, exe string, ctrID string) (*process.Process, *testCloser) {
	closer := &testCloser{}
	return &process.Process{
		Pid:         pid,
		PidExe:      fmt.Sprintf("/proc/%d/exe", pid),
		Exe:         exe,
		Strategy:    process.StrategyObserve,
		ContainerID: ctrID,
		Root:        fmt.Sprintf("/proc/%d/root", pid),
	}, closer
}
