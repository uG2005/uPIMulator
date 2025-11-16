package interconnect

import (
	"errors"
	"fmt"
	"sync"
)

// InterRankMessage represents a message broadcast across ranks via DDR bus
type InterRankMessage struct {
	SourceRank      int
	DestinationRank int // -1 for broadcast to all ranks
	Data            []byte
	Timestamp       int64
	MessageID       int
}

// InterRankControlQ manages the DDR bus as a broadcast channel for inter-rank communication
type InterRankControlQ struct {
	mu sync.RWMutex

	// Configuration
	numChannels  int
	numRanks     int
	busWidth     int   // DDR bus width in bytes
	bandwidth    int64 // bytes per cycle
	bufferSize   int

	// Message queue for broadcast channel (DDR bus)
	messageQueue []*InterRankMessage

	// Per-rank delivery tracking
	pendingMessages map[string][]*InterRankMessage // channel-rank -> messages

	totalMessages         int64
	totalBroadcasts       int64
	totalBytesTransferred int64
	cycles                int64
	messageIDCounter      int
}

func (irq *InterRankControlQ) Init(numChannels, numRanks, busWidth int, bandwidth int64) {
	if numChannels <= 0 || numRanks <= 0 || busWidth <= 0 {
		panic(errors.New("invalid inter-rank control dimensions"))
	}

	irq.numChannels = numChannels
	irq.numRanks = numRanks
	irq.busWidth = busWidth
	irq.bandwidth = bandwidth
	irq.bufferSize = 128 // Default buffer size

	irq.messageQueue = make([]*InterRankMessage, 0, irq.bufferSize)
	irq.pendingMessages = make(map[string][]*InterRankMessage)

	irq.totalMessages = 0
	irq.totalBroadcasts = 0
	irq.totalBytesTransferred = 0
	irq.cycles = 0
	irq.messageIDCounter = 0

	for ch := 0; ch < numChannels; ch++ {
		for r := 0; r < numRanks; r++ {
			key := irq.makeKey(ch, r)
			irq.pendingMessages[key] = make([]*InterRankMessage, 0)
		}
	}
}

// Broadcast sends a message to all ranks in a channel via DDR bus
func (irq *InterRankControlQ) Broadcast(channelID, sourceRank int, data []byte) error {
	irq.mu.Lock()
	defer irq.mu.Unlock()

	if err := irq.validateRankCoordinates(channelID, sourceRank); err != nil {
		return err
	}

	if len(irq.messageQueue) >= irq.bufferSize {
		return errors.New("message queue is full")
	}

	// Create broadcast message
	msg := &InterRankMessage{
		SourceRank:      sourceRank,
		DestinationRank: -1, // broadcast to all ranks in channel
		Data:            data,
		Timestamp:       irq.cycles,
		MessageID:       irq.messageIDCounter,
	}
	irq.messageIDCounter++

	irq.messageQueue = append(irq.messageQueue, msg)
	irq.totalMessages++
	irq.totalBroadcasts++
	irq.totalBytesTransferred += int64(len(data))

	return nil
}

// SendPointToPoint sends a message to a specific rank
func (irq *InterRankControlQ) SendPointToPoint(srcChannelID, srcRank, dstChannelID, dstRank int, data []byte) error {
	irq.mu.Lock()
	defer irq.mu.Unlock()

	if err := irq.validateRankCoordinates(srcChannelID, srcRank); err != nil {
		return fmt.Errorf("invalid source: %w", err)
	}
	if err := irq.validateRankCoordinates(dstChannelID, dstRank); err != nil {
		return fmt.Errorf("invalid destination: %w", err)
	}

	if len(irq.messageQueue) >= irq.bufferSize {
		return errors.New("message queue is full")
	}

	msg := &InterRankMessage{
		SourceRank:      srcRank,
		DestinationRank: dstRank,
		Data:            data,
		Timestamp:       irq.cycles,
		MessageID:       irq.messageIDCounter,
	}
	irq.messageIDCounter++

	irq.messageQueue = append(irq.messageQueue, msg)
	irq.totalMessages++
	irq.totalBytesTransferred += int64(len(data))

	return nil
}

