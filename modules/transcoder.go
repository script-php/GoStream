package modules

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// Standard format for normalized audio
// These defaults are used if not specified in config
var (
	DefaultBitrate   = "128k"   // Standard bitrate for transcoding
	DefaultSampleRate = "44100" // Standard sample rate for transcoding
	DefaultCacheDir  = ".cache" // Directory to store cached normalized files
)

// GetFFmpegPath returns the path to ffmpeg binary
func GetFFmpegPath() (string, error) {
	// Try system PATH first
	if path, err := exec.LookPath("ffmpeg"); err == nil {
		return path, nil
	}

	// Try bundled ffmpeg
	var bundledPath string
	if runtime.GOOS == "windows" {
		bundledPath = filepath.Join("ffmpeg", "windows", "ffmpeg.exe")
	} else {
		bundledPath = filepath.Join("ffmpeg", "linux", "ffmpeg")
	}

	if _, err := os.Stat(bundledPath); err == nil {
		return bundledPath, nil
	}

	return "", fmt.Errorf("ffmpeg not found in PATH or bundled directory")
}

// GetCachedPath returns the path where a normalized file should be cached
func GetCachedPath(originalPath string) string {
	filename := filepath.Base(originalPath)
	return filepath.Join(Config.CacheDir, filename)
}

// IsCached checks if a normalized version exists in cache
func IsCached(originalPath string) bool {
	cachedPath := GetCachedPath(originalPath)
	_, err := os.Stat(cachedPath)
	return err == nil
}

// TranscodeAudio transcodes an MP3 file to standard format
// Returns the path to the normalized file (either cached or original)
// Note: Normalization is always enabled for consistent stream quality
func TranscodeAudio(filePath string) (string, error) {
	// Check if already cached
	cachedPath := GetCachedPath(filePath)
	if IsCached(filePath) {
		Logger.Info(fmt.Sprintf("Using cached version: %s", cachedPath))
		return cachedPath, nil
	}

	// Ensure cache directory exists
	if err := os.MkdirAll(Config.CacheDir, 0755); err != nil {
		Logger.Error(fmt.Sprintf("Failed to create cache directory: %v", err))
		return filePath, nil // Fallback to original
	}

	// Get ffmpeg path
	ffmpegPath, err := GetFFmpegPath()
	if err != nil {
		Logger.Error(fmt.Sprintf("FFmpeg not found: %v", err))
		return filePath, nil // Fallback to original
	}

	// Run ffmpeg transcoding
	Logger.Info(fmt.Sprintf("Transcoding: %s", filepath.Base(filePath)))
	cmd := exec.Command(ffmpegPath,
		"-i", filePath,
		"-b:a", Config.StandardBitrate,
		"-ar", Config.StandardSampleRate,
		"-y", // Overwrite if exists
		cachedPath,
	)

	// Suppress output
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Run(); err != nil {
		Logger.Error(fmt.Sprintf("Transcoding failed: %v", err))
		return filePath, nil // Fallback to original
	}

	Logger.Info(fmt.Sprintf("Transcoded successfully: %s", filepath.Base(cachedPath)))
	return cachedPath, nil
}

// CleanupCache removes all cached files
func CleanupCache() error {
	return os.RemoveAll(Config.CacheDir)
}

// CacheSize returns the total size of cached files in MB
func CacheSize() (float64, error) {
	var size int64
	err := filepath.Walk(Config.CacheDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return float64(size) / (1024 * 1024), err // Convert to MB
}

// PreTranscodeAudioAsync transcodes in background without blocking
// Used for pre-caching the next song while current one plays
func PreTranscodeAudioAsync(filePath string) {
	// Skip if already cached
	if IsCached(filePath) {
		return
	}
	
	// Transcode in background
	_, err := TranscodeAudio(filePath)
	if err != nil {
		// Silently fail - not critical if pre-transcoding fails
		return
	}
	
	Logger.Info(fmt.Sprintf("Pre-transcoded (cache): %s", filepath.Base(filePath)))
}

// CleanOldCacheFiles removes cache files older than the configured TTL
func CleanOldCacheFiles() error {
	if Config.CacheTTLMinutes <= 0 {
		// Cleanup disabled
		return nil
	}

	cacheDir := Config.CacheDir
	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		return nil // Cache directory doesn't exist
	}

	ttlDuration := time.Duration(Config.CacheTTLMinutes) * time.Minute
	now := time.Now()
	deletedCount := 0
	totalSize := int64(0)

	err := filepath.Walk(cacheDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Check if file is older than TTL
		fileAge := now.Sub(info.ModTime())
		if fileAge > ttlDuration {
			if err := os.Remove(path); err != nil {
				Logger.Error(fmt.Sprintf("Failed to delete cache file %s: %v", path, err))
				return nil
			}
			deletedCount++
			totalSize += info.Size()
			Logger.Info(fmt.Sprintf("Deleted old cache file: %s (age: %v)", filepath.Base(path), fileAge))
		}

		return nil
	})

	if deletedCount > 0 {
		freedMB := float64(totalSize) / (1024 * 1024)
		Logger.Info(fmt.Sprintf("Cache cleanup: Deleted %d files, freed %.2f MB", deletedCount, freedMB))
	}

	return err
}

// StartCacheCleanupRoutine starts a background routine to periodically clean old cache files
func StartCacheCleanupRoutine() {
	if Config.CacheTTLMinutes <= 0 {
		Logger.Info("Cache cleanup disabled (cache_ttl_minutes = 0)")
		return
	}

	go func() {
		// Run cleanup immediately on start
		if err := CleanOldCacheFiles(); err != nil {
			Logger.Error(fmt.Sprintf("Initial cache cleanup failed: %v", err))
		}

		// Then run cleanup every 5 minutes
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			if err := CleanOldCacheFiles(); err != nil {
				Logger.Error(fmt.Sprintf("Scheduled cache cleanup failed: %v", err))
			}
		}
	}()

	Logger.Info(fmt.Sprintf("Cache cleanup routine started (TTL: %d minutes, check interval: 5 minutes)", Config.CacheTTLMinutes))
}
