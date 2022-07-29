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

package integtest

import (
	"context"
	"sync"
	"time"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	default_config "centaurusinfra.io/fornax-serverless/pkg/config"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc/server"
	"centaurusinfra.io/fornax-serverless/pkg/util"
	podutil "centaurusinfra.io/fornax-serverless/pkg/util"
	"github.com/google/uuid"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

var _ server.NodeMonitor = &integtestNodeMonitor{}

type integtestNodeMonitor struct {
	sync.RWMutex
	nodes map[string]*v1.Node

	chQuit chan interface{}
}

// OnPodUpdate implements server.NodeMonitor
func (*integtestNodeMonitor) OnPodStateUpdate(ctx context.Context, message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error) {
	podState := message.GetPodState()
	klog.InfoS("Received a pod state", "pod", podutil.UniquePodName(podState.GetPod()), "state", podState.GetState())

	if podState.GetState() == grpc.PodState_Running {
		// terminate test pod
		podId := util.UniquePodName(podState.Pod)
		body := grpc.FornaxCoreMessage_PodTerminate{
			PodTerminate: &grpc.PodTerminate{
				PodIdentifier: &podId,
			},
		}
		messageType := grpc.MessageType_POD_TERMINATE
		msg := &grpc.FornaxCoreMessage{
			MessageType: &messageType,
			MessageBody: &body,
		}
		return msg, nil
	} else if podState.GetState() == grpc.PodState_Terminated {
		msg := BuildATestPodCreate("test_app")
		return msg, nil
	}
	return nil, nil
}

// OnReady implements server.NodeMonitor
func (*integtestNodeMonitor) OnNodeReady(ctx context.Context, message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error) {
	// update node, scheduler begin to schedule pod in ready node

	// for test, send node a test pod
	msg := BuildATestPodCreate("test_app")
	klog.InfoS("Send node a pod", "pod", msg)
	return msg, nil
}

// OnRegistry implements server.NodeMonitor
func (*integtestNodeMonitor) OnRegistry(ctx context.Context, message *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error) {
	registry := message.GetNodeRegistry()

	daemonPod := BuildATestDaemonPod()

	// update node, send node configuration
	domain := default_config.DefaultDomainName
	node := registry.Node.DeepCopy()
	node.Spec.PodCIDR = "192.168.68.1/24"
	nodeConig := grpc.FornaxCoreMessage_NodeConfiguration{
		NodeConfiguration: &grpc.NodeConfiguration{
			ClusterDomain: &domain,
			Node:          node,
			DaemonPods:    []*v1.Pod{daemonPod.DeepCopy()},
		},
	}
	messageType := grpc.MessageType_NODE_CONFIGURATION
	m := &grpc.FornaxCoreMessage{
		MessageType: &messageType,
		MessageBody: &nodeConig,
	}

	klog.InfoS("Initialize node", "node configuration", m)
	return m, nil
}

// OnNodeUpdate implements server.NodeMonitor
func (*integtestNodeMonitor) OnNodeStateUpdate(ctx context.Context, state *grpc.FornaxCoreMessage) (*grpc.FornaxCoreMessage, error) {
	// update node resource info
	return nil, nil
}

func NewIntegNodeMonitor() *integtestNodeMonitor {
	return &integtestNodeMonitor{
		nodes:  map[string]*v1.Node{},
		chQuit: make(chan interface{}),
	}
}

func BuildV1Pod(uid types.UID, namespace, name string, isDaemon, hostnetwork bool, initContainers []v1.Container, containers []v1.Container) *v1.Pod {
	enableServiceLinks := false
	setHostnameAsFQDN := false
	mountServiceAccount := false
	shareProcessNamespace := false
	deletionGracePeriod := int64(120)
	activeGracePeriod := int64(120)
	daemonPod := &v1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       namespace,
			UID:             uid,
			ResourceVersion: "1",
			CreationTimestamp: metav1.Time{
				Time: time.Now(),
			},
			DeletionGracePeriodSeconds: &deletionGracePeriod,
			Labels:                     map[string]string{fornaxv1.LabelFornaxCoreApplication: namespace, fornaxv1.LabelFornaxCoreNodeDaemon: "true"},
			Annotations:                map[string]string{},
			OwnerReferences:            []metav1.OwnerReference{},
			Finalizers:                 []string{},
			ManagedFields:              []metav1.ManagedFieldsEntry{},
		},
		Spec: v1.PodSpec{
			Volumes:                       []v1.Volume{},
			InitContainers:                initContainers,
			Containers:                    containers,
			EphemeralContainers:           []v1.EphemeralContainer{},
			RestartPolicy:                 v1.RestartPolicyAlways,
			TerminationGracePeriodSeconds: &deletionGracePeriod,
			ActiveDeadlineSeconds:         &activeGracePeriod,
			DNSPolicy:                     v1.DNSNone,
			NodeSelector:                  map[string]string{},
			AutomountServiceAccountToken:  &mountServiceAccount,
			// ServiceAccountName:            "",
			// DeprecatedServiceAccount:      "",
			// NodeName:                     "",
			HostNetwork:           hostnetwork,
			HostPID:               false,
			HostIPC:               false,
			ShareProcessNamespace: &shareProcessNamespace,
			SecurityContext:       &v1.PodSecurityContext{
				// SELinuxOptions: &v1.SELinuxOptions{
				//  User:  "",
				//  Role:  "",
				//  Type:  "",
				//  Level: "",
				// },
				// WindowsOptions:      &v1.WindowsSecurityContextOptions{},
				// RunAsUser:           new(int64),
				// RunAsGroup:          new(int64),
				// RunAsNonRoot:        new(bool),
				// SupplementalGroups:  []int64{},
				// FSGroup:             new(int64),
				// Sysctls:             []v1.Sysctl{},
				// FSGroupChangePolicy: &"",
				// SeccompProfile:      &v1.SeccompProfile{},
			},
			ImagePullSecrets: []v1.LocalObjectReference{},
			Hostname:         "",
			Subdomain:        default_config.DefaultDomainName,
			Affinity:         &v1.Affinity{},
			// SchedulerName:             "",
			Tolerations: []v1.Toleration{},
			HostAliases: []v1.HostAlias{},
			// PriorityClassName:         "",
			// Priority:       new(int32),
			// DNSConfig:      &v1.PodDNSConfig{},
			ReadinessGates: []v1.PodReadinessGate{
				{
					ConditionType: v1.ContainersReady,
				},
			},
			// RuntimeClassName:          new(string),
			EnableServiceLinks:        &enableServiceLinks,
			Overhead:                  map[v1.ResourceName]resource.Quantity{},
			TopologySpreadConstraints: []v1.TopologySpreadConstraint{},
			SetHostnameAsFQDN:         &setHostnameAsFQDN,
			OS:                        &v1.PodOS{},
		},
		Status: v1.PodStatus{},
	}
	return daemonPod
}

