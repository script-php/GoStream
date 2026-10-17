package main

import (
	"errors"
	"fmt"
	"gostream/middlewares"
	"gostream/modules"
	"gostream/routes"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func main() {
	// Check if we're registering as a systemd service
	if modules.RegisterService {
		registerSystemdService(modules.RegisterConfigPath)
		return
	}

	if modules.Config.IcecastSourcePort > 0 {
		modules.InitIcecastServer(fmt.Sprintf("%d", modules.Config.IcecastSourcePort))
		go func() {
			err := modules.IcecastSource.Start()
			if err != nil {
				modules.Logger.Error(fmt.Sprintf("Icecast server failed: %v", err))
			}
		}()

	} else {
		modules.Logger.Info("Icecast source input disabled")
	}

	if err := modules.InitReader(); err != nil {
		modules.Logger.Error(fmt.Sprintf("Failed to start stream engine: %v", err))
		return
	}

	e := echo.New()

	e.HideBanner = true
	e.HTTPErrorHandler = middlewares.CustomHTTPErrorHandler
	e.Use(middlewares.LoggerIn)
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept},
	}))

	routes.InitRoutes(e)

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- e.Start(fmt.Sprintf("%s:%d", modules.Config.Host, modules.Config.Port))
	}()

	select {
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			modules.Logger.Error(err)
		}
	case err := <-modules.StreamEngine.Failure():
		modules.Logger.Error(fmt.Sprintf("Fatal stream engine failure: %v; shutting down for supervisor restart", err))
		_ = e.Close()
	}
}
