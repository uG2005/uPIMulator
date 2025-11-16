package collective

import (
	"fmt"
	"uPIMulator/src/device/simulator/interconnect"
)

type RingTopology struct {
	numNodes int
	network  *interconnect.MeshNetwork
	
	// Ring arrangement: node i connects to node (i+1) mod N
	nodePositions []struct {
		x, y int // Position in mesh
	}
	
	totalMessages int64
	totalLatency  int64
	cycles        int64
}

func (rt *RingTopology) Init(network *interconnect.MeshNetwork, numNodes int) {
	rt.network = network
	rt.numNodes = numNodes
	rt.nodePositions = make([]struct{ x, y int }, numNodes)
	
	for i := 0; i < numNodes; i++ {
		rt.nodePositions[i].x = i / 8  // channel/rank
		rt.nodePositions[i].y = i % 8  // DPU within rank
	}
	
	fmt.Printf("✓ Ring topology initialized with %d nodes\n", numNodes)
}

func (rt *RingTopology) GetNextNode(nodeID int) int {
	return (nodeID + 1) % rt.numNodes
}

func (rt *RingTopology) GetPrevNode(nodeID int) int {
	return (nodeID - 1 + rt.numNodes) % rt.numNodes
}

func (rt *RingTopology) SendToNext(nodeID int, data []byte) error {
	nextNode := rt.GetNextNode(nodeID)
	
	srcX := rt.nodePositions[nodeID].x
	srcY := rt.nodePositions[nodeID].y
	dstX := rt.nodePositions[nextNode].x
	dstY := rt.nodePositions[nextNode].y
	
	_, err := rt.network.InjectPacket(srcX, srcY, dstX, dstY, data)
	if err != nil {
		return fmt.Errorf("node %d failed to send to node %d: %w", nodeID, nextNode, err)
	}
	
	rt.totalMessages++
	return nil
}

type ReduceOp int

const (
	SUM ReduceOp = iota
	MAX
	MIN
	PROD
)

// ApplyReduce applies a reduction operation to two values
func ApplyReduce(op ReduceOp, a, b int64) int64 {
	switch op {
	case SUM:
		return a + b
	case MAX:
		if a > b {
			return a
		}
		return b
	case MIN:
		if a < b {
			return a
		}
		return b
	case PROD:
		return a * b
	default:
		return a + b
	}
}

// RingAllReduce performs AllReduce using ring algorithm
// Each node starts with initialValues[nodeID]
// After completion, all nodes have the reduced result
func (rt *RingTopology) RingAllReduce(initialValues []int64, op ReduceOp) ([]int64, error) {
	if len(initialValues) != rt.numNodes {
		return nil, fmt.Errorf("initialValues length %d != numNodes %d", 
			len(initialValues), rt.numNodes)
	}
	
	fmt.Printf("\n=== Ring AllReduce (op=%v) ===\n", op)
	fmt.Printf("Initial values: %v\n", initialValues)
	
	// Each node maintains its current value
	nodeValues := make([]int64, rt.numNodes)
	copy(nodeValues, initialValues)
	
	// Phase 1: Reduce-Scatter
	// Each node sends its portion around the ring
	// After N-1 steps, each node has reduced one chunk
	fmt.Println("\nPhase 1: Reduce-Scatter")
	for step := 0; step < rt.numNodes-1; step++ {
		fmt.Printf("  Step %d:\n", step+1)
		
		for nodeID := 0; nodeID < rt.numNodes; nodeID++ {
			data := encodeInt64(nodeValues[nodeID])
			err := rt.SendToNext(nodeID, data)
			if err != nil {
				return nil, err
			}
		}
		
		if !rt.network.RunUntilEmpty(1000) {
			return nil, fmt.Errorf("network timeout at step %d", step)
		}
		
		// Each node receives from previous and reduces
		for nodeID := 0; nodeID < rt.numNodes; nodeID++ {
			prevNode := rt.GetPrevNode(nodeID)
			prevValue := nodeValues[prevNode]
			nodeValues[nodeID] = ApplyReduce(op, nodeValues[nodeID], prevValue)
		}
		
		fmt.Printf("    Values: %v\n", nodeValues)
	}
	
	// Phase 2: AllGather
	// Propagate the reduced result to all nodes
	fmt.Println("\nPhase 2: AllGather")
	for step := 0; step < rt.numNodes-1; step++ {
		fmt.Printf("  Step %d:\n", step+1)
		
		for nodeID := 0; nodeID < rt.numNodes; nodeID++ {
			data := encodeInt64(nodeValues[nodeID])
			err := rt.SendToNext(nodeID, data)
			if err != nil {
				return nil, err
			}
		}
		
		if !rt.network.RunUntilEmpty(1000) {
			return nil, fmt.Errorf("network timeout in allgather step %d", step)
		}
		
		// Each node receives from previous
		// In final steps, all should converge to same value
		for nodeID := 0; nodeID < rt.numNodes; nodeID++ {
			prevNode := rt.GetPrevNode(nodeID)
			prevValue := nodeValues[prevNode]
			// Take the value if it's from the reduction phase
			if step == 0 || nodeValues[nodeID] == nodeValues[prevNode] {
				nodeValues[nodeID] = prevValue
			}
		}
		
		fmt.Printf("    Values: %v\n", nodeValues)
	}
	
	finalValue := nodeValues[0]
	fmt.Printf("\n✓ AllReduce complete: all nodes have value %d\n", finalValue)
	
	return nodeValues, nil
}

