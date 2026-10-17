package modules

import (
	"bufio"
	"bytes"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http/httputil"
	"strings"
	"sync"
	"time"

	"github.com/dmulholl/mp3lib"
)

const (
	maxIcecastHeaderBytes = 64 * 1024
	icecastHeaderTimeout  = 10 * time.Second
)

// AudioBuffer is a simple channel-based buffer for managing audio data
type AudioBuffer struct {
	ch       chan []byte
	isActive bool
	mu       sync.Mutex
	probe    []byte
}

// NewAudioBuffer creates a new audio buffer
func NewAudioBuffer(maxSize int) *AudioBuffer {
	// Convert bytes to number of chunks (4KB chunks)
	maxChunks := (maxSize + 4095) / 4096
	if maxChunks < 10 {
		maxChunks = 10
	}

	// Increase buffer capacity by 2x to reduce drops
	bufferCapacity := maxChunks * 2
	return &AudioBuffer{
		ch:       make(chan []byte, bufferCapacity),
		isActive: true,
	}
}

// Write adds data to the buffer (tries hard not to drop, but will if buffer backs up)
func (ab *AudioBuffer) Write(data []byte) (int, error) {
	// The source connection reuses its socket read buffer. Queue an owned copy
	// so a later Read cannot overwrite MP3 bytes that are still waiting here.
	owned := append([]byte(nil), data...)

	ab.mu.Lock()
	defer ab.mu.Unlock()

	if !ab.isActive {
		return 0, fmt.Errorf("buffer closed")
	}
	if len(ab.probe) < 64*1024 {
		remaining := 64*1024 - len(ab.probe)
		if remaining > len(owned) {
			remaining = len(owned)
		}
		ab.probe = append(ab.probe, owned[:remaining]...)
	}

	// Try to send, non-blocking first
	select {
	case ab.ch <- owned:
		return len(data), nil
	default:
		// Buffer full, try to make room by draining up to 3 chunks
		for i := 0; i < 3; i++ {
			select {
			case <-ab.ch:
				// Made room, try again
				select {
				case ab.ch <- owned:
					return len(data), nil
				default:
					// Still full, continue trying to drain
					continue
				}
			default:
				// Can't drain right now, just skip
				Logger.Debug("Icecast buffer full, dropping chunk")
				return len(data), nil
			}
		}
		// Tried 3 times to drain, give up
		Logger.Debug("Icecast buffer persistently full, dropping chunk")
		return len(data), nil
	}
}

func (ab *AudioBuffer) ProbeBytes() []byte {
	ab.mu.Lock()
	defer ab.mu.Unlock()
	return append([]byte(nil), ab.probe...)
}

// ReadTimeout retrieves data from buffer with timeout
func (ab *AudioBuffer) ReadTimeout(timeout time.Duration) ([]byte, error) {
	ab.mu.Lock()
	isActive := ab.isActive
	ab.mu.Unlock()

	if !isActive {
		return nil, fmt.Errorf("buffer closed")
	}

	select {
	case data, ok := <-ab.ch:
		if !ok {
			return nil, fmt.Errorf("buffer closed")
		}
		return data, nil
	case <-time.After(timeout):
		return nil, nil
	}
}

// Close closes the buffer
func (ab *AudioBuffer) Close() {
	ab.mu.Lock()
	defer ab.mu.Unlock()

	if ab.isActive {
		ab.isActive = false
		close(ab.ch)
	}
}

// Size returns approximate buffer size
func (ab *AudioBuffer) Size() int {
	return len(ab.ch)
}

// IcecastSourceServer handles incoming Icecast source client connections
type IcecastSourceServer struct {
	Port              string
	ln                net.Listener
	audioBuffer       *AudioBuffer
	isRunning         bool
	mu                sync.RWMutex
	currentSourceConn net.Conn
	sourceMetadata    map[string]string

	// Listener tracking
	listeners   map[string]chan []byte // Map of listener ID to channel
	listenersMu sync.RWMutex
	listenerID  int64 // Counter for generating listener IDs

	// Statistics
	bytesReceived    int64
	bytesSent        int64
	sourceGeneration uint64
}

