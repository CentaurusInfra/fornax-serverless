#! /bin/bash

set -e

# remove fornaxcore and nodeagent file from the instance
remove_execute_file() {
    echo -e "## Remove fornaxcore and nodeagent file from the instance\n"
    names=`gcloud compute instances list --project quark-serverless --format="table(name)" | awk '{print $1}'`
    for name in $names
    do
        if [ $name == "NAME" ]; then
            continue
        fi

        if [[ $name == *"fornaxcore"* ]]; then
            echo "remove fornaxcore file from instance: $name"
            gcloud compute ssh ubuntu@$name --command="cd $HOME/go/src/centaurusinfra.io/fornax-serverless/bin && rm fornaxcore" --project=quark-serverless --zone=us-central1-a
            gcloud compute ssh ubuntu@$name --command="cd $HOME/go/src/centaurusinfra.io/fornax-serverless/bin && rm fornaxtest" --project=quark-serverless --zone=us-central1-a
            sleep 1
        fi

        if [[ $name == *"nodeagent"* ]]; then
            echo "remove nodeagent file from instance: $name"
            gcloud compute ssh ubuntu@$name --command="cd $HOME/go/src/centaurusinfra.io/fornax-serverless/bin && rm nodeagent" --project=quark-serverless --zone=us-central1-a &
            sleep 1
        fi
    done    
}

# stop fornaxcore and nodeagent service from the instance
stop_fornaxcore_nodeagent_service() {
    echo -e "## Stop fornaxcore and nodeagent service from the instance\n"
    names=`gcloud compute instances list --project quark-serverless --format="table(name)" | awk '{print $1}'`
    for name in $names
    do
        if [[ $name == *"fornaxcore"* ]]; then
            echo "remove fornaxcore file from instance: $name"
            gcloud compute ssh ubuntu@$name --command="pkill -9 fornaxcore" --project=quark-serverless --zone=us-central1-a &
        fi

        if [[ $name == *"nodeagent"* ]]; then
            echo "remove nodeagent file from instance: $name"
            gcloud compute ssh ubuntu@$name --command="sudo pkill -9 nodeagent" --project=quark-serverless --zone=us-central1-a &
            sleep 1
        fi
    done    
}

# copy fornaxcore and nodeagent file to the instance
copy_execute_file() {
    echo -e "## copy service file to the instance\n"
    names=`gcloud compute instances list --project quark-serverless --format="table(name)" | awk '{print $1}'`

    for name in $names
    do
        if [ $name == "NAME" ]; then
            continue
        fi

        if [[ $name == *"fornaxcore"* ]]; then
            echo "copy fornaxcore file to instance: $name"
            scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -r $HOME/go/src/centaurusinfra.io/fornax-serverless/bin/fornaxcore  ubuntu@$name:$HOME/go/src/centaurusinfra.io/fornax-serverless/bin/
            scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -r $HOME/go/src/centaurusinfra.io/fornax-serverless/bin/fornaxtest  ubuntu@$name:$HOME/go/src/centaurusinfra.io/fornax-serverless/bin/
        fi

        if [[ $name == *"nodeagent"* ]]; then
            echo "copy nodeagent file to instance: $name"
            scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -r $HOME/go/src/centaurusinfra.io/fornax-serverless/bin/nodeagent  ubuntu@$name:$HOME/go/src/centaurusinfra.io/fornax-serverless/bin/
        fi
    done   
}

# delete all app and clear node in the instance
delete_app() {
    for i in {0..100}
    do
        kubectl delete application --kubeconfig kubeconfig --namespace fornaxtest echoserver$i &
    done

    # check if app exist
    kubectl get application --kubeconfig kubeconfig --namespace fornaxtest
    # get appsession info
    kubectl get applicationsession --kubeconfig kubeconfig --namespace fornaxtest echoserver73-dcmwqkrvldfp25g2-cycle-44-session-0 -o yaml
    # grep session
    grep echoserver73-p2xbhgj59vcd2jmc-136 fornaxcore.logs
}

