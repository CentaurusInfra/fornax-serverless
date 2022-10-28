#! /bin/bash

set -e

# Enter the nodeagent number which you want to created 
echo -e "## Enter nodeagent number which you want to created VM in your test:"
read instance_num
echo -e "\n"

echo -e "## Enter account number which you want to created VM in your test:"
read account_number
echo -e "\n"

instance_create() {
    # fornaxcore
    inst=`gcloud compute instances list --project quark-serverless --format="table(name)" --filter="name=fornaxcore" | awk '{print $1}'`
    if [[ $inst == "" ]];
    then
        echo -e "will create a instance: fornaxcore"
        # 32 cpu and 120G memory
        # gcloud compute instances create fornaxcore --project=quark-serverless --zone=us-central1-a --machine-type=n1-standard-32 --network-interface=network-tier=PREMIUM,subnet=default --maintenance-policy=MIGRATE --provisioning-model=STANDARD --service-account=$account_number-compute@developer.gserviceaccount.com --scopes=https://www.googleapis.com/auth/cloud-platform --tags=http-server,https-server --create-disk=auto-delete=yes,boot=yes,device-name=instance-1,image=projects/ubuntu-os-cloud/global/images/ubuntu-2004-focal-v20221018,mode=rw,size=50,type=projects/quark-serverless/zones/us-central1-a/diskTypes/pd-ssd --no-shielded-secure-boot --shielded-vtpm --shielded-integrity-monitoring --reservation-affinity=any &
        # 2 cpu and 4G Memory
        gcloud compute instances create fornaxcore --project=quark-serverless --zone=us-central1-a --machine-type=e2-medium --network-interface=network-tier=PREMIUM,subnet=default --maintenance-policy=MIGRATE --provisioning-model=STANDARD --service-account=$account_number-compute@developer.gserviceaccount.com --scopes=https://www.googleapis.com/auth/cloud-platform --tags=http-server,https-server --create-disk=auto-delete=yes,boot=yes,device-name=instance-1,image=projects/ubuntu-os-cloud/global/images/ubuntu-2004-focal-v20220927,mode=rw,size=50,type=projects/quark-serverless/zones/us-central1-a/diskTypes/pd-ssd --no-shielded-secure-boot --shielded-vtpm --shielded-integrity-monitoring --reservation-affinity=any &
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
            gcloud compute instances create $instance_name --project=quark-serverless --zone=us-central1-a --machine-type=n1-standard-32 --network-interface=network-tier=PREMIUM,subnet=default --maintenance-policy=MIGRATE --provisioning-model=STANDARD --service-account=$account_number-compute@developer.gserviceaccount.com --scopes=https://www.googleapis.com/auth/cloud-platform --tags=http-server,https-server --create-disk=auto-delete=yes,boot=yes,device-name=instance-1,image=projects/ubuntu-os-cloud/global/images/ubuntu-2004-focal-v20221018,mode=rw,size=50,type=projects/quark-serverless/zones/us-central1-a/diskTypes/pd-ssd --no-shielded-secure-boot --shielded-vtpm --shielded-integrity-monitoring --reservation-affinity=any &
            # 2 cpu and 4G Memory
            # gcloud compute instances create $instance_name --project=quark-serverless --zone=us-central1-a --machine-type=e2-medium --network-interface=network-tier=PREMIUM,subnet=default --maintenance-policy=MIGRATE --provisioning-model=STANDARD --service-account=$account_number-compute@developer.gserviceaccount.com --scopes=https://www.googleapis.com/auth/cloud-platform --tags=http-server,https-server --create-disk=auto-delete=yes,boot=yes,device-name=instance-1,image=projects/ubuntu-os-cloud/global/images/ubuntu-2004-focal-v20220927,mode=rw,size=50,type=projects/quark-serverless/zones/us-central1-a/diskTypes/pd-ssd --no-shielded-secure-boot --shielded-vtpm --shielded-integrity-monitoring --reservation-affinity=any &
        done
    else
        echo -e "instance: $inst already exist"
    fi

    echo -e "## All instances created done\n"
    sleep 2
}

# copy exe file to the each instance
deploy_instance_by_filter() {
    echo -e "## Deployed file to the instance and setup machine\n"

    names=`gcloud compute instances list --project quark-serverless --format="table(name)" | awk '{print $1}'`

    for name in $names
    do
        if [ $name == "NAME" ]; then
            continue
        fi

        if [[ $name == *"fornaxcore"* ]]; then
            echo "deploy fornaxcore instance: $name"
            cat ~/.ssh/id_rsa.pub | ssh -o StrictHostKeyChecking=no ubuntu@$name 'cat >> ~/.ssh/authorized_keys'
            ssh -t ubuntu@$name "mkdir -p $HOME/go/src/centaurusinfra.io/fornax-serverless/bin" > /dev/null 2>&1
            scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -r ./bin/fornaxcore  ubuntu@$name:$HOME/go/src/centaurusinfra.io/fornax-serverless/bin/
            scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -r ./bin/fornaxtest  ubuntu@$name:$HOME/go/src/centaurusinfra.io/fornax-serverless/bin/
            scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -r ./kubeconfig  ubuntu@$name:$HOME/go/src/centaurusinfra.io/fornax-serverless/
            scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -r ./hack/test-gce/fornaxcore_deploy.sh  ubuntu@$name:$HOME/
            scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -r ./hack/test-gce/fornaxcore_start.sh  ubuntu@$name:$HOME/
            scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -r ./hack/test-gce/fornaxcore_status.sh  ubuntu@$name:$HOME/
            gcloud compute ssh $name  --zone=us-central1-a -- bash -s < $HOME/fornaxcore_deploy.sh > /dev/null 2>&1 &
            sleep 1
        fi

        if [[ $name == *"nodeagent"* ]]; then
            echo "deploy nodeagent instance: $name"
            cat ~/.ssh/id_rsa.pub | ssh -o StrictHostKeyChecking=no ubuntu@$name "cat >> ~/.ssh/authorized_keys"
            sleep 1
            ssh -t ubuntu@$name "mkdir -p $HOME/go/src/centaurusinfra.io/fornax-serverless/bin" > /dev/null 2>&1
            scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -r ./bin/nodeagent  ubuntu@$name:$HOME/go/src/centaurusinfra.io/fornax-serverless/bin/
            scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -r ./hack/test-gce/nodeagent_deploy.sh  ubuntu@$name:$HOME/
            scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -r ./hack/test-gce/nodeagent_start.sh  ubuntu@$name:$HOME/
            scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -r ./hack/test-gce/nodeagent_status.sh  ubuntu@$name:$HOME/
            gcloud compute ssh $name  --zone=us-central1-a -- bash -s < $HOME/nodeagent_deploy.sh > /dev/null 2>&1 &
            sleep 1
        fi
    done
}

key_gen(){
   if [ "$(ls $HOME/.ssh/id_rsa.pub)" != "$HOME/.ssh/id_rsa.pub" ] > /dev/null 2>&1
   then
       echo -e "## GENERATING KEY."
       # chmod 600 $key_pair_3 
       < /dev/zero ssh-keygen -q -N ""
   fi
} 

check_knownhosts(){
    if [ "$(ls $HOME/.ssh/known_hosts)" != "" ]; then
        echo -e "known_hosts already exits, we remove this file first\n";
        rm -rf ~/.ssh/known_hosts
    fi
}

key_gen

check_knownhosts

instance_create

deploy_instance_by_filter

echo "all instance deploy successfully."