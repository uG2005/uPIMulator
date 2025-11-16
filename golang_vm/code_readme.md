# 📋 Recent Work & Infrastructure Additions

## Interconnect Module for Inter-DPU Communication (Nov 5, 2025)
Initial interconnect module implementation enabling inter-DPU communication with DPU transfer tests and infrastructure. This provides the foundation for all subsequent collective communication features.

## Router and Mesh Network for PIMnet (Nov 5, 2025)

### Bufferless Router Implementation
- 5-port architecture (North/South/East/West/Local)
- Three routing algorithms: XY, YX, and West-First routing
- Backpressure mechanism for congestion control
- Cycle-accurate simulation with multi-hop packet routing
- Statistics tracking (routed packets, blocked packets, average hops)

### 2D Mesh Network
- Topology connecting 32 routers in a 4×8 grid
- Parallel router operation simulation
- Automatic packet delivery tracking
- Network-wide statistics (latency, throughput, congestion)
- Support for multiple concurrent packets

## Ring AllReduce Collective Primitive (Nov 5, 2025)
- Logical ring topology connecting up to 32 DPUs
- Ring traversal pattern: node i → node (i+1) mod N
- **AllReduce Operations:** SUM, MAX, MIN, PROD
- **Algorithm:** 2-phase approach
  - Reduce-Scatter phase: N-1 steps to distribute reduced chunks
  - AllGather phase: N-1 steps to propagate final result
- **Performance:** 
  - 4 nodes: ~38 μs per AllReduce
  - 32 nodes: ~1.3 ms per AllReduce
- 8 comprehensive tests passing

## Broadcast and Reduce-Scatter Collective Primitives (Nov 10, 2025)

### Broadcast Implementation
- Binary tree topology
- O(log N) latency for message propagation
- Optimized for up to 32 DPUs

### Reduce-Scatter Implementation
- Distributed gradient aggregation
- AllGather inverse operation
- Multiple reduction operations: SUM, MAX, MIN, PROD

**Test Coverage:** 24 collective operation tests passing (8 Ring AllReduce + 7 Broadcast + 9 Reduce-Scatter)

## Inter-Chip Switch with DQ Pin Partitioning (Nov 10, 2025)

### DQ Pin Partitioning
- Flexible bus partitioning (e.g., 64 pins → 8 channels × 8 bits)
- Independent per-channel bandwidth allocation
- Configurable channel count

### Crossbar Switch
- N×N switching matrix for any-to-any connectivity
- Conflict detection and blocking (bufferless approach)
- Comprehensive statistics tracking

### Inter-Chip Switch Integration
- Combines DQ partitioning with crossbar switching
- Multi-chip transfer management
- Cycle-accurate simulation with transfer tracking
- 13 tests passing

## Inter-Rank Control with DDR CA Bus Broadcast (Nov 11, 2025)
- DDR Command/Address bus as broadcast channel
- Broadcast to all ranks or selective ranks
- Barrier synchronization primitives for multi-rank coordination
- Rank state tracking and management
- 12 tests passing

**Microarchitecture Requirements Completion:**
- ✓ PIMnet Stop (router)
- ✓ Inter-chip switch
- ✓ Inter-rank control




# Code Documentation - golang_vm Interconnect & Collective Communication

This document provides comprehensive documentation of the interconnect and collective communication infrastructure added to the uPIMulator project. All code is located in `golang_vm/uPIMulator/src/device/simulator/`.



## Table of Contents

