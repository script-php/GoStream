package routes

import (
	"fmt"
	"gostream/modules"
	"gostream/tools"
	"net/http"
	"path/filepath"
	"time"

	"github.com/bogem/id3v2/v2"
	"github.com/labstack/echo/v4"
)

func GetServerInfo(ctx echo.Context) error {
	musicInfo := modules.MusicReader.GetMusicInfo()
	err := ctx.JSON(http.StatusOK, tools.Response.GetResponseBody(struct {
		Name    string              `json:"name"`
		Version string              `json:"version"`
		Time    int64               `json:"time"`
		FMInfo  *modules.IMusicInfo `json:"FMInfo"`
	}{
		Name:    modules.Config.Name,
		Version: modules.Config.Version,
		Time:    modules.Config.Time,
		FMInfo:  musicInfo,
	}))
	if err != nil {
		modules.Logger.Error(err)
		return err
	}
	return nil
}

// GetStats returns current stream stats in Icecast-compatible format
func GetStats(ctx echo.Context) error {
	musicInfo := modules.MusicReader.GetMusicInfo()
	
	stats := map[string]interface{}{
		"icestats": map[string]interface{}{
			"source": map[string]interface{}{
				"title":       musicInfo.Filename,
				"artist":      musicInfo.Artist,
				"name":        modules.Config.Name,
				"description": modules.Config.Name,
				"genre":       "Stream",
				"bitrate":     musicInfo.BitRate,
				"samplerate":  musicInfo.SampleRate,
			},
		},
	}
	
	return ctx.JSON(http.StatusOK, stats)
}

// SkipSong skips to the next song
func SkipSong(ctx echo.Context) error {
	modules.MusicReader.SkipToNext()
	musicInfo := modules.MusicReader.GetMusicInfo()
	
	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"status": "skipped",
		"now_playing": map[string]interface{}{
			"title":    musicInfo.Filename,
			"artist":   musicInfo.Artist,
			"bitrate":  musicInfo.BitRate,
			"samplerate": musicInfo.SampleRate,
		},
	})
}

// GetStreamStatus returns the current stream status
func GetStreamStatus(ctx echo.Context) error {
	musicInfo := modules.MusicReader.GetMusicInfo()
	
	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"status": "playing",
		"now_playing": map[string]interface{}{
			"title":      musicInfo.Filename,
			"artist":     musicInfo.Artist,
			"bitrate":    musicInfo.BitRate,
			"samplerate": musicInfo.SampleRate,
		},
	})
}

// GetNextSong returns info about the next song
func GetNextSong(ctx echo.Context) error {
	nextInfo := modules.MusicReader.GetNextMusicInfo()
	
	if nextInfo == nil {
		return ctx.JSON(http.StatusOK, map[string]interface{}{
			"status": "error",
			"message": "Could not determine next song",
		})
	}
	
	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"status": "success",
		"next_song": map[string]interface{}{
			"title":      nextInfo.Filename,
			"artist":     nextInfo.Artist,
			"bitrate":    nextInfo.BitRate,
			"samplerate": nextInfo.SampleRate,
		},
	})
}

