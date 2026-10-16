package main

import (
	"fmt"
	"gostream/middlewares"
	"gostream/modules"
	"gostream/routes"

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

	err := e.Start(fmt.Sprintf("%s:%d", modules.Config.Host, modules.Config.Port))
	if err != nil {
		modules.Logger.Error(err)
	}
}
