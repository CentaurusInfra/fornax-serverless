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
package pod

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"time"

	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/config"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/qos"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/runtime"
	libcontainercgroups "github.com/opencontainers/runc/libcontainer/cgroups"
	"github.com/opencontainers/selinux/go-selinux"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	criv1 "k8s.io/cri-api/pkg/apis/runtime/v1"
	"k8s.io/klog/v2"
	podutil "k8s.io/kubernetes/pkg/api/v1/pod"
	resourcehelper "k8s.io/kubernetes/pkg/api/v1/resource"
	v1helper "k8s.io/kubernetes/pkg/apis/core/v1/helper"
	kubefeatures "k8s.io/kubernetes/pkg/features"
	"k8s.io/kubernetes/pkg/kubelet/cm"
)

func CleanupPodDataDirs(rootPath string, pod *v1.Pod) error {
	uid := pod.UID
	if err := os.RemoveAll(config.GetPodDir(rootPath, uid)); err != nil && !os.IsExist(err) {
		return err
	}
	if err := os.RemoveAll(config.GetPodVolumesDir(rootPath, uid)); err != nil && !os.IsExist(err) {
		return err
	}
	if err := os.RemoveAll(config.GetPodPluginsDir(rootPath, uid)); err != nil && !os.IsExist(err) {
		return err
	}
	return nil
}

func MakePodDataDirs(rootPath string, pod *v1.Pod) error {
	uid := pod.UID
	if err := os.MkdirAll(config.GetPodDir(rootPath, uid), 0750); err != nil && !os.IsExist(err) {
		return err
	}
	if err := os.MkdirAll(config.GetPodVolumesDir(rootPath, uid), 0750); err != nil && !os.IsExist(err) {
		return err
	}
	if err := os.MkdirAll(config.GetPodPluginsDir(rootPath, uid), 0750); err != nil && !os.IsExist(err) {
		return err
	}
	return nil
}

func CleanupPodLogDir(rootPath string, pod *v1.Pod) error {
	if err := os.RemoveAll(config.GetPodLogDir(rootPath, pod.Namespace, pod.Name, pod.UID)); err != nil && !os.IsExist(err) {
		return err
	}
	return nil
}

func MakePodLogDir(rootPath string, pod *v1.Pod) error {
	if err := os.MkdirAll(config.GetPodLogDir(rootPath, pod.Namespace, pod.Name, pod.UID), 0750); err != nil && !os.IsExist(err) {
		return err
	}
	return nil
}

func GetPullSecretsForPod(pod *v1.Pod) []v1.Secret {
	pullSecrets := []v1.Secret{}

	// for _, secretRef := range pod.Spec.ImagePullSecrets {
	//  if len(secretRef.Name) == 0 {
	//    // API validation permitted entries with empty names (http://issue.k8s.io/99454#issuecomment-787838112).
	//    // Ignore to avoid unnecessary warnings.
	//    continue
	//  }
	//  secret, err := secretManager.GetSecret(pod.Namespace, secretRef.Name)
	//  if err != nil {
	//    klog.InfoS("Unable to retrieve pull secret, the image pull may not succeed.", "pod", klog.KObj(pod), "secret", klog.KObj(secret), "err", err)
	//    continue
	//  }
	//
	//  pullSecrets = append(pullSecrets, *secret)
	// }

	return pullSecrets
}

func IsHostNetworkPod(pod *v1.Pod) bool {
	return pod.Spec.HostNetwork
}

func ContainerLogFileName(containerName string, restartCount int) string {
	return filepath.Join(containerName, fmt.Sprintf("%d.log", restartCount))
}

func BuildContainerLogsDirectory(pod *v1.Pod, containerName string) (string, error) {
	podpath := config.GetPodLogDir(config.DefaultPodLogsRootPath, pod.Namespace, pod.Name, pod.UID)
	containerpath := filepath.Join(podpath, containerName)

	if _, err := os.Stat(containerpath); os.IsNotExist(err) {
		err = os.Mkdir(containerpath, os.FileMode(int(0755)))
		return "", err
	}

	return containerpath, nil
}

