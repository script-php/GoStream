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
