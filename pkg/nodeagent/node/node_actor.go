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
	"sync"
	"time"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	fornaxgrpc "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"
	"centaurusinfra.io/fornax-serverless/pkg/message"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/fornaxcore"
	internal "centaurusinfra.io/fornax-serverless/pkg/nodeagent/message"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/pod"
	fornaxtypes "centaurusinfra.io/fornax-serverless/pkg/nodeagent/types"
	"centaurusinfra.io/fornax-serverless/pkg/util"

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
	nodeMutex     sync.RWMutex
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
	for {
		klog.InfoS("Init node spec for node registry")
		err := n.node.Init()
		if err == nil {
			klog.InfoS("Load pod state from runtime service and nodeagent store")
			runtimeSummary, err := LoadPodsFromContainerRuntime(n.node.Dependencies.CRIRuntimeService, n.node.Dependencies.PodStore)
			if err != nil {
				klog.ErrorS(err, "Failed to load container from runtime, wait for next 5 second")
				time.Sleep(5 * time.Second)
			} else {
				n.recreatePodStateFromRuntimeSummary(runtimeSummary)
				break
			}
		} else {
			klog.ErrorS(err, "Failed to complete node initialization, retry in next 5 seconds")
			time.Sleep(5 * time.Second)
		}
	}

	n.state = NodeStateRegistering
	n.incrementNodeRevision()
	// register with fornax core
	for {
		if n.state != NodeStateRegistering {
			// if node has received node configuration from fornaxcore
			break
		}
		klog.InfoS("Start node registry", "node spec", n.node.V1Node)
		messageType := fornaxgrpc.MessageType_NODE_REGISTER
		n.notify(
			n.fornoxCoreRef,
			&fornaxgrpc.FornaxCoreMessage{
				MessageType: &messageType,
				MessageBody: &fornaxgrpc.FornaxCoreMessage_NodeRegistry{
					NodeRegistry: &fornaxgrpc.NodeRegistry{
						NodeRevision: &n.node.Revision,
						Node:         n.node.V1Node,
					},
				},
			},
		)
		time.Sleep(5 * time.Second)
	}

	return nil
}

// this method need dependencies initialized and load runtime status successfully and report back to fornax
func (n *FornaxNodeActor) recreatePodStateFromRuntimeSummary(runtimeSummary ContainerWorldSummary) {
	for _, fpod := range runtimeSummary.terminatedPods {
		// still recreate pod actor for terminated pod in case some cleanup are required
		// klog.InfoS("Recover pod actor for a terminated pod", "pod", fornaxtypes.UniquePodName(fpod), "state", fpod.PodState)
		klog.InfoS("Recover pod actor for a terminated pod", "pod", fpod)
		n.recoverPodActor(fpod)
		// report back to fornax core, this pod is stopped
		n.notify(n.fornoxCoreRef, pod.BuildFornaxcoreGrpcPodStateForTerminatedPod(n.node.Revision, fpod))
	}

	for _, fpod := range runtimeSummary.runningPods {
		klog.InfoS("Recover pod actor for a running pod", "pod", fornaxtypes.UniquePodName(fpod), "state", fpod.FornaxPodState)
		n.recoverPodActor(fpod)
		// report pod back to fornax core, and recreate pod and actors if it does not exist
		n.notify(n.fornoxCoreRef, pod.BuildFornaxcoreGrpcPodState(n.node.Revision, fpod))
	}
}

func (n *FornaxNodeActor) startStateReport() {
	// start go routine to report node status forever
	go wait.Until(func() {
		SetNodeStatus(n.node)
		n.notify(n.fornoxCoreRef, BuildFornaxGrpcNodeState(n.node))
	}, 1*time.Minute, n.stopCh)
}

