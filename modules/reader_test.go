package modules

import (
	"bytes"
	"os"
	"sync"
	"testing"
)

// writeTestMP3 writes valid MPEG-1 Layer III frames (128 kbps, 44.1 kHz).
// Audio content is irrelevant here; the reader only needs valid frame bounds.
func writeTestMP3(t *testing.T, frameCount int) *os.File {
	t.Helper()

	const frameLength = 417
	header := []byte{0xff, 0xfb, 0x90, 0x64}
	frame := append(append([]byte{}, header...), make([]byte, frameLength-len(header))...)
	data := bytes.Repeat(frame, frameCount)

	file, err := os.CreateTemp(t.TempDir(), "reader-eof-*.mp3")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		file.Close()
		t.Fatal(err)
	}
	return file
}

func newTestReader(file *os.File) *IMusicReader {
	reader := &IMusicReader{
		InitialFrame:   500,
		UnitFrame:      50,
		File:           file,
		Store:          &sync.Map{},
		BufferStoreKey: "Store",
		InfoStoreKey:   "Info",
	}
	reader.SetBufferStoreData(IMusicReaderStoreData{
		InitialBuffer: []byte{1},
		Order:         1,
	})
	return reader
}

func TestSetUnitBufferStopsReadingAfterEOF(t *testing.T) {
	reader := newTestReader(writeTestMP3(t, 1))

	// This used to panic: EOF closed File on the second iteration, then the
	// third iteration passed the nil file to mp3lib.NextFrame.
	reader.SetUnitBuffer()

	store := reader.GetBufferStoreData()
	if store == nil || len(store.UnitBuffer) == 0 {
		t.Fatal("expected the final partial frame batch to be published")
	}
	if reader.File != nil {
		t.Fatal("expected the exhausted file to be closed")
	}
	if store.Order != 2 {
		t.Fatalf("expected buffer order 2, got %d", store.Order)
	}
}

func TestSetInitialBufferHandlesShortTrack(t *testing.T) {
	reader := newTestReader(writeTestMP3(t, 2))

	reader.SetInitialBuffer()

	store := reader.GetBufferStoreData()
	if store == nil || len(store.InitialBuffer) == 0 {
		t.Fatal("expected a short track to produce an initial buffer")
	}
	if len(store.UnitBuffer) == 0 {
		t.Fatal("expected a short track to produce a timed unit buffer")
	}
	if store.Timeout <= Config.GapMs {
		t.Fatalf("expected audio duration plus gap, got timeout %dms", store.Timeout)
	}
	if reader.File != nil {
		t.Fatal("expected the exhausted short track to be closed")
	}
}
