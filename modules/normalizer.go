package modules

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

// AudioNormalizer handles real-time audio normalization using FFmpeg
type AudioNormalizer struct {
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	stdout     io.ReadCloser
	mu         sync.Mutex
	isRunning  bool
	inputFormat string
	outputFormat string
}

// NewAudioNormalizer creates a new audio normalizer with FFmpeg
// It sets up a live FFmpeg process that normalizes audio in real-time
func NewAudioNormalizer(inputFormat, outputFormat string) (*AudioNormalizer, error) {
	ffmpegPath, err := GetFFmpegPath()
	if err != nil {
		Logger.Error(fmt.Sprintf("FFmpeg not found, normalization disabled: %v", err))
		return nil, err
	}

	// FFmpeg filters for normalization:
	// - loudnorm: EBU R128 loudness normalization
	// - anull: pass-through filter (fallback if loudnorm unavailable)
	ffmpegArgs := []string{
		"-i", "pipe:0",                    // Read from stdin
		"-f", outputFormat,                // Output format
		"-acodec", "libmp3lame",           // MP3 codec
		"-b:a", Config.StandardBitrate,    // Bitrate
		"-ar", Config.StandardSampleRate,  // Sample rate
		"-af", "loudnorm=I=-23:TP=-1.5:LRA=11", // EBU R128 normalization
		"-y",                              // Overwrite output
		"pipe:1",                          // Write to stdout
	}

	cmd := exec.Command(ffmpegPath, ffmpegArgs...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %v", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("failed to create stdout pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return nil, fmt.Errorf("failed to start ffmpeg process: %v", err)
	}

	normalizer := &AudioNormalizer{
		cmd:         cmd,
		stdin:       stdin,
		stdout:      stdout,
		isRunning:   true,
		inputFormat: inputFormat,
		outputFormat: outputFormat,
	}

	Logger.Info("Audio normalizer started with loudnorm filter")
	return normalizer, nil
}

// Write sends audio data to the normalizer
func (an *AudioNormalizer) Write(data []byte) (int, error) {
	if !an.isRunning {
		return 0, fmt.Errorf("normalizer not running")
	}

	an.mu.Lock()
	defer an.mu.Unlock()

	if an.stdin == nil {
		return 0, fmt.Errorf("normalizer stdin closed")
	}

	return an.stdin.Write(data)
}

// Read retrieves normalized audio data
func (an *AudioNormalizer) Read(p []byte) (int, error) {
	if !an.isRunning {
		return 0, fmt.Errorf("normalizer not running")
	}

	return an.stdout.Read(p)
}

// Close stops the normalizer and cleans up resources
func (an *AudioNormalizer) Close() error {
	an.mu.Lock()
	defer an.mu.Unlock()

	if !an.isRunning {
		return nil
	}

	an.isRunning = false

	if an.stdin != nil {
		an.stdin.Close()
	}

	if an.stdout != nil {
		an.stdout.Close()
	}

	if an.cmd != nil && an.cmd.Process != nil {
		an.cmd.Process.Kill()
		an.cmd.Wait()
	}

	Logger.Info("Audio normalizer stopped")
	return nil
}

// IsRunning returns whether the normalizer is active
func (an *AudioNormalizer) IsRunning() bool {
	an.mu.Lock()
	defer an.mu.Unlock()
	return an.isRunning
}

// NormalizeChunk applies normalization to a single audio chunk
// This is a simpler alternative to streaming normalization
// It normalizes MP3 data by re-encoding it with FFmpeg
func NormalizeChunk(inputData []byte, contentType string) ([]byte, error) {
	if len(inputData) == 0 {
		return inputData, nil
	}

	ffmpegPath, err := GetFFmpegPath()
	if err != nil {
		// If FFmpeg not available, return original data
		Logger.Debug("FFmpeg not available, skipping normalization")
		return inputData, nil
	}

	// Determine input format from content-type
	var inputFormat string
	switch contentType {
	case "audio/mpeg":
		inputFormat = "mp3"
	case "audio/wav":
		inputFormat = "wav"
	case "audio/ogg":
		inputFormat = "ogg"
	default:
		// Unknown format, return original
		return inputData, nil
	}

	// Create FFmpeg command for normalization with timeout
	cmd := exec.Command(ffmpegPath,
		"-f", inputFormat,
		"-i", "pipe:0",
		"-f", "mp3",
		"-acodec", "libmp3lame",
		"-b:a", "128k",
		"-ar", "44100",
		"-af", "loudnorm=I=-23:TP=-1.5:LRA=11",
		"-y",
		"pipe:1",
	)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdin = bytes.NewReader(inputData)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run with timeout using a goroutine
	done := make(chan error, 1)
	go func() {
		done <- cmd.Run()
	}()

	select {
	case err := <-done:
		if err != nil {
			Logger.Debug(fmt.Sprintf("Normalization failed: %v", err))
			return inputData, nil // Return original on error
		}
	case <-time.After(2 * time.Second):
		// Timeout - kill the process
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		Logger.Debug("Normalization timeout, using original data")
		return inputData, nil
	}

	normalized := stdout.Bytes()
	if len(normalized) == 0 {
		// Empty output, return original
		return inputData, nil
	}

	return normalized, nil
}
