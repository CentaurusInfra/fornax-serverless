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

package node

import (
	"fmt"
	"time"

	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"
	"centaurusinfra.io/fornax-serverless/pkg/message"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/fornaxcore"
	internal "centaurusinfra.io/fornax-serverless/pkg/nodeagent/message"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/pod"
	fornaxtypes "centaurusinfra.io/fornax-serverless/pkg/nodeagent/types"
	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
)

// +enum
type NodeState string

const (
	NodeStateInitializing NodeState = "Initializing"
	NodeStateInitialized  NodeState = "Initialized"
	NodeStateRegistering  NodeState = "Registering"
	NodeStateRegistered   NodeState = "Registered"
	NodeStateReady        NodeState = "Ready"
)

type FornaxNodeActor struct {
	stopCh        chan struct{}
	node          FornaxNode
	state         NodeState
	innerActor    message.Actor
	fornoxCoreRef message.ActorRef
	podActors     map[string]*pod.PodActor
}

func (n *FornaxNodeActor) Stop() error {
	n.stopCh <- struct{}{}
	n.innerActor.Stop()
	for _, v := range n.podActors {
		v.Stop()
	}
	return nil
}

func (n *FornaxNodeActor) Start() error {
	// complete node dependencies
	for {
		err := n.node.Init()
		if err == nil {
			runtimeSummary, err := LoadPodsFromContainerRuntime(n.node.Dependencies.CRIRuntimeService, n.node.Dependencies.PodStore)
			if err != nil {
				klog.ErrorS(err, "failed to load container from runtime, wait for next 5 second")
				time.Sleep(5 * time.Second)
			} else {
				n.recreatePodStateFromRuntimeSummary(runtimeSummary)
				break
			}
		} else {
			klog.ErrorS(err, "failed to complete node initialization, retry in next 5 seconds")
			time.Sleep(5 * time.Second)
		}
	}

	n.state = NodeStateRegistering
	// register with fornax core
	for {
		if n.state != NodeStateRegistering {
			// if node has received node configuration from fornaxcore
			break
		}
		if err := n.innerActor.Reference().Send(
			n.fornoxCoreRef,
			grpc.NodeRegistry{
				NodeIp: &n.node.NodeConfig.NodeIP,
				Node:   n.node.V1Node,
			},
		); err != nil {
			klog.ErrorS(err, "node registry failed, retry in next 5s")
			time.Sleep(5 * time.Second)
		}
	}

	return nil
}

// this method need dependencies initialized and load runtime status successfully and report back to fornax
func (n *FornaxNodeActor) recreatePodStateFromRuntimeSummary(runtimeSummary ContainerWorldSummary) {
	for _, p := range runtimeSummary.terminatedPods {
		// still recreate pod actor for terminated pod in case some cleanup are required
		n.createPodAndActor(p.PodState, p.PodSpec.DeepCopy(), p.ConfigMapSpec.DeepCopy(), p.Daemon, true)
		// report back to fornax core, this pod is stopped
		n.innerActor.Reference().Send(n.fornoxCoreRef, pod.BuildFornaxcoreGrpcPodStateForTerminatedPod(p))
	}

	for _, p := range runtimeSummary.runningPods {
		n.createPodAndActor(p.PodState, p.PodSpec.DeepCopy(), p.ConfigMapSpec.DeepCopy(), p.Daemon, true)
		// report pod back to fornax core, and recreate pod and actors if it does not exist
		n.innerActor.Reference().Send(n.fornoxCoreRef, pod.BuildFornaxcoreGrpcPodState(p))
	}
}

func (n *FornaxNodeActor) MessageProcessor(msg message.ActorMessage) (interface{}, error) {
	switch msg.Body.(type) {
	case *grpc.FornaxCoreMessage:
		return n.processFornaxCoreMessage(msg.Body.(*grpc.FornaxCoreMessage))
	case internal.PodStatusChange:
		// update fornaxcore pod status change
		fppod := msg.Body.(internal.PodStatusChange).Pod
		n.innerActor.Reference().Send(n.fornoxCoreRef, pod.BuildFornaxcoreGrpcPodState(fppod))
	case internal.PodCleanup:
		fppod := msg.Body.(internal.PodCleanup).Pod
		n.innerActor.Reference().Send(n.fornoxCoreRef, pod.BuildFornaxcoreGrpcPodState(fppod))
		actor, found := n.podActors[string(fppod.Identifier)]
		if found {
			actor.Stop()
			delete(n.podActors, string(fppod.Identifier))
			delete(n.node.Pods, fppod.Identifier)
		}
	default:
	}

	return nil, nil
}

