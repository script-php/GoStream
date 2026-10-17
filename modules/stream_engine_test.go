package modules

import (
	"bufio"
	"bytes"
	"io"
	"sync"
	"testing"
	"time"
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

	pcm, finished := state.take(8, 0)
	if !finished {
		t.Fatal("expected source to finish after its final queued chunk")
	}
	if !bytes.Equal(pcm, []byte{1, 2, 3, 4, 0, 0, 0, 0}) {
		t.Fatalf("unexpected padded PCM: %v", pcm)
	}
}

func TestPCMSourceMaintainsRemainderAcrossTicks(t *testing.T) {
	process := &pcmProcess{chunks: make(chan []byte, 2)}
	process.chunks <- []byte{1, 2, 3, 4, 5, 6}
	state := &pcmSourceState{process: process}

	first, finished := state.take(4, 0)
	if finished || !bytes.Equal(first, []byte{1, 2, 3, 4}) {
		t.Fatalf("unexpected first PCM tick: %v, finished=%v", first, finished)
	}
	second, finished := state.take(4, 0)
	if finished || !bytes.Equal(second, []byte{0, 0, 0, 0}) {
		t.Fatalf("expected silence rather than a partial PCM tick: %v, finished=%v", second, finished)
	}

	process.chunks <- []byte{7, 8}
	third, finished := state.take(4, 0)
	if finished || !bytes.Equal(third, []byte{5, 6, 7, 8}) {
		t.Fatalf("unexpected completed PCM tick: %v, finished=%v", third, finished)
	}
}

func TestLivePCMSourceWaitsForPrebuffer(t *testing.T) {
	process := &pcmProcess{chunks: make(chan []byte, 2)}
	process.chunks <- []byte{1, 2, 3, 4}
	state := &pcmSourceState{process: process, prebufferBytes: 8}

	first, finished := state.take(4, 0)
	if finished || !bytes.Equal(first, make([]byte, 4)) {
		t.Fatalf("expected silence while prebuffering, got %v, finished=%v", first, finished)
	}

	process.chunks <- []byte{5, 6, 7, 8}
	second, finished := state.take(4, 0)
	if finished || !bytes.Equal(second, []byte{1, 2, 3, 4}) {
		t.Fatalf("unexpected first buffered PCM tick: %v, finished=%v", second, finished)
	}
}

func TestLivePCMSourceWaitsForDelayedDecoderOutput(t *testing.T) {
	process := &pcmProcess{chunks: make(chan []byte, 1)}
	state := &pcmSourceState{process: process, ready: true}

	go func() {
		time.Sleep(10 * time.Millisecond)
		process.chunks <- []byte{1, 2, 3, 4}
	}()

	pcm, finished := state.take(4, 100*time.Millisecond)
	if finished || !bytes.Equal(pcm, []byte{1, 2, 3, 4}) {
		t.Fatalf("unexpected delayed PCM tick: %v, finished=%v", pcm, finished)
	}
}

func TestIcecastMP3SourceUsesPassthrough(t *testing.T) {
	source := &IcecastSourceServer{sourceMetadata: map[string]string{"content-type": "audio/mpeg; charset=binary"}}
	if !source.IsMP3Source() {
		t.Fatal("expected audio/mpeg source to use MP3 passthrough")
	}
	source.sourceMetadata["content-type"] = "audio/ogg"
	if source.IsMP3Source() {
		t.Fatal("expected non-MP3 source to use FFmpeg transcoding")
	}
}

func TestBufferHistoryPreservesEveryPublishedUnit(t *testing.T) {
	reader := &IMusicReader{Store: &sync.Map{}, BufferStoreKey: "test", BufferHistory: make(map[int][]byte)}
	reader.SetBufferStoreData(IMusicReaderStoreData{UnitBuffer: []byte{1, 2}, Order: 1})
	reader.SetBufferStoreData(IMusicReaderStoreData{UnitBuffer: []byte{3, 4}, Order: 2})
	reader.SetBufferStoreData(IMusicReaderStoreData{UnitBuffer: []byte{5, 6}, Order: 3})

	units, complete := reader.GetBufferUnitsAfter(0, 3)
	if !complete || !bytes.Equal(units, []byte{1, 2, 3, 4, 5, 6}) {
		t.Fatalf("unexpected ordered buffer history: %v, complete=%v", units, complete)
	}
}

func TestIcecastChunkedBodyRemovesHTTPChunkMarkers(t *testing.T) {
	raw := bufio.NewReader(bytes.NewBufferString("4\r\nABCD\r\n3\r\nEFG\r\n0\r\n\r\n"))
	body := icecastBodyReader(raw, map[string]string{"transfer-encoding": "chunked"})
	decoded, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, []byte("ABCDEFG")) {
		t.Fatalf("unexpected decoded chunked body: %q", decoded)
	}
}

func TestAudioBufferOwnsWrittenBytes(t *testing.T) {
	buffer := NewAudioBuffer(4096)
	original := []byte{1, 2, 3, 4}
	if _, err := buffer.Write(original); err != nil {
		t.Fatal(err)
	}
	for i := range original {
		original[i] = 9
	}
	queued, err := buffer.ReadTimeout(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(queued, []byte{1, 2, 3, 4}) {
		t.Fatalf("queued source bytes were mutated: %v", queued)
	}
}

func TestMP3FormatMatchesConfiguredOutput(t *testing.T) {
	originalBitrate := Config.StandardBitrate
	originalRate := Config.StandardSampleRate
	defer func() {
		Config.StandardBitrate = originalBitrate
		Config.StandardSampleRate = originalRate
	}()
	Config.StandardBitrate = "128k"
	Config.StandardSampleRate = "44100"

	// MPEG-1 Layer III, 128 kbps, 44.1 kHz, stereo. A 128 kbps frame at
	// 44.1 kHz is 417 bytes without padding.
	frame := make([]byte, 417)
	copy(frame, []byte{0xff, 0xfb, 0x90, 0x00})
	buffer := NewAudioBuffer(4096)
	if _, err := buffer.Write(frame); err != nil {
		t.Fatal(err)
	}
	source := &IcecastSourceServer{audioBuffer: buffer}
	matches, ready := source.MP3MatchesConfiguredOutput()
	if !ready || !matches {
		t.Fatalf("expected matching MP3 format, matches=%v ready=%v", matches, ready)
	}

	Config.StandardBitrate = "192k"
	matches, ready = source.MP3MatchesConfiguredOutput()
	if !ready || matches {
		t.Fatalf("expected bitrate mismatch, matches=%v ready=%v", matches, ready)
	}
}
