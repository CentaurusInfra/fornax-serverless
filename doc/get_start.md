Fornax serverless has two components, Fornax Core and Node Agent.

Fornax Core is a customised k8s api server and resource controller/scheduler, and it run a grpc service listen on 18001 port to accept Node Agent connection.
Only one Fornax Core server is required, but people can always run multiple Fornax Cores for high availability, 

Node Agent is installed on each node which run containers, Node Agent use Containerd and CNI plugin to implement container.

# Install Fornax Core

## Install Etcd
Fornax Core use etcd as data store, follow <https://etcd.io/docs/v3.4/install/> to install Etcd

## Install Fornax Core

### From source code

1. Install go

Fornax serverless require go 1.8+, install golang from <https://go.dev/doc/installed>

2. Compile source code

Checkout code from <https://github.com/CentaurusInfra/fornax-serverless> into go workspace, from project folder, execute

```sh
make install
```

3. start Fornax core.

```sh
make run-apiserver-local
```

### From binary

Install latest version from <https://github.com/CentaurusInfra/fornax-serverless/releases>, start it as

```sh
bin/apiserver --etcd-servers=http://localhost:2379 --secure-port=9443 --feature-gates=APIPriorityAndFairness=false

```

# Install Node Agent

## Install containerd/cni/runc

follow <https://github.com/containerd/containerd/blob/main/docs/getting-started.md>

## Enable containerd CRI plugin

edit /etc/containerd/config.toml, enable cri plugin

```
#disabled_plugins = ["cri"]
```

## Add CNI config

```json
cat << EOF | tee /etc/cni/net.d/10-containerd-net.conflist
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

## Restart containerd

```sh
sudo systemctl restart containerd
```

## Verification

### Install crictl

follow <https://github.com/kubernetes-sigs/cri-tools/blob/master/docs/crictl.md>

### check containerd state

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

## Install Fornax Node Agent

  You can install node agent in same host as FornaxCore, currently Node agent does not support Windows.
  You need to provide Fornax Core server ip address and port to let node agent know to which FornaxCore server to connect.
  If you have multiple Fornax Core server, provide them in a list

### From source code

  1. Install go
  
  Fornax serverless require go 1.8+, install golang from <https://go.dev/doc/installed>
  
  2. Compile source code
  
  Checkout code from <https://github.com/CentaurusInfra/fornax-serverless> into go workspace, from project folder, execute

  ```
  make install
  ```

  3. start Node Agent

  ```
  sudo ./bin/nodeagent --fornaxcore-ip 192.168.0.45:18001 --disable-swap=false
  ```

### From binary

  Install latest version from <https://github.com/CentaurusInfra/fornax-serverless/releases>, start it as

  ```
  sudo ./bin/nodeagent --fornaxcore-ip 192.168.0.45:18001 --disable-swap=false

  ```

# Play Fornax serverless

## Operate Fornax serverless resources

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

## Run First Fornax Core serverless application

1. Create application spec yaml file

 ```yaml
cat << EOF | tee ./hack/test-data/nginx-create-app.yaml
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
