#! /bin/bash

set -e

pushd $HOME
echo -e "## SETTING UP THE HOSTNAME QUARK-A\n"
sudo hostnamectl set-hostname quark-a
echo -e "## DISABLING FIREWALL\n"
sudo ufw disable
sudo swapoff -a

basic_install() {
    echo -e "## INSTALL BASIC TOOL"
    sudo apt-get update -y
    sudo apt install build-essential -y
    sudo apt install curl -y
    sudo apt-get install vim -y
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
       echo -e "## DOCKER INSTALLED\n"
   fi
}

rust_install(){
    echo -e "## INSTALL RUST"
    curl --proto '=https' --tlsv1.2 https://sh.rustup.rs -sSf | sh -s -- -y
    source $HOME/.cargo/env
    echo -e "## DONE\n"
}

rustup_install(){
    echo -e "## RUSTUP INSTALL"
    rustup update nightly-2022-08-11-x86_64-unknown-linux-gnu
    rustup toolchain install nightly-2022-08-11-x86_64-unknown-linux-gnu
    rustup default nightly-2022-08-11-x86_64-unknown-linux-gnu
    rustup component add rust-src --toolchain nightly-2022-08-11-x86_64-unknown-linux-gnu
    rustup --version
    echo -e "## DONE\n"
}

cargo_build(){
    echo -e "## CARGO INSTALL"
    sudo apt-get update
    cargo install cargo-xbuild 
    echo -e "## INSTALL lcap Library"
    sudo apt-get install libcap-dev > /dev/null 2>&1
    sudo apt-get install build-essential cmake gcc libudev-dev libnl-3-dev libnl-route-3-dev ninja-build pkg-config valgrind python3-dev cython3 python3-docutils pandoc libclang-dev > /dev/null 2>&1
    echo -e "## DONE\n"
}

quark_build(){
    echo -e "## CLONE QUARK SOURCE CODE"
    push $HOME
    sudo git clone https://github.com/QuarkContainer/Quark.git
    push $HOME/Quark
    make
    make install
    sudo mkdir /var/log/quark
    sed -i 's+"ShimMode"      : false+"ShimMode"      : true+g' ./config.json
    echo -e "## DONE\n"
}

quark_setup(){
    echo -e "## Start Setup and Configuration\n"
    daemon_install
    config_runtimes
    crictl_install
    cni_install
    cni_config
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

    sudo systemctl restart containerd
}

config_runtimes(){
    echo -e "## Append Text To config.toml File.\n"
    sudo chmod 777 /etc/containerd/config.toml
    sed -i 's+disable_plugins+#disable_plugins+g' /etc/containerd/config.toml   
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

   sudo systemctl restart containerd    
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
}



basic_install

docker_install

rust_install

rustup_install

cargo_build

quark_build

quark_setup

echo -e "## SETUP SUCCESSSFUL\n"
