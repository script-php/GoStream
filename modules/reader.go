package modules

import (
	_ "embed"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bogem/id3v2/v2"
	"github.com/dmulholl/mp3lib"
)

// Global map to store hash -> filepath mapping
var SongHashMap = &sync.Map{}

// Global list of sorted song hashes
var SortedSongHashes []string

type IMusicReader struct {
	InitialFrame int
	UnitFrame    int

	CurrentSongHash  string // Current song hash (instead of index)
	CachedNextHash   string // Cache the predicted next hash (instead of index)
	File             *os.File
	Playlist         []string // Queue of song hashes to play

	Store          *sync.Map
	BufferStoreKey string
	InfoStoreKey   string

	Lock sync.RWMutex
	
	// Icecast mode support
	IsIcecastMode  bool
	IcecastChunks  chan []byte // Channel for receiving normalized Icecast chunks
	IcecastStopCh  chan struct{} // Signal to stop Icecast processing
}

type IMusicInfoStoreData struct {
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	SampleRate string `json:"SampleRate"`
	BitRate    string `json:"bitRate"`
	Filename   string `json:"filename"`
}

type IMusicInfo struct {
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	SampleRate string `json:"SampleRate"`
	BitRate    string `json:"bitRate"`
	Url        string `json:"url"`
	Filename   string `json:"filename"`
}

var MusicReader = IMusicReader{
	InitialFrame: 500, // Number of frames to read for initial buffer (should be enough to fill Icecast buffer and prevent underflow at start)
	UnitFrame:    50, // Number of frames to read for each buffer unit

	CurrentSongHash: "", // Current song hash
	CachedNextHash:  "", // Cache for the next hash prediction
	File:            nil, // Currently open file handle for the song being read

	Store:          &sync.Map{}, // Thread-safe store for sharing data between reader and routes
	BufferStoreKey: "Store", // Key for storing the current buffer data (initial buffer + unit buffer)
	InfoStoreKey:   "Info", // Key for storing current music info (title, artist, etc.)
	
	IsIcecastMode:  false,
	IcecastChunks:  make(chan []byte, 100), // Buffer up to 100 chunks (400KB at 4KB per chunk)
	IcecastStopCh:  make(chan struct{}),
}

type IMusicReaderStoreData struct {
	InitialBuffer []byte
	UnitBuffer    []byte
	Timeout       int
	Order         int
}

// GenerateSongHash generates a unique hash for a song filepath
func GenerateSongHash(filePath string) string {
	hash := md5.Sum([]byte(filePath))
	return hex.EncodeToString(hash[:])
}

// FindSongByHash finds the filepath for a given song hash
func FindSongByHash(hash string) (string, bool) {
	value, ok := SongHashMap.Load(hash)
	if !ok {
		return "", false
	}
	return value.(string), true
}

// GetNextMusicHash calculates what the next music hash would be
func (musicReader *IMusicReader) GetNextMusicHash(songHashes []string) string {
	if len(songHashes) == 0 {
		return ""
	}
	
	currentIndex := -1
	for i, hash := range songHashes {
		if hash == musicReader.CurrentSongHash {
			currentIndex = i
			break
		}
	}
	
	if Config.Random {
		randomIndex := rand.Intn(len(songHashes))
		return songHashes[randomIndex]
	} else {
		nextIndex := currentIndex + 1
		if currentIndex == -1 || nextIndex >= len(songHashes) {
			nextIndex = 0
		}
		return songHashes[nextIndex]
	}
}