func icecastBodyReader(reader *bufio.Reader, headers map[string]string) io.Reader {
	if strings.Contains(strings.ToLower(headers["transfer-encoding"]), "chunked") {
		return httputil.NewChunkedReader(reader)
	}
	return reader
}

// IcecastSourceInstance is the singleton instance
var IcecastSource *IcecastSourceServer

// InitIcecastServer initializes the Icecast source server
func InitIcecastServer(port string) {
	IcecastSource = &IcecastSourceServer{
		Port:           port,
		listeners:      make(map[string]chan []byte),
		audioBuffer:    NewAudioBuffer(512 * 1024), // 512KB buffer like Icecast
		isRunning:      false,
		sourceMetadata: make(map[string]string),
		bytesReceived:  0,
		bytesSent:      0,
	}
	Logger.Info(fmt.Sprintf("Icecast source server initialized on port %s with 512KB buffer", port))
}

// Start begins listening for Icecast source connections
func (s *IcecastSourceServer) Start() error {
	if s.isRunning {
		return fmt.Errorf("icecast server already running")
	}

	host := Config.Host
	if host == "" {
		host = "0.0.0.0"
	}
	address := net.JoinHostPort(host, s.Port)
	ln, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %v", address, err)
	}

	s.mu.Lock()
	s.isRunning = true
	s.ln = ln
	s.mu.Unlock()

	Logger.Info(fmt.Sprintf("Icecast source server listening on %s", address))

	go func() {
		defer ln.Close()
		for {
			s.mu.RLock()
			isRunning := s.isRunning
			s.mu.RUnlock()
			if !isRunning {
				break
			}

			conn, err := ln.Accept()
			if err != nil {
				if s.isRunning {
					Logger.Debug(fmt.Sprintf("Icecast accept error: %v", err))
				}
				continue
			}

			// Handle source connection in a goroutine
			go s.handleSource(conn)
		}
	}()

	return nil
}

// Stop stops the Icecast server
func (s *IcecastSourceServer) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.isRunning = false

	if s.currentSourceConn != nil {
		s.currentSourceConn.Close()
		s.currentSourceConn = nil
	}

	if s.ln != nil {
		s.ln.Close()
	}

	// Close and recreate buffer to flush all data
	if s.audioBuffer != nil {
		s.audioBuffer.Close()
		s.audioBuffer = NewAudioBuffer(512 * 1024)
	}

	s.listenersMu.Lock()
	for _, ch := range s.listeners {
		close(ch)
	}
	s.listeners = make(map[string]chan []byte)
	s.listenersMu.Unlock()

	Logger.Info("Icecast source server stopped")
	return nil
}