func (n *FornaxNodeActor) processFornaxCoreMessage(msg *grpc.FornaxCoreMessage) (interface{}, error) {
	msgType := msg.GetMessageType()
	switch msgType {
	case grpc.MessageType_NODE_CONFIGURATION:
		n.onNodeConfigurationCommand(msg.GetNodeConfiguration())

	case grpc.MessageType_POD_CREATE:
		n.onPodCreateCommand(msg.GetPodCreate())

	case grpc.MessageType_POD_TERMINATE:
		n.onPodTerminateCommand(msg.GetPodTerminate())

	case grpc.MessageType_POD_ACTIVE:
		n.onPodActiveCommand(msg.GetPodActive())

	case grpc.MessageType_SESSION_START:
		n.onSessionStartCommand(msg.GetSessionStart())

	case grpc.MessageType_SESSION_CLOSE:
		n.onSessionCloseCommand(msg.GetSessionClose())

		// messages are sent to fornaxcore, should just forward
	case grpc.MessageType_SESSION_STATE, grpc.MessageType_POD_STATE:
		n.innerActor.Reference().Send(n.fornoxCoreRef, msg)

		// messages are not supposed to be received by node
	case grpc.MessageType_NODE_READY, grpc.MessageType_NODE_REGISTER, grpc.MessageType_NODE_STATE:
	}
	return nil, nil
}

// initialize node with node spec provided by fornaxcore, especially pod cidr
func (n *FornaxNodeActor) onNodeConfigurationCommand(msg *grpc.NodeConfiguration) error {
	if n.state != NodeStateRegistering {
		return fmt.Errorf("node is not in registering state, it does not expect configuration change after registering")
	}

	apiNode := msg.GetNode()
	klog.Infof("received node configuration from fornaxcore: %v", apiNode)
	errs := ValidateNodeSpec(apiNode)
	if len(errs) > 0 {
		klog.Errorf("api node spec is invalid")
		return fmt.Errorf("api node spec is invalid")
	}

	n.node.V1Node.Spec = *apiNode.Spec.DeepCopy()

	if NodeSpecPodCidrChanged(n.node.V1Node, apiNode) {
		if len(n.node.Pods) > 0 {
			return fmt.Errorf("change pod cidr when node has pods is not allowed, should not happen")
		}
		// TODO, set up pod cidr
	}

	err := n.initializeNodeDaemons(msg.DaemonPods)
	if err != nil {
		klog.ErrorS(err, "Failed to initiaize daemons")
		return err
	}

	n.state = NodeStateRegistered
	// start go routine to report node status forever
	go wait.Until(func() {
		SetNodeStatus(&n.node)
		n.innerActor.Reference().Send(n.fornoxCoreRef, BuildFornaxGrpcNodeState(n.node))
		if err != nil {
			klog.ErrorS(err, "Failed to start healthz server")

		}
	}, 1*time.Minute, n.stopCh)

	// start go routine to check node status until it is ready
	go func() {
		for {
			// finish if node has initialized
			if n.state != NodeStateReady {
				break
			}

			// check node runtime dependencies, send node ready message if node is ready for new pod
			if IsNodeStatusReady(&n.node) {
				n.node.V1Node.Status.Phase = v1.NodeRunning
				n.innerActor.Reference().Send(n.fornoxCoreRef,
					grpc.NodeReady{
						NodeIp: &n.node.NodeConfig.NodeIP,
						Node:   n.node.V1Node,
					},
				)
				n.state = NodeStateReady
			} else {
				klog.InfoS("node is not ready yet, check again in 5s")
				time.Sleep(5 * time.Second)
			}
		}
	}()
	return nil
}

func (n *FornaxNodeActor) initializeNodeDaemons(pods []*v1.Pod) error {
	for _, p := range pods {
		errs := pod.ValidatePodSpec(p)
		if len(errs) != 0 {
			return errors.Errorf("pod spec is invalid %v", errs)
		}

		if len(p.Spec.Containers) != 1 {
			return errors.Errorf("daemon pod can only have one container, but this pod spec has %d container(s)", len(p.Spec.Containers))
		}

		if !p.Spec.HostNetwork {
			return errors.Errorf("daemon pod must use host network")
		}

		_, found := n.node.Pods[string(p.UID)]
		if !found {
			n.createPodAndActor(fornaxtypes.PodStateCreating, p.DeepCopy(), nil, true, false)
		}
	}
	return nil
}

func buildAFornaxPod(state fornaxtypes.PodState, v1pod *v1.Pod, configMap *v1.ConfigMap, podIdentifier string, daemon bool) (*fornaxtypes.FornaxPod, error) {
	errs := pod.ValidatePodSpec(v1pod)
	if len(errs) > 0 {
		return nil, errors.New("pod spec is invalid")
	}
	errs = pod.ValidateConfigMapSpec(configMap)
	if len(errs) > 0 {
		return nil, errors.New("pod spec is invalid")
	}
	fornaxPod := &fornaxtypes.FornaxPod{
		Identifier:    podIdentifier,
		ApplicationId: "",
		Daemon:        daemon,
		PodState:      state,
		PodSpec:       v1pod.DeepCopy(),
	}

	if configMap != nil {
		fornaxPod.ConfigMapSpec = configMap.DeepCopy()
	}
	return fornaxPod, nil
}

