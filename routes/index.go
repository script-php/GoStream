package routes

import (
	"net/http"
	"gostream/middlewares"

	"github.com/labstack/echo/v4"
)

func InitRoutes(e *echo.Echo) {
	// Public endpoints - no auth required
	e.GET("/", GetFMStream)
	e.GET("/stream.mp3", GetFMStream)
	e.GET("/info", GetServerInfo)
	e.GET("/stats", GetStats)
	e.GET("/status", GetStreamStatus)
	e.GET("/next", GetNextSong)
	e.GET("/songs", GetSongsList)
	e.GET("/metrics", GetMetrics)
	e.GET("/mode", GetStreamMode)
	
	// Protected endpoints - require authentication
	e.GET("/skip", SkipSong, middlewares.BasicAuth)
	e.POST("/next/set", SetNextSong, middlewares.BasicAuth)
	
	// Playlist endpoints - protected
	e.POST("/playlist/add", AddToPlaylist, middlewares.BasicAuth)
	e.DELETE("/playlist/remove", RemoveFromPlaylist, middlewares.BasicAuth)
	e.GET("/playlist", GetPlaylist, middlewares.BasicAuth)
	e.DELETE("/playlist", ClearPlaylist, middlewares.BasicAuth)
	e.POST("/playlist/reorder", ReorderPlaylist, middlewares.BasicAuth)
	
	// Icecast mode endpoints - protected (for manual override only - automatic switching is enabled)
	e.POST("/icecast/enable", EnableIcecastMode, middlewares.BasicAuth)
	e.POST("/icecast/disable", DisableIcecastMode, middlewares.BasicAuth)
	
	e.GET("/favicon.ico", func(c echo.Context) error {
        return c.NoContent(http.StatusNoContent)
    })
}

