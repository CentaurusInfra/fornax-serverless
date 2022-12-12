#! /bin/bash

set -e

FORNAX_ROOT=$(dirname "${BASH_SOURCE}")/../../..
source "${FORNAX_ROOT}/hack/test/gce/config_default.sh"

instance_create() {
    # fornaxcore
    inst=`gcloud compute instances list --project ${PROJECT} --format="table(name)" --filter="name=fornaxcore" | awk '{print $1}'`
    if [[ $inst == "" ]];
    then
        # 32 cpu and 120G memory
        # gcloud compute instances create fornaxcore --project=${PROJECT} --zone=${ZONE} --machine-type=n1-standard-32 --network-interface=network-tier=PREMIUM,subnet=default --maintenance-policy=MIGRATE --provisioning-model=STANDARD --service-account=$account_number-compute@developer.gserviceaccount.com --scopes=https://www.googleapis.com/auth/cloud-platform --tags=http-server,https-server --create-disk=auto-delete=yes,boot=yes,device-name=instance-1,image=projects/ubuntu-os-cloud/global/images/ubuntu-2004-focal-v20221018,mode=rw,size=${CORE_DISK_SIZE},type=projects/${PROJECT}/zones/${ZONE}/diskTypes/${CORE_DISK_TYPE} --no-shielded-secure-boot --shielded-vtpm --shielded-integrity-monitoring --reservation-affinity=any &
        # 4 cpu and 16G memory and machiny type: e2-standard-4
        gcloud compute instances create ${CORE_INSTANCE_PREFIX} --project=${PROJECT} --zone=${ZONE} --machine-type=${CORE_MACHINE_TYPE} --network-interface=network-tier=PREMIUM,subnet=default --maintenance-policy=MIGRATE --provisioning-model=STANDARD --service-account=${CORE_SERVICE_ACCOUNT} --scopes=https://www.googleapis.com/auth/cloud-platform --tags=http-server,https-server --create-disk=auto-delete=yes,boot=yes,device-name=instance-1,image=projects/${CORE_IMAGE_PROJECT}/global/images/${CORE_IMAGE},mode=rw,size=${CORE_DISK_SIZE},type=projects/${PROJECT}/zones/${ZONE}/diskTypes/${CORE_DISK_TYPE} --no-shielded-secure-boot --shielded-vtpm --shielded-integrity-monitoring --reservation-affinity=any &
    else
        echo -e "instance: $inst already exist"
    fi

    # using for loop to create node agentinstance, for example: nodeagent1, 2, 3...
    # NODE_NUM=2
    inst=`gcloud compute instances list --project ${PROJECT} --format="table(name)" --filter="name=nodeagent-1" | awk '{print $1}'`
    if [[ $inst == "" ]];
    then
        for ((i = 1; i<=$NODE_NUM; i++))
        do
            instance_name="${NODE_INSTANCE_PREFIX}-$i"
            echo -e "created $instance_name \n"
            # 32 cpu and 120G memory
            gcloud compute instances create $instance_name --project=${PROJECT} --zone=${ZONE} --machine-type=${NODE_MACHINE_TYPE} --network-interface=network-tier=PREMIUM,subnet=default --maintenance-policy=MIGRATE --provisioning-model=STANDARD --service-account=${NODE_SERVICE_ACCOUNT} --scopes=https://www.googleapis.com/auth/cloud-platform --tags=http-server,https-server --create-disk=auto-delete=yes,boot=yes,device-name=instance-1,image=projects/${NODE_IMAGE_PROJECT}/global/images/${NODE_IMAGE},mode=rw,size=${NODE_DISK_SIZE},type=projects/${PROJECT}/zones/${ZONE}/diskTypes/${NODE_DISK_TYPE} --no-shielded-secure-boot --shielded-vtpm --shielded-integrity-monitoring --reservation-affinity=any &
            # 2 cpu and 4G Memory
            # gcloud compute instances create $instance_name --project=${PROJECT} --zone=${ZONE} --machine-type=e2-medium --network-interface=network-tier=PREMIUM,subnet=default --maintenance-policy=MIGRATE --provisioning-model=STANDARD --service-account=$account_number-compute@developer.gserviceaccount.com --scopes=https://www.googleapis.com/auth/cloud-platform --tags=http-server,https-server --create-disk=auto-delete=yes,boot=yes,device-name=instance-1,image=projects/ubuntu-os-cloud/global/images/ubuntu-2004-focal-v20220927,mode=rw,size=${NODE_DISK_SIZE},type=projects/${PROJECT}/zones/${ZONE}/diskTypes/${NODE_DISK_TYPE} --no-shielded-secure-boot --shielded-vtpm --shielded-integrity-monitoring --reservation-affinity=any &
        done
    else
        echo -e "instance: $inst already exist"
    fi
 
    echo -e "## Please waiting instance ready.\n"
    sleep 100
    echo -e "## All instances created done\n"
}

copy_basicfile_to_instance() {
    echo -e "## Copy basic file to the instance\n"
    names=`gcloud compute instances list --project ${PROJECT} --format="table(name)" | awk '{print $1}'`
    for name in $names
    do
        if [ $name == "NAME" ]; then
            continue
        fi

        if [[ $name == *"${CORE_INSTANCE_PREFIX}"* ]] || [[ $name == *"${NODE_INSTANCE_PREFIX}"* ]]; then
            echo "copy file to instance: $name"
            gcloud compute ssh $name --command="mkdir -p ~/go/src/centaurusinfra.io/fornax-serverless/bin" --project=${PROJECT} --zone=${ZONE} > /dev/null 2>&1 &
            gcloud compute scp ./hack/test/gce/nodeagent_podcount.sh $name:~/ --project=${PROJECT} --zone=${ZONE} &
        fi
    done
    
    echo -e "## Please waiting basic file copy done.\n"
    sleep 60
    echo -e "Copy basic file is done.\n"    
}

