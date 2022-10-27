#! /bin/bash

set -e

pushd $HOME

# restart serverless fornaxcore and nodeagent
start_serverless_by_filter() {
    echo -e "## Restart serverless fornaxcore and nodeagent\n"

    names=`gcloud compute instances list --project quark-serverless --format="table(name)" | awk '{print $1}'`

    for name in $names
    do
        if [ $name == "NAME" ]; then
            continue
        fi

        if [[ $name == *"davidzhu-fornaxcore"* ]]; then
            echo "start fornaxcore service: $name"
            gcloud compute ssh $name  --zone=us-central1-a -- bash -s < $HOME/fornaxcore_start.sh > /dev/null 2>&1 &
            sleep 1
        fi

        if [[ $name == *"davidzhu-nodeagent"* ]]; then
            echo "Start nodeagent service: $name"
            gcloud compute ssh $name  --zone=us-central1-a -- bash -s < $HOME/nodeagent_start.sh > /dev/null 2>&1 &
            sleep 1
        fi
    done
}

start_serverless_by_filter

echo "all instance services started successfully."