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