func (n *FornaxNodeActor) createPodAndActor(state fornaxtypes.PodState, p *v1.Pod, c *v1.ConfigMap, daemon bool, recreate bool) (*fornaxtypes.FornaxPod, *pod.PodActor, error) {
	fp, err := buildAFornaxPod(state, p, c, string(p.GetUID()), daemon)
	if err != nil {
		klog.Errorf("failed to initialize a Pod, %v", err)
		return nil, nil, err
	}

	fpActor := pod.NewPodActor(n.innerActor.Reference(), fp, &n.node.NodeConfig, n.node.Dependencies)
	fpActor.Start()
	n.node.Pods[fp.Identifier] = fp
	n.podActors[fp.Identifier] = fpActor
	return fp, fpActor, nil
}

// find pod actor and send a message to it
// if pod actor does not exist, create one
func (n *FornaxNodeActor) onPodCreateCommand(msg *grpc.PodCreate) error {
	if n.state != NodeStateReady {
		return fmt.Errorf("node is not in ready state to create a new pod")
	}
	_, found := n.node.Pods[*msg.PodIdentifier]
	if !found {
		pod, podActor, err := n.createPodAndActor(fornaxtypes.PodStateCreating, msg.GetPod().DeepCopy(), msg.GetConfigMap().DeepCopy(), false, false)
		if err != nil {
			return err
		}
		n.innerActor.Reference().Send(podActor.Reference(), internal.PodCreate{
			Pod: pod,
		})
	} else {
		// not supposed to receive create command for a existing pod, ignore it and send back pod status
		return fmt.Errorf("pod: %s already exist", *msg.PodIdentifier)
	}
	return nil
}

// find pod actor and send a message to it
// if pod actor does not exist, create one
func (n *FornaxNodeActor) onPodTerminateCommand(msg *grpc.PodTerminate) error {
	podActor, found := n.podActors[*msg.PodIdentifier]
	if !found {
		return fmt.Errorf("pod: %s does not exist, probably fornax core is not in sync", *msg.PodIdentifier)
	} else {
		n.innerActor.Reference().Send(podActor.Reference(), internal.PodTerminate{})
	}
	return nil

}

// find pod actor and send a message to it
// if pod actor does not exist, return error
func (n *FornaxNodeActor) onPodActiveCommand(msg *grpc.PodActive) error {
	if n.state != NodeStateReady {
		return fmt.Errorf("node is not in ready state to active a standby pod")
	}
	podActor, found := n.podActors[*msg.PodIdentifier]
	if !found {
		return fmt.Errorf("pod: %s does not exist, probably fornax core is not in sync", *msg.PodIdentifier)
	} else {
		n.innerActor.Reference().Send(podActor.Reference(), internal.PodActive{})
	}
	return nil

}

// find pod actor to let it start a session, if pod actor does not exist, return failure
func (n *FornaxNodeActor) onSessionStartCommand(msg *grpc.SessionStart) error {
	if n.state != NodeStateReady {
		return fmt.Errorf("node is not in ready state to start a session")
	}
	podActor, found := n.podActors[*msg.PodIdentifier]
	if !found {
		return fmt.Errorf("pod: %s does not exist, can not start session", *msg.PodIdentifier)
	} else {
		n.innerActor.Reference().Send(podActor.Reference(), internal.SessionStart{})
	}
	return nil
}

// find pod actor to let it terminate a session, if pod actor does not exist, return failure
func (n *FornaxNodeActor) onSessionCloseCommand(msg *grpc.SessionClose) error {
	podActor, found := n.podActors[*msg.PodIdentifier]
	if !found {
		return fmt.Errorf("pod: %s does not exist, probably fornax core is not in sync, can not close session", *msg.PodIdentifier)
	} else {
		n.innerActor.Reference().Send(podActor.Reference(), internal.SessionClose{})
	}
	return nil
}

func NewNodeActor(node FornaxNode) (*FornaxNodeActor, error) {
	if node.V1Node == nil {
		v1node, err := node.initV1Node()
		if err != nil {
			return nil, err
		} else {
			node.V1Node = v1node
		}
	}

	actor := &FornaxNodeActor{
		stopCh:     make(chan struct{}),
		node:       node,
		state:      NodeStateInitializing,
		innerActor: nil,
		podActors:  map[string]*pod.PodActor{},
	}
	actor.innerActor = message.NewLocalChannelActor(string(node.V1Node.GetUID()), actor.MessageProcessor)

	fornaxCoreActor := fornaxcore.NewFornaxCoreActor(node.NodeConfig.Hostname, node.NodeConfig.FornaxCoreIPs)
	actor.fornoxCoreRef = fornaxCoreActor.Reference()

	err := fornaxCoreActor.Start(actor.innerActor.Reference())
	if err != nil {
		klog.Errorf("can not start fornax core actor, error %v", err)
	}

	return actor, nil
}
