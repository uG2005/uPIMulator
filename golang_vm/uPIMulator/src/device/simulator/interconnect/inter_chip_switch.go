package interconnect

import (
	"fmt"
	"sync"
)

// DQPinPartition represents DQ (Data) pin partitioning
// Splits the wide data bus into multiple narrow channels
type DQPinPartition struct {
	totalPins      int
	numChannels    int
	pinsPerChannel int
	
	// Pin assignment: which pins belong to which channel
	channelPins map[int][]int
}

func (dq *DQPinPartition) Init(totalPins, numChannels int) error {
	if totalPins%numChannels != 0 {
		return fmt.Errorf("totalPins %d not evenly divisible by numChannels %d", 
			totalPins, numChannels)
	}
	
	dq.totalPins = totalPins
	dq.numChannels = numChannels
	dq.pinsPerChannel = totalPins / numChannels
	
	// Assign pins to channels
	dq.channelPins = make(map[int][]int)
	for ch := 0; ch < numChannels; ch++ {
		pins := make([]int, dq.pinsPerChannel)
		for i := 0; i < dq.pinsPerChannel; i++ {
			pins[i] = ch*dq.pinsPerChannel + i
		}
		dq.channelPins[ch] = pins
	}
	
	fmt.Printf("✓ DQ Pin Partition: %d pins → %d channels × %d pins\n", 
		totalPins, numChannels, dq.pinsPerChannel)
	
	return nil
}

func (dq *DQPinPartition) GetChannelPins(channelID int) []int {
	return dq.channelPins[channelID]
}

func (dq *DQPinPartition) GetChannelBandwidth() int {
	return dq.pinsPerChannel
}

// CrossbarSwitch implements an N×N crossbar switching matrix
// Allows any input to connect to any output
type CrossbarSwitch struct {
	mu sync.Mutex
	
	numInputs  int
	numOutputs int
	
	// Current connections: inputID -> outputID
	// -1 means not connected
	connections []int
	
	// Reverse mapping: outputID -> inputID
	reverseConnections []int
	
	totalSwitches int64
	blockedAttempts int64
	cycles int64
}

func (cs *CrossbarSwitch) Init(numInputs, numOutputs int) {
	cs.numInputs = numInputs
	cs.numOutputs = numOutputs
	
	// Initialize connection maps
	cs.connections = make([]int, numInputs)
	cs.reverseConnections = make([]int, numOutputs)
	
	for i := 0; i < numInputs; i++ {
		cs.connections[i] = -1
	}
	for i := 0; i < numOutputs; i++ {
		cs.reverseConnections[i] = -1
	}
	
	fmt.Printf("✓ Crossbar Switch: %d×%d matrix\n", numInputs, numOutputs)
}

// Connect attempts to connect an input to an output
// Returns true if successful, false if output is busy
func (cs *CrossbarSwitch) Connect(inputID, outputID int) bool {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	
	if inputID < 0 || inputID >= cs.numInputs {
		return false
	}
	if outputID < 0 || outputID >= cs.numOutputs {
		return false
	}
	
	if cs.reverseConnections[outputID] != -1 {
		cs.blockedAttempts++
		return false
	}
	
	if cs.connections[inputID] != -1 {
		prevOutput := cs.connections[inputID]
		cs.reverseConnections[prevOutput] = -1
	}
	
	cs.connections[inputID] = outputID
	cs.reverseConnections[outputID] = inputID
	cs.totalSwitches++
	
	return true
}

func (cs *CrossbarSwitch) Disconnect(inputID int) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	
	if inputID < 0 || inputID >= cs.numInputs {
		return
	}
	
	outputID := cs.connections[inputID]
	if outputID != -1 {
		cs.connections[inputID] = -1
		cs.reverseConnections[outputID] = -1
	}
}

func (cs *CrossbarSwitch) IsConnected(inputID int) bool {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	
	return cs.connections[inputID] != -1
}

func (cs *CrossbarSwitch) GetConnection(inputID int) int {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	
	return cs.connections[inputID]
}

func (cs *CrossbarSwitch) DisconnectAll() {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	
	for i := 0; i < cs.numInputs; i++ {
		cs.connections[i] = -1
	}
	for i := 0; i < cs.numOutputs; i++ {
		cs.reverseConnections[i] = -1
	}
}

func (cs *CrossbarSwitch) Cycle() {
	cs.cycles++
}

func (cs *CrossbarSwitch) GetStatistics() map[string]interface{} {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	
	stats := make(map[string]interface{})
	stats["num_inputs"] = cs.numInputs
	stats["num_outputs"] = cs.numOutputs
	stats["total_switches"] = cs.totalSwitches
	stats["blocked_attempts"] = cs.blockedAttempts
	stats["cycles"] = cs.cycles
	
	if cs.totalSwitches+cs.blockedAttempts > 0 {
		blockRate := float64(cs.blockedAttempts) / float64(cs.totalSwitches+cs.blockedAttempts)
		stats["block_rate"] = blockRate
	}
	
	activeCount := 0
	for _, conn := range cs.connections {
		if conn != -1 {
			activeCount++
		}
	}
	stats["active_connections"] = activeCount
	
	return stats
}