func (n *FornaxNodeActor) incrementNodeRevision() error {
	n.nodeMutex.Lock()
	defer n.nodeMutex.Unlock()

	n.node.Revision += 1
	return n.node.Dependencies.NodeStore.PutNode(
		&fornaxtypes.FornaxNodeWithRevision{
			Identifier: util.UniqueNodeName(n.node.V1Node),
			Node:       n.node.V1Node.DeepCopy(),
			Revision:   n.node.Revision,
		},
	)
}

func (n *FornaxNodeActor) actorMessageProcess(msg message.ActorMessage) (interface{}, error) {
	switch msg.Body.(type) {
	case *fornaxgrpc.FornaxCoreMessage:
		return n.processFornaxCoreMessage(msg.Body.(*fornaxgrpc.FornaxCoreMessage))
	case internal.PodStatusChange:
		// increment node revision since one pod status changed
		fppod := msg.Body.(internal.PodStatusChange).Pod
		n.incrementNodeRevision()
		n.notify(n.fornoxCoreRef, pod.BuildFornaxcoreGrpcPodState(n.node.Revision, fppod))
	case internal.PodCleanup:
		fppod := msg.Body.(internal.PodCleanup).Pod
		klog.InfoS("Cleanup pod actor and store", "pod", fornaxtypes.UniquePodName(fppod), "state", fppod.FornaxPodState)
		if fppod.FornaxPodState != fornaxtypes.PodStateTerminated && fppod.FornaxPodState != fornaxtypes.PodStateFailed {
			klog.Warning("Received cleanup when pod is not in terminated or failed state", "pod", fornaxtypes.UniquePodName(fppod))
			return nil, fmt.Errorf("Pod is not in terminated state, pod: %s", fornaxtypes.UniquePodName(fppod))
		}
		actor, found := n.podActors[string(fppod.Identifier)]
		if found {
			actor.Stop()
			delete(n.podActors, string(fppod.Identifier))
			delete(n.node.Pods, fppod.Identifier)
		}
		err := n.node.Dependencies.PodStore.DelObject(fppod.Identifier)
		if err != nil {
			// TODO, if failed to delete a terminated pod, it should be fine, but need better cleanup
			return nil, err
		}
	default:
		klog.InfoS("Received unknown message", "from", msg.Sender, "msg", msg.Body)
	}

	return nil, nil
}

// TODO, notify fornax core fatal error
func (n *FornaxNodeActor) processFornaxCoreMessage(msg *fornaxgrpc.FornaxCoreMessage) (interface{}, error) {
	var err error
	msgType := msg.GetMessageType()
	switch msgType {
	case fornaxgrpc.MessageType_NODE_CONFIGURATION:
		err = n.onNodeConfigurationCommand(msg.GetNodeConfiguration())
	case fornaxgrpc.MessageType_NODE_FULL_SYNC:
		err = n.onNodeFullSyncCommand(msg.GetNodeFullSync())
	case fornaxgrpc.MessageType_POD_CREATE:
		err = n.onPodCreateCommand(msg.GetPodCreate())
	case fornaxgrpc.MessageType_POD_TERMINATE:
		err = n.onPodTerminateCommand(msg.GetPodTerminate())
	case fornaxgrpc.MessageType_POD_ACTIVE:
		err = n.onPodActiveCommand(msg.GetPodActive())
	case fornaxgrpc.MessageType_SESSION_START:
		err = n.onSessionStartCommand(msg.GetSessionStart())
	case fornaxgrpc.MessageType_SESSION_CLOSE:
		err = n.onSessionCloseCommand(msg.GetSessionClose())
	case fornaxgrpc.MessageType_SESSION_STATE, fornaxgrpc.MessageType_POD_STATE, fornaxgrpc.MessageType_NODE_STATE:
		// messages are sent to fornaxcore, should just forward
		n.notify(n.fornoxCoreRef, msg)
		// messages are not supposed to be received by node
	default:
		klog.Warningf("FornaxCoreMessage of type %s is not handled by actor", msgType)
	}
	// TODO, handle error, define reply message to fornax core
	return nil, err
}

