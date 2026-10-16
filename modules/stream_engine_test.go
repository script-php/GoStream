package modules

import (
	"bytes"
	"testing"
)

func TestConfiguredBitrateBps(t *testing.T) {
	original := Config.StandardBitrate
	defer func() { Config.StandardBitrate = original }()

	tests := map[string]int{
		"128k":    128000,
		"192K":    192000,
		"256000":  256000,
		"invalid": 128000,
	}
	for input, expected := range tests {
		Config.StandardBitrate = input
		if actual := configuredBitrateBps(); actual != expected {
			t.Errorf("configuredBitrateBps(%q) = %d, want %d", input, actual, expected)
		}
	}
}

func TestPCMSourcePadsAClosedFinalChunkWithSilence(t *testing.T) {
	process := &pcmProcess{chunks: make(chan []byte, 1)}
	process.chunks <- []byte{1, 2, 3, 4}
	close(process.chunks)
	state := &pcmSourceState{process: process}

	pcm, finished := state.take(8)
	if !finished {
		t.Fatal("expected source to finish after its final queued chunk")
	}
	if !bytes.Equal(pcm, []byte{1, 2, 3, 4, 0, 0, 0, 0}) {
		t.Fatalf("unexpected padded PCM: %v", pcm)
	}
}

func TestPCMSourceMaintainsRemainderAcrossTicks(t *testing.T) {
	process := &pcmProcess{chunks: make(chan []byte, 1)}
	process.chunks <- []byte{1, 2, 3, 4, 5, 6}
	state := &pcmSourceState{process: process}

	first, finished := state.take(4)
	if finished || !bytes.Equal(first, []byte{1, 2, 3, 4}) {
		t.Fatalf("unexpected first PCM tick: %v, finished=%v", first, finished)
	}
	second, finished := state.take(4)
	if finished || !bytes.Equal(second, []byte{5, 6, 0, 0}) {
		t.Fatalf("unexpected second PCM tick: %v, finished=%v", second, finished)
	}
}
