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
package sdependency

import (
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/cadvisor"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/config"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/images"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/network"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/qos"
	resourcemanager "centaurusinfra.io/fornax-serverless/pkg/nodeagent/resource"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/runtime"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/store/factory"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/store/sqlite"
	v1 "k8s.io/api/core/v1"
	criv1 "k8s.io/cri-api/pkg/apis/runtime/v1"
	"k8s.io/klog/v2"
	kubeletcm "k8s.io/kubernetes/pkg/kubelet/cm"
	"k8s.io/kubernetes/pkg/kubelet/cri/remote"
	"k8s.io/mount-utils"
)

type Dependencies struct {
	NetworkProvider   network.NetworkAddressProvider
	CAdvisor          cadvisor.CAdvisorInfoProvider
	CRIRuntimeService runtime.RuntimeService
	QosManager        qos.QoSManager
	ImageManager      images.ImageManager
	MemoryManager     resourcemanager.MemoryManager
	CPUManager        resourcemanager.CPUManager
	VolumeManager     resourcemanager.VolumeManager
	PodStore          *factory.PodStore

	// mounter := mount.New(s.ExperimentalMounterPath)
	// subpather := subpath.New(mounter)
	// hu := hostutil.NewHostUtil()
	// var pluginRunner = exec.New()
	//
	// plugins, err := ProbeVolumePlugins(featureGate)
	// if err != nil {
	//  return nil, err
	// }
	// return &dependency.Dependencies{
	// Auth:                nil, // default does not enforce auth[nz]
	// CAdvisorInterface:   nil, // cadvisor.New launches background processes (bg http.ListenAndServe, and some bg cleaners), not set here
	// Cloud:               nil, // cloud provider might start background processes
	// ContainerManager:    nil,
	// KubeClient:          nil,
	// HeartbeatClient:     nil,
	// EventClient:         nil,
	// HostUtil:            hu,
	// Mounter:             mounter,
	// Subpather:           subpather,
	// OOMAdjuster:         oom.NewOOMAdjuster(),
	// OSInterface:         kubecontainer.RealOS{},
	// VolumePlugins:       plugins,
	// DynamicPluginProber: GetDynamicPluginProber(s.VolumePluginDir, pluginRunner),
	// TLSOptions:          tlsOptions}, nil

	// Options []Option
	//
	// // Injected Dependencies
	// Auth                 server.AuthInterface
	// CAdvisorInterface    cadvisor.Interface
	// Cloud                cloudprovider.Interface
	// ContainerManager     cm.ContainerManager
	// EventClient          v1core.EventsGetter
	// HeartbeatClient      clientset.Interface
	// OnHeartbeatFailure   func()
	// KubeClient           clientset.Interface
	// Mounter              mount.Interface
	// HostUtil             hostutil.HostUtils
	// OOMAdjuster          *oom.OOMAdjuster
	// OSInterface          kubecontainer.OSInterface
	// PodConfig            *config.PodConfig
	// ProbeManager         prober.Manager
	// Recorder             record.EventRecorder
	// Subpather            subpath.Interface
	// VolumePlugins        []volume.VolumePlugin
	// DynamicPluginProber  volume.DynamicPluginProber
	// TLSOptions           *server.TLSOptions
	// RemoteRuntimeService internalapi.RuntimeService
	// RemoteImageService   internalapi.ImageManagerService
	// // remove it after cadvisor.UsingLegacyCadvisorStats dropped.
	// useLegacyCadvisorStats bool
}