// Cycle simulates one cycle of the DDR bus broadcast channel
func (irq *InterRankControlQ) Cycle() {
	irq.mu.Lock()
	defer irq.mu.Unlock()

	irq.cycles++

	// Process messages in the queue within bandwidth constraints
	bytesProcessed := int64(0)
	messagesProcessed := 0

	for i := 0; i < len(irq.messageQueue) && bytesProcessed < irq.bandwidth; i++ {
		msg := irq.messageQueue[i]
		messageSize := int64(len(msg.Data))

		// Check if we have bandwidth for this message
		if bytesProcessed+messageSize <= irq.bandwidth {
			// Deliver message via DDR bus broadcast
			if msg.DestinationRank == -1 {
				// Broadcast to all ranks in all channels except source
				for ch := 0; ch < irq.numChannels; ch++ {
					for rank := 0; rank < irq.numRanks; rank++ {
						if rank != msg.SourceRank {
							key := irq.makeKey(ch, rank)
							irq.pendingMessages[key] = append(irq.pendingMessages[key], msg)
						}
					}
				}
			} else {
				// Point-to-point delivery
				for ch := 0; ch < irq.numChannels; ch++ {
					key := irq.makeKey(ch, msg.DestinationRank)
					irq.pendingMessages[key] = append(irq.pendingMessages[key], msg)
				}
			}

			bytesProcessed += messageSize
			messagesProcessed++
		} else {
			break // No more bandwidth available this cycle
		}
	}

	// Remove processed messages from queue
	if messagesProcessed > 0 {
		irq.messageQueue = irq.messageQueue[messagesProcessed:]
	}
}

// Read retrieves pending messages for a specific rank in a channel
func (irq *InterRankControlQ) Read(channelID, rank int) ([]*InterRankMessage, error) {
	irq.mu.Lock()
	defer irq.mu.Unlock()

	if err := irq.validateRankCoordinates(channelID, rank); err != nil {
		return nil, err
	}

	key := irq.makeKey(channelID, rank)
	messages := irq.pendingMessages[key]
	
	// Create copy of messages
	result := make([]*InterRankMessage, len(messages))
	copy(result, messages)

	// Clear pending messages for this rank
	irq.pendingMessages[key] = make([]*InterRankMessage, 0)

	return result, nil
}

func (irq *InterRankControlQ) GetStatistics() map[string]interface{} {
	irq.mu.RLock()
	defer irq.mu.RUnlock()

	stats := make(map[string]interface{})
	stats["total_messages"] = irq.totalMessages
	stats["total_broadcasts"] = irq.totalBroadcasts
	stats["total_bytes_transferred"] = irq.totalBytesTransferred
	stats["cycles"] = irq.cycles
	stats["num_channels"] = irq.numChannels
	stats["num_ranks"] = irq.numRanks
	stats["bus_width"] = irq.busWidth

	if irq.totalMessages > 0 {
		stats["avg_bytes_per_message"] = float64(irq.totalBytesTransferred) / float64(irq.totalMessages)
	}

	if irq.cycles > 0 {
		stats["bandwidth_utilization"] = float64(irq.totalBytesTransferred) / (float64(irq.cycles) * float64(irq.bandwidth))
	}

	return stats
}

func (irq *InterRankControlQ) IsEmpty() bool {
	irq.mu.RLock()
	defer irq.mu.RUnlock()

	return len(irq.messageQueue) == 0
}

func (irq *InterRankControlQ) GetPendingCount(channelID, rank int) (int, error) {
	irq.mu.RLock()
	defer irq.mu.RUnlock()

	if err := irq.validateRankCoordinates(channelID, rank); err != nil {
		return 0, err
	}

	key := irq.makeKey(channelID, rank)
	return len(irq.pendingMessages[key]), nil
}

func (irq *InterRankControlQ) Clear(channelID, rank int) {
	irq.mu.Lock()
	defer irq.mu.Unlock()

	key := irq.makeKey(channelID, rank)
	irq.pendingMessages[key] = make([]*InterRankMessage, 0)
}

func (irq *InterRankControlQ) makeKey(channelID, rankID int) string {
	return fmt.Sprintf("%d-%d", channelID, rankID)
}

func (irq *InterRankControlQ) validateRankCoordinates(channelID, rankID int) error {
	if channelID < 0 || channelID >= irq.numChannels {
		return fmt.Errorf("invalid channel ID: %d", channelID)
	}
	if rankID < 0 || rankID >= irq.numRanks {
		return fmt.Errorf("invalid rank ID: %d", rankID)
	}
	return nil
}

func (irq *InterRankControlQ) Fini() {
	irq.mu.Lock()
	defer irq.mu.Unlock()

	irq.messageQueue = nil
	irq.pendingMessages = nil
}
