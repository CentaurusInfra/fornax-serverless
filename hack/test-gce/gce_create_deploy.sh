#! /bin/bash

set -e

# Enter the nodeagent number which you want to created 
use_input(){
    inst=`gcloud compute instances list --project quark-serverless --format="table(name)" --filter="name=fornaxcore" | awk '{print $1}'`
    if [[ $inst == "" ]];
    then
        echo -e "## Enter nodeagent number which you want to created VM in your test:"
        read instance_num
        echo -e "\n"

        echo -e "## Enter account number which you want to created VM in your test:"
        read account_number
        echo -e "\n"
    fi
}

instance_create() {
    # fornaxcore
    inst=`gcloud compute instances list --project quark-serverless --format="table(name)" --filter="name=fornaxcore" | awk '{print $1}'`
    if [[ $inst == "" ]];
    then
        echo -e "will create a instance: fornaxcore"
        # 32 cpu and 120G memory
        # gcloud compute instances create fornaxcore --project=quark-serverless --zone=us-central1-a --machine-type=n1-standard-32 --network-interface=network-tier=PREMIUM,subnet=default --maintenance-policy=MIGRATE --provisioning-model=STANDARD --service-account=$account_number-compute@developer.gserviceaccount.com --scopes=https://www.googleapis.com/auth/cloud-platform --tags=http-server,https-server --create-disk=auto-delete=yes,boot=yes,device-name=instance-1,image=projects/ubuntu-os-cloud/global/images/ubuntu-2004-focal-v20221018,mode=rw,size=30,type=projects/quark-serverless/zones/us-central1-a/diskTypes/pd-ssd --no-shielded-secure-boot --shielded-vtpm --shielded-integrity-monitoring --reservation-affinity=any &
        # 4 cpu and 16G memory and machiny type: e2-standard-4
        gcloud compute instances create fornaxcore --project=quark-serverless --zone=us-central1-a --machine-type=e2-standard-4 --network-interface=network-tier=PREMIUM,subnet=default --maintenance-policy=MIGRATE --provisioning-model=STANDARD --service-account=$account_number-compute@developer.gserviceaccount.com --scopes=https://www.googleapis.com/auth/cloud-platform --tags=http-server,https-server --create-disk=auto-delete=yes,boot=yes,device-name=instance-1,image=projects/ubuntu-os-cloud/global/images/ubuntu-2004-focal-v20221018,mode=rw,size=40,type=projects/quark-serverless/zones/us-central1-a/diskTypes/pd-ssd --no-shielded-secure-boot --shielded-vtpm --shielded-integrity-monitoring --reservation-affinity=any &
    else
        echo -e "instance: $inst already exist"
    fi

    # using for loop to create node agentinstance, for example: nodeagent1, 2, 3...
    # instance_num=2
    inst=`gcloud compute instances list --project quark-serverless --format="table(name)" --filter="name=nodeagent-1" | awk '{print $1}'`
    if [[ $inst == "" ]];
    then
        for ((i = 1; i<=$instance_num; i++))
        do
            instance_name='nodeagent-'$i
            echo -e "created $instance_name \n"
            # 32 cpu and 120G memory
            gcloud compute instances create $instance_name --project=quark-serverless --zone=us-central1-a --machine-type=n1-standard-32 --network-interface=network-tier=PREMIUM,subnet=default --maintenance-policy=MIGRATE --provisioning-model=STANDARD --service-account=$account_number-compute@developer.gserviceaccount.com --scopes=https://www.googleapis.com/auth/cloud-platform --tags=http-server,https-server --create-disk=auto-delete=yes,boot=yes,device-name=instance-1,image=projects/ubuntu-os-cloud/global/images/ubuntu-2004-focal-v20221018,mode=rw,size=30,type=projects/quark-serverless/zones/us-central1-a/diskTypes/pd-ssd --no-shielded-secure-boot --shielded-vtpm --shielded-integrity-monitoring --reservation-affinity=any &
            # 2 cpu and 4G Memory
            # gcloud compute instances create $instance_name --project=quark-serverless --zone=us-central1-a --machine-type=e2-medium --network-interface=network-tier=PREMIUM,subnet=default --maintenance-policy=MIGRATE --provisioning-model=STANDARD --service-account=$account_number-compute@developer.gserviceaccount.com --scopes=https://www.googleapis.com/auth/cloud-platform --tags=http-server,https-server --create-disk=auto-delete=yes,boot=yes,device-name=instance-1,image=projects/ubuntu-os-cloud/global/images/ubuntu-2004-focal-v20220927,mode=rw,size=50,type=projects/quark-serverless/zones/us-central1-a/diskTypes/pd-ssd --no-shielded-secure-boot --shielded-vtpm --shielded-integrity-monitoring --reservation-affinity=any &
        done
    else
        echo -e "instance: $inst already exist"
    fi
 
    echo -e "## Please waiting instance ready.\n"
    sleep 180
    echo -e "## All instances created done\n"
}