// handleSource processes an incoming Icecast source client connection
func (s *IcecastSourceServer) handleSource(conn net.Conn) {
	defer conn.Close()

	remoteAddr := conn.RemoteAddr().String()
	Logger.Info(fmt.Sprintf("New Icecast source connection from %s", remoteAddr))

	reader := bufio.NewReader(conn)
	if err := conn.SetReadDeadline(time.Now().Add(icecastHeaderTimeout)); err != nil {
		Logger.Debug(fmt.Sprintf("Could not set Icecast header deadline for %s: %v", remoteAddr, err))
	}

	// Read HTTP-like headers from source client (like Icecast does)
	headers := make(map[string]string)
	contentType := ""

	headerBytes := 0
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			Logger.Error(fmt.Sprintf("Error reading Icecast headers from %s: %v", remoteAddr, err))
			return
		}
		headerBytes += len(line)
		if headerBytes > maxIcecastHeaderBytes {
			Logger.Info(fmt.Sprintf("Icecast source from %s sent oversized headers", remoteAddr))
			conn.Write([]byte("HTTP/1.0 431 Request Header Fields Too Large\r\nContent-Length: 17\r\n\r\nHeaders Too Large"))
			return
		}

		line = strings.TrimSpace(line)

		// Empty line marks end of headers
		if line == "" {
			break
		}

		// Parse header
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			key := strings.ToLower(strings.TrimSpace(parts[0]))
			value := strings.TrimSpace(parts[1])
			headers[key] = value

			if key == "content-type" {
				contentType = value
			}
		}
	}

	// Validate content-type
	if contentType == "" {
		Logger.Info(fmt.Sprintf("Icecast source from %s missing content-type, rejecting", remoteAddr))
		conn.Write([]byte("HTTP/1.0 400 Bad Request\r\nContent-Length: 11\r\n\r\nBad Request"))
		return
	}

	// The continuous live decoder uses FFmpeg, so any supported audio content
	// type can be converted into the station's configured MP3 output format.
	if !strings.HasPrefix(strings.ToLower(contentType), "audio/") {
		Logger.Info(fmt.Sprintf("Icecast source from %s unsupported content-type: %s", remoteAddr, contentType))
		conn.Write([]byte("HTTP/1.0 415 Unsupported Media Type\r\nContent-Length: 21\r\n\r\nUnsupported Media Type"))
		return
	}

	// Reuse the configured API credentials for source authentication. Both
	// values empty is the explicit unauthenticated mode; partial pairs are
	// rejected during configuration validation.
	if Config.Username != "" || Config.Password != "" {
		expected := "Basic " + base64.StdEncoding.EncodeToString([]byte(Config.Username+":"+Config.Password))
		provided := headers["authorization"]
		if subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			Logger.Info(fmt.Sprintf("Unauthorized Icecast source connection from %s", remoteAddr))
			conn.Write([]byte("HTTP/1.0 401 Unauthorized\r\nWWW-Authenticate: Basic realm=\"GoStream Source\"\r\nContent-Length: 12\r\n\r\nUnauthorized"))
			return
		}
	}

	// Store metadata
	s.mu.Lock()
	if s.currentSourceConn != nil && s.currentSourceConn != conn {
		s.currentSourceConn.Close()
	}
	s.sourceMetadata = headers
	s.currentSourceConn = conn
	s.sourceGeneration++

	// Clear the audio buffer to remove stale data
	if s.audioBuffer != nil {
		s.audioBuffer.Close()
	}
	sourceBuffer := NewAudioBuffer(512 * 1024)
	s.audioBuffer = sourceBuffer

	s.mu.Unlock()

	// Send success response
	conn.Write([]byte("HTTP/1.0 200 OK\r\n\r\n"))
	transferEncoding := headers["transfer-encoding"]
	Logger.Info(fmt.Sprintf("Icecast source from %s accepted (content-type: %s, transfer-encoding: %s)", remoteAddr, contentType, transferEncoding))

	// Read and buffer audio data
	_ = conn.SetReadDeadline(time.Time{})
	bodyReader := icecastBodyReader(reader, headers)
	buffer := make([]byte, 4096)
	for {
		// A source that disappears without a clean TCP close must not leave the
		// station stuck in live mode indefinitely.
		conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		n, err := bodyReader.Read(buffer)
		if err != nil {
			Logger.Info(fmt.Sprintf("Icecast source from %s disconnected", remoteAddr))
			s.mu.Lock()
			if s.currentSourceConn == conn {
				s.currentSourceConn = nil
				// Don't clear buffer here - let it drain naturally
			}
			s.mu.Unlock()
			return
		}

		// Write to buffer (non-blocking, drops oldest chunks if full)
		_, err = sourceBuffer.Write(buffer[:n])
		if err != nil {
			Logger.Info(fmt.Sprintf("Icecast source %s: buffer write error: %v", remoteAddr, err))
			return
		}

		s.mu.Lock()
		s.bytesReceived += int64(n)
		s.mu.Unlock()
	}
}

