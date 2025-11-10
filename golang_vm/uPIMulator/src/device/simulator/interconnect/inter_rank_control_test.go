package interconnect

import (
	"testing"
)

func TestInterRankInit(t *testing.T) {
	irq := &InterRankControlQ{}

	// Valid initialization
	irq.Init(2, 4, 64, 128)

	if irq.numChannels != 2 {
		t.Errorf("Expected numChannels=2, got %d", irq.numChannels)
	}
	if irq.numRanks != 4 {
		t.Errorf("Expected numRanks=4, got %d", irq.numRanks)
	}
	if irq.busWidth != 64 {
		t.Errorf("Expected busWidth=64, got %d", irq.busWidth)
	}
	if irq.bandwidth != 128 {
		t.Errorf("Expected bandwidth=128, got %d", irq.bandwidth)
	}
}

func TestInterRankInitInvalid(t *testing.T) {
	tests := []struct {
		name        string
		numChannels int
		numRanks    int
		busWidth    int
		bandwidth   int64
		shouldPanic bool
	}{
		{"Valid", 2, 4, 64, 128, false},
		{"Invalid channels", 0, 4, 64, 128, true},
		{"Invalid ranks", 2, 0, 64, 128, true},
		{"Invalid busWidth", 2, 4, 0, 128, true},
		{"Negative channels", -1, 4, 64, 128, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if (r != nil) != tt.shouldPanic {
					t.Errorf("Init() panic = %v, shouldPanic %v", r, tt.shouldPanic)
				}
			}()

			irq := &InterRankControlQ{}
			irq.Init(tt.numChannels, tt.numRanks, tt.busWidth, tt.bandwidth)
		})
	}
}

func TestInterRankBroadcast(t *testing.T) {
	irq := &InterRankControlQ{}
	irq.Init(2, 4, 64, 128)

	data := []byte("test broadcast message")
	err := irq.Broadcast(0, 0, data)
	if err != nil {
		t.Fatalf("Broadcast failed: %v", err)
	}

	if irq.IsEmpty() {
		t.Error("Expected queue to not be empty after broadcast")
	}

	stats := irq.GetStatistics()
	if stats["total_broadcasts"].(int64) != 1 {
		t.Errorf("Expected total_broadcasts=1, got %d", stats["total_broadcasts"])
	}
}