# copy exe file to the each instance
deploy_instance_by_filter() {
    echo -e "## Copy file to the instance\n"
    names=`gcloud compute instances list --project ${PROJECT} --format="table(name)" | awk '{print $1}'`
    for name in $names
    do
        if [ $name == "NAME" ]; then
            continue
        fi

        if [[ $name == *"${CORE_INSTANCE_PREFIX}"* ]]; then
            echo "copy file to fornaxcore instance: $name"
            gcloud compute ssh $name --command="mkdir -p ~/go/src/centaurusinfra.io/fornax-serverless/bin" --project=${PROJECT} --zone=${ZONE} > /dev/null 2>&1 &
            gcloud compute scp ./bin/fornaxcore ./bin/fornaxtest $name:~/go/src/centaurusinfra.io/fornax-serverless/bin/ --project=${PROJECT} --zone=${ZONE} &
            gcloud compute scp ./kubeconfig $name:~/go/src/centaurusinfra.io/fornax-serverless/ --project=${PROJECT} --zone=${ZONE} &
            gcloud compute scp ./hack/test/gce/fornaxcore_deploy.sh ./hack/test/gce/fornaxcore_start.sh  ./hack/test/gce/fornaxcore_status.sh $name:~/ --project=${PROJECT} --zone=${ZONE} &
        fi

        if [[ $name == *"${NODE_INSTANCE_PREFIX}"* ]]; then
            echo "copy file to nodeagent instance: $name"
            gcloud compute ssh $name --command="mkdir -p ~/go/src/centaurusinfra.io/fornax-serverless/bin" --project=${PROJECT} --zone=${ZONE} > /dev/null 2>&1 &
            gcloud compute scp ./bin/nodeagent $name:~/go/src/centaurusinfra.io/fornax-serverless/bin/ --project=${PROJECT} --zone=${ZONE} &
            gcloud compute scp ./bin/simulatenode $name:~/go/src/centaurusinfra.io/fornax-serverless/bin/  --project=${PROJECT} --zone=${ZONE} &
	    gcloud compute scp ./hack/test/gce/nodeagent_deploy.sh ./hack/test/gce/nodeagent_start.sh  ./hack/test/gce/nodeagent_status.sh ./hack/test/gce/config_default.sh $name:~/ --project=${PROJECT} --zone=${ZONE} &
        fi
    done
    
    echo -e "## Please waiting file copy done.\n"
    sleep 60
    echo -e "Copy file is done.\n"
}

install_required_software(){
    echo -e "## Install required software to the instance and setup machine\n"
    names=`gcloud compute instances list --project ${PROJECT} --format="table(name)" | awk '{print $1}'`
    fornaxcoreip=`gcloud compute instances list --format='table(INTERNAL_IP)' --filter="name=${CORE_INSTANCE_PREFIX}" | awk '{if(NR==2) print $1}'`
    for name in $names
    do
        if [ $name == "NAME" ]; then
            continue
        fi

        if [[ $name == *"${CORE_INSTANCE_PREFIX}"* ]]; then
            echo "install third party software in fornaxcore instance: $name"
            gcloud compute ssh $name --command="bash ~/fornaxcore_deploy.sh ${CORE_AUTO_START} ${CORE_ETCD_SERVERS} ${CORE_SECURE_PORT} ${CORE_BIND_ADDRESS} ${CORE_LOG_FILE} >> ${name}_deploy.log" --project=${PROJECT} --zone=${ZONE} > /dev/null 2>&1 &
        fi

        if [[ $name == *"${NODE_INSTANCE_PREFIX}"* ]]; then
            echo "install third party software in nodeagent instance: $name"
            gcloud compute ssh $name --command="bash ~/nodeagent_deploy.sh ${fornaxcoreip} ${NODEAGENT_AUTO_START} ${SIM_AUTO_START} ${CORE_DEFAULT_PORT} ${NODE_DISABLE_SWAP} ${NODE_LOG_FILE} ${SIM_NUM_OF_NODE} ${SIM_LOG_FILE} >> ${name}_deploy.log" --project=${PROJECT} --zone=${ZONE} > /dev/null 2>&1 &
        fi
    done

    echo -e "## Please waiting software to install.\n"
    sleep 120
    echo -e "install and configue is done.\n"
}

key_config_ssh(){
   if [ "$(ls $HOME/.ssh/google_compute_engine.pub)" != "$HOME/.ssh/google_compute_engine.pub" ] > /dev/null 2>&1
   then
       echo -e "## gcloud compute config-ssh."
       < /dev/zero gcloud compute config-ssh --quiet
   fi
} 

key_gen(){
   if [ "$(ls $HOME/.ssh/id_rsa.pub)" != "$HOME/.ssh/id_rsa.pub" ] > /dev/null 2>&1
   then
       echo -e "## GENERATING KEY."
       < /dev/zero ssh-keygen -q -N ""
   fi
} 

# key_gen
key_config_ssh
echo "## Starting to deploy test environments..."
echo "## Creating fornaxcore and node instances..."
instance_create

copy_basicfile_to_instance

deploy_instance_by_filter

install_required_software

echo "## Done to deploy test environments."
