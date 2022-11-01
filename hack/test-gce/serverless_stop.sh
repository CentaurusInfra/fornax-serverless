#! /bin/bash

set -e

pushd $HOME

# stop fornaxcore and nodeagent service from the instance
stop_fornaxcore_nodeagent_service() {
    echo -e "## Stop fornaxcore and nodeagent service in the instances\n"
    names=`gcloud compute instances list --project quark-serverless --format="table(name)" | awk '{print $1}'`
    for name in $names
    do
        if [[ $name == *"fornaxcore"* ]]; then
            echo "stop fornaxcore service in the instance: $name"
            gcloud compute ssh ubuntu@$name --command="pkill -9 fornaxcore" --project=quark-serverless --zone=us-central1-a &
        fi

        if [[ $name == *"nodeagent"* ]]; then
            echo "stop nodeagent service in the instance: $name"
            gcloud compute ssh ubuntu@$name --command="sudo pkill -9 nodeagent" --project=quark-serverless --zone=us-central1-a &
            sleep 1
        fi
    done    
}

stop_fornaxcore_nodeagent_service

echo "all instance services stopped successfully."