func BuildPodLogsDirectory(podNamespace, podName string, podUID types.UID) (string, error) {
	podpath := filepath.Join(config.DefaultPodLogsRootPath, podNamespace, podName, string(podUID))
	if _, err := os.Stat(podpath); os.IsNotExist(err) {
		err = os.MkdirAll(podpath, os.FileMode(int(0755)))
		if err != nil {
			return podpath, err
		}
		return podpath, nil
	}
	return podpath, nil
}

func MakePortMappings(container *v1.Container) []criv1.PortMapping {
	pms := []criv1.PortMapping{}
	for _, p := range container.Ports {
		pms = append(pms, criv1.PortMapping{
			HostIp:        p.HostIP,
			HostPort:      int32(p.HostPort),
			ContainerPort: int32(p.ContainerPort),
			Protocol:      ToRuntimeProtocol(p.Protocol),
		})
	}

	return pms
}

func HasPrivilegedContainer(pod *v1.Pod) bool {
	var hasPrivileged bool
	podutil.VisitContainers(&pod.Spec, podutil.AllFeatureEnabledContainers(), func(c *v1.Container, containerType podutil.ContainerType) bool {
		if c.SecurityContext != nil && c.SecurityContext.Privileged != nil && *c.SecurityContext.Privileged {
			hasPrivileged = true
			return false
		}
		return true
	})
	return hasPrivileged
}

func ToRuntimeProtocol(protocol v1.Protocol) criv1.Protocol {
	switch protocol {
	case v1.ProtocolTCP:
		return criv1.Protocol_TCP
	case v1.ProtocolUDP:
		return criv1.Protocol_UDP
	case v1.ProtocolSCTP:
		return criv1.Protocol_SCTP
	}

	return criv1.Protocol_TCP
}

func ipcNamespaceForPod(pod *v1.Pod) criv1.NamespaceMode {
	if pod != nil && pod.Spec.HostIPC {
		return criv1.NamespaceMode_NODE
	}
	return criv1.NamespaceMode_POD
}

func networkNamespaceForPod(pod *v1.Pod) criv1.NamespaceMode {
	if pod != nil && pod.Spec.HostNetwork {
		return criv1.NamespaceMode_NODE
	}
	return criv1.NamespaceMode_POD
}

func pidNamespaceForPod(pod *v1.Pod) criv1.NamespaceMode {
	if pod != nil {
		if pod.Spec.HostPID {
			return criv1.NamespaceMode_NODE
		}
		if pod.Spec.ShareProcessNamespace != nil && *pod.Spec.ShareProcessNamespace {
			return criv1.NamespaceMode_POD
		}
	}
	// Note that PID does not default to the zero value for v1.Pod
	return criv1.NamespaceMode_CONTAINER
}

// namespacesForPod returns the criv1.NamespaceOption for a given pod.
// An empty or nil pod can be used to get the namespace defaults for v1.Pod.
func namespacesForPod(pod *v1.Pod) *criv1.NamespaceOption {
	return &criv1.NamespaceOption{
		Ipc:     ipcNamespaceForPod(pod),
		Network: networkNamespaceForPod(pod),
		Pid:     pidNamespaceForPod(pod),
	}
}

func convertOverheadToLinuxResources(nodeConfig *config.NodeConfiguration, pod *v1.Pod) *criv1.LinuxContainerResources {
	resources := &criv1.LinuxContainerResources{}
	if pod.Spec.Overhead != nil {
		cpu := pod.Spec.Overhead.Cpu()
		memory := pod.Spec.Overhead.Memory()

		// For overhead, we do not differentiate between requests and limits. Treat this overhead
		// as "guaranteed", with requests == limits
		resources = calculateLinuxResources(nodeConfig, cpu, cpu, memory)
	}

	return resources
}

