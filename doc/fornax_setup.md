# Fornax Core and Nodeagent Setting Up Detail

## Overview
This doc is the detail steps which setting up, configuration and install component for Fornax Serverless Machine.

## Machine Prepare
If you have a brand new machine, you may need to install following component to start the machine. (Following is detail steps for AWS EC2 virtual machine).

```script
sudo apt-get update
sudo apt install build-essential
sudo snap install curl  # version 7.81.0
sudo apt-get install vim
sudo apt install git
```

## 1. Install and Setup Fornax Core
### 1.1 Install Etcd 
Install etcd by using apt-get
```script
sudo apt-get update
sudo apt-get -y install etcd
```

Verify etcd
```script
sudo systemctl status etcd.service
sudo systemctl enable etcd
sudo systemctl restart etcd
```

### 1.2 Install Fornax Core From Source Code
#### 1.2.1 Install Golang
In your home directory, do following command
```script
mkdir -p go
cd go
GOLANG_VERSION=${GOLANG_VERSION:-"1.18"}
sudo apt -y update
sudo apt -y install make
sudo apt -y install gcc
sudo apt -y install jq
wget https://dl.google.com/go/go${GOLANG_VERSION}.linux-amd64.tar.gz -P /tmp
sudo tar -C /usr/local -xzf /tmp/go${GOLANG_VERSION}.linux-amd64.tar.gz
```
veify go version
```script
export PATH=$PATH:/usr/local/go/bin
go version
```
the result should be: go version go1.18 linux/amd64

make following directory (under "go" directory)
```script
mkdir -p bin src pkg
```

