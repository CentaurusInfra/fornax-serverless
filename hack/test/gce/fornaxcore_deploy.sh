#! /bin/bash

set -e

##get all input arguments and parameters
CORE_AUTO_START=${1:-true}
CORE_ETCD_SERVERS=${2:-http://127.0.0.1:2379}
CORE_SECURE_PORT=${3:-9443}
CORE_BIND_ADDRESS=${4:-127.0.0.1}
CORE_LOG_FILE=${5:-fornaxcore-$(date '+%s').log}


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
    if [[ "${CORE_AUTO_START}" == "true" ]]; then
        # run fornaxcore on the background
        nohup ./bin/fornaxcore --etcd-servers=${CORE_ETCD_SERVERS} --secure-port=${CORE_SECURE_PORT} --standalone-debug-mode --bind-address=${CORE_BIND_ADDRESS} >> ${CORE_LOG_FILE} 2>&1 &
    fi
    echo -e "## DONE\n"
}


basic_install

etcd_install

kubectl_install

fornaxcore_deploy

echo -e "## SETUP SUCCESSSFUL\n"
