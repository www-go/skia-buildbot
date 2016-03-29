#!/bin/bash
#
# Creates the compute instance for skia-fiddle.
#
set -x -e

source vm_config.sh

FIDDLE_MACHINE_TYPE=n1-standard-4
FIDDLE_SOURCE_SNAPSHOT=skia-systemd-pushable-base
FIDDLE_SCOPES='https://www.googleapis.com/auth/devstorage.full_control'
FIDDLE_IP_ADDRESS=104.154.112.126

# Create a boot disk from the pushable base snapshot.
gcloud compute --project $PROJECT_ID disks create $INSTANCE_NAME \
  --zone $ZONE \
  --source-snapshot $FIDDLE_SOURCE_SNAPSHOT \
  --type "pd-standard"

# Create a large data disk.
gcloud compute --project $PROJECT_ID disks create $INSTANCE_NAME"-data" \
  --size "1000" \
  --zone $ZONE \
  --type "pd-standard"

# Create the instance with the two disks attached.
gcloud compute --project $PROJECT_ID instances create $INSTANCE_NAME \
  --zone $ZONE \
  --machine-type $FIDDLE_MACHINE_TYPE \
  --network "default" \
  --maintenance-policy "MIGRATE" \
  --scopes $FIDDLE_SCOPES \
  --tags "http-server,https-server" \
  --metadata-from-file "startup-script=startup-script.sh" \
  --disk "name=${INSTANCE_NAME},device-name=${INSTANCE_NAME},mode=rw,boot=yes,auto-delete=yes" \
  --disk "name=${INSTANCE_NAME}-data,device-name=${INSTANCE_NAME}-data,mode=rw,boot=no" \
  --address=$FIDDLE_IP_ADDRESS

WAIT_TIME_AFTER_CREATION_SECS=600
echo
echo "===== Wait $WAIT_TIME_AFTER_CREATION_SECS secs for instance to" \
  "complete startup script. ====="
echo
sleep $WAIT_TIME_AFTER_CREATION_SECS

# The instance believes it is skia-systemd-snapshot-maker until it is rebooted.
echo
echo "===== Rebooting the instance ======"
# Using "shutdown -r +1" rather than "reboot" so that the connection isn't
# terminated immediately, which causes a non-zero exit code.
gcloud compute --project $PROJECT_ID ssh $PROJECT_USER@$INSTANCE_NAME \
  --zone $ZONE \
  --command "sudo shutdown -r +1" \
  || echo "Reboot failed; please reboot the instance manually."