func (musicReader *IMusicReader) SelectNextMusic() {
	_, err := GetMp3FilePaths()
	if err != nil {
		Logger.Error(err)
		return
	}
	
	// Use cached next hash as current song if available, otherwise calculate it
	if musicReader.CachedNextHash != "" {
		MusicReader.CurrentSongHash = musicReader.CachedNextHash
	} else {
		// Calculate next song based on random or sequential mode
		if Config.Random {
			randomIndex := rand.Intn(len(SortedSongHashes))
			MusicReader.CurrentSongHash = SortedSongHashes[randomIndex]
		} else {
			// Find current index and move to next
			currentIndex := -1
			for i, hash := range SortedSongHashes {
				if hash == musicReader.CurrentSongHash {
					currentIndex = i
					break
				}
			}
			nextIndex := currentIndex + 1
			if currentIndex == -1 || nextIndex >= len(SortedSongHashes) {
				nextIndex = 0
			}
			MusicReader.CurrentSongHash = SortedSongHashes[nextIndex]
		}
	}
	
	// Determine the NEXT song to be cached and pre-transcoded
	// Priority 1: Check if there are songs in the playlist
	var nextHash string
	if len(musicReader.Playlist) > 0 {
		musicReader.Lock.Lock()
		nextHash = musicReader.Playlist[0]
		musicReader.Playlist = musicReader.Playlist[1:] // Remove from playlist
		musicReader.Lock.Unlock()
	} else {
		// Priority 2: Calculate next song based on random or sequential mode
		nextHash = musicReader.GetNextMusicHash(SortedSongHashes)
	}
	musicReader.SetCachedNextHash(nextHash)

	filePath, exists := FindSongByHash(musicReader.CurrentSongHash)
	if !exists {
		Logger.Error(fmt.Sprintf("Could not find file for hash %s", musicReader.CurrentSongHash))
		musicReader.SelectNextMusic()
		return
	}
	
	// Transcode to standard format for consistent stream quality
	transcodedPath, err := TranscodeAudio(filePath)
	if err == nil {
		filePath = transcodedPath
	}
	
	// Always pre-transcode the next song (whether it's from playlist or random/sequential)
	nextFilePath, nextExists := FindSongByHash(nextHash)
	if nextExists {
		go PreTranscodeAudioAsync(nextFilePath)
	}
	
	file, err := os.Open(filePath)
	if err != nil {
		Logger.Error(err)
		musicReader.SelectNextMusic()
		return
	}
	MusicReader.File = file

	MusicReader.ResetMusicInfo(filePath)
}

func (musicReader *IMusicReader) ResetMusicInfo(filePath string) {
	tag, err := id3v2.Open(filePath, id3v2.Options{Parse: true})
	if err != nil {
		Logger.Error(err)
		return
	}

	title := tag.Title()
	if title == "" {
		title = filepath.Base(filePath)
	}
	artist := tag.Artist()
	if artist == "" {
		artist = "Unknown"
	}

	// Extract filename without .mp3 extension
	filename := filepath.Base(filePath)
	if strings.HasSuffix(filename, ".mp3") {
		filename = filename[:len(filename)-4]
	}

	// Read first frame to get bitrate and sample rate
	sampleRate := ""
	bitRate := ""
	frame := mp3lib.NextFrame(musicReader.File)
	if frame != nil {
		sampleRate = fmt.Sprintf("%d", frame.SamplingRate)
		bitRate = fmt.Sprintf("%d", frame.BitRate/1000) // Convert to kbps
	}
	// Reset file position by reopening
	musicReader.CloseFile()
	file, err := os.Open(filePath)
	if err != nil {
		Logger.Error(err)
		return
	}
	musicReader.File = file

	musicInfo := IMusicInfoStoreData{
		Title:      title,
		Artist:     artist,
		Filename:   filename,
		SampleRate: sampleRate,
		BitRate:    bitRate,
	}

	musicReader.SetInfoStoreData(musicInfo)
}

func (musicReader *IMusicReader) CloseFile() {
	if musicReader.File != nil {
		err := musicReader.File.Close()
		if err != nil {
			Logger.Error(err)
		}
		musicReader.File = nil
	}
}

func (musicReader *IMusicReader) NoFile() bool {
	return musicReader.File == nil
}

func (musicReader *IMusicReader) GetMusicInfoStoreData() *IMusicInfoStoreData {
	info, ok := musicReader.Store.Load(musicReader.InfoStoreKey)
	if !ok {
		return nil
	}
	data := info.(IMusicInfoStoreData)
	return &data
}

func (musicReader *IMusicReader) GetMusicInfo() *IMusicInfo {
	info := musicReader.GetMusicInfoStoreData()
	return &IMusicInfo{
		Url:        "/",
		Title:      info.Title,
		Artist:     info.Artist,
		SampleRate: info.SampleRate,
		BitRate:    info.BitRate,
		Filename:   info.Filename,
	}
}

