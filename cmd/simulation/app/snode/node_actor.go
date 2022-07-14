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

package snode

import (
	"fmt"
	"time"

	fornaxgrpc "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"
	"centaurusinfra.io/fornax-serverless/pkg/message"

	//"centaurusinfra.io/fornax-serverless/pkg/nodeagent/fornaxcore"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/fornaxcore"
	internal "centaurusinfra.io/fornax-serverless/pkg/nodeagent/message"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/pod"
	fornaxtypes "centaurusinfra.io/fornax-serverless/pkg/nodeagent/types"
	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
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
	node          *FornaxNode
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
	n.innerActor.Start()
	// complete node dependencies
	// for {
	// 	klog.InfoS("Init node spec for node registry")
	// 	err := n.node.Init()
	// 	if err == nil {
	// 		klog.InfoS("Load pod state from runtime service and nodeagent store")
	// 		runtimeSummary, err := LoadPodsFromContainerRuntime(n.node.Dependencies.CRIRuntimeService, n.node.Dependencies.PodStore)
	// 		if err != nil {
	// 			klog.ErrorS(err, "Failed to load container from runtime, wait for next 5 second")
	// 			time.Sleep(5 * time.Second)
	// 		} else {
	// 			n.recreatePodStateFromRuntimeSummary(runtimeSummary)
	// 			break
	// 		}
	// 	} else {
	// 		klog.ErrorS(err, "Failed to complete node initialization, retry in next 5 seconds")
	// 		time.Sleep(5 * time.Second)
	// 	}
	// }

	n.state = NodeStateRegistering
	var count int32
	// register with fornax core
	for {
		if n.state != NodeStateRegistering {
			// if node has received node configuration from fornaxcore
			break
		}
		klog.InfoS("Start node registry", "node spec", n.node.V1Node)
		messageType := fornaxgrpc.MessageType_NODE_REGISTER
		klog.Infof("Node Message Type %d : %s", count, messageType) //test purpose for this line
		n.notify(
			n.fornoxCoreRef,
			&fornaxgrpc.FornaxCoreMessage{
				MessageType: &messageType,
				MessageBody: &fornaxgrpc.FornaxCoreMessage_NodeRegistry{
					NodeRegistry: &fornaxgrpc.NodeRegistry{
						NodeIp: &n.node.NodeConfig.NodeIP,
						Node:   n.node.V1Node,
					},
				},
			},
		)
		time.Sleep(5 * time.Second)
		count++
	}

	return nil
}

// this method need dependencies initialized and load runtime status successfully and report back to fornax
func (n *FornaxNodeActor) recreatePodStateFromRuntimeSummary(runtimeSummary ContainerWorldSummary) {
	for _, p := range runtimeSummary.terminatedPods {
		// still recreate pod actor for terminated pod in case some cleanup are required
		n.recoverPodActor(p)
		// report back to fornax core, this pod is stopped
		n.notify(n.fornoxCoreRef, pod.BuildFornaxcoreGrpcPodStateForTerminatedPod(p))
	}

	for _, p := range runtimeSummary.runningPods {
		n.recoverPodActor(p)
		// report pod back to fornax core, and recreate pod and actors if it does not exist
		n.notify(n.fornoxCoreRef, pod.BuildFornaxcoreGrpcPodState(p))
	}
}

