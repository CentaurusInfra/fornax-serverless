#! /bin/bash

set -e

#Golang version
GO_VERSION=${GO_VERSION:-"1.18.7"}

pushd $HOME

echo -e "## DISABLING FIREWALL\n"
sudo ufw disable
sudo swapoff -a

basic_install() {
    echo -e "## INSTALL BASIC TOOL"
    sudo apt-get -y update
    sudo apt -y install build-essential
    sudo apt -y install curl
    sudo apt-get -y install vim
    echo -e "## DONE BASIC TOOL\n"
}


docker_install(){
   sudo apt-get update -y > /dev/null 2>&1
   if [ "$(which docker)" != "" ] > /dev/null 2>&1
    then
       echo -e "## DOCKER IS ALREADY INSTALLED\n"
    else
       echo -e "##INSTALLING DOCKER"
       sudo curl -fsSL https://get.docker.com -o get-docker.sh
       sudo sh get-docker.sh > /dev/null 2>&1
       sudo chmod o+rw /var/run/docker.sock; 
       ls -al /var/run/docker.sock
       echo -e "## DOCKER INSTALLED\n"
   fi
}


runtimes_setup(){
    echo -e "## Start Setup and Configuration\n"
    daemon_install
    config_runtimes
    crictl_install
    cni_install
    cni_config
    runsc_install
    kata_install

    systemctl restart docker
    sleep 5
}

daemon_install() {
    sudo apt-get -y update > /dev/null 2>&1
    echo -e "## Write daeman.json File.\n"
    sudo touch /etc/docker/daemon.json
    sudo chmod 777 /etc/docker/daemon.json
cat << EOF | sudo tee /etc/docker/daemon.json
{
    "runtimes": {
        "quark": {
            "path": "/usr/local/bin/quark"
        },
        "quark_d": {
            "path": "/usr/local/bin/quark_d"
        },
        "runsc": {
            "path": "/usr/local/bin/runsc"
        },
        "kata-runtime": {
            "path": "/usr/bin/kata-runtime"
        }
    }
}
EOF

    # sudo systemctl restart containerd
    sleep 2
}

config_runtimes(){
	if [ ! -d "/etc/containerd" ]; then
	   echo " Directory /etc/containerd is not exist."
	   sudo mkdir /etc/containerd
	   sudo touch /etc/containerd/config.toml
	fi
	
  sudo chmod 777 /etc/containerd/config.toml
  if grep -q "disabled_plugins" "/etc/containerd/config.toml"; then
	   sudo sed -i 's/disabled_plugins/#disabled_plugins/' /etc/containerd/config.toml
  fi

if ! grep -q "version = 2" "/etc/containerd/config.toml"; then
cat << EOF | sudo tee -a /etc/containerd/config.toml

version = 2
[plugins."io.containerd.runtime.v1.linux"]
  shim_debug = true
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runc]
  runtime_type = "io.containerd.runc.v2"
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runsc]
  runtime_type = "io.containerd.runsc.v1"
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.quark]
  runtime_type = "io.containerd.quark.v1"
EOF
fi

  sudo systemctl restart containerd 
  sleep 3   
}

crictl_install(){
    echo -e "## Install crictl.\n"
    VERSION="v1.24.1"
    wget https://github.com/kubernetes-sigs/cri-tools/releases/download/$VERSION/crictl-$VERSION-linux-amd64.tar.gz
    sudo tar zxvf crictl-$VERSION-linux-amd64.tar.gz -C /usr/local/bin
    rm -f crictl-$VERSION-linux-amd64.tar.gz

    sudo touch /etc/crictl.yaml
    sudo chmod 777 /etc/crictl.yaml
    echo "runtime-endpoint: unix:///run/containerd/containerd.sock" | sudo tee /etc/crictl.yaml  > /dev/null
}

cni_install(){
    echo -e "## Install CNI.\n"
    sudo mkdir -p /opt/cni/bin
    VERSION="v1.1.1"
    sudo wget https://github.com/containernetworking/plugins/releases/download/$VERSION/cni-plugins-linux-amd64-$VERSION.tgz
    sudo tar zxvf cni-plugins-linux-amd64-$VERSION.tgz -C /opt/cni/bin
    sudo rm -f cni-plugins-linux-amd64-$VERSION.tgz
}

cni_config(){
    sudo apt-get -y update > /dev/null 2>&1
    echo -e "## Write CNI Config File.\n"
    sudo mkdir -p /etc/cni/net.d
    sudo touch /etc/cni/net.d/10-containerd-net.conflist
    sudo chmod a+x /etc/cni/net.d/10-containerd-net.conflist
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

    sudo systemctl restart containerd
    sleep 3    
}