// GetMetrics returns system and stream metrics
func GetMetrics(ctx echo.Context) error {
	metricsData := modules.GetMetrics()
	
	// Format bytes to human-readable format
	formatBytes := func(bytes int64) string {
		const (
			KB = 1024
			MB = KB * 1024
			GB = MB * 1024
		)
		
		switch {
		case bytes >= GB:
			return fmt.Sprintf("%.2f GB", float64(bytes)/float64(GB))
		case bytes >= MB:
			return fmt.Sprintf("%.2f MB", float64(bytes)/float64(MB))
		case bytes >= KB:
			return fmt.Sprintf("%.2f KB", float64(bytes)/float64(KB))
		default:
			return fmt.Sprintf("%d B", bytes)
		}
	}
	
	// Format uptime
	uptimeSeconds := (time.Now().UnixMilli() - metricsData.StreamStartTime) / 1000
	hours := uptimeSeconds / 3600
	minutes := (uptimeSeconds % 3600) / 60
	seconds := uptimeSeconds % 60
	uptimeFormatted := fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
	
	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"status": "success",
		"metrics": map[string]interface{}{
			"active_listeners": metricsData.ActiveListeners,
			"total_data_streamed": map[string]interface{}{
				"bytes":      metricsData.TotalBytesStreamed,
				"human":      formatBytes(metricsData.TotalBytesStreamed),
			},
			"stream_uptime": map[string]interface{}{
				"seconds":    uptimeSeconds,
				"formatted":  uptimeFormatted,
			},
			"memory": map[string]interface{}{
				"current_usage_mb": metricsData.MemoryUsage,
				"heap_alloc_mb":    metricsData.MemoryHeapAlloc,
				"heap_sys_mb":      metricsData.MemoryHeapSys,
				"total_alloc_mb":   metricsData.MemoryTotalAlloc,
				"sys_mb":           metricsData.MemorySys,
			},
			"garbage_collection": map[string]interface{}{
				"gc_runs":         metricsData.GCRuns,
				"gc_pause_ms":     fmt.Sprintf("%.2f ms", metricsData.GCPauseMs),
				"gc_pause_raw_ms": metricsData.GCPauseMs,
			},
			"system": map[string]interface{}{
				"goroutines": metricsData.NumGoroutines,
			},
			"bandwidth": map[string]interface{}{
				"current_mbps": fmt.Sprintf("%.2f Mbps", metricsData.BandwidthMbps),
				"raw_mbps":     metricsData.BandwidthMbps,
			},
		},
	})
}

// GetSongsList returns a list of all songs with their hash IDs
func GetSongsList(ctx echo.Context) error {
	mp3FilePaths, err := modules.GetMp3FilePaths()
	if err != nil {
		modules.Logger.Error(err)
		return ctx.JSON(http.StatusOK, map[string]interface{}{
			"status": "error",
			"message": "Could not retrieve songs list",
		})
	}

	type SongItem struct {
		Hash     string `json:"hash"`
		Title    string `json:"title"`
		Artist   string `json:"artist"`
		Filename string `json:"filename"`
	}

	var songs []SongItem

	for _, filePath := range mp3FilePaths {
		hash := modules.GenerateSongHash(filePath)
		tag, err := id3v2.Open(filePath, id3v2.Options{Parse: true})
		if err != nil {
			modules.Logger.Error(err)
			// If we can't read ID3 tags, use filename
			songs = append(songs, SongItem{
				Hash:     hash,
				Title:    filepath.Base(filePath),
				Artist:   "Unknown",
				Filename: filepath.Base(filePath),
			})
			continue
		}

		title := tag.Title()
		if title == "" {
			title = filepath.Base(filePath)
		}

		artist := tag.Artist()
		if artist == "" {
			artist = "Unknown"
		}

		songs = append(songs, SongItem{
			Hash:     hash,
			Title:    title,
			Artist:   artist,
			Filename: filepath.Base(filePath),
		})
		tag.Close()
	}

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"status": "success",
		"total":  len(songs),
		"songs":  songs,
	})
}

// SetNextSong sets the next song to be played by its hash
func SetNextSong(ctx echo.Context) error {
	hash := ctx.QueryParam("hash")
	
	if hash == "" {
		return ctx.JSON(http.StatusBadRequest, map[string]interface{}{
			"status": "error",
			"message": "hash parameter is required",
		})
	}
	
	// Verify the hash exists in our song collection
	filePath, exists := modules.FindSongByHash(hash)
	if !exists {
		return ctx.JSON(http.StatusBadRequest, map[string]interface{}{
			"status": "error",
			"message": "song hash not found",
		})
	}
	
	// Set the cached next hash
	modules.MusicReader.CachedNextHash = hash
	
	// Pre-transcode the song in background so it's ready when it plays
	go modules.PreTranscodeAudioAsync(filePath)
	
	// Get info about the song we just set
	tag, err := id3v2.Open(filePath, id3v2.Options{Parse: true})
	title := filepath.Base(filePath)
	artist := "Unknown"
	
	if err == nil {
		if t := tag.Title(); t != "" {
			title = t
		}
		if a := tag.Artist(); a != "" {
			artist = a
		}
		tag.Close()
	}
	
	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"status": "success",
		"message": "next song set",
		"next_song": map[string]interface{}{
			"hash":     hash,
			"title":    title,
			"artist":   artist,
			"filename": filepath.Base(filePath),
		},
	})
}