Add following setting to machine ~/.bashrc (notes: ubuntu is yourself user login directory, remember to replace your's)
```script
sudo vi ~/.bashrc
```

```script
export GOPATH="/home/ubuntu/go"
export GOROOT="/usr/local/go"
export GOBIN="/home/ubuntu/go/bin"
export PATH="$PATH:/usr/local/go/bin:/home/ubuntu/go/bin"
```
Take effetive
```script
source ~/.bashrc
```

#### 1.2.2 Download and Compile Source Code
Create working folder, download code, and go working folder (fornax-serverles)
```script
cd src
mkdir -p centaurusinfra.io
cd centaurusinfra.io
```
In centaurusinfra.io folder and run follwing command
```script
git clone https://github.com/CentaurusInfra/fornax-serverless.git
cd fornax-serverless
```
You are in your workplace folder: fornax-serverless

```script
make all
```
Now, you can back to [get_start.md](https://github.com/CentaurusInfra/fornax-serverless/edit/main/doc/get_start.md) and continue next step.



## 2. Install and Setup Fornax Node Agent
### 2.1 Install containerd/cni/runc
#### 2.1.1 Install containerd
Run following command
```script
sudo curl -fsSL https://get.docker.com -o get-docker.sh
sudo sh get-docker.sh
```

Verify containerd containerd service is runing
```script
sudo systemctl restart containerd
sudo systemctl status containerd
```
Notes: Fornax-serverless using containerd.service. So you need stop docker.service.
```script
sudo systemctl status docker
sudo systemctl stop docker
sudo systemctl status docker
```

#### 2.1.2 Install CNI 
Run following command
```script
sudo mkdir -p /opt/cni/bin
VERSION="v1.1.1"
sudo wget https://github.com/containernetworking/plugins/releases/download/$VERSION/cni-plugins-linux-amd64-$VERSION.tgz
sudo tar zxvf cni-plugins-linux-amd64-$VERSION.tgz -C /opt/cni/bin
sudo rm -f cni-plugins-linux-amd64-$VERSION.tgz
```

#### 2.1.3 Install runc
```sh
sudo apt-get update
sudo apt-get install runc
```
or specific verion
```script
VERSION="v1.1.3"
sudo wget https://github.com/opencontainers/runc/releases/download/$VERSION/runc.amd64 -P /usr/local/bin
install -m 755 /usr/local/bin/runc.amd64 /usr/local/bin/runc
```

### 2.2 Enable Containerd CRI Plugins

Find containerd config.toml file
```script
dpkg -L containerd.io | grep toml
```
You can using vi or other tool to open the file
```script
sudo chmod 777 /etc/containerd/config.toml
sudo vi /etc/containerd/config.toml
```

Comment line "disable_plugins = ["cri"]", see screen shot. add # before "disable_plugins"

Save and exit file.

### 2.3 Add CNI config
If there are no 10-containerd-net.conflist file exits, you need create one.
```script
sudo mkdir -p /etc/cni/net.d
sudo touch /etc/cni/net.d/10-containerd-net.conflist
sudo chmod 777 /etc/cni/net.d/10-containerd-net.conflist
```
You can back to [get_start.md](https://github.com/CentaurusInfra/fornax-serverless/edit/main/doc/get_start.md) and continue Add CNI config step.

### 2.4 Restart containerd and check containerd status
```sh
sudo systemctl restart containerd
sudo systemctl status containerd
```

### 2.5 Verification
#### 2.5.1 Install crictl and update endpoints
1. By using wget install
```script
VERSION="v1.24.1"
wget https://github.com/kubernetes-sigs/cri-tools/releases/download/$VERSION/crictl-$VERSION-linux-amd64.tar.gz
sudo tar zxvf crictl-$VERSION-linux-amd64.tar.gz -C /usr/local/bin
rm -f crictl-$VERSION-linux-amd64.tar.gz
```
2. update default endpoints
if you did not have /etc/crictl.yaml  add this file and copy following line to the file. 

```script
sudo vi /etc/crictl.yaml
```

copy following content to crictl.yaml:
```script
runtime-endpoint: unix:///run/containerd/containerd.sock
image-endpoint: unix:///run/containerd/containerd.sock
timeout: 2
debug: true
pull-image-on-create: false
```

notes: Note: The default endpoints are now deprecated and the runtime endpoint should always be set instead.

#### 2.5.2 Check Containerd State
```script
sudo crictl info
sudo systemctl status containerd
```
If there are any error, run and see log
```sh
sudo journalctl -fu containerd
```


### 2.6 Install Fornax Node Agent 

#### 2.6.1 Install Golang (See 1.2.1)
#### 2.6.2 Compile Source Code (See 1.2.2)
#### 2.6.2 Start Node Agent (See [get_start.md](https://github.com/CentaurusInfra/fornax-serverless/edit/main/doc/get_start.md)


## 3. Play Fornax Serverless
### 3.1 Install Kubectl In The VM Machine
1. Update the apt package index and install packages needed to use the Kubernetes apt repository:
```script
sudo apt-get update
sudo apt-get install -y ca-certificates curl
sudo apt-get install -y apt-transport-https
```
2. Download the Google Cloud public signing key:
```script
sudo curl -fsSLo /usr/share/keyrings/kubernetes-archive-keyring.gpg https://packages.cloud.google.com/apt/doc/apt-key.gpg
```
3. Add the Kubernetes apt repository:
```script
echo "deb [signed-by=/usr/share/keyrings/kubernetes-archive-keyring.gpg] https://apt.kubernetes.io/ kubernetes-xenial main" | sudo tee /etc/apt/sources.list.d/kubernetes.list
```
4. Update apt package index with the new repository and install kubectl:
```script
sudo apt-get update
sudo apt-get install -y kubectl
sudo apt-mark hold kubectl
```
5. Verify kubectl version you installed (Test to ensure the version you installed is up-to-date)
```script
kubectl version --client
```
or
```script
kubectl version --client --output=yaml
```

### 3.2 Start Fornax Core API-Server And Node Agent
1. Start API-Server
```sh
./bin/fornaxcore --etcd-servers=http://127.0.0.1:2379 --secure-port=9443 --standalone-debug-mode --bind-address=127.0.0.1
```
or
```script
make run-fornaxcore-local
```

2. Start Node Agent
```script
sudo ./bin/nodeagent --fornaxcore-url 127.0.0.1:18001 --disable-swap=false
```
3. Notes: Based on your server machine, you maybe need update localhost to specific ip (for exmaple: 192.168.0.45:18001)

### 3.3 Operate Fornax serverless resources
See [get_start.md](https://github.com/CentaurusInfra/fornax-serverless/edit/main/doc/get_start.md)
### 3.4 Run First Fornax Core serverless application
See [get_start.md](https://github.com/CentaurusInfra/fornax-serverless/edit/main/doc/get_start.md)

1. See [get_start.md](https://github.com/CentaurusInfra/fornax-serverless/edit/main/doc/get_start.md)

2. Create First Application.
```sh
kubectl --kubeconfig kubeconfig proxy
```
First time use zsh, install zsh
```sh
sudo apt install zsh
```
```sh
zsh
source ./hack/fornax_curl.zshrc
```
```sh
post_app  game1  ./hack/test-data/sessionwrapper-echoserver-app-create.yaml
kubectl --kubeconfig kubeconfig get applications --all-namespaces
```

3. Create First ApplicationSession
```sh
post_session game1  ./hack/test-data/sessionwrapper-echoserver-session-create.yaml
kubectl --kubeconfig kubeconfig get applicationsession --all-namespaces
kubectl get applicationsessions --kubeconfig kubeconfig --namespace game1 -o yaml
```
If you want delete Application Session, you can use following command
```sh
kubectl delete applicationsession --kubeconfig kubeconfig --namespace game1 nginx-session2
kubectl --kubeconfig kubeconfig delete applicationsession --namespace game1 nginx-session2
```
```sh
kubectl delete application --kubeconfig kubeconfig --namespace game1 echoserver1
```

4. Verify session is accessable using access point
```sh
sudo nc -zv 10.218.233.95 1024
```
## 4. Reference and Help
1. If you want run git pull or some other git command, but you only see this error message:
   - error: cannot open .git/FETCH_HEAD: Permission denied
   - you can run following command to solve this issues:
   ```sh
   sudo chown -R $USER: .
   ```
2. If you run "make" and have a issues, you can change go/pkg folder permission to a+x or 777
```sh
sudo chmod a+x go/pkg
```
3. If you use Quark container, you also need add content to /etc/containerd/config.toml file
```
version = 2
[plugins."io.containerd.runtime.v1.linux"]
  shim_debug = true
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runc]
  runtime_type = "io.containerd.runc.v2"
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runsc]
  runtime_type = "io.containerd.runsc.v1"
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.quark]
  runtime_type = "io.containerd.quark.v1"
```

4. Test command
```sh
./bin/fornaxtest --test-case session_full_cycle --num-of-session-per-app 1 --num-of-init-pod-per-app 0 --burst-of-app-pods 10 --run-once
```