// AddListener registers a new listener and returns a channel for audio data
func (s *IcecastSourceServer) AddListener() (string, chan []byte) {
	s.listenersMu.Lock()
	defer s.listenersMu.Unlock()

	s.listenerID++
	id := fmt.Sprintf("listener_%d", s.listenerID)
	ch := make(chan []byte, 10) // Small buffer for each listener
	s.listeners[id] = ch

	return id, ch
}

// RemoveListener unregisters a listener
func (s *IcecastSourceServer) RemoveListener(id string) {
	s.listenersMu.Lock()
	defer s.listenersMu.Unlock()

	if ch, ok := s.listeners[id]; ok {
		close(ch)
		delete(s.listeners, id)
	}
}

// BroadcastAudio sends audio data to all listeners
func (s *IcecastSourceServer) BroadcastAudio(data []byte) {
	s.listenersMu.RLock()
	defer s.listenersMu.RUnlock()

	for _, ch := range s.listeners {
		select {
		case ch <- data:
			s.mu.Lock()
			s.bytesSent += int64(len(data))
			s.mu.Unlock()
		default:
			// Listener buffer full, skip this chunk (client fell behind)
			Logger.Debug("Listener buffer full, skipping chunk")
		}
	}
}

// GetAudioChunk retrieves the next audio chunk from the buffer
func (s *IcecastSourceServer) GetAudioChunk() ([]byte, bool) {
	s.mu.RLock()
	buffer := s.audioBuffer
	s.mu.RUnlock()
	if buffer == nil {
		return nil, false
	}

	chunk, err := buffer.ReadTimeout(100 * time.Millisecond)
	if err != nil || len(chunk) == 0 {
		return nil, false
	}
	return chunk, true
}

// HasActiveSource returns true if there's an active source connection
func (s *IcecastSourceServer) HasActiveSource() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentSourceConn != nil
}

// GetSourceMetadata returns the metadata from the current source
func (s *IcecastSourceServer) GetSourceMetadata() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Create a copy to avoid race conditions
	meta := make(map[string]string)
	for k, v := range s.sourceMetadata {
		meta[k] = v
	}
	return meta
}

// GetSourceGeneration changes whenever a new live source is accepted. The
// stream engine uses it to restart FFmpeg cleanly when one source replaces
// another without an inactive interval.
func (s *IcecastSourceServer) GetSourceGeneration() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sourceGeneration
}

// IsMP3Source reports whether the active source can be forwarded directly to
// listeners without decoding or re-encoding.
func (s *IcecastSourceServer) IsMP3Source() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	mediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(s.sourceMetadata["content-type"], ";", 2)[0]))
	return mediaType == "audio/mpeg" || mediaType == "audio/mp3"
}

// MP3MatchesConfiguredOutput inspects the first complete MP3 frame without
// consuming source bytes. ready is false until enough source data has arrived
// to make a routing decision.
func (s *IcecastSourceServer) MP3MatchesConfiguredOutput() (matches, ready bool) {
	s.mu.RLock()
	buffer := s.audioBuffer
	s.mu.RUnlock()
	if buffer == nil {
		return false, false
	}
	probe := buffer.ProbeBytes()
	frame := mp3lib.NextFrame(bytes.NewReader(probe))
	if frame == nil {
		return false, len(probe) >= 64*1024
	}
	channels := 2
	if frame.ChannelMode == mp3lib.Mono {
		channels = 1
	}
	return frame.BitRate == configuredBitrateBps() && frame.SamplingRate == configuredSampleRate() && channels == pcmChannels, true
}

// BufferSize returns the current number of bytes in the audio buffer
func (s *IcecastSourceServer) BufferSize() int {
	s.mu.RLock()
	buffer := s.audioBuffer
	s.mu.RUnlock()
	if buffer == nil {
		return 0
	}
	return buffer.Size()
}

// GetStats returns current statistics
func (s *IcecastSourceServer) GetStats() (bytesReceived, bytesSent int64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.bytesReceived, s.bytesSent
}
