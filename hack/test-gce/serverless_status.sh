#! /bin/bash

set -e

pushd $HOME

# check fornaxcore and nodeagent service status from the instance
check_fornaxcore_nodeagent_service() {
    echo -e "## check fornaxcore and nodeagent service in the instances\n"
    names=`gcloud compute instances list --project quark-serverless --format="table(name)" | awk '{print $1}'`
    for name in $names
    do
        if [[ $name == *"fornaxcore"* ]]; then
            echo "check fornaxcore service in: $name"
            # gcloud compute ssh $name  --zone=us-central1-a -- bash -s < $HOME/fornaxcore_status.sh &
            gcloud compute ssh $name --command="bash ~/fornaxcore_status.sh" --project=quark-serverless --zone=us-central1-a &
            sleep 1
        fi

        if [[ $name == *"nodeagent"* ]]; then
            echo "check nodeagent service in: $name"
            # gcloud compute ssh $name  --zone=us-central1-a -- bash -s < $HOME/nodeagent_status.sh &
            gcloud compute ssh $name --command="bash ~/nodeagent_status.sh" --project=quark-serverless --zone=us-central1-a &
            sleep 1
        fi
    done    
}

check_fornaxcore_nodeagent_service

echo "all instance services check done"