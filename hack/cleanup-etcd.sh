echo "Stoping etcd"
sudo service etcd stop
sudo service etcd status
echo "Cleanup etcd data"
sudo rm -rf /var/lib/etcd/default/member/wal
sudo rm -rf /var/lib/etcd/default/member/snap
echo "Starting etcd"
sudo service etcd start
sudo service etcd status
