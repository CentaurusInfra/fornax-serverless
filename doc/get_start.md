# Overview
Fornax serverless has two components, Fornax Core and Node Agent.

Fornax Core is a customised k8s api server and resource controller/scheduler, and it run a grpc service listen on 18001 port to accept Node Agent connection.
Only one Fornax Core server is required, but people can always run multiple Fornax Cores for high availability, 

Node Agent is installed on each node which run containers, Node Agent use Containerd and CNI plugin to implement container.

If you are not familiar these third party software component when you install, you can reference [Setting Up Detail](https://github.com/CentaurusInfra/fornax-serverless/blob/main/doc/fornax_setup.md).

# 1. Install Fornax Core

## 1.1 Install Etcd
Fornax Core use etcd as data store, follow <https://etcd.io/docs/v3.4/install/> to install Etcd

## 1.2 Install Fornax Core

### 1.2.1 From source code

1. Install go

Fornax serverless require go 1.8+, install golang from <https://go.dev/doc/install>

2. Compile source code

Checkout code from <https://github.com/CentaurusInfra/fornax-serverless> into go workspace, from project folder, execute

```sh
make
```

### 1.2.2 From binary

Install latest version from <https://github.com/CentaurusInfra/fornax-serverless/releases>, start it as

# 2. Install Node Agent
Node Agent is installed on every host which run containers, it need Containerd and CNI as depenency, install and configure hosts accordingly

## 2.1 Install containerd/cni/runc

follow <https://github.com/containerd/containerd/blob/main/docs/getting-started.md>

## 2.2 Enable containerd CRI plugin

edit /etc/containerd/config.toml, enable cri plugin

```
#disabled_plugins = ["cri"]
```

## 2.3 Add CNI config

```json
cat << EOF | sudo tee /etc/cni/net.d/10-containerd-net.conflist
{
  "cniVersion": "0.4.0",
    "name": "containerd-net",
    "plugins": [
    {
      "type": "bridge",
      "bridge": "cni0",
      "isGateway": true,
      "ipMasq": true,
      "promiscMode": true,
      "ipam": {
        "type": "host-local",
        "ranges": [
          [{
            "subnet": "10.22.0.0/16"

          }]

        ],
        "routes": [
        { "dst": "0.0.0.0/0" }

        ]
      }
    },
    {
      "type": "portmap",
      "capabilities": {"portMappings": true}
    }
  ]
}
EOF
```

## 2.4 Restart containerd

```sh
sudo systemctl restart containerd
```

## 2.6 Install Fornax Node Agent

  You can install node agent in same host as FornaxCore, currently Node agent does not support Windows.
  You need to provide Fornax Core server ip address and port to let node agent know to which FornaxCore server to connect.
  If you have multiple Fornax Core server, provide them in a list

### 2.6.1 From source code

  1. Install go
  
  Fornax serverless require go 1.8+, install golang from <https://go.dev/doc/install>
  
  2. Compile source code
  
  Checkout code from <https://github.com/CentaurusInfra/fornax-serverless> into go workspace, from project folder, execute

  ```
  make
  ```

### 2.6.2 From binary

  Install latest version from <https://github.com/CentaurusInfra/fornax-serverless/releases>, 

# 3. Play Fornax serverless

## 3.1 Install Kubectl In The VM Machine
  Install and Set Up kubectl tool on Linux (https://kubernetes.io/docs/tasks/tools/install-kubectl-linux/)
  
## 3.2 Start Fornax Core Server And Node Agent
From you install directory or project path
  1. Fornax Core API-Server.
  ```sh
  ./bin/fornaxcore --etcd-servers=http://127.0.0.1:2379 --secure-port=9443 --standalone-debug-mode --bind-address=127.0.0.1
  ```
  
  2. Run Node Agent on host which run pods, Node agent require root permission
  ```sh
  sudo ./bin/nodeagent --fornaxcore-url 127.0.0.1:18001 --disable-swap=false
  ```
  Notes: You should replace 127.0.0.1 with correct fornax core host ip address if fornaxcore is not running on same host

## 3.3 Create First Fornax Core serverless application and session

1. Create application

 ```yaml
cat << EOF | sudo tee ./nodejs-hw-app-create.yaml
apiVersion: core.fornax-serverless.centaurusinfra.io/v1
kind: Application
metadata:
  name: nodejs-hw
  labels:
    name: nodejs-hw
spec:
  usingNodeSessionService: false
  scalingPolicy:
    minimumInstance: 1
    maximumInstance: 30
    burst: 1
    scalingPolicyType: idle_session_number
    idleSessionNumThreshold:
      high: 3
      low: 0
  containers:
    - image: centaurusinfra.io/fornax-serverless/nodejs-hw:v0.1.0
      name: nodejs-hw
      resources:
        requests:
          memory: "500M"
          cpu: "0.5"
        limits:
          memory: "500M"
          cpu: "0.5"
      ports:
        - containerPort: 8080
          name: nodejs
EOF
```

```sh
kubectl apply --kubeconfig kubeconfig -f ./nodejs-hw-app-create.yaml --namespace game1
```

create application session using created yaml file

 ```yaml
cat << EOF | sudo tee ./nodejs-hw-session-create.yaml
apiVersion: core.fornax-serverless.centaurusinfra.io/v1
kind: ApplicationSession
metadata:
  name: nodejs-hw-session-2
  labels:
    application: nodejs-hw
spec:
  applicationName: nodejs-hw
  sessionData: my-nginx1-session-data
  openTimeoutSeconds: 30
  closeGracePeriodSeconds: 30
  killInstanceWhenSessionClosed: true
EOF
```

```sh
kubectl apply --kubeconfig kubeconfig --validate=false  -f ./nodejs-hw-session-create.yaml --namespace game1
```
3. Describe session and find session ingress endpoint
```sh
kubectl get applicationsessions --kubeconfig kubeconfig --namespace game1 -o yaml
```
you should get below output
```yaml
apiVersion: core.fornax-serverless.centaurusinfra.io/v1
kind: ApplicationSession
metadata:
  annotations:
    kubectl.kubernetes.io/last-applied-configuration: |
      {"apiVersion":"core.fornax-serverless.centaurusinfra.io/v1","kind":"ApplicationSession","metadata":{"annotations":{},"labels":{"application":"nodejs-hw"},"name":"nodejs-hw-session-2","namespace":"game1"},"spec":{"applicationName":"nodejs-hw","closeGracePeriodSeconds":30,"killInstanceWhenSessionClosed":false,"openTimeoutSeconds":30,"sessionData":"my-nginx1-session-data"}}
  creationTimestamp: "2022-12-10T00:44:51Z"
  finalizers:
  - opensession.core.fornax-serverless.centaurusinfra.io
  labels:
    application: nodejs-hw
  name: nodejs-hw-session-2
  namespace: game1
  resourceVersion: "6282319024943921651"
  uid: 13e26131-22fd-454e-909d-dad17e4c17a7
spec:
  applicationName: nodejs-hw
  closeGracePeriodSeconds: 30
  openTimeoutSeconds: 30
  sessionData: my-nginx1-session-data
status:
  accessEndPoints:
  - ipAddress: 192.168.0.71
    port: 1024
  availableTime: "2022-12-10T00:44:51Z"
  availableTimeMicro: 1670633091906489
  podReference:
    name: game1/nodejs-hw-f2zjd4lrb228j7ps-6930
  sessionStatus: Available
```
4. Verify session is accessable using access point
```sh
curl http://192.168.0.71:1024
```
so on, create new session if we need more application instances

## 4 Explore Fornax serverless resources

  Fornax serverless expose two resources to client, you can use kubectl to create and explore these resouces

  1. Check api resources

  ```
  [main] # kubectl --kubeconfig ./kubeconfig api-resources
  NAME                  SHORTNAMES   APIVERSION                                    NAMESPACED   KIND
  applications                       core.fornax-serverless.centaurusinfra.io/v1   true         Application
  applicationsessions                core.fornax-serverless.centaurusinfra.io/v1   true         ApplicationSession
  nodes                              k8s.io/v1                                     true         Node
  pods                               k8s.io/v1                                     true         Pod
  ```

  2. Get applications

  ````
  [main] # kubectl --kubeconfig kubeconfig get applications -A
  NAMESPACE   NAME          CREATED AT
  game1       nginx         2022-08-08T18:59:35Z
  game2       nginx-mysql   2022-08-08T19:10:41Z
  ````

## 5. Use Quark as container runtime
Fornax node agent use runc as default container runtime, if you use Quark container, follow [Quark installation](https://github.com/CentaurusInfra/fornax-serverless/blob/main/doc/containerd/quark_install.md) to install and setup Quark

restart Node Agent using 
```script
sudo ./bin/nodeagent --fornaxcore-url 127.0.0.1:18001 --disable-swap=false --runtime-handler=quark
```