cold_test(){
    # current use 5000 pod for 20 node
    ./bin/fornaxtest --test-case session_create --num-of-session-per-app 1 --num-of-app 100 --num-of-init-pod-per-app 0 --num-of-test-cycle 50
}

warm_test() {
    # warm application
    ./bin/fornaxtest --test-case session_create --num-of-session-per-app 0 --num-of-app 100 --num-of-init-pod-per-app 50 --num-of-burst-pod-per-app 1  
    # bind session to application
    ./bin/fornaxtest --test-case session_create --num-of-session-per-app 1 --num-of-app 100 --num-of-init-pod-per-app 0 --num-of-test-cycle 50
}

create_instance(){
   # Enter the nodeagent number which you want to created 
    echo -e "## Enter nodeagent number which you want to created VM in your test:"
    read instance_num
    echo -e "\n"

    echo -e "## Enter account number which you want to created VM in your test:"
    read account_number
    echo -e "\n"


    # fornaxcore
    inst=`gcloud compute instances list --project quark-serverless --format="table(name)" --filter="name=fornaxcore" | awk '{print $1}'`
    if [[ $inst == "" ]];
    then
        echo -e "will create a instance: fornaxcore"
        gcloud compute instances create fornaxcore --project=quark-serverless --zone=us-central1-a --machine-type=e2-medium --network-interface=network-tier=PREMIUM,subnet=default --maintenance-policy=MIGRATE --provisioning-model=STANDARD --service-account=$account_number-compute@developer.gserviceaccount.com --scopes=https://www.googleapis.com/auth/cloud-platform --tags=http-server,https-server --create-disk=auto-delete=yes,boot=yes,device-name=instance-1,image=projects/ubuntu-os-cloud/global/images/ubuntu-2004-focal-v20220927,mode=rw,size=50,type=projects/quark-serverless/zones/us-central1-a/diskTypes/pd-ssd --no-shielded-secure-boot --shielded-vtpm --shielded-integrity-monitoring --reservation-affinity=any &
    else
        echo -e "instance: $inst already exist"
    fi


    # using for loop to create node agentinstance, for example: nodeagent1, 2, 3...
    # instance_num=2
    for ((i = 1; i<=$instance_num; i++))
    do
        instance_name='nodeagent-'$i
        echo -e "created $instance_name \n"
        gcloud compute instances create $instance_name --project=quark-serverless --zone=us-central1-a --machine-type=e2-medium --network-interface=network-tier=PREMIUM,subnet=default --maintenance-policy=MIGRATE --provisioning-model=STANDARD --service-account=$account_number-compute@developer.gserviceaccount.com --scopes=https://www.googleapis.com/auth/cloud-platform --tags=http-server,https-server --create-disk=auto-delete=yes,boot=yes,device-name=instance-1,image=projects/ubuntu-os-cloud/global/images/ubuntu-2004-focal-v20220927,mode=rw,size=50,type=projects/quark-serverless/zones/us-central1-a/diskTypes/pd-ssd --no-shielded-secure-boot --shielded-vtpm --shielded-integrity-monitoring --reservation-affinity=any &
    done

    echo "all instance created successfully." 
}

copy_ssh_key_to_project() {
    # copy ssh key to the project
    echo -e "## Copy ssh key to the project instance\n"
    gcloud compute project-info add-metadata \
    --metadata ssh-keys="$(gcloud compute project-info describe \
    --format="value(commonInstanceMetadata.items.filter(key:ssh-keys).firstof(value))")
    $(whoami):$(cat ~/.ssh/id_rsa.pub)"
}

delete_instance_by_number() {
    # Enter the nodeagent number which you want to delete 
    echo -e "## Enter nodeagent number which you want to delete in your test:"
    read instance_num
    echo -e "\n"

    # delete virtual machine instance
    gcloud compute instances delete fornaxcore --zone=us-central1-a

    # instance_num=2
    for ((i = 1; i<=$instance_num; i++))
    do
        instance_name='nodeagent-'$i
        echo -e "delete $instance_name \n"
        gcloud compute instances delete $instance_name --zone=us-central1-a --quiet &

    done
}