func calculateSandboxResources(nodeConfig *config.NodeConfiguration, pod *v1.Pod) *criv1.LinuxContainerResources {
	req, lim := resourcehelper.PodRequestsAndLimitsWithoutOverhead(pod)
	return calculateLinuxResources(nodeConfig, req.Cpu(), lim.Cpu(), lim.Memory())
}

func applySandboxResources(nodeConfig *config.NodeConfiguration, pod *v1.Pod, psc *criv1.PodSandboxConfig) error {

	if psc.Linux == nil {
		return nil
	}
	psc.Linux.Resources = calculateSandboxResources(nodeConfig, pod)
	psc.Linux.Overhead = convertOverheadToLinuxResources(nodeConfig, pod)

	return nil
}

const (
	milliCPUToCPU = 1000

	// 100000 is equivalent to 100ms
	quotaPeriod    = 100000
	minQuotaPeriod = 1000
)

// milliCPUToQuota converts milliCPU to CFS quota and period values
func milliCPUToQuota(milliCPU int64, period int64) (quota int64) {
	// CFS quota is measured in two values:
	//  - cfs_period_us=100ms (the amount of time to measure usage across)
	//  - cfs_quota=20ms (the amount of cpu time allowed to be used across a period)
	// so in the above example, you are limited to 20% of a single CPU
	// for multi-cpu environments, you just scale equivalent amounts
	// see https://www.kernel.org/doc/Documentation/scheduler/sched-bwc.txt for details
	if milliCPU == 0 {
		return
	}

	// we then convert your milliCPU to a value normalized over a period
	quota = (milliCPU * period) / milliCPUToCPU

	// quota needs to be a minimum of 1ms.
	if quota < minQuotaPeriod {
		quota = minQuotaPeriod
	}

	return
}

// generateLinuxContainerConfig generates linux container config for kubelet runtime v1.
func generateLinuxContainerConfig(nodeConfig *config.NodeConfiguration, container *v1.Container, pod *v1.Pod, uid *int64, username string, enforceMemoryQoS bool) *criv1.LinuxContainerConfig {
	lc := &criv1.LinuxContainerConfig{
		Resources:       &criv1.LinuxContainerResources{},
		SecurityContext: determineEffectiveSecurityContext(pod, container, uid, username, nodeConfig.SeccompDefault, nodeConfig.SeccompProfileRoot),
	}

	// if nsTarget != nil && lc.SecurityContext.NamespaceOptions.Pid == criv1.NamespaceMode_CONTAINER {
	//  lc.SecurityContext.NamespaceOptions.Pid = criv1.NamespaceMode_TARGET
	//  lc.SecurityContext.NamespaceOptions.TargetId = nsTarget.ID
	// }

	// set linux container resources
	lc.Resources = calculateLinuxResources(nodeConfig, container.Resources.Requests.Cpu(), container.Resources.Limits.Cpu(), container.Resources.Limits.Memory())

	lc.Resources.OomScoreAdj = int64(nodeConfig.OOMScoreAdj)
	lc.Resources.HugepageLimits = GetHugepageLimitsFromResources(container.Resources)
	lc.Resources.MemorySwapLimitInBytes = lc.Resources.MemoryLimitInBytes

	// Set memory.min and memory.high to enforce MemoryQoS
	if enforceMemoryQoS {
		unified := map[string]string{}
		memoryRequest := container.Resources.Requests.Memory().Value()
		memoryLimit := container.Resources.Limits.Memory().Value()
		if memoryRequest != 0 {
			unified[qos.MemoryMin] = strconv.FormatInt(memoryRequest, 10)
		}

		// If container sets limits.memory, we set memory.high=pod.spec.containers[i].resources.limits[memory] * memory_throttling_factor
		// for container level cgroup if memory.high>memory.min.
		// If container doesn't set limits.memory, we set memory.high=node_allocatable_memory * memory_throttling_factor
		// for container level cgroup.
		memoryHigh := int64(0)
		if memoryLimit != 0 {
			memoryHigh = int64(float64(memoryLimit) * config.DefaultMemoryThrottlingFactor)
		} else {
			// allocatable := m.getNodeAllocatable()
			// allocatableMemory, ok := allocatable[v1.ResourceMemory]
			// if ok && allocatableMemory.Value() > 0 {
			//  memoryHigh = int64(float64(allocatableMemory.Value()) * m.memoryThrottlingFactor)
			// }
		}
		if memoryHigh > memoryRequest {
			unified[qos.MemoryHigh] = strconv.FormatInt(memoryHigh, 10)
		}
		if len(unified) > 0 {
			if lc.Resources.Unified == nil {
				lc.Resources.Unified = unified
			} else {
				for k, v := range unified {
					lc.Resources.Unified[k] = v
				}
			}
			klog.V(4).InfoS("MemoryQoS config for container", "pod", klog.KObj(pod), "containerName", container.Name, "unified", unified)
		}
	}

	return lc
}

