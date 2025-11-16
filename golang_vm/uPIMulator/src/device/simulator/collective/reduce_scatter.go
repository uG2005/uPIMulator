package collective

import (
	"fmt"
	"uPIMulator/src/device/simulator/interconnect"
)

type ReduceScatterTopology struct {
	numNodes int
	network  *interconnect.MeshNetwork
	
	nodePositions []struct {
		x, y int
	}
	
	totalMessages int64
	cycles        int64
}

func (rst *ReduceScatterTopology) Init(network *interconnect.MeshNetwork, numNodes int) {
	rst.network = network
	rst.numNodes = numNodes
	rst.nodePositions = make([]struct{ x, y int }, numNodes)
	
	for i := 0; i < numNodes; i++ {
		rst.nodePositions[i].x = i / 8
		rst.nodePositions[i].y = i % 8
	}
	
	fmt.Printf("✓ Reduce-Scatter topology initialized: %d nodes\n", numNodes)
}

// After operation, each node has ONE reduced value (its assigned chunk)
// 
// Example with 4 nodes, each with 4 values:
// Node 0: [10, 20, 30, 40]
// Node 1: [1,  2,  3,  4]
// Node 2: [5,  6,  7,  8]
// Node 3: [9,  10, 11, 12]
//
// After Reduce-Scatter (SUM):
// Node 0 gets: 10+1+5+9 = 25    (sum of index 0 from all nodes)
// Node 1 gets: 20+2+6+10 = 38   (sum of index 1 from all nodes)
// Node 2 gets: 30+3+7+11 = 51   (sum of index 2 from all nodes)
// Node 3 gets: 40+4+8+12 = 64   (sum of index 3 from all nodes)

func (rst *ReduceScatterTopology) ReduceScatter(
	initialData [][]int64, // [nodeID][values]
	op ReduceOp,
) ([]int64, error) {
	
	if len(initialData) != rst.numNodes {
		return nil, fmt.Errorf("initialData length %d != numNodes %d", 
			len(initialData), rst.numNodes)
	}
	
	chunkSize := len(initialData[0])
	for i := 1; i < rst.numNodes; i++ {
		if len(initialData[i]) != chunkSize {
			return nil, fmt.Errorf("node %d has %d values, expected %d", 
				i, len(initialData[i]), chunkSize)
		}
	}
	
	fmt.Printf("\n=== Reduce-Scatter (op=%v) ===\n", op)
	fmt.Printf("Nodes: %d, Values per node: %d\n", rst.numNodes, chunkSize)
	
	result := make([]int64, rst.numNodes)
	
	// Each node is responsible for reducing one chunk (index = nodeID)
	for step := 0; step < rst.numNodes-1; step++ {
		fmt.Printf("\nStep %d:\n", step+1)
		
		// Each node sends its data to next node in ring
		for nodeID := 0; nodeID < rst.numNodes; nodeID++ {
			nextNode := (nodeID + 1) % rst.numNodes
			
			// Send data to next node
			srcX := rst.nodePositions[nodeID].x
			srcY := rst.nodePositions[nodeID].y
			dstX := rst.nodePositions[nextNode].x
			dstY := rst.nodePositions[nextNode].y
			
			// Encode the chunk this node is working on
			data := encodeInt64Array(initialData[nodeID])
			_, err := rst.network.InjectPacket(srcX, srcY, dstX, dstY, data)
			if err != nil {
				return nil, fmt.Errorf("send failed at step %d: %w", step, err)
			}
			
			rst.totalMessages++
		}
		
		if !rst.network.RunUntilEmpty(1000) {
			return nil, fmt.Errorf("network timeout at step %d", step)
		}
		
		// Each node reduces received data with its own
		// Node N is responsible for reducing chunk N
		newData := make([][]int64, rst.numNodes)
		for nodeID := 0; nodeID < rst.numNodes; nodeID++ {
			prevNode := (nodeID - 1 + rst.numNodes) % rst.numNodes
			
			// Reduce: combine received chunk with own chunk
			newData[nodeID] = make([]int64, chunkSize)
			for i := 0; i < chunkSize; i++ {
				newData[nodeID][i] = ApplyReduce(op, 
					initialData[nodeID][i], 
					initialData[prevNode][i])
			}
		}
		initialData = newData
	}
	
	// Phase 2: Each node extracts its final result
	// Node N gets the value at index N
	for nodeID := 0; nodeID < rst.numNodes; nodeID++ {
		result[nodeID] = initialData[nodeID][nodeID]
	}
	
	fmt.Printf("\n✓ Reduce-Scatter complete\n")
	fmt.Printf("Results: %v\n", result)
	
	return result, nil
}

