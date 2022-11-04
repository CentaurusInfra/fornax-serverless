/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/network"
	libcontainercgroups "github.com/opencontainers/runc/libcontainer/cgroups"
	"github.com/spf13/pflag"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpuset"
)

type RuntimeDependencyType string

const (
	CRIRuntime = "CRIRuntime"
	CAdvisor   = "CAdvisor"
)

type ResourceType string

const (
	CPU                               = "CPU"
	Memory                            = "Memory"
	PID                               = "PID"
	DefaultRootPath                   = "/var/lib/nodeagent"
	DefaultDBName                     = "nodeagent.sqlite"
	DefaultContainerRuntimeEndpoint   = "/run/containerd/containerd.sock"
	DefaultMaxPods                    = 1000
	DefaultPodPidLimits               = -1
	DefaultCgroupRoot                 = "/"
	DefaultCgroupDriver               = "cgroupfs"
	DefaultMaxContainerPerPod         = 10
	DefaultMounter                    = "mount"
	DefaultPodsPerCore                = 0
	DefaultNodeAgentCgroupName        = ""
	DefaultSystemCgroupName           = ""
	DefaultPodsDirName                = "pods"
	DefaultPodLogsRootPath            = "/var/log/pods"
	DefaultVolumesDirName             = "volumes"
	DefaultVolumeSubpathsDirName      = "volume-subpaths"
	DefaultVolumeDevicesDirName       = "volumeDevices"
	DefaultPluginsDirName             = "plugins"
	DefaultPluginsRegistrationDirName = "plugins_registry"
	DefaultContainersDirName          = "containers"
	DefaultPluginContainersDirName    = "plugin-containers"
	DefaultPodResourcesDirName        = "pod-resources"
	DefaultMemoryThrottlingFactor     = 0.8
	DefaultSessionServicePort         = 1022
	DefaultNodePortStartingNum        = 1024
	KubeletPluginsDirSELinuxLabel     = "system_u:object_r:container_file_t:s0"
	DefaultPodCgroupName              = "containers"
	DefaultRuntimeHandler             = "runc"
)

type NodeConfiguration struct {
	ContainerRuntime         string
	ContainerRuntimeEndpoint string
	CgroupRoot               string
	CgroupDriver             string
	DatabaseURL              string // /var/lib/nodeagent/db/nodeagent.sqlite
	FornaxCoreUrls           []string
	Hostname                 string
	MemoryQoS                bool
	DisableSwap              bool
	MaxPods                  int
	MaxContainerPerPod       int
	MounterPath              string // a mounter bin path, leave it empty if use default
	NodeIP                   string
	NodeAgentCgroupName      string
	OOMScoreAdj              int32
	QOSReserved              map[v1.ResourceName]int64
	PodLogRootPath           string
	PodPidLimits             int // default 100
	PodsPerCore              int
	PodCgroupName            string
	RootPath                 string // node agent state root, /var/lib/nodeagent/
	RuntimeHandler           string
	ProtectKernelDefaults    bool
	SystemCgroupName         string
	EnforceCPULimits         bool
	CPUCFSQuota              bool
	CPUCFSQuotaPeriod        time.Duration
	ReservedSystemCPUs       cpuset.CPUSet
	EnforceNodeAllocatable   sets.String
	NodeAgentReserved        v1.ResourceList
	SystemReserved           v1.ResourceList
	SeccompProfileRoot       string
	SeccompDefault           bool
	NodePortStartingNo       int32
	SessionServicePort       int32
}