func BuildATestDaemonPod() *v1.Pod {
	uid := uuid.New()
	daemonPod := BuildV1Pod(types.UID(uid.String()),
		default_config.DefaultFornaxCoreNodeNameSpace,
		"quark_daemon",
		true,
		true,
		[]v1.Container{
			{
				Name:      "busybox",
				Image:     "busybox:latest",
				Command:   []string{},
				Args:      []string{},
				Stdin:     false,
				StdinOnce: false,
				TTY:       false,
			},
		},
		[]v1.Container{util.BuildContainer("webserver", "nginx:latest", 0, 0, []v1.EnvVar{{
			Name:  "NGINX_PORT",
			Value: "8081",
			// ValueFrom: &v1.EnvVarSource{
			//  FieldRef:         &v1.ObjectFieldSelector{},
			//  ResourceFieldRef: &v1.ResourceFieldSelector{},
			//  ConfigMapKeyRef:  &v1.ConfigMapKeySelector{},
			//  SecretKeyRef:     &v1.SecretKeySelector{},
			// },
		}})},
	)

	return daemonPod
}

func BuildATestPodCreate(appId string) *grpc.FornaxCoreMessage {
	uid := uuid.New()
	testPod := BuildV1Pod(types.UID(uid.String()),
		"test_namespace",
		"test_nginx",
		false,
		false,
		[]v1.Container{},
		[]v1.Container{util.BuildContainer("webserver", "nginx:latest", 8082, 80, []v1.EnvVar{})},
	)

	// update node, send node configuration
	podId := uuid.New().String()
	mode := grpc.PodCreate_Active
	body := grpc.FornaxCoreMessage_PodCreate{
		PodCreate: &grpc.PodCreate{
			PodIdentifier: &podId,
			Mode:          &mode,
			Pod:           testPod,
			ConfigMap:     &v1.ConfigMap{},
		},
	}
	messageType := grpc.MessageType_POD_CREATE
	msg := &grpc.FornaxCoreMessage{
		MessageType: &messageType,
		MessageBody: &body,
	}
	return msg
}
