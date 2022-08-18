# Overview
Fornax serverless has two components, Fornax Core and Node Agent.

Fornax Core is a customised k8s api server and resource controller/scheduler, and it run a grpc service listen on 18001 port to accept Node Agent connection.
Only one Fornax Core server is required, but people can always run multiple Fornax Cores for high availability, 

Node Agent is installed on each node which run containers, Node Agent use Containerd and CNI plugin to implement container.

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
make install
```

3. start Fornax core.

```sh
make run-apiserver-local
```

### 1.2.2 From binary

Install latest version from <https://github.com/CentaurusInfra/fornax-serverless/releases>, start it as

```sh
bin/apiserver --etcd-servers=http://localhost:2379 --secure-port=9443 --feature-gates=APIPriorityAndFairness=false

```

# 2. Install Node Agent

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

## 2.5 Verification

### 2.5.1 Install crictl

follow <https://github.com/kubernetes-sigs/cri-tools/blob/master/docs/crictl.md>

### 2.5.2 Check containerd state

```
crictl info
```

it's expected runtime and network is ready in output, like

```json
"status": {
  "conditions": [
  {
    "type": "RuntimeReady",
      "status": true,
      "reason": "",
      "message": ""

  },
  {
    "type": "NetworkReady",
    "status": true,
    "reason": "",
    "message": ""

  }
  ]
},
```

If there is any error, check containerd log,

```sh
journalctl -u containerd -f
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
  make install
  ```

  3. start Node Agent

  ```
  sudo ./bin/nodeagent --fornaxcore-ip localhost:18001 --disable-swap=false
  ```

### 2.6.2 From binary

  Install latest version from <https://github.com/CentaurusInfra/fornax-serverless/releases>, start it as

  ```
  sudo ./bin/nodeagent --fornaxcore-ip localhost:18001 --disable-swap=false

  ```
  Notes: You also can replace localhost with specific ip address (for example: 192.168.0.45)

# 3. Play Fornax serverless

## 3.1 Install Kubectl In The VM Machine
  Install and Set Up kubectl tool on Linux (https://kubernetes.io/docs/tasks/tools/install-kubectl-linux/)
  
## 3.2 Operate Fornax serverless resources

  Fornax serverless expose two resources to client, you can use kubectl to create and explore these resouces

  1. Check api resources

  ```
  [main] # kubectl --kubeconfig ./kubeconfig api-resources
  NAME                  SHORTNAMES   APIVERSION                                    NAMESPACED   KIND
  applications                       core.fornax-serverless.centaurusinfra.io/v1   true         Application
  applicationsessions                core.fornax-serverless.centaurusinfra.io/v1   true         ApplicationSession
  ```

  2. Get applications

  ````
  [main] # kubectl --kubeconfig kubeconfig get applications --all-namespaces
  NAMESPACE   NAME          CREATED AT
  game1       nginx         2022-08-08T18:59:35Z
  game2       nginx-mysql   2022-08-08T19:10:41Z
  ````

## 3.3 Run First Fornax Core serverless application

1. Create application spec yaml file

 ```yaml
cat << EOF | sudo tee ./hack/test-data/nginx-create-app.yaml
apiVersion: core.fornax-serverless.centaurusinfra.io/v1
kind: Application
metadata:
  name: nginx
  labels:
    name: nginx
spec:
  sessionConfig:
    numOfSessionOfInstance: 1
    minSessions: 1
    maxSessions: 10
    maxSurge: 1
    minOfIdleSessions: 0
  scalingPolicy:
    minimumTarget: 1
    maximumTarget: 3
    burst: 1
  containers:
    - image: nginx:latest
      name: nginx
      resources:
        requests:
          memory: "500M"
          cpu: "0.5"
        limits:
          memory: "500M"
          cpu: "0.5"
      ports:
        - containerPort: 80
          name: nginx
  configData:
    config1: data1
EOF
```

2. Create application

```sh
kubectl apply --kubeconfig kubeconfig --namespace game2 application nginx -f ./hack/test-data/nginx-create-app.yaml
```

3. verify pods are created on Node

```sh
crictl pods

```