// InterChipSwitch combines DQ partitioning with crossbar switching
type InterChipSwitch struct {
	numChips int
	
	dqPartition *DQPinPartition
	crossbar    *CrossbarSwitch
	
	// Transfer tracking
	activeTransfers map[int]*ChipTransfer // transferID -> transfer
	nextTransferID  int
	
	totalTransfers int64
	totalBytes     int64
	cycles         int64
}

type ChipTransfer struct {
	TransferID int
	SrcChipID  int
	DstChipID  int
	ChannelID  int
	Data       []byte
	StartCycle int64
	EndCycle   int64
}

func (ics *InterChipSwitch) Init(numChips, totalDQPins, numChannels int) error {
	ics.numChips = numChips
	
	// Initialize DQ pin partitioning
	ics.dqPartition = &DQPinPartition{}
	err := ics.dqPartition.Init(totalDQPins, numChannels)
	if err != nil {
		return err
	}
	
	// Initialize crossbar (chips can connect to each other)
	ics.crossbar = &CrossbarSwitch{}
	ics.crossbar.Init(numChips, numChips)
	
	ics.activeTransfers = make(map[int]*ChipTransfer)
	ics.nextTransferID = 0
	
	fmt.Printf("✓ Inter-Chip Switch initialized: %d chips, %d channels\n", 
		numChips, numChannels)
	
	return nil
}

func (ics *InterChipSwitch) StartTransfer(srcChip, dstChip, channelID int, data []byte) (int, error) {
	if srcChip < 0 || srcChip >= ics.numChips {
		return -1, fmt.Errorf("invalid source chip: %d", srcChip)
	}
	if dstChip < 0 || dstChip >= ics.numChips {
		return -1, fmt.Errorf("invalid destination chip: %d", dstChip)
	}
	if channelID < 0 || channelID >= ics.dqPartition.numChannels {
		return -1, fmt.Errorf("invalid channel: %d", channelID)
	}
	
	// Try to connect in crossbar
	if !ics.crossbar.Connect(srcChip, dstChip) {
		return -1, fmt.Errorf("crossbar connection blocked: chip %d busy", dstChip)
	}
	
	// Create transfer
	transfer := &ChipTransfer{
		TransferID: ics.nextTransferID,
		SrcChipID:  srcChip,
		DstChipID:  dstChip,
		ChannelID:  channelID,
		Data:       data,
		StartCycle: ics.cycles,
		EndCycle:   -1,
	}
	
	ics.activeTransfers[ics.nextTransferID] = transfer
	ics.nextTransferID++
	ics.totalTransfers++
	ics.totalBytes += int64(len(data))
	
	return transfer.TransferID, nil
}

func (ics *InterChipSwitch) CompleteTransfer(transferID int) error {
	transfer, exists := ics.activeTransfers[transferID]
	if !exists {
		return fmt.Errorf("transfer %d not found", transferID)
	}
	
	transfer.EndCycle = ics.cycles
	
	// Disconnect crossbar
	ics.crossbar.Disconnect(transfer.SrcChipID)
	
	delete(ics.activeTransfers, transferID)
	
	return nil
}

func (ics *InterChipSwitch) Cycle() {
	ics.crossbar.Cycle()
	ics.cycles++
}

func (ics *InterChipSwitch) GetStatistics() map[string]interface{} {
	stats := make(map[string]interface{})
	stats["num_chips"] = ics.numChips
	stats["dq_pins"] = ics.dqPartition.totalPins
	stats["num_channels"] = ics.dqPartition.numChannels
	stats["pins_per_channel"] = ics.dqPartition.pinsPerChannel
	stats["total_transfers"] = ics.totalTransfers
	stats["total_bytes"] = ics.totalBytes
	stats["active_transfers"] = len(ics.activeTransfers)
	stats["cycles"] = ics.cycles
	
	if ics.totalTransfers > 0 {
		stats["avg_bytes_per_transfer"] = float64(ics.totalBytes) / float64(ics.totalTransfers)
	}
	
	crossbarStats := ics.crossbar.GetStatistics()
	stats["crossbar_switches"] = crossbarStats["total_switches"]
	stats["crossbar_blocks"] = crossbarStats["blocked_attempts"]
	stats["crossbar_block_rate"] = crossbarStats["block_rate"]
	
	return stats
}

func (ics *InterChipSwitch) Fini() {
	ics.activeTransfers = nil
}