copy_basicfile_to_instance() {
    echo -e "## Copy basic file to the instance\n"
    names=`gcloud compute instances list --project quark-serverless --format="table(name)" | awk '{print $1}'`
    for name in $names
    do
        if [ $name == "NAME" ]; then
            continue
        fi

        if [[ $name == *"fornaxcore"* ]] || [[ $name == *"nodeagent"* ]]; then
            echo "copy file to instance: $name"
            gcloud compute ssh $name --command="mkdir -p ~/go/src/centaurusinfra.io/fornax-serverless/bin" --project=quark-serverless --zone=us-central1-a > /dev/null 2>&1 &
            gcloud compute scp ./hack/test-gce/nodeagent_podcount.sh $name:~/ --project=quark-serverless --zone=us-central1-a &
        fi
    done
    
    echo -e "## Please waiting basic file copy done.\n"
    sleep 60
    echo -e "Copy basic file is done.\n"    
}

# copy exe file to the each instance
deploy_instance_by_filter() {
    echo -e "## Copy file to the instance\n"
    names=`gcloud compute instances list --project quark-serverless --format="table(name)" | awk '{print $1}'`
    for name in $names
    do
        if [ $name == "NAME" ]; then
            continue
        fi

        if [[ $name == *"fornaxcore"* ]]; then
            echo "copy file to fornaxcore instance: $name"
            gcloud compute ssh $name --command="mkdir -p ~/go/src/centaurusinfra.io/fornax-serverless/bin" --project=quark-serverless --zone=us-central1-a > /dev/null 2>&1 &
            gcloud compute scp ./bin/fornaxcore ./bin/fornaxtest $name:~/go/src/centaurusinfra.io/fornax-serverless/bin/ --project=quark-serverless --zone=us-central1-a &
            gcloud compute scp ./kubeconfig $name:~/go/src/centaurusinfra.io/fornax-serverless/ --project=quark-serverless --zone=us-central1-a &
            gcloud compute scp ./hack/test-gce/fornaxcore_deploy.sh ./hack/test-gce/fornaxcore_start.sh  ./hack/test-gce/fornaxcore_status.sh $name:~/ --project=quark-serverless --zone=us-central1-a &
        fi

        if [[ $name == *"nodeagent"* ]]; then
            echo "copy file to nodeagent instance: $name"
            gcloud compute ssh $name --command="mkdir -p ~/go/src/centaurusinfra.io/fornax-serverless/bin" --project=quark-serverless --zone=us-central1-a > /dev/null 2>&1 &
            gcloud compute scp ./bin/nodeagent $name:~/go/src/centaurusinfra.io/fornax-serverless/bin/ --project=quark-serverless --zone=us-central1-a &
            gcloud compute scp ./hack/test-gce/nodeagent_deploy.sh ./hack/test-gce/nodeagent_start.sh  ./hack/test-gce/nodeagent_status.sh $name:~/ --project=quark-serverless --zone=us-central1-a &
        fi
    done
    
    echo -e "## Please waiting file copy done.\n"
    sleep 60
    echo -e "Copy file is done.\n"
}

install_required_software(){
    echo -e "## Install required software to the instance and setup machine\n"
    names=`gcloud compute instances list --project quark-serverless --format="table(name)" | awk '{print $1}'`
    for name in $names
    do
        if [ $name == "NAME" ]; then
            continue
        fi

        if [[ $name == *"fornaxcore"* ]]; then
            echo "install third party software in fornaxcore instance: $name"
            gcloud compute ssh $name --command="bash ~/fornaxcore_deploy.sh" --project=quark-serverless --zone=us-central1-a > /dev/null 2>&1 &
        fi

        if [[ $name == *"nodeagent"* ]]; then
            echo "install third party software in nodeagent instance: $name"
            gcloud compute ssh $name --command="bash ~/nodeagent_deploy.sh" --project=quark-serverless --zone=us-central1-a > /dev/null 2>&1 &
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

check_knownhosts(){
    if [ "$(ls $HOME/.ssh/known_hosts)" != "" ]; then
        echo -e "known_hosts already exits, we remove this file first\n";
        rm -rf ~/.ssh/known_hosts
    fi
}

check_knownhosts_google(){
    if [ "$(ls $HOME/.ssh/google_compute_known_hosts)" != "" ]; then
        echo -e "google_compute_known_hosts already exits, we remove this file first\n";
        rm -rf ~/.ssh/google_compute_known_hosts
    fi
}

use_input

# key_gen
key_config_ssh

# check_knownhosts
# check_knownhosts_google

instance_create

copy_basicfile_to_instance

deploy_instance_by_filter

install_required_software

echo "all instance deploy done."