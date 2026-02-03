package tap

//go:generate go tool github.com/cilium/ebpf/cmd/bpf2go -target arm64,amd64 Tap ../../bpf/tap/bpf2go.c -- -O2 -target bpf -g -I../../bpf/headers -I../../bpf/tap -DBPF_NO_PRESERVE_ACCESS_INDEX