func InitBasicDependencies(nodeConfig config.NodeConfiguration) (*Dependencies, error) {
	dependencies := Dependencies{
		NetworkProvider: nil,
		CAdvisor:        nil,
		//CRIRuntimeService: nil,
		QosManager:    nil,
		MemoryManager: resourcemanager.MemoryManager{},
		CPUManager:    resourcemanager.CPUManager{},
		VolumeManager: resourcemanager.VolumeManager{},
		//PodStore:          &factory.PodStore{},
	}

	// SqliteStore
	var err error
	dependencies.PodStore, err = InitPodStore(nodeConfig.DatabaseURL)
	if err != nil {
		return nil, err
	}

	// networkProvider
	dependencies.NetworkProvider = InitNetworkProvider(nodeConfig.Hostname)

	// // CRIRuntime
	// dependencies.CRIRuntimeService, err = InitRuntimeService(nodeConfig.ContainerRuntimeEndpoint)
	// if err != nil {
	// 	klog.ErrorS(err, "failed to init container runtime client")
	// 	return nil, err
	// }

	// // cAdvisor
	// dependencies.CAdvisor, err = InitCAdvisor(cadvisor.DefaultCAdvisorConfig(nodeConfig), dependencies.CRIRuntimeService)
	// if err != nil {
	// 	klog.ErrorS(err, "failed to init cadvisor")
	// 	return nil, err
	// }

	return &dependencies, nil
}

func InitRuntimeService(endpoint string) (runtime.RuntimeService, error) {
	return runtime.NewRemoteRuntimeService(endpoint, runtime.DefaultTimeout)
}

func InitImageService(endpoint string) (images.ImageManager, error) {
	klog.V(3).InfoS("Connecting to runtime service", "endpoint", endpoint)
	remoteService, err := remote.NewRemoteImageService(endpoint, runtime.DefaultTimeout)
	if err != nil {
		klog.ErrorS(err, "Connect remote runtime failed", "address", endpoint)
		return nil, err
	}

	return images.NewImageManager(remoteService, &criv1.AuthConfig{}), nil
}

func InitNetworkProvider(hostname string) network.NetworkAddressProvider {
	return &network.LocalNetworkAddressProvider{
		Hostname: hostname,
	}
}

func InitPodStore(databaseURL string) (*factory.PodStore, error) {
	return factory.NewPodSqliteStore(&sqlite.SQLiteStoreOptions{
		ConnUrl: databaseURL,
	})
}

func InitCAdvisor(cAdvisorConfig cadvisor.CAdvisorConfig, CRIRuntime runtime.RuntimeService) (cadvisor.CAdvisorInfoProvider, error) {
	return cadvisor.NewCAdvisorInfoProvider(cAdvisorConfig, CRIRuntime)
}

func (n *Dependencies) Complete(node *v1.Node, nodeConfig config.NodeConfiguration, activePods kubeletcm.ActivePodsFunc) error {

	// SqliteStore
	var err error
	if n.PodStore == nil {
		n.PodStore, err = InitPodStore(nodeConfig.DatabaseURL)
		if err != nil {
			klog.ErrorS(err, "Failed to init node agent store")
			return err
		}
	}

	// networkProvider
	if n.NetworkProvider == nil {
		n.NetworkProvider = InitNetworkProvider(nodeConfig.Hostname)
	}

	// CRIRuntime
	if n.CRIRuntimeService == nil {
		n.CRIRuntimeService, err = InitRuntimeService(nodeConfig.ContainerRuntimeEndpoint)
		if err != nil {
			klog.ErrorS(err, "Failed to init runtime service")
			return err
		}
	}

	// CRIRuntime
	if n.ImageManager == nil {
		n.ImageManager, err = InitImageService(nodeConfig.ContainerRuntimeEndpoint)
		if err != nil {
			klog.ErrorS(err, "Failed to init runtime image manager")
			return err
		}
	}

	// cAdvisor
	if n.CAdvisor == nil {
		n.CAdvisor, err = InitCAdvisor(cadvisor.DefaultCAdvisorConfig(nodeConfig), n.CRIRuntimeService)
		if err != nil {
			klog.ErrorS(err, "Failed to init cadvisor info provider")
			return err
		}
	}

	// QosManager
	if n.QosManager == nil {
		mounter := mount.New(nodeConfig.MounterPath)
		n.QosManager, err = qos.NewQoSManager(node, activePods, mounter, n.CAdvisor, nodeConfig)
		if err != nil {
			klog.ErrorS(err, "Failed to init qos manager")
			return err
		}
	}

	// TODO
	// MemoryManager   resourcemanager.MemoryManager
	// CPUManager      resourcemanager.CPUManager
	// VolumeManager   resourcemanager.VolumeManager
	return nil
}
