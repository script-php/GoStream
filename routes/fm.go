package routes

import (
	"gostream/modules"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// BuildIcecastMetadata creates an ICY metadata block according to Shoutcast protocol
// Metadata format: [1-byte-length][metadata-string][padding]
// Length is in 16-byte chunks, not bytes
func BuildIcecastMetadata(filename, url string) []byte {
	// Build metadata string: StreamTitle='Filename';StreamUrl='url';
	metadataStr := fmt.Sprintf("StreamTitle='%s';StreamUrl='%s';", filename, url)
	
	// Calculate length in 16-byte blocks (rounded up)
	metadataLen := len(metadataStr)
	blockCount := (metadataLen + 15) / 16 // Round up to nearest 16-byte block
	
	// Create full metadata block with 1-byte length header + metadata + padding
	totalLen := 1 + (blockCount * 16)
	metadataBlock := make([]byte, totalLen)
	
	// First byte is the length in 16-byte blocks
	metadataBlock[0] = byte(blockCount)
	
	// Copy metadata string after the length byte
	copy(metadataBlock[1:], metadataStr)
	
	// Rest is null-padded (already zero-initialized)
	return metadataBlock
}

func GetFMStream(ctx echo.Context) error {

	ip := GetRealIP(ctx.Request())
	requestID := fmt.Sprintf("%d", time.Now().UnixNano())

	// Increment active listener count
	modules.IncrementListener()
	defer modules.DecrementListener()

	modules.Logger.Info(fmt.Sprintf("[%s] Client %s connected", requestID, ip))

	res := ctx.Response()

	store := modules.MusicReader.GetBufferStoreData()
	if store == nil {
		err := errors.New("oops, it seems like the FM hasn't started up")
		modules.Logger.Error(fmt.Sprintf("[%s] %v", requestID, err))
		return err
	}

	res.Header().Set("Connection", "Keep-Alive")
	res.Header().Set("Access-Control-Allow-Origin", "*")
	res.Header().Set("X-Content-Type-Options", "nosniff")
	res.Header().Set("Transfer-Encoding", "chunked")
	res.Header().Set("Content-Type", "audio/mpeg")
	
	// Set Shoutcast metadata headers
	// icy-name from station name
	if modules.Config.Name != "" {
		res.Header().Set("icy-name", modules.Config.Name)
	}
	// icy-genre from config
	if modules.Config.Genre != "" {
		res.Header().Set("icy-genre", modules.Config.Genre)
	}
	// icy-url from config
	if modules.Config.URL != "" {
		res.Header().Set("icy-url", modules.Config.URL)
	}
	// icy-br: extract bitrate number without 'k' suffix (e.g., "128k" -> "128")
	if modules.Config.StandardBitrate != "" {
		br := strings.TrimSuffix(modules.Config.StandardBitrate, "k")
		res.Header().Set("icy-br", br)
	}
	// icy-sr from standard sample rate
	if modules.Config.StandardSampleRate != "" {
		res.Header().Set("icy-sr", modules.Config.StandardSampleRate)
	}
	// icy-pub: always 1 (stream is public)
	res.Header().Set("icy-pub", "1")
	// icy-notice1 and icy-notice2 from config
	if modules.Config.Notice1 != "" {
		res.Header().Set("icy-notice1", modules.Config.Notice1)
	}
	if modules.Config.Notice2 != "" {
		res.Header().Set("icy-notice2", modules.Config.Notice2)
	}

	// Check if client wants metadata
	wantMetadata := strings.EqualFold(ctx.Request().Header.Get("Icy-MetaData"), "1")
	metaintInterval := modules.Config.MetaInterval
	if metaintInterval <= 0 {
		metaintInterval = 8192 // Default if not configured
	}
	
	if wantMetadata {
		// Set icy-metaint header to tell client where metadata blocks are
		res.Header().Set("icy-metaint", fmt.Sprintf("%d", metaintInterval))
	}

	init := false
	order := 0
	sinceMetaBlock := 0 // Track bytes sent since last metadata (Icecast style)

	for {
		var targetBuffer []byte
		var currentOrder int

		// Lock while reading store to prevent race condition
		modules.MusicReader.Lock.RLock()
		store := modules.MusicReader.GetBufferStoreData()
		
		if store.Order == order {
			modules.MusicReader.Lock.RUnlock()
			time.Sleep(time.Millisecond * 100)
			continue
		}

		// Capture order and buffer atomically
		currentOrder = store.Order
		if !init {
			init = true
			targetBuffer = store.InitialBuffer[:]
		} else {
			targetBuffer = store.UnitBuffer[:]
		}
		timeout := store.Timeout
		modules.MusicReader.Lock.RUnlock()
		
		order = currentOrder

		// Process buffer with Icecast-style metadata injection
		if wantMetadata {
			bufLen := len(targetBuffer)
			offset := 0

			for offset < bufLen {
				// Calculate how many bytes until metadata boundary
				remaining := metaintInterval - sinceMetaBlock
				
				// How much of this buffer to send
				toSend := remaining
				if offset+toSend > bufLen {
					toSend = bufLen - offset
				}

				// Send audio chunk (up to metadata boundary)
				if toSend > 0 {
					chunk := targetBuffer[offset : offset+toSend]
					n, err := res.Write(chunk)
					if err != nil {
						modules.Logger.Info(fmt.Sprintf("[%s] Client %s disconnected", requestID, ip))
						return nil
					}
					modules.AddBytesStreamed(int64(n))
					sinceMetaBlock += n
					offset += n
				}

				// If we hit metadata boundary, inject metadata
				if sinceMetaBlock >= metaintInterval && offset < bufLen {
					musicInfo := modules.MusicReader.GetMusicInfo()
					if musicInfo != nil && musicInfo.Filename != "" {
						metadata := BuildIcecastMetadata(musicInfo.Filename, musicInfo.Url)
						_, err := res.Write(metadata)
						if err != nil {
							modules.Logger.Info(fmt.Sprintf("[%s] Client %s disconnected", requestID, ip))
							return nil
						}
						modules.AddBytesStreamed(int64(len(metadata)))
					}
					sinceMetaBlock = 0 // Reset for next interval
				}
			}
		} else {
			// No metadata requested, stream normally
			n, err := res.Write(targetBuffer)
			if err != nil {
				modules.Logger.Info(fmt.Sprintf("[%s] Client %s disconnected", requestID, ip))
				return nil
			}
			modules.AddBytesStreamed(int64(n))
		}

		time.Sleep(time.Millisecond * time.Duration(timeout))
	}
}

func GetRealIP(r *http.Request) string {
	forwardedFor := r.Header.Get("X-Forwarded-For")

	if forwardedFor == "" {
		return strings.Split(r.RemoteAddr, ":")[0]
	}

	ips := strings.Split(forwardedFor, ", ")
	return ips[len(ips)-1]
}
