package routes

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

func InitRoutes(e *echo.Echo) {
	e.GET("/info", GetServerInfo)
	e.GET("/stats", GetStats)
	e.GET("/skip", SkipSong)
	e.GET("/status", GetStreamStatus)
	e.GET("/next", GetNextSong)
	e.POST("/next/set", SetNextSong)
	e.GET("/songs", GetSongsList)
	e.GET("/metrics", GetMetrics)
	e.GET("/", GetFMStream)
	e.GET("/stream.mp3", GetFMStream)
	e.GET("/favicon.ico", func(c echo.Context) error {
        return c.NoContent(http.StatusNoContent)
    })
}