func DefaultNodeConfiguration() (*NodeConfiguration, error) {
	var err error
	var hostname string
	var ips []net.IP
	hostname, err = os.Hostname()
	if err != nil {
		return nil, err
	}

	ips, err = network.GetLocalV4IP()
	if err != nil {
		return nil, err
	}

	return &NodeConfiguration{
		ContainerRuntime:         "remote",
		ContainerRuntimeEndpoint: DefaultContainerRuntimeEndpoint,
		CgroupRoot:               DefaultCgroupRoot,
		CgroupDriver:             DefaultCgroupDriver,
		DatabaseURL:              fmt.Sprintf("file:%s/db/%s?cache=shared&mode=rwc", DefaultRootPath, DefaultDBName),
		FornaxCoreUrls:           []string{},
		Hostname:                 hostname,
		MaxPods:                  DefaultMaxPods,
		MaxContainerPerPod:       DefaultMaxContainerPerPod,
		MounterPath:              DefaultMounter,
		NodeIP:                   ips[0].String(),
		NodeAgentCgroupName:      DefaultNodeAgentCgroupName,
		OOMScoreAdj:              -999,
		QOSReserved:              map[v1.ResourceName]int64{},
		PodLogRootPath:           DefaultPodLogsRootPath,
		PodPidLimits:             DefaultPodPidLimits,
		PodsPerCore:              DefaultPodsPerCore,
		PodCgroupName:            DefaultPodCgroupName,
		RootPath:                 DefaultRootPath,
		RuntimeHandler:           DefaultRuntimeHandler,
		SeccompProfileRoot:       filepath.Join(DefaultRootPath, "seccomp"),
		NodePortStartingNo:       DefaultNodePortStartingNum,
		SessionServicePort:       DefaultSessionServicePort,
		SeccompDefault:           false,
		ProtectKernelDefaults:    false,
		SystemCgroupName:         DefaultSystemCgroupName,
		MemoryQoS:                true,
		DisableSwap:              true,
		EnforceCPULimits:         true,
		CPUCFSQuota:              true,
		CPUCFSQuotaPeriod:        100 * time.Millisecond,
		ReservedSystemCPUs:       cpuset.CPUSet{},
		EnforceNodeAllocatable:   map[string]sets.Empty{},
		NodeAgentReserved:        map[v1.ResourceName]resource.Quantity{},
		SystemReserved:           map[v1.ResourceName]resource.Quantity{},
	}, nil
}

func ValidateNodeConfiguration(nodeConfig NodeConfiguration) []error {
	errs := []error{}
	if nodeConfig.MemoryQoS && libcontainercgroups.IsCgroup2UnifiedMode() {
		errs = append(errs, errors.New("memory qos is true but cgroup is not running in v2 unified mode "))
	}

	var err error
	if _, err = os.Stat(nodeConfig.RootPath); os.IsNotExist(err) {
		err = os.Mkdir(nodeConfig.RootPath, os.FileMode(int(0755)))
		if err != nil {
			errs = append(errs, fmt.Errorf("%s does not exist, and can not be created due to %v", nodeConfig.RootPath, err))
		}
	}

	if _, err = os.Stat(nodeConfig.PodLogRootPath); os.IsNotExist(err) {
		err = os.Mkdir(nodeConfig.PodLogRootPath, os.FileMode(int(0755)))
		if err != nil {
			errs = append(errs, fmt.Errorf("%s does not exist, and can not be created due to %v", nodeConfig.PodLogRootPath, err))
		}
	}

	dbDir := fmt.Sprintf("%s/db", nodeConfig.RootPath)
	if _, err = os.Stat(dbDir); os.IsNotExist(err) {
		err = os.Mkdir(dbDir, os.FileMode(int(0755)))
		if err != nil {
			errs = append(errs, fmt.Errorf("%s does not exist, and can not be created due to %v", dbDir, err))
		}
	}

	return errs
}

func AddConfigFlags(flagSet *pflag.FlagSet, nodeConfig *NodeConfiguration) {
	flagSet.BoolVar(&nodeConfig.DisableSwap, "disable-swap", nodeConfig.DisableSwap, "should disable swap, fail when host swap is on")

	flagSet.StringVar(&nodeConfig.NodeIP, "node-ip", nodeConfig.NodeIP, "IPv4 addresses of the node. If unset, use the node's default IPv4 address")

	flagSet.StringVar(&nodeConfig.ContainerRuntimeEndpoint, "remote-runtime-endpoint", nodeConfig.ContainerRuntimeEndpoint, "container runtime remote endpoint")

	flagSet.StringArrayVar(&nodeConfig.FornaxCoreUrls, "fornaxcore-url", nodeConfig.FornaxCoreUrls, "addresses of the fornaxcores, format is ip:port. must provided")

	flagSet.StringVar(&nodeConfig.RuntimeHandler, "runtime-handler", nodeConfig.RuntimeHandler, "container runtime handler name, check /etc/docker/daemon.json for valid name")
}
