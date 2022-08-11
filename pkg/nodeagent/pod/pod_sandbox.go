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
package pod

import (
	"fmt"
	"os"
	goruntime "runtime"

	v1 "k8s.io/api/core/v1"
	criv1 "k8s.io/cri-api/pkg/apis/runtime/v1"
	"k8s.io/klog/v2"

	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/config"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/runtime"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/types"
	"k8s.io/kubernetes/pkg/kubelet/util/format"
	netutils "k8s.io/utils/net"
)

// createPodSandbox creates a pod sandbox and returns (podSandBoxID, message, error).
func (m *PodActor) createPodSandbox() (*runtime.Pod, error) {
	pod := m.pod.Pod
	klog.InfoS("Generate pod sandbox config", "pod", types.UniquePodName(m.pod))
	podSandboxConfig, err := m.generatePodSandboxConfig()
	if err != nil {
		message := fmt.Sprintf("Failed to generate sandbox config for pod %s", types.UniquePodName(m.pod))
		klog.ErrorS(err, message)
		return nil, err
	}

	// Create pod logs directory
	klog.InfoS("Make pod log dir", "pod", types.UniquePodName(m.pod))
	err = os.MkdirAll(podSandboxConfig.LogDirectory, 0755)
	if err != nil {
		message := fmt.Sprintf("Failed to create log directory %s", podSandboxConfig.LogDirectory)
		klog.ErrorS(err, message)
		return nil, err
	}

	runtimeHandler := "runc"
	// if m.runtimeClassManager != nil {
	//  runtimeHandler, err = m.runtimeClassManager.LookupRuntimeHandler(pod.Spec.RuntimeClassName)
	//  if err != nil {
	//    message := fmt.Sprintf("Failed to create sandbox for pod %q: %v", format.Pod(pod), err)
	//    return "", message, err
	//  }
	//  if runtimeHandler != "" {
	//    klog.V(2).InfoS("Running pod with runtime handler", "pod", klog.KObj(pod), "runtimeHandler", runtimeHandler)
	//  }
	// }

	klog.InfoS("Call runtime to create sandbox", "pod", types.UniquePodName(m.pod), "sandboxConfig", podSandboxConfig)
	runtimepod, err := m.dependencies.CRIRuntimeService.CreateSandbox(podSandboxConfig, runtimeHandler)
	if err != nil {
		message := fmt.Sprintf("Failed to create sandbox for pod %q: %v", format.Pod(pod), err)
		klog.ErrorS(err, message)
		return nil, err
	}

	runtimepod.SandboxConfig = podSandboxConfig

	return runtimepod, nil
}

func (m *PodActor) removePodSandbox(podSandboxId string, podSandboxConfig *criv1.PodSandboxConfig) error {
	var err error

	// remove pod sandbox, assume all containers have been terminated
	err = m.dependencies.CRIRuntimeService.TerminatePod(podSandboxId, []string{})
	if err != nil {
		klog.ErrorS(err, "Failed to remove pod sandbox", "Pod", types.UniquePodName(m.pod))
		return err
	}

	// remove pod logs directory
	err = os.RemoveAll(podSandboxConfig.LogDirectory)
	if err != nil {
		klog.ErrorS(err, "Failed to remove pod log directory", "Pod", types.UniquePodName(m.pod))
		return err
	}

	return nil
}

// generatePodSandboxConfig generates pod sandbox config from fornaxtypes.FornaxPod.
func (m *PodActor) generatePodSandboxConfig() (*criv1.PodSandboxConfig, error) {
	// fornax node will expect fornaxcore populate most of pod spec before send it
	// it will not calulate hostname, all these staff
	pod := m.pod.Pod
	podUID := string(pod.UID)
	podSandboxConfig := &criv1.PodSandboxConfig{
		Metadata: &criv1.PodSandboxMetadata{
			Name:      pod.Name,
			Namespace: pod.Namespace,
			Uid:       podUID,
		},
		Labels:      newPodLabels(pod),
		Annotations: newPodAnnotations(pod),
	}

	// use empty dns config for now
	podSandboxConfig.DnsConfig = &criv1.DNSConfig{
		Servers:              []string{},
		Searches:             []string{},
		Options:              []string{},
		XXX_NoUnkeyedLiteral: struct{}{},
		XXX_sizecache:        0,
	}

	if !IsHostNetworkPod(pod) && len(pod.Spec.Hostname) != 0 {
		podSandboxConfig.Hostname = pod.Spec.Hostname
	}

	podSandboxConfig.LogDirectory = config.GetPodLogDir(config.DefaultPodLogsRootPath, pod.Namespace, pod.Name, pod.UID)

	portMappings := []*criv1.PortMapping{}
	for _, c := range pod.Spec.Containers {
		for _, v := range MakePortMappings(&c) {
			portMappings = append(portMappings, &v)
		}
	}
	if len(portMappings) > 0 {
		podSandboxConfig.PortMappings = portMappings
	}

	lc, err := m.generatePodSandboxLinuxConfig()
	if err != nil {
		return nil, err
	}
	podSandboxConfig.Linux = lc

	// Update config to include overhead, sandbox level resources
	if err := applySandboxResources(m.nodeConfig, pod, podSandboxConfig); err != nil {
		return nil, err
	}
	return podSandboxConfig, nil
}

