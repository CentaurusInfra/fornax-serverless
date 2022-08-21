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
	"encoding/json"
	"fmt"
	"sync"
	"time"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	fornaxgrpc "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"
	"centaurusinfra.io/fornax-serverless/pkg/message"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/fornaxcore"
	internal "centaurusinfra.io/fornax-serverless/pkg/nodeagent/message"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/pod"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/session"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/types"
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
	nodeMutex       sync.RWMutex
	stopCh          chan struct{}
	node            *FornaxNode
	state           NodeState
	innerActor      message.Actor
	fornoxCoreRef   message.ActorRef
	podActors       *PodActorPool
	nodePortManager *nodePortManager
}

func (n *FornaxNodeActor) Stop() error {
	n.stopCh <- struct{}{}
	n.innerActor.Stop()
	for _, v := range n.podActors.List() {
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
	// register with Fornax core
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
		klog.InfoS("Recover pod actor for a terminated pod", "pod", fpod)
		n.startPodActor(fpod)
	}

	for _, fpod := range runtimeSummary.runningPods {
		klog.InfoS("Recover pod actor for a running pod", "pod", types.UniquePodName(fpod), "state", fpod.FornaxPodState)
		n.nodePortManager.initNodePortRangeSlot(n.node.V1Node, fpod.Pod)
		n.startPodActor(fpod)
	}
}

func (n *FornaxNodeActor) startStateReport() {
	// start go routine to report node status forever
	go wait.Until(func() {
		n.notify(n.innerActor.Reference(), internal.NodeUpdate{})
	}, 1*time.Minute, n.stopCh)
}

func (n *FornaxNodeActor) incrementNodeRevision() int64 {
	n.nodeMutex.Lock()
	defer n.nodeMutex.Unlock()

	n.node.Revision += 1
	n.node.V1Node.ResourceVersion = fmt.Sprint(n.node.Revision)
	n.node.Dependencies.NodeStore.PutNode(
		&types.FornaxNodeWithRevision{
			Identifier: util.ResourceName(n.node.V1Node),
			Node:       n.node.V1Node.DeepCopy(),
			Revision:   n.node.Revision,
		},
	)

	return n.node.Revision
}

func (n *FornaxNodeActor) nodeHandler(msg message.ActorMessage) (interface{}, error) {
	switch msg.Body.(type) {
	case *fornaxgrpc.FornaxCoreMessage:
		return n.processFornaxCoreMessage(msg.Body.(*fornaxgrpc.FornaxCoreMessage))
	case internal.PodStatusChange:
		fppod := msg.Body.(internal.PodStatusChange).Pod
		revision := n.incrementNodeRevision()
		fppod.Pod.ResourceVersion = fmt.Sprint(revision)
		n.notify(n.fornoxCoreRef, pod.BuildFornaxcoreGrpcPodState(revision, fppod))
		if fppod.FornaxPodState == types.PodStateTerminated {
			klog.InfoS("Cleanup pod actor and store", "pod", types.UniquePodName(fppod), "state", fppod.FornaxPodState)
			actor := n.podActors.Get(string(fppod.Identifier))
			if actor != nil {
				actor.Stop()
				n.podActors.Del(string(fppod.Identifier))
				n.node.Pods.Del(fppod.Identifier)
			}
			n.nodePortManager.DeallocatePodPortMapping(n.node.V1Node, fppod.Pod)
			err := n.node.Dependencies.PodStore.DelObject(fppod.Identifier)
			if err != nil {
				// TODO, if failed to delete a terminated pod, it should be fine, but need better cleanup
				return nil, err
			}
		}
	case internal.SessionStatusChange:
		fpsession := msg.Body.(internal.SessionStatusChange).Session
		klog.Warningf("Received session status change from session %v", fpsession.Session.Status)
		revision := n.incrementNodeRevision()
		fpsession.Session.ResourceVersion = fmt.Sprint(revision)
		n.notify(n.fornoxCoreRef, session.BuildFornaxcoreGrpcSessionState(revision, fpsession))
	case internal.NodeUpdate:
		SetNodeStatus(n.node)
		n.notify(n.fornoxCoreRef, BuildFornaxGrpcNodeState(n.node, n.node.Revision))
	default:
		klog.InfoS("Received unknown message", "from", msg.Sender, "msg", msg.Body)
	}

	return nil, nil
}