// GetNextMusicInfo returns info about the next song without loading it
func (musicReader *IMusicReader) GetNextMusicInfo() *IMusicInfo {
	_, err := GetMp3FilePaths()
	if err != nil {
		Logger.Error(err)
		return nil
	}
	
	if len(SortedSongHashes) == 0 {
		return nil
	}
	
	// Get or calculate the cached next hash
	nextHash := musicReader.GetCachedNextHash()
	if nextHash == "" {
		nextHash = musicReader.GetNextMusicHash(SortedSongHashes)
		musicReader.SetCachedNextHash(nextHash)
	}
	
	nextFilePath, exists := FindSongByHash(nextHash)
	if !exists {
		Logger.Error(fmt.Sprintf("Could not find file for hash %s", nextHash))
		return nil
	}
	
	// Extract metadata without loading the file
	tag, err := id3v2.Open(nextFilePath, id3v2.Options{Parse: true})
	if err != nil {
		Logger.Error(err)
		return nil
	}
	defer tag.Close()
	
	title := tag.Title()
	if title == "" {
		title = filepath.Base(nextFilePath)
	}
	artist := tag.Artist()
	if artist == "" {
		artist = "Unknown"
	}
	
	// Extract filename without .mp3 extension
	filename := filepath.Base(nextFilePath)
	if strings.HasSuffix(filename, ".mp3") {
		filename = filename[:len(filename)-4]
	}
	
	// Try to detect bitrate and sample rate by reading first frame
	tempFile, err := os.Open(nextFilePath)
	if err != nil {
		return &IMusicInfo{
			Title:    title,
			Artist:   artist,
			Filename: filename,
		}
	}
	defer tempFile.Close()
	
	sampleRate := ""
	bitRate := ""
	frame := mp3lib.NextFrame(tempFile)
	if frame != nil {
		sampleRate = fmt.Sprintf("%d", frame.SamplingRate)
		bitRate = fmt.Sprintf("%d", frame.BitRate/1000)
	}
	
	return &IMusicInfo{
		Url:        "/",
		Title:      title,
		Artist:     artist,
		Filename:   filename,
		SampleRate: sampleRate,
		BitRate:    bitRate,
	}
}

func (musicReader *IMusicReader) GetBufferStoreData() *IMusicReaderStoreData {
	store, ok := musicReader.Store.Load(musicReader.BufferStoreKey)
	if !ok {
		return nil
	}
	data := store.(IMusicReaderStoreData)
	return &data
}

func (musicReader *IMusicReader) SetInfoStoreData(data IMusicInfoStoreData) {
	musicReader.Store.Store(musicReader.InfoStoreKey, data)
}

func (musicReader *IMusicReader) SetBufferStoreData(data IMusicReaderStoreData) {
	musicReader.Store.Store(musicReader.BufferStoreKey, data)
}

// Thread-safe getter for CachedNextHash
func (musicReader *IMusicReader) GetCachedNextHash() string {
	musicReader.Lock.RLock()
	defer musicReader.Lock.RUnlock()
	return musicReader.CachedNextHash
}

// Thread-safe setter for CachedNextHash
func (musicReader *IMusicReader) SetCachedNextHash(hash string) {
	musicReader.Lock.Lock()
	defer musicReader.Lock.Unlock()
	musicReader.CachedNextHash = hash
}

// AddToPlaylist adds a song hash to the end of the playlist
func (musicReader *IMusicReader) AddToPlaylist(hash string) {
	musicReader.Lock.Lock()
	defer musicReader.Lock.Unlock()
	musicReader.Playlist = append(musicReader.Playlist, hash)
}

// RemoveFromPlaylist removes a song at a specific position from the playlist
func (musicReader *IMusicReader) RemoveFromPlaylist(index int) bool {
	musicReader.Lock.Lock()
	defer musicReader.Lock.Unlock()
	
	if index < 0 || index >= len(musicReader.Playlist) {
		return false
	}
	
	musicReader.Playlist = append(musicReader.Playlist[:index], musicReader.Playlist[index+1:]...)
	return true
}

// GetPlaylist returns a copy of the current playlist
func (musicReader *IMusicReader) GetPlaylist() []string {
	musicReader.Lock.RLock()
	defer musicReader.Lock.RUnlock()
	
	playlist := make([]string, len(musicReader.Playlist))
	copy(playlist, musicReader.Playlist)
	return playlist
}

// ClearPlaylist clears all songs from the playlist
func (musicReader *IMusicReader) ClearPlaylist() {
	musicReader.Lock.Lock()
	defer musicReader.Lock.Unlock()
	musicReader.Playlist = []string{}
}

