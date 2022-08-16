# Fornax Core and Nodeagent Setting Up Detail

## Overview
This doc is the detail steps which setting up, configuration and install component for Fornax Serverless Machine.

## Machine Prepare
If you have a brand new machine, your should install following component to start the machine. (Following is detail steps for AWS EC2 virtual machine).

```script
sudo apt-get update
sudo apt install build-essential
sudo snap install curl  # version 7.81.0
sudo apt-get install vim
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

Stop etcd.service. (note: we use the local etcd by default fornax serverless, so we need stop etcd.service)
```script
sudo systemctl stop etcd.service
```

### 1.2 Install Fornax Core From Source Code
#### 1.2.1 Install Golang
```script
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

make following directory (under yourself home directory)
```script
sudo mkdir -p go/bin
sudo mkdir -p go/src
```

Add following setting to machine ~/.bashrc (notes: ubuntu is yourself user login directory, remember to replace your's)
```script
sudo vi ~/.bashrc
```

```sh
export GOPATH="/home/ubuntu/go"
export GOROOT="/usr/local/go"
export GOBIN="/home/ubuntu/go/bin"
export PATH="$PATH:/usr/local/go/bin:/home/ubuntu/go/bin"
```
Take effetive
```script
sudo source ~/.bashrc
```

#### 1.2.2 Download and Compile Source Code
Create working folder, download code, and go working folder (fornax-serverles)
```script
sudo mkdir -p src/centaurusinfra.io
cd src/centaurusinfra.io
```
In centaurusinfra.io folder and run follwing command
```script
git clone https://github.com/CentaurusInfra/fornax-serverless.git
cd fornax-serverless
```
You are in your workplace folder: fornax-serverless

```script
sudo make all
```
Now, you can back to get_start.md ![get_start.md](get_start.md) and continue next step.



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

#### 2.1.2 Enable Containerd CRI Plugins

Find containerd config.toml file
```script
dpkg -L containerd.io | grep toml
```
Using vi to open the file
```script
sudo chmod 777 /etc/containerd/config.toml
sudo vi /etc/containerd/config.toml
```

Comment line "disable_plugins = ["cri"]", see screen shot. add # before "disable_plugins"

Save and exit file.

#### 2.1.3 Install CNI 
Run following command
```script
sudo mkdir -p /opt/cni/bin
VERSION="v1.1.1"
sudo wget https://github.com/containernetworking/plugins/releases/download/$VERSION/cni-plugins-linux-amd64-$VERSION.tgz
sudo tar zxvf cni-plugins-linux-amd64-$VERSION.tgz -C /opt/cni/bin
sudo rm -f cni-plugins-linux-amd64-$VERSION.tgz
```

#### 2.1.4 Install runc
```script
VERSION="v1.1.3"
sudo wget https://github.com/opencontainers/runc/releases/download/$VERSION/runc.amd64 -P /usr/local/bin
```

#### 2.1.5 Add CNI config
If there are no 10-containerd-net.conflist file exits, you need create one.
```script
sudo mkdir -p /etc/cni/net.d
sudo vi /etc/cni/net.d/10-containerd-net.conflist
sudo chmod 777 /etc/cni/net.d/10-containerd-net.conflist
```
You can back to get_start.md ![get_start.md](get_start.md) and continue Add CNI config step.


## 3. Verification
### 3.1 Install crictl
### 3.1.1 By using wget install
```script
VERSION="v1.24.1"
wget https://github.com/kubernetes-sigs/cri-tools/releases/download/$VERSION/crictl-$VERSION-linux-amd64.tar.gz
sudo tar zxvf crictl-$VERSION-linux-amd64.tar.gz -C /usr/local/bin
rm -f crictl-$VERSION-linux-amd64.tar.gz
```
### 3.1.2 update default endpoints
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

### 3.2 Check Containerd State
```script
crictl info
sudo systemctl status containerd
```

### 3.3 Install Fornax Node Agent 

#### 3.3.1 Install Golang (See 1.2.1)
#### 3.3.2 Compile Source Code (See 1.2.2)
#### 3.3.2 Start Node Agent (See ![get_start.md](get_start.md))


## 4. Play Fornax Serverless
### 4.1 Install Kubectl In The VM Machine
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

### 4.2 Start Fornax Core API-Server And Node Agent
1. Start API-Server
```script
make run-apiserver-local
```

2. Start Node Agent
```script
sudo ./bin/nodeagent --fornaxcore-ip localhost:18001 --disable-swap=false
```
3. Notes: Based on your server machine, you maybe need update localhost to specific ip (for exmaple: 192.168.0.45:18001)

### 4.3 Operate Fornax serverless resources
See ![get_start.md](get_start.md)
### 4.4 Run First Fornax Core serverless application
See ![get_start.md](get_start.md)