// initialize node with node spec provided by fornaxcore, especially pod cidr
func (n *FornaxNodeActor) onNodeFullSyncCommand(msg *fornaxgrpc.NodeFullSync) error {
	n.notify(n.fornoxCoreRef, BuildFornaxGrpcNodeState(n.node))
	return nil
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
	// start go routine to check node status until it is ready
	go func() {
		for {
			// finish if node has initialized
			if n.state != NodeStateRegistered {
				break
			}

			// check node runtime dependencies, send node ready message if node is ready for new pod
			SetNodeStatus(n.node)
			if IsNodeStatusReady(n.node) {
				klog.InfoS("Node is ready, tell fornax core")
				n.node.V1Node.Status.Phase = v1.NodeRunning
				// bump revision to let fornax core to update node status
				n.incrementNodeRevision()
				n.notify(n.fornoxCoreRef, BuildFornaxGrpcNodeReady(n.node))
				n.state = NodeStateReady
				n.startStateReport()
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

		_, found := n.node.Pods[util.UniquePodName(p)]
		if !found {
			_, actor, err := n.createPodAndActor(fornaxtypes.PodStateCreating, p.DeepCopy(), nil, true)
			if err != nil {
				return err
			} else {
				n.notify(actor.Reference(), internal.PodCreate{})
			}
		} else {
			// TODO, need to update daemon if spec changed
		}
	}
	return nil
}

func (n *FornaxNodeActor) buildAFornaxPod(state fornaxtypes.PodState,
	v1pod *v1.Pod,
	configMap *v1.ConfigMap,
	isDaemon bool) (*fornaxtypes.FornaxPod, error) {
	errs := pod.ValidatePodSpec(v1pod)
	if len(errs) > 0 {
		return nil, errors.New("Pod spec is invalid")
	}
	fornaxPod := &fornaxtypes.FornaxPod{
		Identifier:              util.UniquePodName(v1pod),
		FornaxPodState:          state,
		Daemon:                  isDaemon,
		Pod:                     v1pod.DeepCopy(),
		RuntimePod:              nil,
		Containers:              map[string]*fornaxtypes.Container{},
		LastStateTransitionTime: time.Now(),
	}
	pod.SetPodStatus(fornaxPod, n.node.V1Node)
	fornaxPod.Pod.Labels[fornaxv1.LabelFornaxCoreNode] = util.UniqueNodeName(n.node.V1Node)

	if configMap != nil {
		errs = pod.ValidateConfigMapSpec(configMap)
		if len(errs) > 0 {
			return nil, errors.New("ConfigMap spec is invalid")
		}
		fornaxPod.ConfigMap = configMap.DeepCopy()
	}
	return fornaxPod, nil
}

func (n *FornaxNodeActor) recoverPodActor(fpod *fornaxtypes.FornaxPod) (*fornaxtypes.FornaxPod, *pod.PodActor, error) {
	// set pod node related information in case node name and ip changed
	fpod.Pod = fpod.Pod.DeepCopy()
	fpod.Pod.Status.HostIP = n.node.V1Node.Status.Addresses[0].Address
	fpod.Pod.Labels[fornaxv1.LabelFornaxCoreNode] = util.UniqueNodeName(n.node.V1Node)

	fpActor := pod.NewPodActor(n.innerActor.Reference(), fpod, &n.node.NodeConfig, n.node.Dependencies, pod.ErrRecoverPod)
	fpActor.Start()
	n.node.Pods[fpod.Identifier] = fpod
	n.podActors[fpod.Identifier] = fpActor
	return fpod, fpActor, nil
}

func (n *FornaxNodeActor) createPodAndActor(state fornaxtypes.PodState,
	v1Pod *v1.Pod,
	v1Config *v1.ConfigMap,
	isDaemon bool) (*fornaxtypes.FornaxPod, *pod.PodActor, error) {
	// create fornax pod obj
	fpod, err := n.buildAFornaxPod(state, v1Pod, v1Config, isDaemon)
	if err != nil {
		klog.ErrorS(err, "Failed to build a FornaxPod from pod spec", "namespace", v1Pod.Namespace, "name", v1Pod.Name)
		return nil, nil, err
	}

	err = n.node.Dependencies.PodStore.PutPod(fpod)
	if err != nil {
		// failed to save this pod
		klog.ErrorS(err, "Failed to save FornaxPod", "namespace", v1Pod.Namespace, "name", v1Pod.Name)
		return nil, nil, err
	}

	// new pod actor and start it
	fpActor := pod.NewPodActor(n.innerActor.Reference(), fpod, &n.node.NodeConfig, n.node.Dependencies, nil)
	fpActor.Start()
	n.node.Pods[fpod.Identifier] = fpod
	n.podActors[fpod.Identifier] = fpActor
	return fpod, fpActor, nil
}

// find pod actor and send a message to it, if pod actor does not exist, create one
func (n *FornaxNodeActor) onPodCreateCommand(msg *fornaxgrpc.PodCreate) error {
	if n.state != NodeStateReady {
		return fmt.Errorf("Node is not in ready state to create a new pod")
	}
	_, found := n.node.Pods[*msg.PodIdentifier]
	if !found {
		pod, actor, err := n.createPodAndActor(
			fornaxtypes.PodStateCreating,
			msg.GetPod().DeepCopy(),
			msg.GetConfigMap().DeepCopy(),
			false,
		)
		if err != nil {
			return err
		}
		n.notify(actor.Reference(), internal.PodCreate{Pod: pod})
	} else {
		// TODO, need to update daemon if spec changed
		// not supposed to receive create command for a existing pod, ignore it and send back pod status
		// pod should be terminate and recreated for update case
		return fmt.Errorf("Pod: %s already exist", *msg.PodIdentifier)
	}
	return nil
}

// find pod actor and send a message to it, if pod actor does not exist, return error
func (n *FornaxNodeActor) onPodTerminateCommand(msg *fornaxgrpc.PodTerminate) error {
	podActor, found := n.podActors[*msg.PodIdentifier]
	if !found {
		return fmt.Errorf("Pod: %s does not exist, fornax core is not in sync", *msg.PodIdentifier)
	} else {
		n.notify(podActor.Reference(), internal.PodTerminate{})
	}
	return nil
}

// find pod actor and send a message to it, if pod actor does not exist, return error
func (n *FornaxNodeActor) onPodActiveCommand(msg *fornaxgrpc.PodActive) error {
	if n.state != NodeStateReady {
		return fmt.Errorf("Node is not in ready state to active a standby pod")
	}
	podActor, found := n.podActors[*msg.PodIdentifier]
	if !found {
		return fmt.Errorf("Pod: %s does not exist, fornax core is not in sync", *msg.PodIdentifier)
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
		return fmt.Errorf("Pod: %s does not exist, fornax core is not in sync, can not close session", *msg.PodIdentifier)
	} else {
		n.notify(podActor.Reference(), internal.SessionClose{})
	}
	return nil
}

func (n *FornaxNodeActor) notify(receiver message.ActorRef, msg interface{}) {
	message.Send(n.innerActor.Reference(), receiver, msg)
}

func NewNodeActor(node *FornaxNode) (*FornaxNodeActor, error) {
	actor := &FornaxNodeActor{
		nodeMutex:     sync.RWMutex{},
		stopCh:        make(chan struct{}),
		node:          node,
		state:         NodeStateInitializing,
		innerActor:    nil,
		fornoxCoreRef: nil,
		podActors:     map[string]*pod.PodActor{},
	}
	actor.innerActor = message.NewLocalChannelActor(node.V1Node.GetName(), actor.actorMessageProcess)

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
