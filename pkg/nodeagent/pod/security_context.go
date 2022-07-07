/*
Copyright 2016 The Kubernetes Authors.

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
	"path/filepath"
	"strings"

	v1 "k8s.io/api/core/v1"
	criv1 "k8s.io/cri-api/pkg/apis/runtime/v1"
	"k8s.io/kubernetes/pkg/security/apparmor"
	"k8s.io/kubernetes/pkg/securitycontext"
)

// determineEffectiveSecurityContext gets container's security context from v1.Pod and v1.Container.
func determineEffectiveSecurityContext(pod *v1.Pod, container *v1.Container, uid *int64, username string, seccompDefault bool, seccompProfileRoot string) *criv1.LinuxContainerSecurityContext {
	effectiveSc := securitycontext.DetermineEffectiveSecurityContext(pod, container)
	synthesized := convertToRuntimeSecurityContext(effectiveSc)
	if synthesized == nil {
		synthesized = &criv1.LinuxContainerSecurityContext{
			MaskedPaths:   securitycontext.ConvertToRuntimeMaskedPaths(effectiveSc.ProcMount),
			ReadonlyPaths: securitycontext.ConvertToRuntimeReadonlyPaths(effectiveSc.ProcMount),
		}
	}

	// TODO: Deprecated, remove after we switch to Seccomp field
	// set SeccompProfilePath.
	synthesized.SeccompProfilePath = getSeccompProfilePath(pod.Annotations, container.Name, pod.Spec.SecurityContext, container.SecurityContext, seccompDefault, seccompProfileRoot)

	synthesized.Seccomp = getSeccompProfile(seccompProfileRoot, pod.Annotations, container.Name, pod.Spec.SecurityContext, container.SecurityContext, seccompDefault)

	// set ApparmorProfile.
	synthesized.ApparmorProfile = apparmor.GetProfileNameFromPodAnnotations(pod.Annotations, container.Name)

	// set RunAsUser.
	if synthesized.RunAsUser == nil {
		if uid != nil {
			synthesized.RunAsUser = &criv1.Int64Value{Value: *uid}
		}
		synthesized.RunAsUsername = username
	}

	// set namespace options and supplemental groups.
	synthesized.NamespaceOptions = namespacesForPod(pod)
	podSc := pod.Spec.SecurityContext
	if podSc != nil {
		if podSc.FSGroup != nil {
			synthesized.SupplementalGroups = append(synthesized.SupplementalGroups, int64(*podSc.FSGroup))
		}

		if podSc.SupplementalGroups != nil {
			for _, sg := range podSc.SupplementalGroups {
				synthesized.SupplementalGroups = append(synthesized.SupplementalGroups, int64(sg))
			}
		}
	}

	// if groups := m.runtimeHelper.GetExtraSupplementalGroupsForPod(pod); len(groups) > 0 {
	// 	synthesized.SupplementalGroups = append(synthesized.SupplementalGroups, groups...)
	// }

	synthesized.NoNewPrivs = securitycontext.AddNoNewPrivileges(effectiveSc)

	synthesized.MaskedPaths = securitycontext.ConvertToRuntimeMaskedPaths(effectiveSc.ProcMount)
	synthesized.ReadonlyPaths = securitycontext.ConvertToRuntimeReadonlyPaths(effectiveSc.ProcMount)

	return synthesized
}

// convertToRuntimeSecurityContext converts v1.SecurityContext to criv1.SecurityContext.
func convertToRuntimeSecurityContext(securityContext *v1.SecurityContext) *criv1.LinuxContainerSecurityContext {
	if securityContext == nil {
		return nil
	}

	sc := &criv1.LinuxContainerSecurityContext{
		Capabilities:   convertToRuntimeCapabilities(securityContext.Capabilities),
		SelinuxOptions: convertToRuntimeSELinuxOption(securityContext.SELinuxOptions),
	}
	if securityContext.RunAsUser != nil {
		sc.RunAsUser = &criv1.Int64Value{Value: int64(*securityContext.RunAsUser)}
	}
	if securityContext.RunAsGroup != nil {
		sc.RunAsGroup = &criv1.Int64Value{Value: int64(*securityContext.RunAsGroup)}
	}
	if securityContext.Privileged != nil {
		sc.Privileged = *securityContext.Privileged
	}
	if securityContext.ReadOnlyRootFilesystem != nil {
		sc.ReadonlyRootfs = *securityContext.ReadOnlyRootFilesystem
	}

	return sc
}

// convertToRuntimeSELinuxOption converts v1.SELinuxOptions to criv1.SELinuxOption.
func convertToRuntimeSELinuxOption(opts *v1.SELinuxOptions) *criv1.SELinuxOption {
	if opts == nil {
		return nil
	}

	return &criv1.SELinuxOption{
		User:  opts.User,
		Role:  opts.Role,
		Type:  opts.Type,
		Level: opts.Level,
	}
}

// convertToRuntimeCapabilities converts v1.Capabilities to criv1.Capability.
func convertToRuntimeCapabilities(opts *v1.Capabilities) *criv1.Capability {
	if opts == nil {
		return nil
	}

	capabilities := &criv1.Capability{
		AddCapabilities:  make([]string, len(opts.Add)),
		DropCapabilities: make([]string, len(opts.Drop)),
	}
	for index, value := range opts.Add {
		capabilities.AddCapabilities[index] = string(value)
	}
	for index, value := range opts.Drop {
		capabilities.DropCapabilities[index] = string(value)
	}

	return capabilities
}

func fieldProfile(scmp *v1.SeccompProfile, profileRootPath string, fallbackToRuntimeDefault bool) string {
	if scmp == nil {
		if fallbackToRuntimeDefault {
			return v1.SeccompProfileRuntimeDefault
		}
		return ""
	}
	if scmp.Type == v1.SeccompProfileTypeRuntimeDefault {
		return v1.SeccompProfileRuntimeDefault
	}
	if scmp.Type == v1.SeccompProfileTypeLocalhost && scmp.LocalhostProfile != nil && len(*scmp.LocalhostProfile) > 0 {
		fname := filepath.Join(profileRootPath, *scmp.LocalhostProfile)
		return v1.SeccompLocalhostProfileNamePrefix + fname
	}
	if scmp.Type == v1.SeccompProfileTypeUnconfined {
		return v1.SeccompProfileNameUnconfined
	}

	if fallbackToRuntimeDefault {
		return v1.SeccompProfileRuntimeDefault
	}
	return ""
}

func annotationProfile(profile, profileRootPath string) string {
	if strings.HasPrefix(profile, v1.SeccompLocalhostProfileNamePrefix) {
		name := strings.TrimPrefix(profile, v1.SeccompLocalhostProfileNamePrefix)
		fname := filepath.Join(profileRootPath, filepath.FromSlash(name))
		return v1.SeccompLocalhostProfileNamePrefix + fname
	}
	return profile
}

func getSeccompProfilePath(annotations map[string]string, containerName string,
	podSecContext *v1.PodSecurityContext, containerSecContext *v1.SecurityContext, fallbackToRuntimeDefault bool, seccompProfileRoot string) string {
	// container fields are applied first
	if containerSecContext != nil && containerSecContext.SeccompProfile != nil {
		return fieldProfile(containerSecContext.SeccompProfile, seccompProfileRoot, fallbackToRuntimeDefault)
	}

	// if container field does not exist, try container annotation (deprecated)
	if containerName != "" {
		if profile, ok := annotations[v1.SeccompContainerAnnotationKeyPrefix+containerName]; ok {
			return annotationProfile(profile, seccompProfileRoot)
		}
	}

	// when container seccomp is not defined, try to apply from pod field
	if podSecContext != nil && podSecContext.SeccompProfile != nil {
		return fieldProfile(podSecContext.SeccompProfile, seccompProfileRoot, fallbackToRuntimeDefault)
	}

	// as last resort, try to apply pod annotation (deprecated)
	if profile, ok := annotations[v1.SeccompPodAnnotationKey]; ok {
		return annotationProfile(profile, seccompProfileRoot)
	}

	if fallbackToRuntimeDefault {
		return v1.SeccompProfileRuntimeDefault
	}

	return ""
}

func fieldSeccompProfile(scmp *v1.SeccompProfile, profileRootPath string, fallbackToRuntimeDefault bool) *criv1.SecurityProfile {
	if scmp == nil {
		if fallbackToRuntimeDefault {
			return &criv1.SecurityProfile{
				ProfileType: criv1.SecurityProfile_RuntimeDefault,
			}
		}
		return &criv1.SecurityProfile{
			ProfileType: criv1.SecurityProfile_Unconfined,
		}
	}
	if scmp.Type == v1.SeccompProfileTypeRuntimeDefault {
		return &criv1.SecurityProfile{
			ProfileType: criv1.SecurityProfile_RuntimeDefault,
		}
	}
	if scmp.Type == v1.SeccompProfileTypeLocalhost && scmp.LocalhostProfile != nil && len(*scmp.LocalhostProfile) > 0 {
		fname := filepath.Join(profileRootPath, *scmp.LocalhostProfile)
		return &criv1.SecurityProfile{
			ProfileType:  criv1.SecurityProfile_Localhost,
			LocalhostRef: fname,
		}
	}
	return &criv1.SecurityProfile{
		ProfileType: criv1.SecurityProfile_Unconfined,
	}
}

func getSeccompProfile(seccompProfileRoot string, annotations map[string]string, containerName string,
	podSecContext *v1.PodSecurityContext, containerSecContext *v1.SecurityContext, fallbackToRuntimeDefault bool) *criv1.SecurityProfile {
	// container fields are applied first
	if containerSecContext != nil && containerSecContext.SeccompProfile != nil {
		return fieldSeccompProfile(containerSecContext.SeccompProfile, seccompProfileRoot, fallbackToRuntimeDefault)
	}

	// when container seccomp is not defined, try to apply from pod field
	if podSecContext != nil && podSecContext.SeccompProfile != nil {
		return fieldSeccompProfile(podSecContext.SeccompProfile, seccompProfileRoot, fallbackToRuntimeDefault)
	}

	if fallbackToRuntimeDefault {
		return &criv1.SecurityProfile{
			ProfileType: criv1.SecurityProfile_RuntimeDefault,
		}
	}

	return &criv1.SecurityProfile{
		ProfileType: criv1.SecurityProfile_Unconfined,
	}
}
