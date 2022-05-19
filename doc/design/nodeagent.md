Word to Markdown Converter
Results of converting "NodeAgent"
Markdown
# Node Agent

Node agent provides services to help customers to deploy serverless applications and monitorX applications session status

FornaxCore uses k8s as internal driver force, but provides new apis to expose application resources, k8s apis are not exposed and hidden from customer.

It manages the following Fornax serverless resources on Node agent:

- Application instance

Application instance is essentially a pod, application could have multiple pods work as a service, or work independently, an entry point is exposed to allow customers to access exposed application (application protocol are application specific)

Some Application instances work as a daemon, it processes data provided by other internal or external applications. E.g., Streaming data processing.

- Session

An application session is created and returned to customers when customers want to use an application, an instance is chosen to serve this session or created to serve this session if there are no idle application instances

It also provides method to monitor

1/ application resource usage

2/ application logs

3/ application metrics

Since customers will not get k8s access, this information needs to be collected and stored to allow customers to access it.

# Node agent components

![](RackMultipart20220519-1-a6n0i1_html_189e55a41b96a84e.png)

When NodeAgent started it connect to FornaxCore and start to exchange messages encoded using protobuf. FornaxCore is implemented as a gRPC service, it provides two apis SendMessage and GetMessage.

NodeAgent use SendMessage to notify FornaxCore and use GetMessage to receive response or any other command request. FornaxCore implements SendMessage as a unary async api, and FornaxCore implements GetMessage as a streaming API to allow NodeAgent to receive response message or other command message from FornaxCore asynchronously.

when NodeAgent firstly connects to FornaxCore, it needs to start a registration process to sync its data with FornaxCore, also FornaxCore send back NodeConfiguration back to let NodeAgent to finish necessary initialization. Then NodeAgent notify FornaxCore it&#39;s ready for workload.

Node agent is deployed on each node as a daemon, In each Node Agent, there is one Node Actor is created to handle node resource messages and node registration with FornaxCore, it also maintain heartbeat between NodeAgent and FornaxCore.

And multiple Pod Actors and Session Actors according to how application instances and sessions are created on this node, these Actors will handle their own pod and session messages.

## FornaxCore to Node messages

These messages are mostly command message to request Node to execute

- FornaxCoreConfiguration
  - Primary FornaxCore
  - List of standby FornaxCore
- NodeConfiguration
  - Node spec
- PodCreate
  - Pod spec
  - Config map spec
  - Secret spec
- PodTerminate
  - Pod name
- PodActive
  - Pod name
- SessionStart
  - Session spec
  - Pod name
- SessionClose
  - Session name

## Node to FornaxCore messages

are mostly message about Node, Pod and Session State.

- NodeRegister
  - Node Ip
  - Node Resources
- NodeReady
  - Node Ip
  - Node Resources
- NodeState
  - Node name
  - Node Resources
  - Running Pods
  - Standby Pods
  - Running Sessions
- PodState
  - Pod name
  - Pod state, see pod state lifecycle management
- SessionState
  - Session name
  - Session state, see session lifecycle management

## FornaxCore Node Agent interaction diagram