// ReorderPlaylist changes the order of songs in the playlist
// moveFrom: current index
// moveTo: target index
func (musicReader *IMusicReader) ReorderPlaylist(moveFrom, moveTo int) bool {
	musicReader.Lock.Lock()
	defer musicReader.Lock.Unlock()
	
	if moveFrom < 0 || moveFrom >= len(musicReader.Playlist) || moveTo < 0 || moveTo >= len(musicReader.Playlist) {
		return false
	}
	
	// Remove the item from source
	item := musicReader.Playlist[moveFrom]
	musicReader.Playlist = append(musicReader.Playlist[:moveFrom], musicReader.Playlist[moveFrom+1:]...)
	
	// Insert at destination
	newPlaylist := make([]string, 0, len(musicReader.Playlist)+1)
	newPlaylist = append(newPlaylist, musicReader.Playlist[:moveTo]...)
	newPlaylist = append(newPlaylist, item)
	newPlaylist = append(newPlaylist, musicReader.Playlist[moveTo:]...)
	
	musicReader.Playlist = newPlaylist
	return true
}


func (musicReader *IMusicReader) Sleep() {
	store := musicReader.GetBufferStoreData()
	if store != nil {
		time.Sleep(time.Millisecond * time.Duration(store.Timeout))
	}
}

// SkipToNext forces the reader to skip to the next song
func (musicReader *IMusicReader) SkipToNext() {
	musicReader.CloseFile()
	musicReader.SelectNextMusic()
}

func (musicReader *IMusicReader) SetInitialBuffer() {
	var initialBuffer []byte
	var unitBuffer []byte

	var timeout = 0
	var sampleRate string
	var bitRate string

	for i := 0; i < musicReader.InitialFrame; i++ {
		frame := mp3lib.NextFrame(musicReader.File)
		if frame == nil {
			musicReader.CloseFile()
			continue
		}
		initialBuffer = append(initialBuffer, frame.RawBytes...)

		// Capture sample rate and bitrate from first frame
		if i == 0 {
			sampleRate = fmt.Sprintf("%d", frame.SamplingRate)
			bitRate = fmt.Sprintf("%d", frame.BitRate/1000) // Convert to kbps
		}

		if i >= musicReader.InitialFrame-musicReader.UnitFrame {

			unitBuffer = append(unitBuffer, frame.RawBytes...)

			timeout += 1000 * frame.SampleCount / frame.SamplingRate
		}
	}

	// Update music info with sample rate and bitrate
	info := musicReader.GetMusicInfoStoreData()
	if info != nil {
		info.SampleRate = sampleRate
		info.BitRate = bitRate
		musicReader.SetInfoStoreData(*info)
	}

	musicReader.SetBufferStoreData(IMusicReaderStoreData{
		InitialBuffer: initialBuffer,
		UnitBuffer:    unitBuffer,
		Timeout:       timeout,
		Order:         1,
	})
}

func (musicReader *IMusicReader) SetUnitBuffer() {
	var unitBuffer []byte
	var timeout = 0
	maxRetries := 5
	retry := 0

	for {
		unitBuffer = nil
		timeout = 0

		// Try to read frames from current file
		for i := 0; i < musicReader.UnitFrame; i++ {
			frame := mp3lib.NextFrame(musicReader.File)
			if frame == nil {
				musicReader.CloseFile()
				continue
			}
			unitBuffer = append(unitBuffer, frame.RawBytes...)
			timeout += 1000 * frame.SampleCount / frame.SamplingRate
		}

		// If we got frames, we're done
		if len(unitBuffer) > 0 {
			break
		}

		// No frames read - file is exhausted or we have no file
		if musicReader.NoFile() {
			musicReader.SelectNextMusic()
			retry++
			if retry > maxRetries {
				Logger.Error("Failed to load next music after multiple retries")
				return
			}
			// Continue loop to try reading from new file
			continue
		}

		// Should not reach here, but break to avoid infinite loop
		Logger.Error("Unable to read frames and no valid file")
		return
	}

	store := musicReader.GetBufferStoreData()

	initialBuffer := store.InitialBuffer[:]
	// Only shift if we have enough data to shift
	if len(initialBuffer) > len(unitBuffer) {
		initialBuffer = initialBuffer[len(unitBuffer):]
	} else {
		initialBuffer = []byte{}
	}
	initialBuffer = append(initialBuffer, unitBuffer...)

	musicReader.SetBufferStoreData(IMusicReaderStoreData{
		InitialBuffer: initialBuffer,
		UnitBuffer:    unitBuffer,
		Timeout:       timeout,
		Order:         store.Order + 1,
	})
}