func makeDevices(opts *runtime.RunContainerOptions) []*criv1.Device {
	devices := make([]*criv1.Device, len(opts.Devices))

	for idx := range opts.Devices {
		device := opts.Devices[idx]
		devices[idx] = &criv1.Device{
			HostPath:      device.PathOnHost,
			ContainerPath: device.PathInContainer,
			Permissions:   device.Permissions,
		}
	}

	return devices
}

// makeMounts generates container volume mounts for kubelet runtime v1.
func makeMounts(opts *runtime.RunContainerOptions, container *v1.Container) []*criv1.Mount {
	volumeMounts := []*criv1.Mount{}

	for idx := range opts.Mounts {
		v := opts.Mounts[idx]
		selinuxRelabel := v.SELinuxRelabel && selinux.GetEnabled()
		mount := &criv1.Mount{
			HostPath:       v.HostPath,
			ContainerPath:  v.ContainerPath,
			Readonly:       v.ReadOnly,
			SelinuxRelabel: selinuxRelabel,
			Propagation:    v.Propagation,
		}

		volumeMounts = append(volumeMounts, mount)
	}

	// The reason we create and mount the log file in here (not in kubelet) is because
	// the file's location depends on the ID of the container, and we need to create and
	// mount the file before actually starting the container.
	if opts.PodContainerDir != "" && len(container.TerminationMessagePath) != 0 {
		// Because the PodContainerDir contains pod uid and container name which is unique enough,
		// here we just add a random id to make the path unique for different instances
		// of the same container.
		cid := makeUID()
		containerLogPath := filepath.Join(opts.PodContainerDir, cid)
		fs, err := os.Create(containerLogPath)
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("error on creating termination-log file %q: %v", containerLogPath, err))
		} else {
			fs.Close()

			// Chmod is needed because ioutil.WriteFile() ends up calling
			// open(2) to create the file, so the final mode used is "mode &
			// ~umask". But we want to make sure the specified mode is used
			// in the file no matter what the umask is.
			if err := os.Chmod(containerLogPath, 0666); err != nil {
				utilruntime.HandleError(fmt.Errorf("unable to set termination-log file permissions %q: %v", containerLogPath, err))
			}

			// Volume Mounts fail on Windows if it is not of the form C:/
			containerLogPath = MakeAbsolutePath(goruntime.GOOS, containerLogPath)
			terminationMessagePath := MakeAbsolutePath(goruntime.GOOS, container.TerminationMessagePath)
			selinuxRelabel := selinux.GetEnabled()
			volumeMounts = append(volumeMounts, &criv1.Mount{
				HostPath:       containerLogPath,
				ContainerPath:  terminationMessagePath,
				SelinuxRelabel: selinuxRelabel,
			})
		}
	}

	return volumeMounts
}