Check in [ForanxCore-NodeAgent-Container-interact-uml.png](https://efutureway.sharepoint.com/:i:/s/CloudFabric-All/ERbX0NwjBJFNhtieJaGQkuUBNLkZsvrgPavZZJFuGB6pBg?e=4a76hS)

### FornaxCore cluster support

System operator can setup a cluster of FornaxCore to manage Node Agents, this cluster could be in active/standby mode or partitioned active/active mode.

Operator provision NodeAgent using a list of seed FornaxCore, NodeAgent setup connection to these seeds, and send NodeRegister message to all of them.

If none of these seeds are primary FornaxCore of this node, FornaxCore should sent back a new FornaxCoreConfiguration including list of FornaxCore Ips and primary owner of this node. NodeAgent reset its connection to primary one and start register again.

Whenever FornaxCore cluster change a node ownership, it will notify node about new configuration, this information could also be included as an in-bind data in other command messages sent to NodeAgent.

## Legacy Kubelet http server

Kubelet provides some apis to allow attach/exec command on pod and collect pod logs and metrics. These management apis are important to allow operators to monitor their instances.

## Node Actor

When Node Actor started, it&#39;s responsible for setup connection with FornaxCore and registers itself.

Node Actor act as entry point to receive command and configuration messages from FornaxCore, it keeps connection alive and setup new connection when FornaxCore change Node ownership.

When Node Actor start, it report persisted pod and session state to FornaxCore, also work as proxy of state/message change for pod/sessions.

## Pod Actors

Pod Actors work as a state machine to act on each pod or session.

Whenever pod or session state changes, it uses node actor to send messages back to FornaxCore.

Created for each app pod, it talks with container runtime

1 create app pod

2 active standby app pod

3 pod liveness and readiness probe

4 react to pod lifecycle event, pod start/stop

### Pod lifecycle management

Application instances use pod and container to host customer application, a Pod Actor work as state machine, it has different behaviors in different state, state transit when pod actor receives Fornax core command messages or receive internal lifecycle events.

```
@startuml
Creating: CRI Create pod container 
LivenessChecking : pod container exists
Standby : liveness check
Activating: listen application session
Running : monitor session heartbeat
Evacuating: close session on pod
Terminating: CRI Remove pod container
Terminated: delete pod table item

[*] --> Creating
Creating --> LivenessChecking
LivenessChecking --> Standby: succeed
LivenessChecking --> Terminating: check timeout
Standby --> Terminating: delete pod
Standby --> Activating: active a session
Activating --> Running: session ready callback
Activating --> Evacuating: delete pod
Activating --> Terminating: ready callback timeout
Running --> Evacuating: delete pod
Evacuating --> Terminating: session closed callback
Terminating --> Terminated
Terminated --> [*]
@enduml
```

## Session Actors

Session actor with with Pod Actor to active a session on a pod and talk with application using Session SDK to probe session state, also maintain client session associations, same as pod actor, a session actor is created for each session, and it works in a state machine mode also.

When application session is setup, application container is supposed to send heartbeat message back continuously, if heartbeat is not received, session is flagged in as state messaged sent back to FornaxCore, it&#39;s up to FornaxCore to determine if it want to close this session.

### Application session lifecycle management

Application sessions go through the states below from customer begin a session until session is ended.

```
@startuml
Starting: create session table item
ReadinessChecking : session sdk on session create
Ready: receive session heartbeat
Live: receive session heartbeat
Evacuating: session sdk on session delete
Terminating: receive session heartbeat
Terminated: delete session table item

[*] --> Starting
Starting --> ReadinessChecking: wait for session sdk
ReadinessChecking --> Ready: session ready callback
ReadinessChecking --> Terminated: timeout
Ready --> Terminating: delete session
Ready --> Live: client session join
Live --> Evacuating: delete session
Live --> Terminating: session closed
Evacuating --> Terminating: client session left
Terminating --> Terminated: session gone
Terminated --> [*]
@enduml

```

A Session service is provided as a local gRPC service, Session Service provide similar api as FornaxCore gRPC service to send and receive messages from application, a sdk is provided to allow aplication to connect to Session Service when application container is initialized and started, container sends session event and heartbeat message back to Session Service, also Session Service send command message to application to setup or tear down some internal states.

### Session command message

message is sent to server when Customer call StartApplicationSession and CloseApplicationSession.

- SessionStart
- SessionClose

### Session callback message

Node agents provide a few apis to allow application call when session is in a state, e.g. session is initialized inside application instance, or session is clear.

- SessionReady
- SessionEvacuated
- SessionClosed

Node agents create a certificate on-fly for each application instance when instance is created, application instance uses this certificate to talk to node agent and authorize only issued certificate can only change associated sessions with this instance.

### Session configuration

Customer can provide some customized data in session and application spec to allow instance and Session use this data to initialize, node agent create an application instance and sessions metadata file for each instance on this node, and mount it as a read-only file inside container, node agent will write this file whenever some runtime configuration changed of an application instance.

## Node reconciler

To let node agent to recover from crash or message loss, Node Agent saves all pod and session information in its own SQLite db. meantime a Node conciliar use this saved information to check with Containerd and Session Service to make sure pod and session are implemented or cleaned properly.

This information is also reported back to FornaxCore as Application Instance and Session authority.

Except node startup or recovery, Node reconciler is also doing some housekeeping work to

## CRI Proxy

Fornax serverless plan to onboard on quark container runtime, one difference with standard runc runtime is.
squark container will provide a standby/active mode to run a container, a standby container is not fully bootstraped and paused on entry point,  consume lower CPU and memory, standby containers can be activated when it's needed to put into service quickly.

### onboard Quark runtime

Containerd define a [default oci runtime](https://github.com/containerd/containerd/blob/main/pkg/cri/config/config_unix.go#L76) to use runc, to use quark standby and resume feature, here are some changes may need

### Standby container

Still use standard CRI Create Container and Start Container apis, a container spec is created by containerd to call runtime Create and Start method, in container spec, a path through annotation is provided to let

process pause on provided memory threshold, this memory threshold is put into [container spec](https://github.com/opencontainers/runtime-spec/blob/main/specs-go/config.go#L20) as a [oci annotation which passthrough from cri container config](https://github.com/containerd/containerd/blob/bb8b134a17974a49c448e3368bd75954810cd86d/pkg/cri/server/container_create_linux.go#L280), or [cri pod config](../containerd/container_create_linux.go%20at%20bb8b134a17974a49c448e3368bd75954810cd86d%20%C2%B7%20containerd/containerd%20(github.com)).

[k8s pod config is easy place to put this customized config since it&#39;s just a copy from pod spec](https://github.com/kubernetes/kubernetes/blob/master/pkg/kubelet/kuberuntime/kuberuntime_sandbox.go#L90)

[cri container config is selected attributes only](https://github.com/kubernetes/kubernetes/blob/537941765fe1304dd096c1a2d4d4e70f10768218/pkg/kubelet/kuberuntime/labels.go#L107),

The node agent only needs to put an annotation into pod spec, and quark runtime will check this annotation to standby or fully active.

### Active container

Container runtime method does not have active syntax, there are two methods which can be repurposed for a hack,

Option 1, pause and resume

use containerd PauseTaskRequest and ResumeTaskRequest,

Option 2, kill using sig\_user1/2 signal
repurpose containerd KillRequest to send user sinal

[Here is containerd task service definition](https://github.com/containerd/containerd/blob/main/api/services/tasks/v1/tasks.proto#L31)

add a new cri stopContainer implementation to use KILL

[Default cri stopContainer implementation](https://github.com/containerd/containerd/blob/main/pkg/cri/server/container_stop.go#L112) use SIG\_TERM or SIG\_KILL to stop a container.


## NodeAgentDB

NodeAgent persist Pod and Session information in its own SQLite db, hence two tables are created.

Pod Table

| Pod name | Namespace/name, unique id |
| --- | --- |
| Pod spec | K8s spec |
| Pod ip | Created by CNI |
| Pod state | Pod state lifecycle management |
| Container id | Containerd id |
| Config spec | K8s spec |
| Secret spec | K8s spec |
|


Session Table

| Session name | Namespace/name, unique id |
| --- | --- |
| Session spec | Fornax spec |
| Pod name | Namespace/name, unique id |
| Session state | Session lifecycle management |

Container Table

| Container id | Containerd id |
| --- | --- |
| Pod name | Namespace/name, unique id |