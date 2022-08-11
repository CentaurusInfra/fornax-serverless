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
	"path/filepath"

	"k8s.io/apimachinery/pkg/types"
)

func GetPodsDir(rootPath string) string {
	return filepath.Join(rootPath, DefaultPodsDirName)
}

// getPodLogDir returns the full path to the pod log dir
func GetPodLogDir(rootPath string, podNamespace, podName string, podUID types.UID) string {
	return filepath.Join(rootPath, podNamespace, podName, string(podUID))
}

// getPluginsDir returns the full path to the directory under which plugin
// directories are created.  Plugins can use these directories for data that
// they need to persist.  Plugins should create subdirectories under this named
// after their own names.
func GetPluginsDir(rootPath string) string {
	return filepath.Join(rootPath, DefaultPluginsDirName)
}

// getPluginsRegistrationDir returns the full path to the directory under which
// plugins socket should be placed to be registered.
// More information is available about plugin registration in the pluginwatcher
// module
func GetPluginsRegistrationDir(rootPath string) string {
	return filepath.Join(rootPath, DefaultPluginsRegistrationDirName)
}

// getPluginDir returns a data directory name for a given plugin name.
// Plugins can use these directories to store data that they need to persist.
// For per-pod plugin data, see getPodPluginDir.
func GetPluginDir(rootPath string, pluginName string) string {
	return filepath.Join(rootPath, pluginName)
}

// getVolumeDevicePluginsDir returns the full path to the directory under which plugin
// directories are created.  Plugins can use these directories for data that
// they need to persist.  Plugins should create subdirectories under this named
// after their own names.
func GetVolumeDevicePluginsDir(rootPath string) string {
	return filepath.Join(rootPath, DefaultPluginsDirName)
}

// getVolumeDevicePluginDir returns a data directory name for a given plugin name.
// Plugins can use these directories to store data that they need to persist.
// For per-pod plugin data, see getVolumeDevicePluginsDir.
func GetVolumeDevicePluginDir(rootPath string, pluginName string) string {
	return filepath.Join(GetVolumeDevicePluginsDir(rootPath), pluginName, DefaultVolumeDevicesDirName)
}

// getPodDir returns the full path to the per-pod directory for the pod with
// the given UID.
func GetPodDir(rootPath string, podUID types.UID) string {
	return filepath.Join(GetPodsDir(rootPath), string(podUID))
}

// getPodVolumesSubpathsDir returns the full path to the per-pod subpaths directory under
// which subpath volumes are created for the specified pod.  This directory may not
// exist if the pod does not exist or subpaths are not specified.
func GetPodVolumeSubpathsDir(rootPath string, podUID types.UID) string {
	return filepath.Join(GetPodDir(rootPath, podUID), DefaultVolumeSubpathsDirName)
}

// getPodVolumesDir returns the full path to the per-pod data directory under
// which volumes are created for the specified pod.  This directory may not
// exist if the pod does not exist.
func GetPodVolumesDir(rootPath string, podUID types.UID) string {
	return filepath.Join(GetPodDir(rootPath, podUID), DefaultVolumesDirName)
}

// getPodVolumeDir returns the full path to the directory which represents the
// named volume under the named plugin for specified pod.  This directory may not
// exist if the pod does not exist.
func GetPodVolumeDir(rootPath string, podUID types.UID, pluginName string, volumeName string) string {
	return filepath.Join(GetPodVolumesDir(rootPath, podUID), pluginName, volumeName)
}

// getPodVolumeDevicesDir returns the full path to the per-pod data directory under
// which volumes are created for the specified pod. This directory may not
// exist if the pod does not exist.
func GetPodVolumeDevicesDir(rootPath string, podUID types.UID) string {
	return filepath.Join(GetPodDir(rootPath, podUID), DefaultVolumeDevicesDirName)
}

// getPodVolumeDeviceDir returns the full path to the directory which represents the
// named plugin for specified pod. This directory may not exist if the pod does not exist.
func GetPodVolumeDeviceDir(rootPath string, podUID types.UID, pluginName string) string {
	return filepath.Join(GetPodVolumeDevicesDir(rootPath, podUID), pluginName)
}

// getPodPluginsDir returns the full path to the per-pod data directory under
// which plugins may store data for the specified pod.  This directory may not
// exist if the pod does not exist.
func GetPodPluginsDir(rootPath string, podUID types.UID) string {
	return filepath.Join(GetPodDir(rootPath, podUID), DefaultPluginsDirName)
}

// getPodPluginDir returns a data directory name for a given plugin name for a
// given pod UID.  Plugins can use these directories to store data that they
// need to persist.  For non-per-pod plugin data, see getPluginDir.
func GetPodPluginDir(rootPath string, podUID types.UID, pluginName string) string {
	return filepath.Join(GetPodPluginsDir(rootPath, podUID), pluginName)
}

// getPodContainerDir returns the full path to the per-pod data directory under
// which container data is held for the specified pod.  This directory may not
// exist if the pod or container does not exist.
func GetPodContainerDir(rootPath string, podUID types.UID, ctrName string) string {
	return filepath.Join(GetPodDir(rootPath, podUID), DefaultContainersDirName, ctrName)
}

// getPodResourcesSocket returns the full path to the directory containing the pod resources socket
func GetPodResourcesDir(rootPath string) string {
	return filepath.Join(rootPath, DefaultPodResourcesDirName)
}