1. [Interconnect Module](#interconnect-module)
2. [Router Implementation](#router-implementation)
3. [Mesh Network](#mesh-network)
4. [Inter-Chip Switch](#inter-chip-switch)
5. [Inter-Rank Control](#inter-rank-control)
6. [Collective Operations](#collective-operations)
   - [Ring AllReduce](#ring-allreduce)
   - [Broadcast](#broadcast)
   - [Reduce-Scatter](#reduce-scatter)
7. [Test Coverage](#test-coverage)

---

## Interconnect Module

**File:** `interconnect/interconnect.go`

### Purpose
The Interconnect module manages data transfers between DPUs across different channels, ranks, and DPUs within the PIM hierarchy. It serves as the foundation for all inter-DPU communication.

### Key Components

#### TransferRequest Structure
```go
type TransferRequest struct {
    SrcChannelID int
    SrcRankID    int
    SrcDpuID     int
    DstChannelID int
    DstRankID    int
    DstDpuID     int
    Data         []byte
    Timestamp    int64
}
```
Encapsulates a data transfer request with source and destination coordinates in the 3D DPU hierarchy (Channel-Rank-DPU).

#### Interconnect Structure
- **sharedBuffer**: Map-based shared memory for inter-DPU communication
- **transferQueues**: Per-channel transfer request queues
- **Statistics**: Tracks total transfers, bytes transferred, and cycle counts
- **Configuration**: Stores network dimensions and bandwidth parameters

### Key Methods

#### Init(numChannels, numRanks, numDPUs, bandwidth)
Initializes the interconnect with the given topology dimensions and bandwidth constraints. Creates transfer queues for each channel and sets up the shared buffer.

#### Write(channelID, rankID, dpuID, data)
Writes data from a source DPU to the shared buffer. This is where DPUs put data they want to send to other DPUs.

#### Read(srcChannelID, srcRankID, srcDpuID)
Reads data that a source DPU has written to the shared buffer. Returns error if no data exists from that DPU.

#### Transfer()
Executes the actual data transfer based on bandwidth constraints. Moves data from source to destination buffers across cycle boundaries.

### How It Works

1. **Data Preparation**: Source DPU writes data to shared buffer using `Write()`
2. **Transfer Scheduling**: Transfer request is queued in the appropriate channel queue
3. **Cycle-Accurate Transfer**: `Transfer()` moves data respecting bandwidth limits (bytes per cycle)
4. **Data Retrieval**: Destination DPU reads data using `Read()`

---

## Router Implementation

**File:** `interconnect/router.go`

### Purpose
The Router implements a **bufferless, 5-port router** for inter-DPU mesh communication. Each router handles packet routing using configurable algorithms and manages backpressure to prevent packet loss.

### Key Concepts

#### Port Architecture
Each router has 5 ports:
- **NORTH**: Connection upward in mesh
- **SOUTH**: Connection downward in mesh
- **EAST**: Connection rightward in mesh
- **WEST**: Connection leftward in mesh
- **LOCAL**: Connection to local DPU

Each port has input and output sides, modeled as RouterPort structures.

#### Packet Structure
```go
type Packet struct {
    SrcChannelID int
    SrcRankID    int
    SrcDpuID     int
    DstChannelID int
    DstRankID    int
    DstDpuID     int
    Data         []byte
    HopCount     int
    CurrentX     int
    CurrentY     int
    Timestamp    int64
}
```
Represents a data packet routed through the mesh with source, destination, payload, and hop tracking.

### Routing Algorithms

#### 1. XY Routing (Deterministic)
- Route in X direction first, then Y
- Always moves west/east before north/south
- Guaranteed deadlock-free for mesh networks
- Algorithm:
  - If ΔX > 0: go EAST
  - If ΔX < 0: go WEST
  - If ΔX = 0 and ΔY > 0: go NORTH
  - If ΔX = 0 and ΔY < 0: go SOUTH

#### 2. YX Routing (Deterministic)
- Route in Y direction first, then X
- Opposite of XY routing
- Also deadlock-free
- Used as an alternative for load balancing

#### 3. West-First Routing (Turn Model)
- Prioritizes westward (leftward) movement
- Only allows turns after moving west or if destination is east
- Reduces deadlock probability in heavily loaded networks

### Key Methods

#### Init(posX, posY, algorithm)
Initializes the router at a specific mesh position with the chosen routing algorithm.

#### ComputeNextHop(packet)
**The routing brain of the system.** Determines which output port (NORTH/SOUTH/EAST/WEST/LOCAL) the packet should be sent to based on:
1. Current router position (PositionX, PositionY)
2. Destination DPU coordinates
3. Routing algorithm selected

Returns LOCAL if packet has reached destination.

#### ReceivePacket(packet, incomingPort)
Attempts to place incoming packet on input port. Returns false if port is busy (backpressure).

#### Cycle()
Per-cycle router operation:
1. **Route Decision**: Uses `ComputeNextHop()` for each input packet
2. **Arbitration**: If multiple packets want same output, prioritize (usually oldest first)
3. **Port Transfer**: Move packet from input to output port
4. **Backpressure**: If output port blocked, hold packet (contributes to hop delay)

### How It Works

```
Packet Flow Through Router:
1. Packet arrives at input port (e.g., from SOUTH via previous router)
2. Router examines destination coordinates
3. Routing algorithm computes next hop (e.g., EAST)
4. Check if EAST output port is available
5. If available: transfer packet to EAST output port
6. If not available: hold packet at input (backpressure), try again next cycle
7. Packet ready to transfer to EAST neighbor next cycle
```

### Statistics Tracked
- `packetsRouted`: Successfully routed packets
- `packetsBlocked`: Packets experiencing backpressure
- `totalHops`: Sum of all packet hops
- `cycles`: Cycle count

---

## Mesh Network

**File:** `interconnect/mesh_network.go`

### Purpose
The MeshNetwork coordinates multiple routers to form a complete 2D mesh topology. It handles packet injection, delivery tracking, and network-wide statistics.

### Key Concepts

#### Network Topology
- **Grid Size**: Configurable width × height (typically 4×8 = 32 routers)
- **Position Mapping**:
  - X-axis: Channel and Rank indices
  - Y-axis: DPU indices within rank
- **Example**: 32 routers = 4 channels × 8 DPUs per rank

#### Packet Lifecycle in MeshNetwork

```
User -> InjectPacket() -> Router[srcX][srcY] input port
     -> Router cycles and routes packet
     -> Transfer to neighbor router's input port
     -> Neighbor routes packet
     -> Eventually packet reaches Router[dstX][dstY]
     -> Packet delivered to LOCAL DPU
```

### Key Methods

#### Init(width, height, algorithm)
Creates a 2D grid of routers at every (x, y) position and initializes each with the specified routing algorithm.

#### InjectPacket(srcX, srcY, dstX, dstY, data)
Injects a packet from a source DPU into the network:
1. Validates source and destination coordinates
2. Creates Packet structure with timestamp
3. Injects packet into LOCAL input port of source router
4. Returns packet ID for tracking

#### Cycle()
Simulates one network cycle:
1. **Phase 1**: All routers execute their per-cycle routing logic in parallel
2. **Phase 2**: Transfer packets between adjacent routers
   - Check each router's output ports
   - Transfer east packets to neighbor's west input
   - Transfer west packets to neighbor's east input
   - Transfer north packets to neighbor's south input
   - Transfer south packets to neighbor's north input

#### IsPacketDelivered(packetID)
Checks if a packet has reached its destination and been delivered to LOCAL DPU.

#### GetPacketLatency(packetID)
Returns the number of cycles from injection to delivery for a packet.

#### RunUntilEmpty(maxCycles)
Runs the network for up to maxCycles or until all packets are delivered. Used for testing.

### Statistics
- `totalPacketsDelivered`: Successfully delivered packets
- `totalPacketLatency`: Sum of all packet latencies
- `totalPacketsInjected`: Total packets ever injected
- Network-wide latency tracking

---

## Inter-Chip Switch

**File:** `interconnect/inter_chip_switch.go`

### Purpose
Models inter-chip communication by partitioning the DDR data (DQ) bus and implementing an N×N crossbar switch for multi-chip PIM systems.

### Key Concepts

#### DQ Pin Partitioning

**What is DQ?** Data (DQ) pins on DDR memory modules carry the actual data signals.

**How Partitioning Works**: Instead of one wide data bus (e.g., 64 bits), partition it into N narrower channels:
- **Total Pins**: 64 (example for DDR4)
- **Partition Count**: 8 channels
- **Pins Per Channel**: 8 bits per channel
- **Advantage**: Allows independent data transfers on each channel

#### CrossbarSwitch

**Purpose**: An N×N switching matrix allowing any input to connect to any output.

**Bufferless Operation**: Packets cannot be buffered; connections block if output is busy.

**Connection Management**:
- `connections[inputID]`: Current output connected to this input (-1 if not connected)
- `reverseConnections[outputID]`: Current input connected to this output (-1 if not connected)
- Ensures single connection per input/output (no multiplexing)

#### InterChipSwitch

Combines DQ partitioning with crossbar switching:
- Multiple independent transfer channels
- Each channel has its own partition of pins and crossbar entries
- Enables parallel multi-chip data transfers

### Key Data Structures

#### DQPinPartition
```go
type DQPinPartition struct {
    totalPins      int        // e.g., 64
    numChannels    int        // e.g., 8
    pinsPerChannel int        // Calculated: 64/8 = 8
    channelPins    map[int][]int // Maps channel -> pin numbers
}
```

#### CrossbarSwitch
```go
type CrossbarSwitch struct {
    numInputs          int   // Number of input ports
    numOutputs         int   // Number of output ports
    connections        []int // inputID -> outputID mapping
    reverseConnections []int // outputID -> inputID mapping
    totalSwitches      int64 // Statistics
    blockedAttempts    int64 // Statistics
}
```

#### InterChipSwitch
```go
type InterChipSwitch struct {
    dqPartition *DQPinPartition    // Manages pin allocation
    switchMatrix *CrossbarSwitch    // Routes transfers
    transferCommands []*TransferCommand // In-flight transfers
    cycles       int64
}
```

### Key Methods

#### DQPinPartition.Init(totalPins, numChannels)
Partitions total pins evenly across channels. For 64 pins and 8 channels:
- Channel 0 gets pins [0, 1, ..., 7]
- Channel 1 gets pins [8, 9, ..., 15]
- etc.

#### CrossbarSwitch.Connect(inputID, outputID)
Attempts to establish a connection:
- Returns false if output already connected (blocking)
- Disconnects previous output if input already connected
- Enables dynamic reconfiguration

#### InterChipSwitch.TransferData(srcChip, dstChip, data)
Initiates a transfer:
1. Checks if crossbar can connect source to destination
2. If yes: queues TransferCommand
3. If no: returns error (backpressure)

#### InterChipSwitch.Cycle()
Per-cycle operation:
1. Process queued transfers
2. Move data according to bandwidth constraints
3. Complete transfers and free connections
4. Update statistics

### How It Works

```
Transfer Flow:
1. DMA requests inter-chip transfer
2. CrossbarSwitch.Connect(src, dst)
3. If connection succeeds:
   - Data begins transferring on allocated DQ pins
   - Transfer progresses each cycle based on bandwidth
4. If connection blocked (output busy):
   - Transfer request queued or rejected
   - Backpressure applied to source
5. After all data transferred:
   - Connection freed for other transfers
   - Statistics updated
```

---

## Inter-Rank Control

**File:** `interconnect/inter_rank_control.go`

### Purpose
Manages multi-rank synchronization and broadcast operations using the DDR Command/Address (CA) bus as a broadcast channel. Enables barrier synchronization and selective rank communication.

### Key Concepts

#### DDR CA Bus as Broadcast Channel
- **Traditional Use**: Command and Address signals for memory operations
- **New Use**: Repurposed as broadcast channel for inter-rank control messages
- **Advantage**: Inherently connects all ranks, perfect for broadcast
- **Signal**: Low latency, independent from data path (DQ)

#### Broadcast vs Point-to-Point
- **Broadcast**: One rank sends to all ranks in same channel (-1 destination)
- **Point-to-Point**: One rank sends to specific destination rank
- **Selective Broadcast**: Future: Send to subset of ranks

### Key Data Structures

#### InterRankMessage
```go
type InterRankMessage struct {
    SourceRank      int     // Sending rank
    DestinationRank int     // -1 for broadcast, or specific rank ID
    Data            []byte  // Message payload
    Timestamp       int64   // When message sent
    MessageID       int     // Unique identifier
}
```

#### InterRankControlQ
```go
type InterRankControlQ struct {
    numChannels       int                          // PIM channels
    numRanks          int                          // Ranks per channel
    busWidth          int                          // CA bus width (bytes)
    bandwidth         int64                        // Message bytes per cycle
    messageQueue      []*InterRankMessage          // Outgoing messages
    pendingMessages   map[string][]*InterRankMessage // Per-rank receive buffers
    statistics        ...
}
```

### Key Methods

#### Init(numChannels, numRanks, busWidth, bandwidth)
Initializes the inter-rank control infrastructure with given dimensions and bandwidth.

#### Broadcast(channelID, sourceRank, data)
Sends a message from sourceRank to all ranks in the channel via CA bus broadcast:
1. Create InterRankMessage with DestinationRank = -1
2. Add to message queue
3. Update statistics

#### SendPointToPoint(srcChannelID, srcRank, dstChannelID, dstRank, data)
Sends a message from one rank to specific rank:
1. Create InterRankMessage with specific destination
2. Add to message queue
3. Message routed through CA bus

#### ReceiveMessage(channelID, rankID)
Receives next pending message for a rank:
1. Look up pending messages for rank
2. Return oldest message
3. Remove from pending list

#### Barrier(channelID)
Implements barrier synchronization for all ranks in a channel:
1. Each rank sends BARRIER_REACHED message
2. Broadcast via CA bus
3. Each rank waits to receive BARRIER_REACHED from all other ranks
4. All ranks proceed once all reach barrier

#### Cycle()
Per-cycle operation:
1. Process outgoing messages from queue
2. Calculate delivery based on bandwidth
3. Deliver messages to destination rank buffers
4. Update statistics

### How It Works

```
Broadcast Example:
1. Rank 0 calls Broadcast(0, 0, "sync_data")
2. Message queued on CA bus
3. CA bus signals all ranks simultaneously (broadcast)
4. Ranks 0,1,2,3 all receive "sync_data" from Rank 0
5. Each rank can retrieve message via ReceiveMessage()

Barrier Example:
1. All ranks call Barrier(channel)
2. Each rank broadcasts BARRIER_REACHED via CA bus
3. Rank 0 receives broadcasts from ranks 0,1,2,3
4. Rank 1 receives broadcasts from ranks 0,1,2,3
5. ... (all ranks receive all BARRIER_REACHED messages)
6. All ranks unblock and proceed
```

### Statistics
- `totalMessages`: All messages sent (broadcast + point-to-point)
- `totalBroadcasts`: Only broadcast messages
- `totalBytesTransferred`: Sum of all message sizes
- Per-rank delivery tracking

---

## Collective Operations

Collective operations are coordinated multi-rank communication patterns where all DPUs participate in a synchronized computation.

### Ring AllReduce

**File:** `collective/ring.go`

#### Purpose
Performs reduction operations (SUM, MAX, MIN, PROD) across all DPUs using a ring topology. Each DPU has an initial value; after operation, all DPUs have the same reduced result.

#### Ring Topology Concept
- DPUs arranged in logical ring: 0 → 1 → 2 → ... → N-1 → 0
- Each DPU's next node in ring: `next = (id + 1) % N`
- Ring mapped to mesh network positions for actual routing

#### Algorithm: Ring AllReduce

**Phase 1: Reduce-Scatter** (N-1 steps)
```
Each step:
1. Every DPU sends its current value to next DPU in ring
2. Network delivers packets (multi-hop through mesh)
3. Each DPU receives from previous DPU
4. Each DPU applies reduction: value = reduce(my_value, received_value)

After N-1 steps:
- Each DPU has a different chunk of reduced data
- DPU i has reduction of elements [i, i+N-1, i+2N-1, ...]
```

**Phase 2: AllGather** (N-1 steps)
```
Each step:
1. Every DPU sends its current value to next DPU
2. Each DPU receives and keeps track (updates don't reduce)

After N-1 steps:
- Each DPU has received all reduced chunks
- All DPUs converge to same final reduced value
```

#### Key Data Structures

```go
type RingTopology struct {
    numNodes      int                      // Number of DPUs
    network       *interconnect.MeshNetwork
    nodePositions []struct{ x, y int }    // Position in mesh
}
```

#### Reduction Operations

```go
type ReduceOp int

const (
    SUM  ReduceOp = iota  // a + b
    MAX                    // max(a, b)
    MIN                    // min(a, b)
    PROD                   // a * b
)

func ApplyReduce(op ReduceOp, a, b int64) int64 {
    // Applies operation to two values
}
```

#### Key Methods

#### Init(network, numNodes)
Initializes ring topology by mapping DPU IDs to mesh positions:
- Node 0 → Position (0, 0)
- Node 1 → Position (0, 1)
- ...
- Node 31 → Position (3, 7) [for 4×8 mesh]

#### GetNextNode(nodeID)
Returns next node in ring: `(nodeID + 1) % numNodes`

#### RingAllReduce(initialValues, op)
Executes complete ring allreduce algorithm:
1. Validate input array length
2. **Reduce-Scatter Phase**:
   - Each node sends to next (N-1 iterations)
   - Network runs until all packets delivered
   - Each node reduces received value
3. **AllGather Phase**:
   - Each node sends to next (N-1 iterations)
   - Network runs until all packets delivered
   - Each node collects final result
4. Return array where result[i] = final reduced value for all i

#### Performance
- 4 DPUs: ~38 μs
- 32 DPUs: ~1.3 ms

---

### Broadcast

**File:** `collective/broadcast.go`

#### Purpose
Broadcasts data from a root DPU to all other DPUs using a binary tree topology. Achieves O(log N) latency.

#### Binary Tree Topology
```
        Root (Node 0)
       /             \
    Node 1           Node 2
    /    \          /    \
  Node 3 Node 4  Node 5 Node 6
  ...
```

**Parent-Child Relationships**:
- Parent of node i: `(i - 1) / 2`
- Children of node i: `[2i+1, 2i+2]` (left and right children)
- Tree depth: O(log N)

#### Algorithm: Binary Tree Broadcast

```
1. Root (Node 0) has data to broadcast
2. Level 0 to 1:
   - Root sends to children (Node 1, Node 2)
3. Level 1 to 2:
   - Node 1 sends to children (Node 3, Node 4)
   - Node 2 sends to children (Node 5, Node 6)
4. ... continue level by level
5. After log N steps, all nodes have data

Latency: O(log N) tree depth * mesh hop latency
```

#### Key Data Structures

```go
type BroadcastTopology struct {
    numNodes        int
    network         *interconnect.MeshNetwork
    branchingFactor int              // 2 for binary tree
    nodePositions   []struct{ x, y int }
}
```

#### Key Methods

#### Init(network, numNodes)
Initializes binary tree topology (branching factor = 2).

#### GetParent(nodeID)
Returns parent node in tree: `(nodeID - 1) / 2` (or -1 for root)

#### GetChildren(nodeID)
Returns children nodes:
- Left child: `2 * nodeID + 1`
- Right child: `2 * nodeID + 2`

#### GetTreeDepth()
Calculates maximum tree depth = O(log N)

#### Broadcast(rootID, data)
Executes tree broadcast from root to all nodes:
1. Initialize queue with root node
2. Level-by-level processing:
   - For each node in current level
   - Send data to all children via mesh
   - Wait for delivery
   - Move to next level
3. After log N levels, all nodes received data

#### Performance
- Latency: O(log N) * mesh_hop_latency
- Example: 32 DPUs in 4×8 mesh: ~5 hops + network delays

---

### Reduce-Scatter

**File:** `collective/reduce_scatter.go`

#### Purpose
Reduces data across all DPUs and scatters (distributes) the reduced chunks back. Each DPU ends up with one chunk of the reduction result. Inverse operation of AllGather.

#### Use Cases
- Distributed gradient aggregation in ML training
- Partial result reduction in data-parallel computing
- Preparation for subsequent AllGather operations

#### Algorithm: Ring Reduce-Scatter

```
Initial State: Each DPU has N data elements
DPU i has: [d[i,0], d[i,1], ..., d[i,N-1]]

Phase 1: N-1 reduce steps (ring pattern)
  Step 1: Each DPU sends elements to next, receives from previous
          DPU i reduces: d[i,0] = reduce(my_d[i,0], prev_d[i,0])
  Step 2: Send next chunk, reduce again
  ...
  Step N-1: All chunks processed

Final State: Each DPU has reduced ONE chunk
DPU i ends up with reduced value of element chunk i
```

#### Key Data Structures

```go
type ReduceScatterTopology struct {
    numNodes int
    network  *interconnect.MeshNetwork
    nodePositions []struct{ x, y int }
}
```

#### Key Methods

#### Init(network, numNodes)
Initializes reduce-scatter topology on mesh network.

#### ReduceScatter(inputArrays, op)
Executes reduce-scatter algorithm:
1. **Input**: `inputArrays[i]` = array of values for DPU i
2. **Processing**: Ring reduce-scatter with N-1 steps
3. **Output**: Each DPU i receives `result[i]` = reduced value of chunk i

#### AllGather(scatteredData)
Inverse operation: Takes scattered chunks and gathers back to all DPUs.

---

## Test Coverage

**Files:**
- `interconnect/interconnect_test.go`
- `interconnect/router_test.go`
- `interconnect/mesh_network_test.go`
- `interconnect/inter_chip_switch_test.go`
- `interconnect/inter_rank_control_test.go`
- `collective/collective_test.go`
- `dpu/dpu_transfer_test.go`

### Test Examples

#### Interconnect Tests
- `TestInterconnect`: Basic write/read functionality
- `TestBasicTransfer`: Single transfer between DPUs
- `TestMultiDPU`: Multiple concurrent transfers
- `TestStatistics`: Verify cycle counting

#### Router Tests
- `TestRouter`: Router initialization
- `TestXYRouting`: XY routing algorithm validation
- `TestBufferless`: Bufferless operation (no packet loss)
- `TestBackpressure`: Congestion handling
- `TestMultiHop`: Multi-hop packet delivery

#### Mesh Network Tests
- `TestMesh`: Network setup with multiple routers
- `TestSimplePacket`: Single packet routing
- `TestMultiHopDelivery`: Packets traversing multiple hops
- `TestAllToAll`: Many packets simultaneously

#### Collective Tests
- `TestRingAllReduce`: SUM/MAX/MIN/PROD operations
- `TestBroadcast`: Binary tree broadcast
- `TestReduceScatter`: Reduce-scatter algorithm
- Performance benchmarks for latency

#### Running Tests
```bash
# From golang_vm/uPIMulator directory

# Interconnect tests
go test -v ./src/device/simulator/interconnect/

# Collective tests
go test -v ./src/device/simulator/collective/

# All tests
go test -v ./src/device/simulator/...
```

---

## Integration & Data Flow

### Complete Communication Flow

```
Application Layer
       ↓
Interconnect Module (inter-DPU)
       ↓
Router (routing logic)
       ↓
Mesh Network (packet delivery)
       ↓
Inter-Chip Switch (multi-chip)
       ↓
Inter-Rank Control (synchronization)
```

### Collective Operations Flow

```
Ring AllReduce Request
       ↓
Split into N-1 reduce steps
       ↓
Each step: Ring topology → Mesh network → Router → Packet delivery
       ↓
Reduce values at each DPU
       ↓
AllGather phase: Propagate result
       ↓
All DPUs converge to same result
```

### Example: Broadcasting Data

```
Broadcast(root=0, data="hello")
       ↓
BroadcastTopology tree
       ↓
Root sends to children via mesh
       ↓
Router routes each packet
       ↓
Children receive and send to their children
       ↓
... (log N levels)
       ↓
All DPUs have data
```

---

## Performance Characteristics

### Latency
- **Interconnect**: O(1) - shared memory
- **Router**: 1 cycle if port available, more with backpressure
- **Mesh Network**: O(hops) where hops = Manhattan distance
- **Ring AllReduce**: O(N) steps × O(Manhattan distance) latency
- **Broadcast**: O(log N) tree levels × O(Manhattan distance) latency

### Throughput
- **Interconnect**: Limited by bandwidth parameter
- **Router**: 1 packet per port per cycle (bufferless)
- **Mesh**: Parallel operation across all routers
- **Inter-Chip Switch**: Multiple channels in parallel (DQ partitioning)
- **Inter-Rank Control**: CA bus bandwidth

### Scalability
- **Router**: Handles 5 ports, N×N mesh
- **Mesh**: Tested up to 32 routers (4×8 grid)
- **Collectives**: Scales to 32 DPUs
- **Inter-Rank**: Supports N channels × M ranks

---

## Future Extensions

1. **Adaptive Routing**: Choose routing algorithm based on network load
2. **Quality of Service**: Priority queues for different packet types
3. **Virtual Channels**: Multiple virtual networks on same physical network
4. **Advanced Collectives**: AllGather, ReduceScatter, AllToAll
5. **Congestion Management**: Intelligent backpressure algorithms
6. **Multi-Level Hierarchy**: More than 3D DPU hierarchy

