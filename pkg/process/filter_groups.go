package process

import (
	"os"

	"github.com/qpoint-io/qtap/pkg/config"
)

func getPredefinedFilters(group string) []Filter {
	filters, ok := predefinedFilters[group]
	if !ok {
		return nil
	}
	return filters
}

var predefinedFilters = map[string][]Filter{
	"container-runtimes": {
		// containerd is a container runtime for Linux, written in Go, that is designed to be simple, efficient, and flexible
		&ExeFilter{pattern: "/usr/bin/containerd", strategy: config.MatchStrategy_EXACT, bitmask: config.SkipAllFlag},
		// dockerd is the Docker daemon, which is the main process that runs on a Docker host
		&ExeFilter{pattern: "/usr/bin/dockerd", strategy: config.MatchStrategy_EXACT, bitmask: config.SkipAllFlag},
		// runc is a CLI tool for spawning and running containers according to the OCI specification
		&ExeFilter{pattern: "/usr/bin/runc", strategy: config.MatchStrategy_EXACT, bitmask: config.SkipAllFlag},
	},
	"eks": {
		// AWS Kubernetes Agent (https://github.com/aws/amazon-vpc-cni-k8s/tree/master/cmd/aws-k8s-agent)
		&ExeFilter{pattern: "/app/aws-k8s-agent", strategy: config.MatchStrategy_EXACT, bitmask: config.SkipAllFlag},
		// AWS Systems Manager Agent (SSM Agent)
		&ExeFilter{pattern: "/usr/bin/amazon-ssm-agent", strategy: config.MatchStrategy_EXACT, bitmask: config.SkipAllFlag},
		// AWS Systems Manager Agent (SSM Agent)
		&ExeFilter{pattern: "/usr/bin/ssm-agent-worker", strategy: config.MatchStrategy_EXACT, bitmask: config.SkipAllFlag},
		// AWS Systems Manager Agent (SSM Agent)
		&ExeFilter{pattern: "/usr/bin/ssm-document-worker", strategy: config.MatchStrategy_EXACT, bitmask: config.SkipAllFlag},
	},
	"gke": {
		// gcfsd is the GKE Container Storage Interface (CSI) driver for Google Cloud Filestore
		&ExeFilter{pattern: "/home/kubernetes/bin/gcfsd", strategy: config.MatchStrategy_EXACT, bitmask: config.SkipAllFlag},
		// event-exporter is a component that runs on Kubernetes nodes to collect and export event data
		&ExeFilter{pattern: "/event-exporter", strategy: config.MatchStrategy_EXACT, bitmask: config.SkipAllFlag},
		// fluent-bit-gke-exporter is a component that runs on Kubernetes nodes to collect logs
		&ExeFilter{pattern: "/fluent-bit-gke-exporter", strategy: config.MatchStrategy_EXACT, bitmask: config.SkipAllFlag},
		// Node problem detector is a component that runs on Kubernetes nodes to detect and report problems
		&ExeFilter{pattern: "/home/kubernetes/bin/node-problem-detector", strategy: config.MatchStrategy_EXACT, bitmask: config.SkipAllFlag},
	},
	"kubernetes": {
		// The kubelet is the primary management agent for containers in a Kubernetes cluster
		&ExeFilter{pattern: "/home/kubernetes/bin/kubelet", strategy: config.MatchStrategy_EXACT, bitmask: config.SkipAllFlag},
		// The kube-proxy is a network proxy that runs on each node in a Kubernetes cluster
		&ExeFilter{pattern: "/home/kubernetes/bin/kube-proxy", strategy: config.MatchStrategy_EXACT, bitmask: config.SkipAllFlag},
		// The Konnectivity service provides a TCP level proxy for the control plane to cluster communication
		&ExeFilter{pattern: "/proxy-agent", strategy: config.MatchStrategy_EXACT, bitmask: config.SkipAllFlag},
		// The cluster proportional autoscaler is a component that watches over the number of schedulable nodes and cores of the cluster and resizes the number of replicas for the required resource
		&ExeFilter{pattern: "/cluster-proportional-autoscaler", strategy: config.MatchStrategy_EXACT, bitmask: config.SkipAllFlag},
		// The CSI Node Driver Registrar is a component that registers CSI drivers with the kubelet
		&ExeFilter{pattern: "/csi-node-driver-registrar", strategy: config.MatchStrategy_EXACT, bitmask: config.SkipAllFlag},
	},
	"qpoint": {
		// Filter out the current qtap process
		&PIDFilter{PID: os.Getpid(), bitmask: config.SkipAllFlag},
	},
}
