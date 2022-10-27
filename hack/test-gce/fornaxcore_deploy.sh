#! /bin/bash

set -e

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

fornaxcore_deploy(){
    echo -e "## DEPLOY FORNAXCORE"
    cd ~/go/src/centaurusinfra.io/fornax-serverless
	echo '## RUN FORNAXCORE'
    # run fornaxcore on the background
	nohup ./bin/fornaxcore --etcd-servers=http://127.0.0.1:2379 --secure-port=9443 --standalone-debug-mode --bind-address=127.0.0.1 >> fornaxcore.logs 2>&1 &
    echo -e "## DONE\n"
}


basic_install

etcd_install

kubectl_install

fornaxcore_deploy

echo -e "## SETUP SUCCESSSFUL\n"