runsc_install() {
	echo -e "## Download and Install runsc (gVisor).\n"
	(
	  set -e
	  ARCH=$(uname -m)
	  URL=https://storage.googleapis.com/gvisor/releases/release/latest/${ARCH}
	  wget ${URL}/runsc ${URL}/runsc.sha512 \
		${URL}/containerd-shim-runsc-v1 ${URL}/containerd-shim-runsc-v1.sha512
	  sha512sum -c runsc.sha512 \
		-c containerd-shim-runsc-v1.sha512
	  rm -f *.sha512
	  chmod a+rx runsc containerd-shim-runsc-v1
	  sudo mv runsc containerd-shim-runsc-v1 /usr/local/bin
	)
	# To install gVisor as a Containerd runtime, run the following commands:
	# /usr/local/bin/runsc install
	sudo systemctl restart containerd
  sleep 3
}

kata_install(){
  echo -e "## Install kata.\n"
	ARCH=$(arch)
	BRANCH="${BRANCH:-master}"
	sudo sh -c "echo 'deb http://download.opensuse.org/repositories/home:/katacontainers:/releases:/${ARCH}:/${BRANCH}/xUbuntu_$(lsb_release -rs)/ /' > /etc/apt/sources.list.d/kata-containers.list"
	curl -sL  http://download.opensuse.org/repositories/home:/katacontainers:/releases:/${ARCH}:/${BRANCH}/xUbuntu_$(lsb_release -rs)/Release.key | sudo apt-key add -
	sudo -E apt-get update
	sudo -E apt-get -y install kata-runtime kata-proxy kata-shim
  echo -e "## kata done.\n"
  sleep 3
}


golang_tools(){
   if [ "$(go version)" != "go version go1.18.7 linux/amd64" ] > /dev/null 2>&1
    then
      echo -e "## INSTALLING GOLANG TOOLS FOR FORNAXCORE AND NODEAGENT"
      sudo apt -y install make gcc jq > /dev/null 2>&1
	    echo "Install golang."
      wget https://dl.google.com/go/go${GO_VERSION}.linux-amd64.tar.gz -P /tmp
      sudo tar -C /usr/local -xzf /tmp/go${GO_VERSION}.linux-amd64.tar.gz
      # echo -e 'export PATH=$PATH:/usr/local/go/bin\nexport GOPATH=/usr/local/go/bin\nexport KUBECONFIG=/etc/kubernetes/admin.conf' |cat >> ~/.bashrc
	    echo -e '\n' >> ~/.bashrc
	    echo export GOROOT=\"/usr/local/go\" >> ~/.bashrc
	    echo export GOPATH=\"\$HOME/go\" >> ~/.bashrc
	    echo export GOBIN=\"\$HOME/go/bin\" >> ~/.bashrc
	    echo -e 'export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin' | cat >> ~/.bashrc
      source $HOME/.bashrc
      echo -e "## DONE\n"
      export PATH=$PATH:/usr/local/go/bin
    else
      echo -e "## go${GO_VERSION} already installed\n "
      export PATH=$PATH:/usr/local/go/bin
   fi
}


nodeagent_build(){
    echo -e "## CLONE NODEAGENT SOURCE CODE"
    mkdir -p ~/go
    cd go
	  mkdir -p bin src pkg
	  cd src
	  mkdir -p centaurusinfra.io
	  cd centaurusinfra.io
	  # pushd $HOME/go/src/centaurusinfra.io
    sudo git clone https://github.com/CentaurusInfra/fornax-serverless.git
    # pushd $HOME/go/src/centaurusinfra.io/fornax-serverless
	  cd fornax-serverless
	  # sudo chown -R $USER: .
    make all
    echo "## Build session-wrapper docker image\n"
    image_build
	  echo '## RUN NODEAGENT To Connect to FORNAXCORE'
    echo '# Get Fornaxcore IP'
    fornaxcoreip=`gcloud compute instances list --format='table(INTERNAL_IP)' --filter="name=fornaxcore" | awk '{if(NR==2) print $1}'`
    echo "Fornaxcore IP is: $fornaxcoreip"
    sleep 3
	  # following line command, put nodeagent run at background
	  nohup sudo ./bin/nodeagent --fornaxcore-url fornaxcoreip:18001 --disable-swap=false >> nodeagent.logs 2>&1 &
    echo -e "## DONE\n"
}

image_build() {
  echo -e "## Start to build session-wrapper docker image\n"
  make docker-build
  make
  sudo sed -i "s/@sudo crictl rmi/#@sudo crictl rmi/" ./Makefile
  make containerd-local-push
  sudo sed -i "s/#@sudo crictl rmi/@sudo crictl rmi/" ./Makefile
  echo -e "## Build session-wrapper docker image finished and deployed\n"
}

basic_install

docker_install

runtimes_setup

golang_tools

nodeagent_build

echo -e "## Nodeagent SETUP SUCCESSSFUL\n"