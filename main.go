package main

import (
	"gostream/middlewares"
	"gostream/modules"
	"gostream/routes"
	"fmt"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// icecastNormalizerFeeder reads chunks from Icecast source, normalizes them, and feeds to MusicReader
// It automatically manages mode switching based on whether an Icecast source is connected
func icecastNormalizerFeeder() {
	modules.Logger.Info("Icecast normalizer feeder started")
	var isIcecastProcessing bool
	var processorWaitCh chan struct{}

	for {
		// Check if there's an active Icecast source connection
		hasSource := modules.IcecastSource.HasActiveSource()

		// Transition: Source connected, start Icecast mode
		if hasSource && !isIcecastProcessing {
			modules.Logger.Info("Icecast source connected - switching to Icecast mode")
			
			// Close current file and advance to next song before switching modes
			// This ensures: 1) current file is released (no lock for cleanup), 2) next song is ready when Icecast disconnects
			modules.MusicReader.SkipToNext()
			modules.Logger.Info("Advancing to next song in preparation for Icecast mode")
			
			modules.MusicReader.EnableIcecastMode()
			
			// Create a channel to signal when processor is done
			processorWaitCh = make(chan struct{})
			go func() {
				modules.MusicReader.ProcessIcecastStream()
				close(processorWaitCh)
			}()
			
			isIcecastProcessing = true
			time.Sleep(200 * time.Millisecond) // Give processor time to start
			continue
		}

		// Transition: Source disconnected, revert to file mode
		if !hasSource && isIcecastProcessing {
			modules.Logger.Info("Icecast source disconnected - reverting to file mode")
			modules.MusicReader.DisableIcecastMode()
			
			// Wait for processor to exit (with timeout)
			select {
			case <-processorWaitCh:
				modules.Logger.Info("Icecast processor exited cleanly")
			case <-time.After(2 * time.Second):
				modules.Logger.Info("Icecast processor did not exit within 2s, continuing")
			}
			
			isIcecastProcessing = false
			time.Sleep(200 * time.Millisecond) // Give StartLoop time to resume file feeding
			continue
		}

		// No source and not processing - check periodically
		if !hasSource && !isIcecastProcessing {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		// Source is active and we're processing - get next chunk
		chunk, ok := modules.IcecastSource.GetAudioChunk()
		if !ok {
			time.Sleep(10 * time.Millisecond)
			continue
		}

		// For live streaming, pass chunks through directly (no re-encoding)
		// Icecast already provides MP3 data from Mixxx
		// Skip FFmpeg normalization to avoid latency/jerkiness
		
		// Feed to MusicReader buffer system
		err := modules.MusicReader.FeedIcecastChunk(chunk)
		if err != nil {
			modules.Logger.Debug("Failed to feed chunk: " + err.Error())
		}
	}
}

func main() {

	modules.InitReader()
	
	// Initialize Icecast source server on port 8001
	modules.InitIcecastServer("8001")
	go func() {
		err := modules.IcecastSource.Start()
		if err != nil {
			modules.Logger.Error(fmt.Sprintf("Icecast server failed: %v", err))
		}
	}()

	// Start Icecast normalizer feeder (runs continuously, checks mode)
	go icecastNormalizerFeeder()

	e := echo.New()

	e.HideBanner = true
	e.HTTPErrorHandler = middlewares.CustomHTTPErrorHandler
	e.Use(middlewares.LoggerIn)
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept},
	}))

	routes.InitRoutes(e)

	err := e.Start(fmt.Sprintf("%s:%d", modules.Config.Host, modules.Config.Port))
	if err != nil {
		modules.Logger.Error(err)
	}
}