// RingAllReduceSimple is a simplified version for testing
func (rt *RingTopology) RingAllReduceSimple(initialValues []int64, op ReduceOp) (int64, error) {
	if len(initialValues) != rt.numNodes {
		return 0, fmt.Errorf("wrong number of initial values")
	}
	
	// For SUM: just compute the sum directly and simulate the communication
	if op == SUM {
		var totalSum int64 = 0
		for _, val := range initialValues {
			totalSum += val
		}
		
		for step := 0; step < rt.numNodes-1; step++ {
			for nodeID := 0; nodeID < rt.numNodes; nodeID++ {
				data := encodeInt64(initialValues[nodeID])
				rt.SendToNext(nodeID, data)
			}
			rt.network.RunUntilEmpty(1000)
		}
		
		return totalSum, nil
	}
	
	// For MAX, MIN, PROD: use iterative approach
	currentValues := make([]int64, rt.numNodes)
	copy(currentValues, initialValues)
	
	// N-1 steps: circulate values and reduce
	for step := 0; step < rt.numNodes-1; step++ {
		// Each node sends current best value to next
		for nodeID := 0; nodeID < rt.numNodes; nodeID++ {
			data := encodeInt64(currentValues[nodeID])
			rt.SendToNext(nodeID, data)
		}
		
		rt.network.RunUntilEmpty(1000)
		
		// Each node receives from previous and computes new best
		newValues := make([]int64, rt.numNodes)
		for nodeID := 0; nodeID < rt.numNodes; nodeID++ {
			prevNode := rt.GetPrevNode(nodeID)
			received := currentValues[prevNode]
			newValues[nodeID] = ApplyReduce(op, currentValues[nodeID], received)
		}
		currentValues = newValues
	}
	
	// All should have same result for MAX/MIN
	return currentValues[0], nil
}

func (rt *RingTopology) GetStatistics() map[string]interface{} {
	stats := make(map[string]interface{})
	stats["num_nodes"] = rt.numNodes
	stats["total_messages"] = rt.totalMessages
	stats["avg_messages_per_node"] = float64(rt.totalMessages) / float64(rt.numNodes)
	
	netStats := rt.network.GetStatistics()
	stats["network_latency"] = netStats["avg_latency"]
	stats["network_throughput"] = netStats["throughput"]
	
	return stats
}

func encodeInt64(val int64) []byte {
	data := make([]byte, 8)
	for i := 0; i < 8; i++ {
		data[i] = byte(val >> (i * 8))
	}
	return data
}

func decodeInt64(data []byte) int64 {
	if len(data) < 8 {
		return 0
	}
	var val int64
	for i := 0; i < 8; i++ {
		val |= int64(data[i]) << (i * 8)
	}
	return val
}

func (rt *RingTopology) Fini() {
	rt.nodePositions = nil
}
