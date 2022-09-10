//go:build linux
// +build linux

/*
Copyright 2015 The Kubernetes Authors.

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

package cm

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/opencontainers/runc/libcontainer/cgroups"
	"github.com/opencontainers/runc/libcontainer/cgroups/manager"
	"github.com/opencontainers/runc/libcontainer/configs"
	"k8s.io/klog/v2"
	"k8s.io/mount-utils"
	utilpath "k8s.io/utils/path"

	libcontaineruserns "github.com/opencontainers/runc/libcontainer/userns"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"

	// utilfeature "k8s.io/apiserver/pkg/util/feature"
	cadvisorv1 "github.com/google/cadvisor/info/v1"
	"k8s.io/client-go/tools/record"
	utilsysctl "k8s.io/component-helpers/node/util/sysctl"

	// podresourcesapi "k8s.io/kubelet/pkg/apis/podresources/v1"
	// kubefeatures "k8s.io/kubernetes/pkg/features"
	// "k8s.io/kubernetes/pkg/kubelet/cadvisor"
	// "k8s.io/kubernetes/pkg/kubelet/cm/admission"
	// "k8s.io/kubernetes/pkg/kubelet/cm/cpumanager"
	// "k8s.io/kubernetes/pkg/kubelet/cm/devicemanager"
	// "k8s.io/kubernetes/pkg/kubelet/cm/memorymanager"
	// memorymanagerstate "k8s.io/kubernetes/pkg/kubelet/cm/memorymanager/state"
	// "k8s.io/kubernetes/pkg/kubelet/cm/topologymanager"
	cmutil "k8s.io/kubernetes/pkg/kubelet/cm/util"

	// "k8s.io/kubernetes/pkg/kubelet/pluginmanager/cache"
	// "k8s.io/kubernetes/pkg/kubelet/stats/pidlimit"
	// "k8s.io/kubernetes/pkg/kubelet/status"
	// schedulerframework "k8s.io/kubernetes/pkg/scheduler/framework"
	"k8s.io/kubernetes/pkg/util/oom"
)

const (
	ResourcePID v1.ResourceName = "pid"
	MaxPID                      = 1000000
)

type ActivePodsFunc func() []*v1.Pod

// A non-user container tracked by the Kubelet.
type systemContainer struct {
	// Absolute name of the container.
	name string

	// CPU limit in millicores.
	cpuMillicores int64

	// Function that ensures the state of the container.
	// m is the cgroup manager for the specified container.
	ensureStateFunc func(m cgroups.Manager) error

	// Manager for the cgroups of the external container.
	manager cgroups.Manager
}

func newSystemCgroups(containerName string) (*systemContainer, error) {
	manager, err := createManager(containerName)
	if err != nil {
		return nil, err
	}
	return &systemContainer{
		name:    containerName,
		manager: manager,
	}, nil
}

type ContainerManagerImpl struct {
	sync.RWMutex
	NodeConfig
	machineInfo cadvisorv1.MachineInfo
	mountUtil   mount.Interface
	nodeInfo    *v1.Node
	activePods  ActivePodsFunc
	// External containers being managed.
	systemContainers []*systemContainer
	// Tasks that are run periodically
	periodicTasks []func()
	// Holds all the mounted cgroup subsystems
	subsystems *CgroupSubsystems
	// Interface for cgroup management
	cgroupManager CgroupManager
	// Capacity of this node.
	capacity v1.ResourceList
	// Capacity of this node, including internal resources.
	internalCapacity v1.ResourceList
	// Absolute cgroupfs path to a cgroup that Kubelet needs to place all pods under.
	// This path include a top level container for enforcing Node Allocatable.
	cgroupRoot CgroupName
	// Event recorder interface.
	recorder record.EventRecorder
	// Interface for QoS cgroup management
	qosContainerManager QOSContainerManager

	// Interface for exporting and allocating devices reported by device plugins.
	// deviceManager devicemanager.Manager
	// Interface for CPU affinity management.
	// cpuManager cpumanager.Manager
	// Interface for memory affinity management.
	// memoryManager memorymanager.Manager
	// Interface for Topology resource co-ordination
	// topologyManager topologymanager.Manager
}

type features struct {
	cpuHardcapping bool
}

var _ ContainerManager = &ContainerManagerImpl{}

// checks if the required cgroups subsystems are mounted.
// As of now, only 'cpu' and 'memory' are required.
// cpu quota is a soft requirement.
func ValidateSystemRequirements(mountUtil mount.Interface) (features, error) {
	const (
		cgroupMountType = "cgroup"
		localErr        = "system validation failed"
	)
	var (
		cpuMountPoint string
		f             features
	)
	mountPoints, err := mountUtil.List()
	if err != nil {
		return f, fmt.Errorf("%s - %v", localErr, err)
	}

	if cgroups.IsCgroup2UnifiedMode() {
		f.cpuHardcapping = true
		return f, nil
	}

	expectedCgroups := sets.NewString("cpu", "cpuacct", "cpuset", "memory")
	for _, mountPoint := range mountPoints {
		if mountPoint.Type == cgroupMountType {
			for _, opt := range mountPoint.Opts {
				if expectedCgroups.Has(opt) {
					expectedCgroups.Delete(opt)
				}
				if opt == "cpu" {
					cpuMountPoint = mountPoint.Path
				}
			}
		}
	}

	if expectedCgroups.Len() > 0 {
		return f, fmt.Errorf("%s - Following Cgroup subsystem not mounted: %v", localErr, expectedCgroups.List())
	}

	// Check if cpu quota is available.
	// CPU cgroup is required and so it expected to be mounted at this point.
	periodExists, err := utilpath.Exists(utilpath.CheckFollowSymlink, path.Join(cpuMountPoint, "cpu.cfs_period_us"))
	if err != nil {
		klog.ErrorS(err, "Failed to detect if CPU cgroup cpu.cfs_period_us is available")
	}
	quotaExists, err := utilpath.Exists(utilpath.CheckFollowSymlink, path.Join(cpuMountPoint, "cpu.cfs_quota_us"))
	if err != nil {
		klog.ErrorS(err, "Failed to detect if CPU cgroup cpu.cfs_quota_us is available")
	}
	if quotaExists && periodExists {
		f.cpuHardcapping = true
	}
	return f, nil
}

// TODO(vmarmol): Add limits to the system containers.
// Takes the absolute name of the specified containers.
// Empty container name disables use of the specified container.
func NewContainerManager(mountUtil mount.Interface, node *v1.Node, capacity v1.ResourceList, nodeConfig NodeConfig, failSwapOn bool, devicePluginEnabled bool, recorder record.EventRecorder) (ContainerManager, error) {
	subsystems, err := GetCgroupSubsystems()
	if err != nil {
		return nil, fmt.Errorf("failed to get mounted cgroup subsystems: %v", err)
	}

	if failSwapOn {
		// Check whether swap is enabled. The Kubelet does not support running with swap enabled.
		swapFile := "/proc/swaps"
		swapData, err := ioutil.ReadFile(swapFile)
		if err != nil {
			if os.IsNotExist(err) {
				klog.InfoS("File does not exist, assuming that swap is disabled", "path", swapFile)
			} else {
				return nil, err
			}
		} else {
			swapData = bytes.TrimSpace(swapData) // extra trailing \n
			swapLines := strings.Split(string(swapData), "\n")

			// If there is more than one line (table headers) in /proc/swaps, swap is enabled and we should
			// error out unless --fail-swap-on is set to false.
			if len(swapLines) > 1 {
				return nil, fmt.Errorf("running with swap on is not supported, please disable swap! or set --fail-swap-on flag to false. /proc/swaps contained: %v", swapLines)
			}
		}
	}

	var internalCapacity = v1.ResourceList{}
	for k, v := range capacity {
		internalCapacity[k] = v
	}
	internalCapacity[ResourcePID] = *resource.NewQuantity(
		int64(MaxPID),
		resource.DecimalSI)

	// Turn CgroupRoot from a string (in cgroupfs path format) to internal CgroupName
	cgroupRoot := ParseCgroupfsToCgroupName(nodeConfig.CgroupRoot)
	cgroupManager := NewCgroupManager(subsystems, nodeConfig.CgroupDriver)
	// Check if Cgroup-root actually exists on the node
	// this does default to / when enabled, but this tests against regressions.
	if nodeConfig.CgroupRoot == "" {
		return nil, fmt.Errorf("invalid configuration: cgroups-per-qos was specified and cgroup-root was not specified. To enable the QoS cgroup hierarchy you need to specify a valid cgroup-root")
	}

	// we need to check that the cgroup root actually exists for each subsystem
	// of note, we always use the cgroupfs driver when performing this check since
	// the input is provided in that format.
	// this is important because we do not want any name conversion to occur.
	if err := cgroupManager.Validate(cgroupRoot); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	klog.InfoS("Container manager verified user specified cgroup-root exists", "cgroupRoot", cgroupRoot)
	// Include the top level cgroup for enforcing node allocatable into cgroup-root.
	// This way, all sub modules can avoid having to understand the concept of node allocatable.
	cgroupRoot = NewCgroupName(cgroupRoot, DefaultNodeAllocatableCgroupName)
	klog.InfoS("Creating Container Manager object based on Node Config", "nodeConfig", nodeConfig)

	qosContainerManager, err := NewQOSContainerManager(subsystems, cgroupRoot, nodeConfig, cgroupManager)
	if err != nil {
		return nil, err
	}

	cm := &ContainerManagerImpl{
		nodeInfo:            node,
		mountUtil:           mountUtil,
		NodeConfig:          nodeConfig,
		subsystems:          subsystems,
		cgroupManager:       cgroupManager,
		capacity:            capacity,
		internalCapacity:    internalCapacity,
		cgroupRoot:          cgroupRoot,
		recorder:            recorder,
		qosContainerManager: qosContainerManager,
	}

	// if utilfeature.DefaultFeatureGate.Enabled(kubefeatures.TopologyManager) {
	//  cm.topologyManager, err = topologymanager.NewManager(
	//    machineInfo.Topology,
	//    nodeConfig.ExperimentalTopologyManagerPolicy,
	//    nodeConfig.ExperimentalTopologyManagerScope,
	//  )
	//
	//  if err != nil {
	//    return nil, err
	//  }
	//
	// } else {
	//  cm.topologyManager = topologymanager.NewFakeManager()
	// }
	//
	// klog.InfoS("Creating device plugin manager", "devicePluginEnabled", devicePluginEnabled)
	// if devicePluginEnabled {
	//  cm.deviceManager, err = devicemanager.NewManagerImpl(machineInfo.Topology, cm.topologyManager)
	//  cm.topologyManager.AddHintProvider(cm.deviceManager)
	// } else {
	//  cm.deviceManager, err = devicemanager.NewManagerStub()
	// }
	// if err != nil {
	//  return nil, err
	// }
	//
	// // Initialize CPU manager
	// if utilfeature.DefaultFeatureGate.Enabled(kubefeatures.CPUManager) {
	//  cm.cpuManager, err = cpumanager.NewManager(
	//    nodeConfig.ExperimentalCPUManagerPolicy,
	//    nodeConfig.ExperimentalCPUManagerPolicyOptions,
	//    nodeConfig.ExperimentalCPUManagerReconcilePeriod,
	//    machineInfo,
	//    nodeConfig.NodeAllocatableConfig.ReservedSystemCPUs,
	//    cm.GetNodeAllocatableReservation(),
	//    nodeConfig.KubeletRootDir,
	//    cm.topologyManager,
	//  )
	//  if err != nil {
	//    klog.ErrorS(err, "Failed to initialize cpu manager")
	//    return nil, err
	//  }
	//  cm.topologyManager.AddHintProvider(cm.cpuManager)
	// }
	//
	// if utilfeature.DefaultFeatureGate.Enabled(kubefeatures.MemoryManager) {
	//  cm.memoryManager, err = memorymanager.NewManager(
	//    nodeConfig.ExperimentalMemoryManagerPolicy,
	//    machineInfo,
	//    cm.GetNodeAllocatableReservation(),
	//    nodeConfig.ExperimentalMemoryManagerReservedMemory,
	//    nodeConfig.KubeletRootDir,
	//    cm.topologyManager,
	//  )
	//  if err != nil {
	//    klog.ErrorS(err, "Failed to initialize memory manager")
	//    return nil, err
	//  }
	//  cm.topologyManager.AddHintProvider(cm.memoryManager)
	// }

	return cm, nil
}

// GetNodeConfig implements ContainerManager
func (cm *ContainerManagerImpl) GetNodeConfig() *NodeConfig {
	return &cm.NodeConfig
}

// NewPodContainerManager is a factory method returns a PodContainerManager object
// If qosCgroups are enabled then it returns the general pod container manager
// otherwise it returns a no-op manager which essentially does nothing
func (cm *ContainerManagerImpl) NewPodContainerManager() PodContainerManager {
	return &PodContainerManagerImpl{
		QosContainersInfo: cm.GetQOSContainersInfo(),
		Subsystems:        cm.subsystems,
		CgroupManager:     cm.cgroupManager,
		PodPidsLimit:      100, //cm.ExperimentalPodPidsLimit,
		EnforceCPULimits:  cm.EnforceCPULimits,
		CPUCFSQuotaPeriod: uint64(cm.CPUCFSQuotaPeriod / time.Microsecond),
	}
}

// Create a cgroup container manager.
func createManager(containerName string) (cgroups.Manager, error) {
	cg := &configs.Cgroup{
		Parent: "/",
		Name:   containerName,
		Resources: &configs.Resources{
			SkipDevices: true,
		},
		Systemd: false,
	}

	return manager.New(cg)
}

type KernelTunableBehavior string

const (
	KernelTunableWarn   KernelTunableBehavior = "warn"
	KernelTunableError  KernelTunableBehavior = "error"
	KernelTunableModify KernelTunableBehavior = "modify"
)

// SetupKernelTunables validates kernel tunable flags are set as expected
// depending upon the specified option, it will either warn, error, or modify the kernel tunable flags
func SetupKernelTunables(option KernelTunableBehavior) error {
	desiredState := map[string]int{
		utilsysctl.VMOvercommitMemory: utilsysctl.VMOvercommitMemoryAlways,
		utilsysctl.VMPanicOnOOM:       utilsysctl.VMPanicOnOOMInvokeOOMKiller,
		utilsysctl.KernelPanic:        utilsysctl.KernelPanicRebootTimeout,
		utilsysctl.KernelPanicOnOops:  utilsysctl.KernelPanicOnOopsAlways,
		utilsysctl.RootMaxKeys:        utilsysctl.RootMaxKeysSetting,
		utilsysctl.RootMaxBytes:       utilsysctl.RootMaxBytesSetting,
	}

	sysctl := utilsysctl.New()

	errList := []error{}
	for flag, expectedValue := range desiredState {
		val, err := sysctl.GetSysctl(flag)
		if err != nil {
			errList = append(errList, err)
			continue
		}
		if val == expectedValue {
			continue
		}

		switch option {
		case KernelTunableError:
			errList = append(errList, fmt.Errorf("invalid kernel flag: %v, expected value: %v, actual value: %v", flag, expectedValue, val))
		case KernelTunableWarn:
			klog.V(2).InfoS("Invalid kernel flag", "flag", flag, "expectedValue", expectedValue, "actualValue", val)
		case KernelTunableModify:
			klog.V(2).InfoS("Updating kernel flag", "flag", flag, "expectedValue", expectedValue, "actualValue", val)
			err = sysctl.SetSysctl(flag, expectedValue)
			if err != nil {
				if libcontaineruserns.RunningInUserNS() {
					// if utilfeature.DefaultFeatureGate.Enabled(kubefeatures.KubeletInUserNamespace) {
					klog.V(2).InfoS("Updating kernel flag failed (running in UserNS, ignoring)", "flag", flag, "err", err)
					continue
					// }
					// klog.ErrorS(err, "Updating kernel flag failed (Hint: enable KubeletInUserNamespace feature flag to ignore the error)", "flag", flag)
				}
				errList = append(errList, err)
			}
		}
	}
	return utilerrors.NewAggregate(errList)
}

func (cm *ContainerManagerImpl) setupNode() error {
	f, err := ValidateSystemRequirements(cm.mountUtil)
	if err != nil {
		return err
	}
	if !f.cpuHardcapping {
		return fmt.Errorf("CPU hardcapping unsupported")
	}
	b := KernelTunableModify
	if cm.NodeConfig.ProtectKernelDefaults {
		b = KernelTunableWarn
		// b = KernelTunableError
	}
	if err := SetupKernelTunables(b); err != nil {
		return err
	}

	// Setup top level qos containers only if CgroupsPerQOS flag is specified as true
	if err := cm.createNodeAllocatableCgroups(); err != nil {
		return err
	}
	err = cm.qosContainerManager.Start(cm.GetNodeAllocatableAbsolute, cm.activePods)
	if err != nil {
		return fmt.Errorf("failed to initialize top level QOS containers: %v", err)
	}

	// Enforce Node Allocatable (if required)
	if err := cm.enforceNodeAllocatableCgroups(); err != nil {
		return err
	}

	systemContainers := []*systemContainer{}

	if cm.SystemCgroupsName != "" {
		if cm.SystemCgroupsName == "/" {
			return fmt.Errorf("system container cannot be root (\"/\")")
		}
		cont, err := newSystemCgroups(cm.SystemCgroupsName)
		if err != nil {
			return err
		}
		cont.ensureStateFunc = func(manager cgroups.Manager) error {
			return EnsureSystemCgroups("/", manager)
		}
		systemContainers = append(systemContainers, cont)
	}

	if cm.KubeletCgroupsName != "" {
		cont, err := newSystemCgroups(cm.KubeletCgroupsName)
		if err != nil {
			return err
		}

		cont.ensureStateFunc = func(_ cgroups.Manager) error {
			return EnsureProcessInContainerWithOOMScore(os.Getpid(), int(cm.KubeletOOMScoreAdj), cont.manager)
		}
		systemContainers = append(systemContainers, cont)
	} else {
		cm.periodicTasks = append(cm.periodicTasks, func() {
			if err := EnsureProcessInContainerWithOOMScore(os.Getpid(), int(cm.KubeletOOMScoreAdj), nil); err != nil {
				klog.ErrorS(err, "Failed to ensure process in container with oom score")
				return
			}
			cont, err := GetContainer(os.Getpid())
			if err != nil {
				klog.ErrorS(err, "Failed to find cgroups of kubelet")
				return
			}
			cm.Lock()
			defer cm.Unlock()

			cm.KubeletCgroupsName = cont
		})
	}

	cm.systemContainers = systemContainers
	return nil
}

// GetPodCgroupRoot returns the literal cgroupfs value for the cgroup containing all pods.
func (cm *ContainerManagerImpl) GetPodCgroupRoot() string {
	return cm.cgroupManager.Name(cm.cgroupRoot)
}

func (cm *ContainerManagerImpl) GetMountedSubsystems() *CgroupSubsystems {
	return cm.subsystems
}

func (cm *ContainerManagerImpl) GetQOSContainersInfo() QOSContainersInfo {
	return cm.qosContainerManager.GetQOSContainersInfo()
}

func (cm *ContainerManagerImpl) UpdateQOSCgroups() error {
	return cm.qosContainerManager.UpdateCgroups()
}

func (cm *ContainerManagerImpl) Start(activePods ActivePodsFunc, capacities v1.ResourceList) error {

	// // Initialize CPU manager
	// if utilfeature.DefaultFeatureGate.Enabled(kubefeatures.CPUManager) {
	//  err := cm.cpuManager.Start(cpumanager.ActivePodsFunc(activePods), sourcesReady, podStatusProvider, runtimeService)
	//  if err != nil {
	//    return fmt.Errorf("start cpu manager error: %v", err)
	//  }
	// }

	// // Initialize memory manager
	// if utilfeature.DefaultFeatureGate.Enabled(kubefeatures.MemoryManager) {
	//  err := cm.memoryManager.Start(memorymanager.ActivePodsFunc(activePods), sourcesReady, podStatusProvider, runtimeService)
	//  if err != nil {
	//    return fmt.Errorf("start memory manager error: %v", err)
	//  }
	// }

	cm.activePods = activePods
	if cm.capacity == nil {
		cm.capacity = v1.ResourceList{}
	}
	if cm.internalCapacity == nil {
		cm.internalCapacity = v1.ResourceList{}
	}
	for rName, rCap := range capacities {
		cm.capacity[rName] = rCap
		cm.internalCapacity[rName] = rCap
	}

	// Ensure that node allocatable configuration is valid.
	if err := cm.validateNodeAllocatable(); err != nil {
		return err
	}

	// Setup the node
	if err := cm.setupNode(); err != nil {
		return err
	}

	// Don't run a background thread if there are no ensureStateFuncs.
	hasEnsureStateFuncs := false
	for _, cont := range cm.systemContainers {
		if cont.ensureStateFunc != nil {
			hasEnsureStateFuncs = true
			break
		}
	}
	if hasEnsureStateFuncs {
		// Run ensure state functions every minute.
		go wait.Until(func() {
			for _, cont := range cm.systemContainers {
				if cont.ensureStateFunc != nil {
					if err := cont.ensureStateFunc(cont.manager); err != nil {
						klog.InfoS("Failed to ensure state", "containerName", cont.name, "err", err)
					}
				}
			}
		}, time.Minute, wait.NeverStop)

	}

	if len(cm.periodicTasks) > 0 {
		go wait.Until(func() {
			for _, task := range cm.periodicTasks {
				if task != nil {
					task()
				}
			}
		}, 5*time.Minute, wait.NeverStop)
	}

	// // Starts device manager.
	// if err := cm.deviceManager.Start(devicemanager.ActivePodsFunc(activePods), sourcesReady); err != nil {
	//  return err
	// }

	return nil
}

// func (cm *containerManagerImpl) GetPluginRegistrationHandler() cache.PluginHandler {
//  return cm.deviceManager.GetWatcherHandler()
// }

// TODO: move the GetResources logic to PodContainerManager.
// func (cm *containerManagerImpl) GetResources(pod *v1.Pod, container *v1.Container) (*kubecontainer.RunContainerOptions, error) {
//  opts := &kubecontainer.RunContainerOptions{}
//  // Allocate should already be called during predicateAdmitHandler.Admit(),
//  // just try to fetch device runtime information from cached state here
//  devOpts, err := cm.deviceManager.GetDeviceRunContainerOptions(pod, container)
//  if err != nil {
//    return nil, err
//  } else if devOpts == nil {
//    return opts, nil
//  }
//  opts.Devices = append(opts.Devices, devOpts.Devices...)
//  opts.Mounts = append(opts.Mounts, devOpts.Mounts...)
//  opts.Envs = append(opts.Envs, devOpts.Envs...)
//  opts.Annotations = append(opts.Annotations, devOpts.Annotations...)
//  return opts, nil
// }

// func (cm *containerManagerImpl) UpdatePluginResources(node *schedulerframework.NodeInfo, attrs *lifecycle.PodAdmitAttributes) error {
//  return cm.deviceManager.UpdatePluginResources(node, attrs)
// }

// func (cm *containerManagerImpl) GetAllocateResourcesPodAdmitHandler() lifecycle.PodAdmitHandler {
//  if utilfeature.DefaultFeatureGate.Enabled(kubefeatures.TopologyManager) {
//    return cm.topologyManager
//  }
//  // TODO: we need to think about a better way to do this. This will work for
//  // now so long as we have only the cpuManager and deviceManager relying on
//  // allocations here. However, going forward it is not generalized enough to
//  // work as we add more and more hint providers that the TopologyManager
//  // needs to call Allocate() on (that may not be directly intstantiated
//  // inside this component).
//  return &resourceAllocator{cm.cpuManager, cm.memoryManager, cm.deviceManager}
// }

// type resourceAllocator struct {
//  cpuManager    cpumanager.Manager
//  memoryManager memorymanager.Manager
//  deviceManager devicemanager.Manager
// }

// func (m *resourceAllocator) Admit(attrs *lifecycle.PodAdmitAttributes) lifecycle.PodAdmitResult {
//  pod := attrs.Pod
//
//  for _, container := range append(pod.Spec.InitContainers, pod.Spec.Containers...) {
//    err := m.deviceManager.Allocate(pod, &container)
//    if err != nil {
//      return admission.GetPodAdmitResult(err)
//    }
//
//    if m.cpuManager != nil {
//      err = m.cpuManager.Allocate(pod, &container)
//      if err != nil {
//        return admission.GetPodAdmitResult(err)
//      }
//    }
//
//    if m.memoryManager != nil {
//      err = m.memoryManager.Allocate(pod, &container)
//      if err != nil {
//        return admission.GetPodAdmitResult(err)
//      }
//    }
//  }
//
//  return admission.GetPodAdmitResult(nil)
// }

func (cm *ContainerManagerImpl) SystemCgroupsLimit() v1.ResourceList {
	cpuLimit := int64(0)

	// Sum up resources of all external containers.
	for _, cont := range cm.systemContainers {
		cpuLimit += cont.cpuMillicores
	}

	return v1.ResourceList{
		v1.ResourceCPU: *resource.NewMilliQuantity(
			cpuLimit,
			resource.DecimalSI,
		),
	}

}

func IsProcessRunningInHost(pid int) (bool, error) {
	// Get init pid namespace.
	initPidNs, err := os.Readlink("/proc/1/ns/pid")
	if err != nil {
		return false, fmt.Errorf("failed to find pid namespace of init process")
	}
	klog.V(10).InfoS("Found init PID namespace", "namespace", initPidNs)
	processPidNs, err := os.Readlink(fmt.Sprintf("/proc/%d/ns/pid", pid))
	if err != nil {
		return false, fmt.Errorf("failed to find pid namespace of process %q", pid)
	}
	klog.V(10).InfoS("Process info", "pid", pid, "namespace", processPidNs)
	return initPidNs == processPidNs, nil
}

func EnsureProcessInContainerWithOOMScore(pid int, oomScoreAdj int, manager cgroups.Manager) error {
	if runningInHost, err := IsProcessRunningInHost(pid); err != nil {
		// Err on the side of caution. Avoid moving the docker daemon unless we are able to identify its context.
		return err
	} else if !runningInHost {
		// Process is running inside a container. Don't touch that.
		klog.V(2).InfoS("PID is not running in the host namespace", "pid", pid)
		return nil
	}

	var errs []error
	if manager != nil {
		cont, err := GetContainer(pid)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to find container of PID %d: %v", pid, err))
		}

		name := ""
		cgroups, err := manager.GetCgroups()
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to get cgroups for %d: %v", pid, err))
		} else {
			name = cgroups.Name
		}

		if cont != name {
			err = manager.Apply(pid)
			if err != nil {
				errs = append(errs, fmt.Errorf("failed to move PID %d (in %q) to %q: %v", pid, cont, name, err))
			}
		}
	}

	// Also apply oom-score-adj to processes
	oomAdjuster := oom.NewOOMAdjuster()
	klog.V(5).InfoS("Attempting to apply oom_score_adj to process", "oomScoreAdj", oomScoreAdj, "pid", pid)
	if err := oomAdjuster.ApplyOOMScoreAdj(pid, oomScoreAdj); err != nil {
		klog.V(3).InfoS("Failed to apply oom_score_adj to process", "oomScoreAdj", oomScoreAdj, "pid", pid, "err", err)
		errs = append(errs, fmt.Errorf("failed to apply oom score %d to PID %d: %v", oomScoreAdj, pid, err))
	}
	return utilerrors.NewAggregate(errs)
}

// Ensures the system container is created and all non-kernel threads and process 1
// without a container are moved to it.
//
// The reason of leaving kernel threads at root cgroup is that we don't want to tie the
// execution of these threads with to-be defined /system quota and create priority inversions.
//
func EnsureSystemCgroups(rootCgroupPath string, manager cgroups.Manager) error {
	// Move non-kernel PIDs to the system container.
	// Only keep errors on latest attempt.
	var finalErr error
	for i := 0; i <= 10; i++ {
		allPids, err := cmutil.GetPids(rootCgroupPath)
		if err != nil {
			finalErr = fmt.Errorf("failed to list PIDs for root: %v", err)
			continue
		}

		// Remove kernel pids and other protected PIDs (pid 1, PIDs already in system & kubelet containers)
		pids := make([]int, 0, len(allPids))
		for _, pid := range allPids {
			if pid == 1 || IsKernelPid(pid) {
				continue
			}

			pids = append(pids, pid)
		}

		// Check if we have moved all the non-kernel PIDs.
		if len(pids) == 0 {
			return nil
		}

		klog.V(3).InfoS("Moving non-kernel processes", "pids", pids)
		for _, pid := range pids {
			err := manager.Apply(pid)
			if err != nil {
				name := ""
				cgroups, err := manager.GetCgroups()
				if err == nil {
					name = cgroups.Name
				}

				finalErr = fmt.Errorf("failed to move PID %d into the system container %q: %v", pid, name, err)
			}
		}

	}

	return finalErr
}

// Determines whether the specified PID is a kernel PID.
func IsKernelPid(pid int) bool {
	// Kernel threads have no associated executable.
	_, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	return err != nil && os.IsNotExist(err)
}

func (cm *ContainerManagerImpl) GetCapacity() v1.ResourceList {
	return cm.capacity
}

// func (cm *containerManagerImpl) GetDevicePluginResourceCapacity() (v1.ResourceList, v1.ResourceList, []string) {
//  return cm.deviceManager.GetCapacity()
// }

// func (cm *containerManagerImpl) GetDevices(podUID, containerName string) []*podresourcesapi.ContainerDevices {
//  return containerDevicesFromResourceDeviceInstances(cm.deviceManager.GetDevices(podUID, containerName))
// }

// func (cm *containerManagerImpl) GetAllocatableDevices() []*podresourcesapi.ContainerDevices {
//  return containerDevicesFromResourceDeviceInstances(cm.deviceManager.GetAllocatableDevices())
// }

// func (cm *containerManagerImpl) GetCPUs(podUID, containerName string) []int64 {
//  if cm.cpuManager != nil {
//    return cm.cpuManager.GetExclusiveCPUs(podUID, containerName).ToSliceNoSortInt64()
//  }
//  return []int64{}
// }

// func (cm *containerManagerImpl) GetAllocatableCPUs() []int64 {
//  if cm.cpuManager != nil {
//    return cm.cpuManager.GetAllocatableCPUs().ToSliceNoSortInt64()
//  }
//  return []int64{}
// }

// func (cm *containerManagerImpl) GetMemory(podUID, containerName string) []*podresourcesapi.ContainerMemory {
//  if cm.memoryManager == nil {
//    return []*podresourcesapi.ContainerMemory{}
//  }
//
//  return containerMemoryFromBlock(cm.memoryManager.GetMemory(podUID, containerName))
// }

// func (cm *containerManagerImpl) GetAllocatableMemory() []*podresourcesapi.ContainerMemory {
//  if cm.memoryManager == nil {
//    return []*podresourcesapi.ContainerMemory{}
//  }
//
//  return containerMemoryFromBlock(cm.memoryManager.GetAllocatableMemory())
// }

// func (cm *containerManagerImpl) ShouldResetExtendedResourceCapacity() bool {
//  return cm.deviceManager.ShouldResetExtendedResourceCapacity()
// }

// func (cm *containerManagerImpl) UpdateAllocatedDevices() {
//  cm.deviceManager.UpdateAllocatedDevices()
// }

// func containerMemoryFromBlock(blocks []memorymanagerstate.Block) []*podresourcesapi.ContainerMemory {
//  var containerMemories []*podresourcesapi.ContainerMemory
//
//  for _, b := range blocks {
//    containerMemory := podresourcesapi.ContainerMemory{
//      MemoryType: string(b.Type),
//      Size_:      b.Size,
//      Topology: &podresourcesapi.TopologyInfo{
//        Nodes: []*podresourcesapi.NUMANode{},
//      },
//    }
//
//    for _, numaNodeID := range b.NUMAAffinity {
//      containerMemory.Topology.Nodes = append(containerMemory.Topology.Nodes, &podresourcesapi.NUMANode{ID: int64(numaNodeID)})
//    }
//
//    containerMemories = append(containerMemories, &containerMemory)
//  }
//
//  return containerMemories
// }
