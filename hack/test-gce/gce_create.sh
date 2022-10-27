#! /bin/bash

set -e

# Enter the nodeagent number which you want to created 
echo -e "## Enter nodeagent number which you want to created VM in your test:"
read instance_num
echo -e "\n"

echo -e "## Enter account number which you want to created VM in your test:"
read account_number
echo -e "\n"


# davidzhu-fornaxcore
inst=`gcloud compute instances list --project quark-serverless --format="table(name)" --filter="name=davidzhu-fornaxcore" | awk '{print $1}'`
if [[ $inst == "" ]];
then
	echo -e "will create a instance: davidzhu-fornaxcore"
    gcloud compute instances create davidzhu-fornaxcore --project=quark-serverless --zone=us-central1-a --machine-type=e2-medium --network-interface=network-tier=PREMIUM,subnet=default --maintenance-policy=MIGRATE --provisioning-model=STANDARD --service-account=$account_number-compute@developer.gserviceaccount.com --scopes=https://www.googleapis.com/auth/cloud-platform --tags=http-server,https-server --create-disk=auto-delete=yes,boot=yes,device-name=davidzhu-instance-1,image=projects/ubuntu-os-cloud/global/images/ubuntu-2004-focal-v20220927,mode=rw,size=50,type=projects/quark-serverless/zones/us-central1-a/diskTypes/pd-ssd --no-shielded-secure-boot --shielded-vtpm --shielded-integrity-monitoring --reservation-affinity=any &
else
	echo -e "instance: $inst already exist"
fi


# using for loop to create node agentinstance, for example: davidzhu-nodeagent1, 2, 3...
# instance_num=2
for ((i = 1; i<=$instance_num; i++))
do
    instance_name='davidzhu-nodeagent-'$i
    echo -e "created $instance_name \n"
    gcloud compute instances create $instance_name --project=quark-serverless --zone=us-central1-a --machine-type=e2-medium --network-interface=network-tier=PREMIUM,subnet=default --maintenance-policy=MIGRATE --provisioning-model=STANDARD --service-account=$account_number-compute@developer.gserviceaccount.com --scopes=https://www.googleapis.com/auth/cloud-platform --tags=http-server,https-server --create-disk=auto-delete=yes,boot=yes,device-name=davidzhu-instance-1,image=projects/ubuntu-os-cloud/global/images/ubuntu-2004-focal-v20220927,mode=rw,size=50,type=projects/quark-serverless/zones/us-central1-a/diskTypes/pd-ssd --no-shielded-secure-boot --shielded-vtpm --shielded-integrity-monitoring --reservation-affinity=any &

done

echo "all instance created successfully."