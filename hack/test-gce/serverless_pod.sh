#! /bin/bash

set -e

pushd $HOME

# check nodeagent pod amount in each instance
check_nodeagent_pod_amount() {
    echo -e "## check nodeagent pod amount in the instances\n"
    names=`gcloud compute instances list --project quark-serverless --format="table(name)" | awk '{print $1}'`
    for name in $names
    do
        if [[ $name == *"nodeagent"* ]]; then
            echo "check nodeagent pod amount in: $name"
            gcloud compute ssh $name --command="bash ~/nodeagent_podcount.sh" --project=quark-serverless --zone=us-central1-a &
            sleep 1
        fi
    done    
}

check_nodeagent_pod_amount

echo "check all instance pod amount done"