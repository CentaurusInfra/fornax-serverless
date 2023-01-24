for i in $(seq 1 $1)
do
  echo ./bin/simulatenode --num-of-node 500 --node-name-prefix wenjian-T15g-$i
  nohup ./bin/simulatenode  --fornaxcore-ip localhost:18001 --num-of-node 500 --node-name-prefix wenjian-T15g-$i >> node.log 2&
  sleep 1s
done