func (n *FornaxNodeActor) MessageProcessor(msg message.ActorMessage) (interface{}, error) {
	switch msg.Body.(type) {
	case *fornaxgrpc.FornaxCoreMessage:
		return n.processFornaxCoreMessage(msg.Body.(*fornaxgrpc.FornaxCoreMessage))
	case internal.PodStatusChange:
		fppod := msg.Body.(internal.PodStatusChange).Pod
		n.notify(n.fornoxCoreRef, pod.BuildFornaxcoreGrpcPodState(fppod))
	case internal.PodCleanup:
		fppod := msg.Body.(internal.PodCleanup).Pod
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

func (n *FornaxNodeActor) processFornaxCoreMessage(msg *fornaxgrpc.FornaxCoreMessage) (interface{}, error) {
	msgType := msg.GetMessageType()
	switch msgType {
	case fornaxgrpc.MessageType_NODE_CONFIGURATION:
		n.onNodeConfigurationCommand(msg.GetNodeConfiguration())

	case fornaxgrpc.MessageType_POD_CREATE:
		n.onPodCreateCommand(msg.GetPodCreate())

	case fornaxgrpc.MessageType_POD_TERMINATE:
		n.onPodTerminateCommand(msg.GetPodTerminate())

	case fornaxgrpc.MessageType_POD_ACTIVE:
		n.onPodActiveCommand(msg.GetPodActive())

	case fornaxgrpc.MessageType_SESSION_START:
		n.onSessionStartCommand(msg.GetSessionStart())

	case fornaxgrpc.MessageType_SESSION_CLOSE:
		n.onSessionCloseCommand(msg.GetSessionClose())

		// messages are sent to fornaxcore, should just forward
	case fornaxgrpc.MessageType_SESSION_STATE, fornaxgrpc.MessageType_POD_STATE, fornaxgrpc.MessageType_NODE_STATE:
		n.notify(n.fornoxCoreRef, msg)

		// messages are not supposed to be received by node
	default:
		klog.Errorf("Message type %s is not supposed to sent to node actor", msgType)
	}
	return nil, nil
}

// initialize node with node spec provided by fornaxcore, especially pod cidr
func (n *FornaxNodeActor) onNodeConfigurationCommand(msg *fornaxgrpc.NodeConfiguration) error {
	if n.state != NodeStateRegistering {
		return fmt.Errorf("node is not in registering state, it does not expect configuration change after registering")
	}

	apiNode := msg.GetNode()
	klog.Infof("received node spec from fornaxcore: %v", apiNode)
	errs := ValidateNodeSpec(apiNode)
	if len(errs) > 0 {
		klog.Errorf("api node spec is invalid, %v", errs)
		return fmt.Errorf("api node spec is invalid, %v", errs)
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
		//SetNodeStatus(n.node)
		n.notify(n.fornoxCoreRef, BuildFornaxGrpcNodeState(n.node))
	}, 1*time.Minute, n.stopCh)

	// start go routine to check node status until it is ready
	go func() {
		for {
			// finish if node has initialized
			if n.state != NodeStateRegistered {
				break
			}

			// check node runtime dependencies, send node ready message if node is ready for new pod
			if IsNodeStatusReady(n.node) {
				klog.InfoS("Node is ready, tell fornax core")
				messageType := fornaxgrpc.MessageType_NODE_READY
				n.node.V1Node.Status.Phase = v1.NodeRunning
				n.notify(n.fornoxCoreRef,
					&fornaxgrpc.FornaxCoreMessage{
						MessageType: &messageType,
						MessageBody: &fornaxgrpc.FornaxCoreMessage_NodeReady{
							NodeReady: &fornaxgrpc.NodeReady{
								NodeIp: &n.node.NodeConfig.NodeIP,
								Node:   n.node.V1Node,
							},
						},
					},
				)
				n.state = NodeStateReady
			} else {
				time.Sleep(5 * time.Second)
			}
		}
	}()
	return nil
}

func (n *FornaxNodeActor) initializeNodeDaemons(pods []*v1.Pod) error {
	for _, p := range pods {
		klog.Infof("Initialize daemon pod, %v", p)
		errs := pod.ValidatePodSpec(p)
		if len(errs) != 0 {
			return errors.Errorf("Pod spec is invalid %v", errs)
		}

		if len(p.Spec.Containers) != 1 {
			return errors.Errorf("Daemon pod can only have one container, but this pod spec has %d container(s)", len(p.Spec.Containers))
		}

		if !p.Spec.HostNetwork {
			return errors.Errorf("Daemon pod must use host network")
		}

		_, found := n.node.Pods[string(p.UID)]
		if !found {
			_, actor, err := n.createPodAndActor(fornaxtypes.PodStateCreating, p.UID, p.DeepCopy(), nil, true)
			if err != nil {
				return err
			} else {
				n.notify(actor.Reference(), internal.PodCreate{})
			}
		}
	}
	return nil
}

func buildAFornaxPod(state fornaxtypes.PodState, applicationId types.UID, v1pod *v1.Pod, configMap *v1.ConfigMap, podIdentifier string, daemon bool) (*fornaxtypes.FornaxPod, error) {
	errs := pod.ValidatePodSpec(v1pod)
	if len(errs) > 0 {
		return nil, errors.New("Pod spec is invalid")
	}
	errs = pod.ValidateConfigMapSpec(configMap)
	if len(errs) > 0 {
		return nil, errors.New("ConfigMap spec is invalid")
	}
	fornaxPod := &fornaxtypes.FornaxPod{
		Identifier:              podIdentifier,
		ApplicationId:           applicationId,
		PodState:                state,
		Daemon:                  daemon,
		PodSpec:                 v1pod.DeepCopy(),
		RuntimePod:              nil,
		Containers:              map[string]*fornaxtypes.Container{},
		LastStateTransitionTime: time.Now(),
	}

	if configMap != nil {
		fornaxPod.ConfigMapSpec = configMap.DeepCopy()
	}
	return fornaxPod, nil
}

func (n *FornaxNodeActor) recoverPodActor(fpod *fornaxtypes.FornaxPod) (*fornaxtypes.FornaxPod, *pod.PodActor, error) {
	klog.InfoS("Recover pod actor for a running pod", "pod", fornaxtypes.UniquePodName(fpod))

	// fpActor := pod.NewPodActor(n.innerActor.Reference(), fpod, &n.node.NodeConfig, n.node.Dependencies, pod.ErrRecoverPod)
	fpActor := pod.NewPodActor(n.innerActor.Reference(), fpod, &n.node.NodeConfig, nil, pod.ErrRecoverPod)
	fpActor.Start()
	n.node.Pods[fpod.Identifier] = fpod
	n.podActors[fpod.Identifier] = fpActor
	return fpod, fpActor, nil
}

func (n *FornaxNodeActor) createPodAndActor(state fornaxtypes.PodState, applicationId types.UID, p *v1.Pod, c *v1.ConfigMap, daemon bool) (*fornaxtypes.FornaxPod, *pod.PodActor, error) {
	fpod, err := buildAFornaxPod(state, applicationId, p, c, string(p.GetUID()), daemon)
	if err != nil {
		klog.ErrorS(err, "Failed to build a FornaxPod obj from pod spec", "namespace", p.Namespace, "name", p.Name)
		return nil, nil, err
	}

	// fpActor := pod.NewPodActor(n.innerActor.Reference(), fpod, &n.node.NodeConfig, n.node.Dependencies, nil)
	fpActor := pod.NewPodActor(n.innerActor.Reference(), fpod, &n.node.NodeConfig, nil, nil)
	fpActor.Start()
	n.node.Pods[fpod.Identifier] = fpod
	n.podActors[fpod.Identifier] = fpActor
	return fpod, fpActor, nil
}

// find pod actor and send a message to it
// if pod actor does not exist, create one
func (n *FornaxNodeActor) onPodCreateCommand(msg *fornaxgrpc.PodCreate) error {
	if n.state != NodeStateReady {
		return fmt.Errorf("Node is not in ready state to create a new pod")
	}
	_, found := n.node.Pods[*msg.PodIdentifier]
	if !found {
		pod, actor, err := n.createPodAndActor(fornaxtypes.PodStateCreating, types.UID(*msg.AppIdentifier), msg.GetPod().DeepCopy(), msg.GetConfigMap().DeepCopy(), false)
		if err != nil {
			return err
		}
		n.notify(actor.Reference(), internal.PodCreate{Pod: pod})
	} else {
		// not supposed to receive create command for a existing pod, ignore it and send back pod status
		return fmt.Errorf("Pod: %s already exist", *msg.PodIdentifier)
	}
	return nil
}

// find pod actor and send a message to it
// if pod actor does not exist, create one
func (n *FornaxNodeActor) onPodTerminateCommand(msg *fornaxgrpc.PodTerminate) error {
	podActor, found := n.podActors[*msg.PodIdentifier]
	if !found {
		return fmt.Errorf("Pod: %s does not exist, probably fornax core is not in sync", *msg.PodIdentifier)
	} else {
		n.notify(podActor.Reference(), internal.PodTerminate{})
	}
	return nil

}

// find pod actor and send a message to it
// if pod actor does not exist, return error
func (n *FornaxNodeActor) onPodActiveCommand(msg *fornaxgrpc.PodActive) error {
	if n.state != NodeStateReady {
		return fmt.Errorf("Node is not in ready state to active a standby pod")
	}
	podActor, found := n.podActors[*msg.PodIdentifier]
	if !found {
		return fmt.Errorf("Pod: %s does not exist, probably fornax core is not in sync", *msg.PodIdentifier)
	} else {
		n.notify(podActor.Reference(), internal.PodActive{})
	}
	return nil

}

// find pod actor to let it start a session, if pod actor does not exist, return failure
func (n *FornaxNodeActor) onSessionStartCommand(msg *fornaxgrpc.SessionStart) error {
	if n.state != NodeStateReady {
		return fmt.Errorf("node is not in ready state to start a session")
	}
	podActor, found := n.podActors[*msg.PodIdentifier]
	if !found {
		return fmt.Errorf("Pod: %s does not exist, can not start session", *msg.PodIdentifier)
	} else {
		n.notify(podActor.Reference(), internal.SessionStart{})
	}
	return nil
}

// find pod actor to let it terminate a session, if pod actor does not exist, return failure
func (n *FornaxNodeActor) onSessionCloseCommand(msg *fornaxgrpc.SessionClose) error {
	podActor, found := n.podActors[*msg.PodIdentifier]
	if !found {
		return fmt.Errorf("Pod: %s does not exist, probably fornax core is not in sync, can not close session", *msg.PodIdentifier)
	} else {
		n.notify(podActor.Reference(), internal.SessionClose{})
	}
	return nil
}

func (n *FornaxNodeActor) notify(receiver message.ActorRef, msg interface{}) {
	message.Send(n.innerActor.Reference(), receiver, msg)
}

func NewNodeActor(node *FornaxNode) (*FornaxNodeActor, error) {
	if node.V1Node == nil {
		v1node, err := node.initV1Node()
		if err != nil {
			return nil, err
		} else {
			node.V1Node = v1node
		}
	}
	//SetNodeStatus(node)

	actor := &FornaxNodeActor{
		stopCh:     make(chan struct{}),
		node:       node,
		state:      NodeStateInitializing,
		innerActor: nil,
		podActors:  map[string]*pod.PodActor{},
	}
	actor.innerActor = message.NewLocalChannelActor(node.V1Node.GetName(), actor.MessageProcessor)

	klog.Info("Starting FornaxCore actor")
	fornaxCoreActor := fornaxcore.NewFornaxCoreActor(node.NodeConfig.NodeIP, node.V1Node.Name, node.NodeConfig.FornaxCoreIPs)
	actor.fornoxCoreRef = fornaxCoreActor.Reference()
	err := fornaxCoreActor.Start(actor.innerActor.Reference())
	if err != nil {
		klog.ErrorS(err, "Can not start fornax core actor")
		return nil, err
	}
	klog.Info("Fornax core actor started")

	return actor, nil
}
