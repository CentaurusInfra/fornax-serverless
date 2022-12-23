#! /bin/bash

set -e

FORNAX_ROOT=$(dirname "${BASH_SOURCE}")/../../..
source "${FORNAX_ROOT}/hack/test/gce/config_default.sh"

pushd $HOME

# check fornaxcore and nodeagent service status from the instance
check_fornaxcore_nodeagent_service() {
    echo -e "## check fornaxcore and nodeagent service in the instances\n"
    names=`gcloud compute instances list --project ${PROJECT} --format="table(name)" | awk '{print $1}'`
    for name in $names
    do
        if [[ $name == *"${CORE_INSTANCE_PREFIX}"* ]]; then
            echo "check fornaxcore service in: $name"
            # gcloud compute ssh $name  --zone=${ZONE} -- bash -s < $HOME/fornaxcore_status.sh &
            gcloud compute ssh $name --command="bash ~/fornaxcore_status.sh" --project=${PROJECT} --zone=${ZONE} &
            sleep 1
        fi

        if [[ $name == *"${NODE_INSTANCE_PREFIX}"* ]]; then
            echo "check nodeagent service in: $name"
            # gcloud compute ssh $name  --zone=${ZONE} -- bash -s < $HOME/nodeagent_status.sh &
            gcloud compute ssh $name --command="bash ~/nodeagent_status.sh" --project=${PROJECT} --zone=${ZONE} &
            sleep 1
        fi
    done    
}

check_fornaxcore_nodeagent_service

echo "all instance services check done"