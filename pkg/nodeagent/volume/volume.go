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

package volume

import (
	criv1 "k8s.io/cri-api/pkg/apis/runtime/v1"
)

// Mount represents a volume mount.
type Mount struct {
	// Name of the volume mount.
	Name string
	// Path of the mount within the container.
	ContainerPath string
	// Path of the mount on the host.
	HostPath string
	// Whether the mount is read-only.
	ReadOnly bool
	// Whether the mount needs SELinux relabeling
	SELinuxRelabel bool
	// Requested propagation mode
	Propagation criv1.MountPropagation
}

// DeviceInfo contains information about the device.
type DeviceInfo struct {
	// Path on host for mapping
	PathOnHost string
	// Path in Container to map
	PathInContainer string
	// Cgroup permissions
	Permissions string
}

// VolumeInfo contains information about the volume.
type VolumeInfo struct {
	// Mounter is the volume's mounter
	// Mounter volume.Mounter
	// BlockVolumeMapper is the Block volume's mapper
	// BlockVolumeMapper volume.BlockVolumeMapper
	// SELinuxLabeled indicates whether this volume has had the
	// pod's SELinux label applied to it or not
	SELinuxLabeled bool
	// Whether the volume permission is set to read-only or not
	// This value is passed from volume.spec
	ReadOnly bool
	// Inner volume spec name, which is the PV name if used, otherwise
	// it is the same as the outer volume spec name.
	InnerVolumeSpecName string
}

// VolumeMap represents the map of volumes.
type VolumeMap map[string]VolumeInfo