// MakeAbsolutePath convert path to absolute path according to GOOS
func MakeAbsolutePath(goos, path string) string {
	if goos != "windows" {
		return filepath.Clean("/" + path)
	}
	// These are all for windows
	// If there is a colon, give up.
	if strings.Contains(path, ":") {
		return path
	}
	// If there is a slash, but no drive, add 'c:'
	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, "\\") {
		return "c:" + path
	}
	// Otherwise, add 'c:\'
	return "c:\\" + path
}

// makeUID returns a randomly generated string.
func makeUID() string {
	return fmt.Sprintf("%08x", rand.Uint32())
}

// GetHugepageLimitsFromResources returns limits of each hugepages from resources.
func GetHugepageLimitsFromResources(resources v1.ResourceRequirements) []*criv1.HugepageLimit {
	var hugepageLimits []*criv1.HugepageLimit

	// For each page size, limit to 0.
	for _, pageSize := range libcontainercgroups.HugePageSizes() {
		hugepageLimits = append(hugepageLimits, &criv1.HugepageLimit{
			PageSize: pageSize,
			Limit:    uint64(0),
		})
	}

	requiredHugepageLimits := map[string]uint64{}
	for resourceObj, amountObj := range resources.Limits {
		if !v1helper.IsHugePageResourceName(resourceObj) {
			continue
		}

		pageSize, err := v1helper.HugePageSizeFromResourceName(resourceObj)
		if err != nil {
			klog.InfoS("Failed to get hugepage size from resource", "object", resourceObj, "err", err)
			continue
		}

		sizeString, err := v1helper.HugePageUnitSizeFromByteSize(pageSize.Value())
		if err != nil {
			klog.InfoS("Size is invalid", "object", resourceObj, "err", err)
			continue
		}
		requiredHugepageLimits[sizeString] = uint64(amountObj.Value())
	}

	for _, hugepageLimit := range hugepageLimits {
		if limit, exists := requiredHugepageLimits[hugepageLimit.PageSize]; exists {
			hugepageLimit.Limit = limit
		}
	}

	return hugepageLimits
}

// calculateLinuxResources will create the linuxContainerResources type based on the provided CPU and memory resource requests, limits
func calculateLinuxResources(nodeConfig *config.NodeConfiguration, cpuRequest, cpuLimit, memoryLimit *resource.Quantity) *criv1.LinuxContainerResources {
	resources := criv1.LinuxContainerResources{}
	var cpuShares int64

	memLimit := memoryLimit.Value()

	// If request is not specified, but limit is, we want request to default to limit.
	// API server does this for new containers, but we repeat this logic in Kubelet
	// for containers running on existing Kubernetes clusters.
	if cpuRequest.IsZero() && !cpuLimit.IsZero() {
		cpuShares = int64(cm.MilliCPUToShares(cpuLimit.MilliValue()))
	} else {
		// if cpuRequest.Amount is nil, then MilliCPUToShares will return the minimal number
		// of CPU shares.
		cpuShares = int64(cm.MilliCPUToShares(cpuRequest.MilliValue()))
	}
	resources.CpuShares = cpuShares
	if memLimit != 0 {
		resources.MemoryLimitInBytes = memLimit
	}

	if nodeConfig.CPUCFSQuota {
		// if cpuLimit.Amount is nil, then the appropriate default value is returned
		// to allow full usage of cpu resource.
		cpuPeriod := int64(quotaPeriod)
		if utilfeature.DefaultFeatureGate.Enabled(kubefeatures.CPUCFSQuotaPeriod) {
			cpuPeriod = int64(nodeConfig.CPUCFSQuotaPeriod / time.Microsecond)
		}
		cpuQuota := milliCPUToQuota(cpuLimit.MilliValue(), cpuPeriod)
		resources.CpuQuota = cpuQuota
		resources.CpuPeriod = cpuPeriod
	}

	return &resources
}