// ReduceScatterSimple is a simplified synchronous version
func (rst *ReduceScatterTopology) ReduceScatterSimple(
	initialData [][]int64,
	op ReduceOp,
) ([]int64, error) {
	
	if len(initialData) != rst.numNodes {
		return nil, fmt.Errorf("wrong data size")
	}
	
	chunkSize := len(initialData[0])
	
	// Simple approach: reduce each chunk independently
	result := make([]int64, rst.numNodes)
	
	for chunkIdx := 0; chunkIdx < chunkSize; chunkIdx++ {
		// Reduce values from all nodes at this chunk position
		var reducedValue int64
		
		if op == SUM {
			reducedValue = 0
			for nodeID := 0; nodeID < rst.numNodes; nodeID++ {
				reducedValue += initialData[nodeID][chunkIdx]
			}
		} else {
			reducedValue = initialData[0][chunkIdx]
			for nodeID := 1; nodeID < rst.numNodes; nodeID++ {
				reducedValue = ApplyReduce(op, reducedValue, initialData[nodeID][chunkIdx])
			}
		}
		
		if chunkIdx < rst.numNodes {
			result[chunkIdx] = reducedValue
		}
	}
	
	for step := 0; step < rst.numNodes-1; step++ {
		for nodeID := 0; nodeID < rst.numNodes; nodeID++ {
			nextNode := (nodeID + 1) % rst.numNodes
			
			srcX := rst.nodePositions[nodeID].x
			srcY := rst.nodePositions[nodeID].y
			dstX := rst.nodePositions[nextNode].x
			dstY := rst.nodePositions[nextNode].y
			
			data := encodeInt64(int64(nodeID))
			rst.network.InjectPacket(srcX, srcY, dstX, dstY, data)
			rst.totalMessages++
		}
		rst.network.RunUntilEmpty(1000)
	}
	
	return result, nil
}

// AllGather performs the inverse of Reduce-Scatter
// Each node starts with one value, ends with all values
func (rst *ReduceScatterTopology) AllGather(
	initialValues []int64,
) ([][]int64, error) {
	
	if len(initialValues) != rst.numNodes {
		return nil, fmt.Errorf("wrong number of values")
	}
	
	fmt.Printf("\n=== AllGather ===\n")
	fmt.Printf("Initial: %v\n", initialValues)
	
	// Simplified: just return same values to all nodes
	result := make([][]int64, rst.numNodes)
	for i := 0; i < rst.numNodes; i++ {
		result[i] = make([]int64, rst.numNodes)
		// Each node gets all values
		for j := 0; j < rst.numNodes; j++ {
			result[i][j] = initialValues[j]
		}
	}
	
	// Simulate communication
	for step := 0; step < rst.numNodes-1; step++ {
		for nodeID := 0; nodeID < rst.numNodes; nodeID++ {
			nextNode := (nodeID + 1) % rst.numNodes
			
			srcX := rst.nodePositions[nodeID].x
			srcY := rst.nodePositions[nodeID].y
			dstX := rst.nodePositions[nextNode].x
			dstY := rst.nodePositions[nextNode].y
			
			data := encodeInt64(initialValues[nodeID])
			rst.network.InjectPacket(srcX, srcY, dstX, dstY, data)
			rst.totalMessages++
		}
		rst.network.RunUntilEmpty(1000)
	}
	
	fmt.Printf("✓ AllGather complete\n")
	
	return result, nil
}

func (rst *ReduceScatterTopology) GetStatistics() map[string]interface{} {
	stats := make(map[string]interface{})
	stats["num_nodes"] = rst.numNodes
	stats["total_messages"] = rst.totalMessages
	stats["avg_messages_per_node"] = float64(rst.totalMessages) / float64(rst.numNodes)
	
	netStats := rst.network.GetStatistics()
	stats["network_latency"] = netStats["avg_latency"]
	stats["network_throughput"] = netStats["throughput"]
	
	return stats
}

func encodeInt64Array(values []int64) []byte {
	data := make([]byte, len(values)*8)
	for i, val := range values {
		for j := 0; j < 8; j++ {
			data[i*8+j] = byte(val >> (j * 8))
		}
	}
	return data
}

func decodeInt64Array(data []byte) []int64 {
	if len(data)%8 != 0 {
		return nil
	}
	
	count := len(data) / 8
	values := make([]int64, count)
	
	for i := 0; i < count; i++ {
		var val int64
		for j := 0; j < 8; j++ {
			val |= int64(data[i*8+j]) << (j * 8)
		}
		values[i] = val
	}
	
	return values
}

func (rst *ReduceScatterTopology) Fini() {
	rst.nodePositions = nil
}