func TestInterRankBroadcastInvalidRank(t *testing.T) {
	irq := &InterRankControlQ{}
	irq.Init(2, 4, 64, 128)

	data := []byte("test")
	
	tests := []struct {
		name       string
		channelID  int
		sourceRank int
		wantErr    bool
	}{
		{"Valid", 0, 0, false},
		{"Valid rank 3", 0, 3, false},
		{"Invalid channel", 5, 0, true},
		{"Invalid rank", 0, 10, true},
		{"Negative channel", -1, 0, true},
		{"Negative rank", 0, -1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := irq.Broadcast(tt.channelID, tt.sourceRank, data)
			if (err != nil) != tt.wantErr {
				t.Errorf("Broadcast() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestInterRankSendPointToPoint(t *testing.T) {
	irq := &InterRankControlQ{}
	irq.Init(2, 4, 64, 128)

	data := []byte("p2p message")
	err := irq.SendPointToPoint(0, 0, 0, 2, data)
	if err != nil {
		t.Fatalf("SendPointToPoint failed: %v", err)
	}

	if irq.IsEmpty() {
		t.Error("Expected queue to not be empty")
	}

	stats := irq.GetStatistics()
	if stats["total_messages"].(int64) != 1 {
		t.Errorf("Expected total_messages=1, got %d", stats["total_messages"])
	}
}

func TestInterRankSendPointToPointInvalid(t *testing.T) {
	irq := &InterRankControlQ{}
	irq.Init(2, 4, 64, 128)

	data := []byte("test")
	
	tests := []struct {
		name         string
		srcChannelID int
		srcRank      int
		dstChannelID int
		dstRank      int
		wantErr      bool
	}{
		{"Valid", 0, 0, 0, 1, false},
		{"Invalid src channel", -1, 0, 0, 1, true},
		{"Invalid dst channel", 0, 0, 5, 1, true},
		{"Invalid src rank", 0, 10, 0, 1, true},
		{"Invalid dst rank", 0, 0, 0, 10, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := irq.SendPointToPoint(tt.srcChannelID, tt.srcRank, tt.dstChannelID, tt.dstRank, data)
			if (err != nil) != tt.wantErr {
				t.Errorf("SendPointToPoint() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestInterRankCycle(t *testing.T) {
	irq := &InterRankControlQ{}
	irq.Init(2, 4, 64, 128)

	// Send a broadcast from rank 0
	data := []byte("test message")
	err := irq.Broadcast(0, 0, data)
	if err != nil {
		t.Fatalf("Broadcast failed: %v", err)
	}

	// Cycle should process the message
	irq.Cycle()

	// Check that message was delivered to other ranks
	for ch := 0; ch < 2; ch++ {
		for rank := 0; rank < 4; rank++ {
			if rank == 0 {
				// Source rank should have no pending messages
				count, _ := irq.GetPendingCount(ch, rank)
				if count != 0 {
					t.Errorf("Expected 0 pending for source rank, got %d", count)
				}
			} else {
				// Other ranks should have 1 pending message
				count, err := irq.GetPendingCount(ch, rank)
				if err != nil {
					t.Errorf("GetPendingCount failed: %v", err)
				}
				if count != 1 {
					t.Errorf("Expected 1 pending for rank %d ch %d, got %d", rank, ch, count)
				}
			}
		}
	}
}

func TestInterRankCycleBandwidthLimit(t *testing.T) {
	irq := &InterRankControlQ{}
	irq.Init(2, 4, 10, 10) // Small bandwidth

	// Send multiple large messages
	largeData := make([]byte, 20)
	for i := 0; i < 5; i++ {
		err := irq.Broadcast(0, 0, largeData)
		if err != nil {
			t.Fatalf("Broadcast %d failed: %v", i, err)
		}
	}

	// First cycle should only process messages within bandwidth
	irq.Cycle()

	// Not all messages should be processed due to bandwidth constraint
	if irq.IsEmpty() {
		t.Error("Expected some messages to remain in queue due to bandwidth limit")
	}
}

func TestInterRankRead(t *testing.T) {
	irq := &InterRankControlQ{}
	irq.Init(2, 4, 64, 128)

	// Broadcast a message
	data := []byte("test")
	err := irq.Broadcast(0, 0, data)
	if err != nil {
		t.Fatalf("Broadcast failed: %v", err)
	}

	// Cycle to process
	irq.Cycle()

	// Read messages for rank 1, channel 0
	messages, err := irq.Read(0, 1)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if len(messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(messages))
	}

	// After reading, pending count should be 0
	count, _ := irq.GetPendingCount(0, 1)
	if count != 0 {
		t.Errorf("Expected 0 pending messages after read, got %d", count)
	}
}

func TestInterRankReadInvalid(t *testing.T) {
	irq := &InterRankControlQ{}
	irq.Init(2, 4, 64, 128)

	_, err := irq.Read(-1, 0)
	if err == nil {
		t.Error("Expected error for invalid channel")
	}

	_, err = irq.Read(0, 10)
	if err == nil {
		t.Error("Expected error for invalid rank")
	}
}

func TestInterRankPointToPointDelivery(t *testing.T) {
	irq := &InterRankControlQ{}
	irq.Init(2, 4, 64, 128)

	// Send point-to-point message from rank 0 to rank 2
	data := []byte("p2p message")
	err := irq.SendPointToPoint(0, 0, 0, 2, data)
	if err != nil {
		t.Fatalf("SendPointToPoint failed: %v", err)
	}

	// Process message
	irq.Cycle()

	// Only rank 2 in all channels should have a pending message
	for ch := 0; ch < 2; ch++ {
		for rank := 0; rank < 4; rank++ {
			count, _ := irq.GetPendingCount(ch, rank)
			if rank == 2 {
				if count != 1 {
					t.Errorf("Expected 1 pending for rank 2 ch %d, got %d", ch, count)
				}
			} else {
				if count != 0 {
					t.Errorf("Expected 0 pending for rank %d ch %d, got %d", rank, ch, count)
				}
			}
		}
	}
}

func TestInterRankIsEmpty(t *testing.T) {
	irq := &InterRankControlQ{}
	irq.Init(2, 4, 64, 128)

	if !irq.IsEmpty() {
		t.Error("Expected queue to be empty initially")
	}

	irq.Broadcast(0, 0, []byte("test"))

	if irq.IsEmpty() {
		t.Error("Expected queue to not be empty after broadcast")
	}

	irq.Cycle()

	if !irq.IsEmpty() {
		t.Error("Expected queue to be empty after processing")
	}
}

func TestInterRankGetStatistics(t *testing.T) {
	irq := &InterRankControlQ{}
	irq.Init(2, 4, 64, 128)

	// Send some messages
	irq.Broadcast(0, 0, []byte("test1"))
	irq.Broadcast(1, 1, []byte("test2"))
	irq.SendPointToPoint(0, 0, 0, 2, []byte("p2p"))

	// Cycle to update statistics
	irq.Cycle()

	stats := irq.GetStatistics()

	if stats["total_messages"].(int64) != 3 {
		t.Errorf("Expected total_messages=3, got %d", stats["total_messages"])
	}

	if stats["total_broadcasts"].(int64) != 2 {
		t.Errorf("Expected total_broadcasts=2, got %d", stats["total_broadcasts"])
	}

	if stats["num_channels"].(int) != 2 {
		t.Errorf("Expected num_channels=2, got %d", stats["num_channels"])
	}

	if stats["num_ranks"].(int) != 4 {
		t.Errorf("Expected num_ranks=4, got %d", stats["num_ranks"])
	}

	if stats["cycles"].(int64) != 1 {
		t.Errorf("Expected cycles=1, got %d", stats["cycles"])
	}
}

func TestInterRankGetPendingCount(t *testing.T) {
	irq := &InterRankControlQ{}
	irq.Init(2, 4, 64, 128)

	// Initially should be 0
	count, err := irq.GetPendingCount(0, 0)
	if err != nil {
		t.Fatalf("GetPendingCount failed: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected 0 pending initially, got %d", count)
	}

	// Broadcast and cycle
	irq.Broadcast(0, 0, []byte("test"))
	irq.Cycle()

	// Rank 1 should have 1 pending
	count, err = irq.GetPendingCount(0, 1)
	if err != nil {
		t.Fatalf("GetPendingCount failed: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 pending, got %d", count)
	}
}

func TestInterRankGetPendingCountInvalid(t *testing.T) {
	irq := &InterRankControlQ{}
	irq.Init(2, 4, 64, 128)

	_, err := irq.GetPendingCount(-1, 0)
	if err == nil {
		t.Error("Expected error for negative channel")
	}

	_, err = irq.GetPendingCount(0, 10)
	if err == nil {
		t.Error("Expected error for out-of-range rank")
	}
}

func TestInterRankClear(t *testing.T) {
	irq := &InterRankControlQ{}
	irq.Init(2, 4, 64, 128)

	// Broadcast and cycle
	irq.Broadcast(0, 0, []byte("test"))
	irq.Cycle()

	// Rank 1 should have pending
	count, _ := irq.GetPendingCount(0, 1)
	if count != 1 {
		t.Errorf("Expected 1 pending before clear, got %d", count)
	}

	// Clear rank 1
	irq.Clear(0, 1)

	// Should be 0 now
	count, _ = irq.GetPendingCount(0, 1)
	if count != 0 {
		t.Errorf("Expected 0 pending after clear, got %d", count)
	}
}

func TestInterRankMultipleBroadcasts(t *testing.T) {
	irq := &InterRankControlQ{}
	irq.Init(2, 4, 64, 1000) // Large bandwidth

	// Send messages from multiple ranks
	for srcRank := 0; srcRank < 4; srcRank++ {
		data := []byte("message from rank")
		err := irq.Broadcast(0, srcRank, data)
		if err != nil {
			t.Fatalf("Broadcast from rank %d failed: %v", srcRank, err)
		}
	}

	// Process all messages
	irq.Cycle()

	// Each rank should receive messages from other ranks
	for rank := 0; rank < 4; rank++ {
		count, err := irq.GetPendingCount(0, rank)
		if err != nil {
			t.Errorf("GetPendingCount failed for rank %d: %v", rank, err)
		}
		// Should receive 3 messages (from all other ranks)
		if count != 3 {
			t.Errorf("Expected 3 pending for rank %d, got %d", rank, count)
		}
	}
}

func TestInterRankFini(t *testing.T) {
	irq := &InterRankControlQ{}
	irq.Init(2, 4, 64, 128)

	// Add some data
	irq.Broadcast(0, 0, []byte("test"))
	irq.Cycle()

	// Call Fini
	irq.Fini()

	// This test mainly ensures Fini doesn't panic
}

func TestInterRankBandwidthUtilization(t *testing.T) {
	irq := &InterRankControlQ{}
	irq.Init(2, 4, 64, 100)

	// Send 50 bytes
	data := make([]byte, 50)
	irq.Broadcast(0, 0, data)
	irq.Cycle()

	stats := irq.GetStatistics()
	utilization := stats["bandwidth_utilization"].(float64)

	// Should be 50/100 = 0.5
	if utilization < 0.49 || utilization > 0.51 {
		t.Errorf("Expected bandwidth utilization ~0.5, got %f", utilization)
	}
}

func TestInterRankMessageOrdering(t *testing.T) {
	irq := &InterRankControlQ{}
	irq.Init(2, 4, 64, 1000)

	// Send three messages
	irq.Broadcast(0, 0, []byte("msg1"))
	irq.Broadcast(0, 0, []byte("msg2"))
	irq.Broadcast(0, 0, []byte("msg3"))

	// Process all
	irq.Cycle()

	// Read and verify order
	messages, _ := irq.Read(0, 1)
	if len(messages) != 3 {
		t.Fatalf("Expected 3 messages, got %d", len(messages))
	}

	if string(messages[0].Data) != "msg1" {
		t.Errorf("Expected first message to be 'msg1', got '%s'", string(messages[0].Data))
	}
	if string(messages[1].Data) != "msg2" {
		t.Errorf("Expected second message to be 'msg2', got '%s'", string(messages[1].Data))
	}
	if string(messages[2].Data) != "msg3" {
		t.Errorf("Expected third message to be 'msg3', got '%s'", string(messages[2].Data))
	}
}