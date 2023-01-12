#! /bin/bash

set -e

FORNAX_ROOT=$(dirname "${BASH_SOURCE}")/../../..
source "${FORNAX_ROOT}/hack/test/gce/config_default.sh"

pushd $HOME

# stop fornaxcore and nodeagent service from the instance
stop_fornaxcore_nodeagent_service() {
    echo -e "## Stop fornaxcore and nodeagent service in the instances\n"
    names=`gcloud compute instances list --project ${PROJECT} --format="table(name)" | awk '{print $1}'`
    for name in $names
    do
        if [[ $name == *"${CORE_INSTANCE_PREFIX}"* ]]; then
            echo "stop fornaxcore service in the instance: $name"
            gcloud compute ssh ubuntu@$name --command="pkill -9 fornaxcore" --project=${PROJECT} --zone=${ZONE} &
        fi

        if [[ $name == *"${NODE_INSTANCE_PREFIX}"* ]]; then
            echo "stop nodeagent service in the instance: $name"
            gcloud compute ssh ubuntu@$name --command="sudo pkill -9 nodeagent" --project=${PROJECT} --zone=${ZONE} &
            sleep 1
        fi
    done    
}

stop_fornaxcore_nodeagent_service

echo "all instance services stopped successfully."