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


etcd_install(){
   sudo apt-get update -y > /dev/null 2>&1
   if [ "$(which etcd)" != "" ] > /dev/null 2>&1
    then
       echo -e "## ETCD IS ALREADY INSTALLED\n"
    else
       echo -e "##INSTALLING ETCD"
       sudo apt-get -y install etcd > /dev/null 2>&1
       echo -e "## ETCD INSTALLED\n"
   fi
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
       export PATH=$PATH:/usr/local/go/bin
       echo -e "## DONE\n"
    else
       echo -e "## go${GO_VERSION} already installed\n "F
       export PATH=$PATH:/usr/local/go/bin
   fi
}

kubectl_install(){
    if [ "$(which kubectl)" != "" ] > /dev/null 2>&1
     then
        echo -e "## kubectl classic IS ALREADY INSTALLED\n"
     else
        echo -e "##INSTALLING kubectl"
        sudo snap install kubectl --classic > /dev/null 2>&1
        echo -e "## kubectl INSTALLED\n"
    fi
}

fornaxcore_build(){
    echo -e "## CLONE FORNAXCORE SOURCE CODE"
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
	sudo chown -R $USER: .
    make all
	echo '## RUN FORNAXCORE'
    # run fornaxcore on the background
	nohup ./bin/fornaxcore --etcd-servers=http://127.0.0.1:2379 --secure-port=9443 --standalone-debug-mode --bind-address=127.0.0.1 >> fornaxcore.logs 2>&1 &
    echo -e "## DONE\n"
}


basic_install

etcd_install

golang_tools

kubectl_install

fornaxcore_build

echo -e "## SETUP SUCCESSSFUL\n"