func (musicReader *IMusicReader) StartLoop() {
	var lastMode bool
	
	for {
		// Check current mode
		musicReader.Lock.RLock()
		currentMode := musicReader.IsIcecastMode
		musicReader.Lock.RUnlock()
		
		// Mode changed - pause feeding briefly for clean transition
		if currentMode != lastMode {
			lastMode = currentMode
			Logger.Info(fmt.Sprintf("Mode changed (Icecast: %v) - transitioning...", currentMode))
			// Don't clear buffer - let new source take over naturally
			// This prevents audio corruption from empty frames
			time.Sleep(200 * time.Millisecond)
			continue
		}
		
		// Skip feeding if in Icecast mode (Icecast processor handles it)
		if currentMode {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		
		// File mode - feed from files
		if musicReader.NoFile() {
			musicReader.SelectNextMusic()
		}
		store := musicReader.GetBufferStoreData()
		// If store is nil OR store has empty buffers (just cleared), rebuild initial buffer
		if store == nil || len(store.InitialBuffer) == 0 {
			musicReader.SetInitialBuffer()
		} else {
			musicReader.SetUnitBuffer()
		}

		musicReader.Sleep()
	}
}

// ClearBuffer resets the buffer store and increments Order significantly
// Used when switching between file and Icecast modes
func (musicReader *IMusicReader) ClearBuffer() {
	musicReader.Lock.Lock()
	defer musicReader.Lock.Unlock()
	
	// Clear by storing empty data with a big Order increment
	// This forces all listening clients to notice the change
	store := musicReader.GetBufferStoreData()
	newOrder := 1
	if store != nil {
		newOrder = store.Order + 1000 // Big jump to ensure clients notice
	}
	
	musicReader.SetBufferStoreData(IMusicReaderStoreData{
		InitialBuffer: []byte{},
		UnitBuffer:    []byte{},
		Timeout:       100,
		Order:         newOrder,
	})
}

// EnableIcecastMode switches to Icecast streaming mode
func (musicReader *IMusicReader) EnableIcecastMode() {
	musicReader.Lock.Lock()
	musicReader.IsIcecastMode = true
	musicReader.Lock.Unlock()
	Logger.Info("Icecast mode enabled - awaiting stream source")
}

// DisableIcecastMode switches back to file streaming mode
func (musicReader *IMusicReader) DisableIcecastMode() {
	musicReader.Lock.Lock()
	musicReader.IsIcecastMode = false
	musicReader.Lock.Unlock()
	
	// Signal Icecast processor to stop
	select {
	case musicReader.IcecastStopCh <- struct{}{}:
	default:
	}
	Logger.Info("Icecast mode disabled - reverting to file streaming")
}

// FeedIcecastChunk accepts normalized audio chunks from the Icecast feeder
func (musicReader *IMusicReader) FeedIcecastChunk(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	
	// Try to send with a small timeout instead of immediately dropping
	select {
	case musicReader.IcecastChunks <- data:
		return nil
	case <-time.After(10 * time.Millisecond):
		// Try one more time before giving up
		select {
		case musicReader.IcecastChunks <- data:
			return nil
		default:
			Logger.Debug("Icecast chunk buffer full, dropping chunk")
			return nil
		}
	}
}

// ProcessIcecastStream handles incoming Icecast chunks and buffers them
// Unlike file streaming which parses frames, live stream chunks are passed as-is
// to avoid breaking MP3 frame boundaries
func (musicReader *IMusicReader) ProcessIcecastStream() {
	var initialBuffer []byte
	// Use same buffer concept as file reader:
	// - InitialBuffer: First 64KB (similar to 500 frames)
	// - UnitBuffer: ~8KB units (similar to 50 frames) for consistent updates
	const targetInitialSize = 64 * 1024
	const targetUnitSize = 8 * 1024   // Accumulate chunks into 8KB units before updating
	
	var unitBuffer []byte              // Accumulate chunks until we have a unit
	initialized := false
	order := 0
	chunkCount := 0

	Logger.Info("Icecast stream processor started - buffering live stream...")
	defer Logger.Info("Icecast stream processor stopped")

	for {
		// Check for stop signal with higher priority
		select {
		case <-musicReader.IcecastStopCh:
			return
		default:
		}

		// Try to get a chunk with short timeout
		select {
		case <-musicReader.IcecastStopCh:
			return
		case chunk := <-musicReader.IcecastChunks:
			if len(chunk) == 0 {
				continue
			}

			chunkCount++
			unitBuffer = append(unitBuffer, chunk...)

			// Check if we have accumulated enough for a unit
			if len(unitBuffer) < targetUnitSize && len(initialBuffer) > 0 {
				// Keep accumulating chunks into unit
				continue
			}

			// We have a unit-sized buffer - process it
			if !initialized {
				// Still building initial buffer
				initialBuffer = append(initialBuffer, unitBuffer...)
				unitBuffer = nil
				
				if len(initialBuffer) >= targetInitialSize {
					// Got enough data for initial buffer
					// Send initial buffer to all connected clients
					order = 1
					timeout := 50 // Consistent timeout like file reader
					musicReader.SetBufferStoreData(IMusicReaderStoreData{
						InitialBuffer: initialBuffer,
						UnitBuffer:    unitBuffer, // Empty unit to start
						Timeout:       timeout,
						Order:         order,
					})
					initialized = true
					Logger.Info(fmt.Sprintf("Icecast stream ready (%d KB, %d chunks)", len(initialBuffer)/1024, chunkCount))
				}
				continue
			}

			// Stream is initialized - use file reader pattern: shift + add
			// This is exactly like SetUnitBuffer() from file reader
			initialBuffer = append(initialBuffer, unitBuffer...)
			
			// Keep buffer size consistent (like file reader does)
			if len(initialBuffer) > targetInitialSize*3 {
				// Trim oldest data
				removeSize := len(initialBuffer) - targetInitialSize*2
				initialBuffer = initialBuffer[removeSize:]
			}
			
			order++
			timeout := 50 // Consistent timing like file reader
			musicReader.SetBufferStoreData(IMusicReaderStoreData{
				InitialBuffer: initialBuffer,
				UnitBuffer:    unitBuffer,
				Timeout:       timeout,
				Order:         order,
			})
			
			unitBuffer = nil // Reset unit for next cycle

		case <-time.After(100 * time.Millisecond):
			// No chunks arriving - flush accumulated unit if we have one
			if len(unitBuffer) > 0 && initialized {
				initialBuffer = append(initialBuffer, unitBuffer...)
				if len(initialBuffer) > targetInitialSize*3 {
					removeSize := len(initialBuffer) - targetInitialSize*2
					initialBuffer = initialBuffer[removeSize:]
				}
				order++
				timeout := 50
				musicReader.SetBufferStoreData(IMusicReaderStoreData{
					InitialBuffer: initialBuffer,
					UnitBuffer:    unitBuffer,
					Timeout:       timeout,
					Order:         order,
				})
				unitBuffer = nil
			}
			
			// Check mode flag
			musicReader.Lock.RLock()
			isIcecastMode := musicReader.IsIcecastMode
			musicReader.Lock.RUnlock()
			
			if !isIcecastMode {
				// Mode was disabled, exit
				return
			}

			if !initialized {
				Logger.Debug(fmt.Sprintf("Waiting for Icecast stream... (%d KB initial, %d KB unit)", len(initialBuffer)/1024, len(unitBuffer)/1024))
			}
		}
	}
}

func GetMp3FilePaths() ([]string, error) {
	type fileInfo struct {
		path    string
		modTime time.Time
	}
	var mp3Files []fileInfo
	err := filepath.Walk(Config.Directory, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(strings.ToLower(info.Name()), ".mp3") {
			mp3Files = append(mp3Files, fileInfo{path, info.ModTime()})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if len(mp3Files) == 0 {
		Logger.Error("There are no MP3 files in the music directory.")
		return nil, fmt.Errorf("no mp3 files found in %s", Config.Directory)
	}

	// Sort alphabetically by path
	sort.Slice(mp3Files, func(i, j int) bool {
		return mp3Files[i].path < mp3Files[j].path
	})

	// Extract paths and generate hashes
	result := make([]string, len(mp3Files))
	hashes := make([]string, len(mp3Files))
	
	for i, f := range mp3Files {
		result[i] = f.path
		hash := GenerateSongHash(f.path)
		hashes[i] = hash
		SongHashMap.Store(hash, f.path)
	}
	
	// Store sorted hashes globally
	SortedSongHashes = hashes
	
	return result, nil
}

func InitReader() {
	go func() {
		MusicReader.StartLoop()
	}()
	Logger.Info(fmt.Sprintf("Music directory is %s.", Config.Directory))
	
	// Start cache cleanup routine for normalized audio cache
	StartCacheCleanupRoutine()
}