// TODO, notify Fornax core fatal error
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
	case fornaxgrpc.MessageType_SESSION_OPEN:
		err = n.onSessionOpenCommand(msg.GetSessionOpen())
	case fornaxgrpc.MessageType_SESSION_CLOSE:
		err = n.onSessionCloseCommand(msg.GetSessionClose())
	case fornaxgrpc.MessageType_SESSION_STATE, fornaxgrpc.MessageType_POD_STATE, fornaxgrpc.MessageType_NODE_STATE:
		// messages are sent to fornaxcore, should just forward
		n.notify(n.fornoxCoreRef, msg)
		// messages are not supposed to be received by node
	default:
		klog.Warningf("FornaxCoreMessage of type %s is not handled by actor", msgType)
	}
	// TODO, handle error, define reply message to Fornax core
	return nil, err
}

// initialize node with node spec provided by fornaxcore, especially pod cidr
func (n *FornaxNodeActor) onNodeFullSyncCommand(msg *fornaxgrpc.NodeFullSync) error {
	n.notify(n.fornoxCoreRef, BuildFornaxGrpcNodeState(n.node, n.node.Revision))
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
		if len(n.node.Pods.List()) > 0 {
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
				klog.InfoS("Node is ready, tell Fornax core")
				// bump revision to let Fornax core to update node status
				revision := n.incrementNodeRevision()
				n.node.V1Node.ResourceVersion = fmt.Sprint(revision)
				n.node.V1Node.Status.Phase = v1.NodeRunning
				n.notify(n.fornoxCoreRef, BuildFornaxGrpcNodeReady(n.node, revision))
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

		v := n.node.Pods.Get(util.ResourceName(p))
		if v == nil {
			_, actor, err := n.createPodAndActor(types.PodStateCreating, p.DeepCopy(), nil, true)
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

// buildAFornaxPod validate pod spec, and allocate host port for pod container port, it also set pod lables,
// modified pod spec will saved in store and return back to FornaxCore to make pod spec in sync
func (n *FornaxNodeActor) buildAFornaxPod(state types.PodState,
	v1pod *v1.Pod,
	configMap *v1.ConfigMap,
	isDaemon bool) (*types.FornaxPod, error) {
	errs := pod.ValidatePodSpec(v1pod)
	if len(errs) > 0 {
		return nil, errors.New("Pod spec is invalid")
	}
	fornaxPod := &types.FornaxPod{
		Identifier:              util.ResourceName(v1pod),
		FornaxPodState:          state,
		Daemon:                  isDaemon,
		Pod:                     v1pod.DeepCopy(),
		RuntimePod:              nil,
		Containers:              map[string]*types.FornaxContainer{},
		Sessions:                map[string]*types.FornaxSession{},
		LastStateTransitionTime: time.Now(),
	}
	pod.SetPodStatus(fornaxPod, n.node.V1Node)
	fornaxPod.Pod.Labels[fornaxv1.LabelFornaxCoreNode] = util.ResourceName(n.node.V1Node)

	if configMap != nil {
		errs = pod.ValidateConfigMapSpec(configMap)
		if len(errs) > 0 {
			return nil, errors.New("ConfigMap spec is invalid")
		}
		fornaxPod.ConfigMap = configMap.DeepCopy()
	}

	// if fornax pod need to expose host port for containter port, there are chance port could be conflict between pods,
	// to avoid port conflict on host of multiple pods, node allocate a unique host port number for each container port
	// and overwrite pod spec's container port mapping, modified pod spec is returned back to FornaxCore,
	// FornaxCore use modified port mapping to let its client to access pod using node allocated host port
	err := n.nodePortManager.AllocatePodPortMapping(n.node.V1Node, fornaxPod.Pod)
	if err != nil {
		return nil, err
	}

	// for _, v := range fornaxPod.Pod.Spec.Containers {
	//  klog.Info("Pod Containers port mapping", v.Ports)
	// }
	return fornaxPod, nil
}

// startPodActor start a actor for a pod
// set pod node related information in case node name and ip changed
func (n *FornaxNodeActor) startPodActor(fpod *types.FornaxPod) (*types.FornaxPod, *pod.PodActor, error) {
	fpod.Pod = fpod.Pod.DeepCopy()
	fpod.Pod.Status.HostIP = n.node.V1Node.Status.Addresses[0].Address
	fpod.Pod.Labels[fornaxv1.LabelFornaxCoreNode] = util.ResourceName(n.node.V1Node)

	fpActor := pod.NewPodActor(n.innerActor.Reference(), fpod, &n.node.NodeConfig, n.node.Dependencies, pod.ErrRecoverPod)
	n.node.Pods.Add(fpod.Identifier, fpod)
	n.podActors.Add(fpod.Identifier, fpActor)
	fpActor.Start()
	return fpod, fpActor, nil
}

func (n *FornaxNodeActor) createPodAndActor(
	state types.PodState,
	v1Pod *v1.Pod,
	v1Config *v1.ConfigMap,
	isDaemon bool) (*types.FornaxPod, *pod.PodActor, error) {
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
	return n.startPodActor(fpod)
}

// find pod actor and send a message to it, if pod actor does not exist, create one
func (n *FornaxNodeActor) onPodCreateCommand(msg *fornaxgrpc.PodCreate) error {
	if n.state != NodeStateReady {
		return fmt.Errorf("Node is not in ready state to create a new pod")
	}
	v := n.node.Pods.Get(*msg.PodIdentifier)
	if v == nil {
		pod, actor, err := n.createPodAndActor(
			types.PodStateCreating,
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
	podActor := n.podActors.Get(*msg.PodIdentifier)
	if podActor == nil {
		return fmt.Errorf("Pod: %s does not exist, Fornax core is not in sync", *msg.PodIdentifier)
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
	podActor := n.podActors.Get(*msg.PodIdentifier)
	if podActor == nil {
		return fmt.Errorf("Pod: %s does not exist, Fornax core is not in sync", *msg.PodIdentifier)
	} else {
		n.notify(podActor.Reference(), internal.PodActive{})
	}
	return nil

}

// build a session actor to start session and monitor session state
func (n *FornaxNodeActor) onSessionOpenCommand(msg *fornaxgrpc.SessionOpen) error {
	if n.state != NodeStateReady {
		return fmt.Errorf("node is not in ready state to open a session")
	}
	s := &fornaxv1.ApplicationSession{}
	if err := json.Unmarshal(msg.GetSessionData(), s); err != nil {
		return err
	}
	podActor := n.podActors.Get(msg.GetPodIdentifier())
	if podActor == nil {
		return fmt.Errorf("Pod: %s does not exist, can not open session", msg.GetPodIdentifier())
	} else {
		n.notify(podActor.Reference(), internal.SessionOpen{
			SessionId: msg.GetSessionIdentifier(),
			Session:   s,
		})
	}
	return nil
}

// find session actor to let it terminate a session, if pod actor does not exist, return failure
func (n *FornaxNodeActor) onSessionCloseCommand(msg *fornaxgrpc.SessionClose) error {
	podActor := n.podActors.Get(msg.GetPodIdentifier())
	if podActor == nil {
		return fmt.Errorf("Pod: %s does not exist, Fornax core is not in sync, can not close session", msg.GetPodIdentifier())
	} else {
		n.notify(podActor.Reference(), internal.SessionClose{
			SessionId: msg.GetSessionIdentifier(),
		})
	}
	return nil
}

func (n *FornaxNodeActor) notify(receiver message.ActorRef, msg interface{}) {
	message.Send(n.innerActor.Reference(), receiver, msg)
}

func NewNodeActor(node *FornaxNode) (*FornaxNodeActor, error) {
	actor := &FornaxNodeActor{
		nodeMutex:       sync.RWMutex{},
		stopCh:          make(chan struct{}),
		node:            node,
		state:           NodeStateInitializing,
		innerActor:      nil,
		fornoxCoreRef:   nil,
		podActors:       NewPodActorPool(),
		nodePortManager: NewNodePortManager(),
	}
	actor.innerActor = message.NewLocalChannelActor(node.V1Node.GetName(), actor.nodeHandler)

	klog.Info("Starting Fornax core actor")
	fornaxCoreActor := fornaxcore.NewFornaxCoreActor(node.NodeConfig.NodeIP, util.ResourceName(node.V1Node), node.NodeConfig.FornaxCoreIPs)
	actor.fornoxCoreRef = fornaxCoreActor.Reference()
	err := fornaxCoreActor.Start(actor.innerActor.Reference())
	if err != nil {
		klog.ErrorS(err, "Can not start Fornax core actor")
		return nil, err
	}
	klog.Info("Fornax core actor started")

	return actor, nil
}
