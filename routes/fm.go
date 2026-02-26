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

	init := false
	order := 0

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

		n, err := res.Write(targetBuffer)
		if err != nil {
			modules.Logger.Info(fmt.Sprintf("[%s] Client %s disconnected", requestID, ip))
			return nil
		}
		
		// Track bandwidth
		modules.AddBytesStreamed(int64(n))

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
