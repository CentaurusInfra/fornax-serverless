sudo service etcd stop
sudo service etcd status
sudo rm -rf /var/lib/etcd/default/member/wal
sudo rm -rf /var/lib/etcd/default/member/snap
sudo service etcd start
sudo service etcd status
