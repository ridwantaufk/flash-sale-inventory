package server

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ridwantaufk/flash-sale-inventory/internal/config"
	"github.com/ridwantaufk/flash-sale-inventory/internal/inventory"
	"github.com/ridwantaufk/flash-sale-inventory/internal/transport/httpx"
)

type DependencyCheck func(context.Context) error

func NewEngine(cfg config.Config, h *inventory.Handler, ready *httpx.Readiness, live DependencyCheck) *gin.Engine {
	gin.SetMode(cfg.GinMode)

	engine := gin.New()
	engine.Use(gin.Recovery(), httpx.RequestID())

	engine.GET("/healthz", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := live(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	engine.GET("/readyz", ready.Handler())

	v1 := engine.Group("/api/v1/inventory")
	v1.POST("/reserve", h.Reserve)
	v1.POST("/confirm", h.Confirm)
	v1.GET("/stock", h.Stock)

	return engine
}

func NewHTTPServer(cfg config.Config, engine *gin.Engine) *http.Server {
	return &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           engine,
		ReadHeaderTimeout: 5 * time.Second,
	}
}

func Pinger(db *sql.DB) DependencyCheck {
	return func(ctx context.Context) error { return db.PingContext(ctx) }
}
