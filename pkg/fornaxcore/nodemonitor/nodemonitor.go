package nodemonitor

import (
	v1 "k8s.io/api/core/v1"
	"math"
	"sync"
	"time"
)

// todo: determine more proper timeout
const StaleNodeTimeout = 10 * time.Second

type nodeMonitor struct {
	sync.RWMutex
	nodes map[string]*v1.Node

	// buckets based on refresh-ness; the last one contains the most recently refreshed nodes
	bucketHead  *bucket
	bucketTail  *bucket
	nodeLocator map[string]*bucket

	// the nodes just been updated in the latest ticker period
	// node should be added in on receipt of node status report
	refreshedNodes map[string]interface{}

	ticker      *time.Ticker
	checkPeriod time.Duration
	countFresh  int
	chQuit      chan interface{}
}

type bucket struct {
	prev, next *bucket
	elements   map[string]interface{}
}

func (n *nodeMonitor) StartDetectStaleNode() {
	go func() {
		for {
			select {
			case <-n.chQuit:
				// todo: log termination
				return
			case <-n.ticker.C:
				n.appendRefreshBucket()
				// todo: process the stale nodes properly
				_ = n.getStaleNodes()
			}
		}
	}()
}

func (n *nodeMonitor) StopDetectStaleNode() {
	close(n.chQuit)
}

func (n *nodeMonitor) replenishUpdatedNodes() map[string]interface{} {
	n.Lock()
	defer n.Unlock()

	oldCopy := n.refreshedNodes
	n.refreshedNodes = make(map[string]interface{})
	return oldCopy
}

func (n *nodeMonitor) appendRefreshBucket() {
	updatedNodes := n.replenishUpdatedNodes()

	n.RLock()
	defer n.RUnlock()
	newBucket := &bucket{
		prev:     n.bucketTail,
		next:     nil,
		elements: make(map[string]interface{}),
	}

	if n.bucketHead == nil {
		n.bucketHead = newBucket
	}
	if n.bucketTail == nil {
		n.bucketTail = newBucket
	} else {
		n.bucketTail.next = newBucket
		n.bucketTail = newBucket
	}

	for nodeName, _ := range updatedNodes {
		if _, ok := n.nodeLocator[nodeName]; ok {
			delete(n.nodeLocator[nodeName].elements, nodeName)
		}
		newBucket.elements[nodeName] = struct{}{}
		n.nodeLocator[nodeName] = newBucket
	}
}

func (n *nodeMonitor) getStaleNodes() []string {
	n.Lock()
	defer n.Unlock()

	var stales []string

	currBucket := n.bucketTail
	count := n.countFresh
	for {
		if currBucket == nil {
			break
		}

		if count > 0 {
			currBucket = currBucket.prev
			count--
			continue
		}

		for name, _ := range currBucket.elements {
			stales = append(stales, name)
		}
		currBucket = currBucket.prev
	}

	return stales
}

func New(checkPeriod time.Duration) *nodeMonitor {
	return &nodeMonitor{
		nodeLocator: map[string]*bucket{},
		checkPeriod: checkPeriod,
		countFresh:  int(math.Ceil(StaleNodeTimeout.Seconds() / checkPeriod.Seconds())),
		ticker:      time.NewTicker(checkPeriod),
		chQuit:      make(chan interface{}),
	}
}
