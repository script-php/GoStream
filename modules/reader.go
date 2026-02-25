package modules

import (
	_ "embed"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bogem/id3v2/v2"
	"github.com/dmulholl/mp3lib"
)

type IMusicReader struct {
	InitialFrame int
	UnitFrame    int

	Index          int
	CachedNextIndex int  // Cache the predicted next index to keep it consistent
	File           *os.File

	Store          *sync.Map
	BufferStoreKey string
	InfoStoreKey   string

	Lock sync.RWMutex
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

	Index:           0, // Current index in the playlist
	CachedNextIndex: -1, // Cache for the next index prediction, -1 means not set
	File:            nil, // Currently open file handle for the song being read

	Store:          &sync.Map{}, // Thread-safe store for sharing data between reader and routes
	BufferStoreKey: "Store", // Key for storing the current buffer data (initial buffer + unit buffer)
	InfoStoreKey:   "Info", // Key for storing current music info (title, artist, etc.)
}

type IMusicReaderStoreData struct {
	InitialBuffer []byte
	UnitBuffer    []byte
	Timeout       int
	Order         int
}

// GetNextMusicIndex calculates what the next music index would be
func (musicReader *IMusicReader) GetNextMusicIndex(mp3FilePaths []string) int {
	if len(mp3FilePaths) == 0 {
		return 0
	}
	
	if Config.Random {
		return rand.Intn(len(mp3FilePaths))
	} else {
		nextIndex := musicReader.Index + 1
		if nextIndex >= len(mp3FilePaths) {
			nextIndex = 0
		}
		return nextIndex
	}
}

func (musicReader *IMusicReader) SelectNextMusic() {
	mp3FilePaths, err := GetMp3FilePaths()
	if err != nil {
		Logger.Error(err)
		return
	}
	
	// Use cached next index if available (from /next prediction or previous song)
	// Otherwise calculate it
	cachedIndex := musicReader.GetCachedNextIndex()
	if cachedIndex >= 0 && cachedIndex < len(mp3FilePaths) {
		MusicReader.Index = cachedIndex
	} else {
		if Config.Random {
			MusicReader.Index = rand.Intn(len(mp3FilePaths))
		} else {
			MusicReader.Index += 1
			if MusicReader.Index >= len(mp3FilePaths) {
				MusicReader.Index = 0
			}
		}
	}
	
	// Cache the next index for after this song
	nextIndex := musicReader.GetNextMusicIndex(mp3FilePaths)
	musicReader.SetCachedNextIndex(nextIndex)

	filePath := mp3FilePaths[MusicReader.Index]
	
	// Transcode to standard format if normalization is enabled
	if Config.Normalize {
		transcodedPath, err := TranscodeAudio(filePath)
		if err == nil {
			filePath = transcodedPath
		}
		
		// Always pre-transcode the next song (from any song in list, check if cached, transcode if needed)
		nextFilePath := mp3FilePaths[nextIndex]
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
	mp3FilePaths, err := GetMp3FilePaths()
	if err != nil {
		Logger.Error(err)
		return nil
	}
	
	if len(mp3FilePaths) == 0 {
		return nil
	}
	
	// Get or calculate the cached next index
	cachedIndex := musicReader.GetCachedNextIndex()
	if cachedIndex < 0 || cachedIndex >= len(mp3FilePaths) {
		cachedIndex = musicReader.GetNextMusicIndex(mp3FilePaths)
		musicReader.SetCachedNextIndex(cachedIndex)
	}
	
	nextFilePath := mp3FilePaths[cachedIndex]
	
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

// Thread-safe getter for CachedNextIndex
func (musicReader *IMusicReader) GetCachedNextIndex() int {
	musicReader.Lock.RLock()
	defer musicReader.Lock.RUnlock()
	return musicReader.CachedNextIndex
}

// Thread-safe setter for CachedNextIndex
func (musicReader *IMusicReader) SetCachedNextIndex(index int) {
	musicReader.Lock.Lock()
	defer musicReader.Lock.Unlock()
	musicReader.CachedNextIndex = index
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
	initialBuffer = initialBuffer[len(unitBuffer):]
	initialBuffer = append(initialBuffer, unitBuffer...)

	musicReader.SetBufferStoreData(IMusicReaderStoreData{
		InitialBuffer: initialBuffer,
		UnitBuffer:    unitBuffer,
		Timeout:       timeout,
		Order:         store.Order + 1,
	})
}

func (musicReader *IMusicReader) StartLoop() {
	for {
		if musicReader.NoFile() {
			musicReader.SelectNextMusic()
		}
		store := musicReader.GetBufferStoreData()
		if store == nil {
			musicReader.SetInitialBuffer()
		} else {
			musicReader.SetUnitBuffer()
		}

		musicReader.Sleep()
	}
}

func GetMp3FilePaths() ([]string, error) {
	var mp3Files []string
	err := filepath.Walk(Config.Directory, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(strings.ToLower(info.Name()), ".mp3") {
			mp3Files = append(mp3Files, path)
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
	return mp3Files, nil
}

func InitReader() {
	go func() {
		MusicReader.StartLoop()
	}()
	Logger.Info(fmt.Sprintf("Music directory is %s.", Config.Directory))
	
	// Start cache cleanup routine if normalization is enabled
	if Config.Normalize {
		StartCacheCleanupRoutine()
	}
}