// generatePodSandboxLinuxConfig generates LinuxPodSandboxConfig from fornaxtypes.FornaxPod.
// We've to call PodSandboxLinuxConfig always irrespective of the underlying OS as securityContext is not part of
// podSandboxConfig. It is currently part of LinuxPodSandboxConfig. In future, if we have securityContext pulled out
// in podSandboxConfig we should be able to use it.
func (m *PodActor) generatePodSandboxLinuxConfig() (*criv1.LinuxPodSandboxConfig, error) {
	pod := m.pod.Pod
	cgroupParent := m.dependencies.QosManager.GetPodCgroupParent(pod)
	lpsc := &criv1.LinuxPodSandboxConfig{
		CgroupParent: cgroupParent,
		SecurityContext: &criv1.LinuxSandboxSecurityContext{
			Privileged: HasPrivilegedContainer(pod),
			Seccomp: &criv1.SecurityProfile{
				ProfileType: criv1.SecurityProfile_RuntimeDefault,
			},
		},
	}

	addPodSecurityContext(pod, lpsc)

	return lpsc, nil
}

// determinePodSandboxIP determines the IP addresses of the given pod sandbox.
func (m *PodActor) determinePodSandboxIPs(podNamespace, podName string, podSandbox *criv1.PodSandboxStatus) []string {
	podIPs := make([]string, 0)
	if podSandbox.Network == nil {
		klog.InfoS("Pod Sandbox status doesn't have network information, cannot report IPs", "pod", klog.KRef(podNamespace, podName))
		return podIPs
	}

	// ip could be an empty string if runtime is not responsible for the
	// IP (e.g., host networking).

	// pick primary IP
	if len(podSandbox.Network.Ip) != 0 {
		if netutils.ParseIPSloppy(podSandbox.Network.Ip) == nil {
			klog.InfoS("Pod Sandbox reported an unparseable primary IP", "pod", klog.KRef(podNamespace, podName), "IP", podSandbox.Network.Ip)
			return nil
		}
		podIPs = append(podIPs, podSandbox.Network.Ip)
	}

	// pick additional ips, if cri reported them
	for _, podIP := range podSandbox.Network.AdditionalIps {
		if nil == netutils.ParseIPSloppy(podIP.Ip) {
			klog.InfoS("Pod Sandbox reported an unparseable additional IP", "pod", klog.KRef(podNamespace, podName), "IP", podIP.Ip)
			return nil
		}
		podIPs = append(podIPs, podIP.Ip)
	}

	return podIPs
}

func addPodSecurityContext(pod *v1.Pod, lpsc *criv1.LinuxPodSandboxConfig) {
	sc := pod.Spec.SecurityContext
	sysctls := make(map[string]string)
	if sc != nil {
		for _, c := range sc.Sysctls {
			sysctls[c.Name] = c.Value
		}
	}

	lpsc.Sysctls = sysctls

	if sc != nil {
		if sc.RunAsUser != nil && goruntime.GOOS != "windows" {
			lpsc.SecurityContext.RunAsUser = &criv1.Int64Value{Value: int64(*sc.RunAsUser)}
		}
		if sc.RunAsGroup != nil && goruntime.GOOS != "windows" {
			lpsc.SecurityContext.RunAsGroup = &criv1.Int64Value{Value: int64(*sc.RunAsGroup)}
		}
		lpsc.SecurityContext.NamespaceOptions = namespacesForPod(pod)

		if sc.FSGroup != nil && goruntime.GOOS != "windows" {
			lpsc.SecurityContext.SupplementalGroups = append(lpsc.SecurityContext.SupplementalGroups, int64(*sc.FSGroup))
		}
		// if groups := m.runtimeHelper.GetExtraSupplementalGroupsForPod(pod); len(groups) > 0 {
		//  lc.SecurityContext.SupplementalGroups = append(lc.SecurityContext.SupplementalGroups, groups...)
		// }
		if sc.SupplementalGroups != nil {
			for _, sg := range sc.SupplementalGroups {
				lpsc.SecurityContext.SupplementalGroups = append(lpsc.SecurityContext.SupplementalGroups, int64(sg))
			}
		}
		if sc.SELinuxOptions != nil && goruntime.GOOS != "windows" {
			lpsc.SecurityContext.SelinuxOptions = &criv1.SELinuxOption{
				User:  sc.SELinuxOptions.User,
				Role:  sc.SELinuxOptions.Role,
				Type:  sc.SELinuxOptions.Type,
				Level: sc.SELinuxOptions.Level,
			}
		}
	}

}
