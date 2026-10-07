package httpx

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

const requestIDKey = "request_id"

func newRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "req_" + strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return "req_" + hex.EncodeToString(b[:])
}

func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if id == "" {
			id = newRequestID()
		}
		c.Set(requestIDKey, id)
		c.Header("X-Request-ID", id)
		c.Next()
	}
}

func ID(c *gin.Context) string {
	if v, ok := c.Get(requestIDKey); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func Error(c *gin.Context, status int, code, message string, details gin.H) {
	resp := gin.H{
		"status":     "error",
		"code":       code,
		"message":    message,
		"request_id": ID(c),
	}
	if len(details) > 0 {
		resp["details"] = details
	}
	c.AbortWithStatusJSON(status, resp)
}

func Internal(c *gin.Context, err error) {
	_ = c.Error(err)
	Error(c, http.StatusInternalServerError, "INTERNAL", "internal server error", nil)
}

type Readiness struct{ ok atomic.Bool }

func NewReadiness() *Readiness {
	r := &Readiness{}
	r.ok.Store(true)
	return r
}

func (r *Readiness) Set(v bool) { r.ok.Store(v) }

func (r *Readiness) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !r.ok.Load() {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "shutting_down"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	}
}

func ErrorDetails(field, reason string) gin.H {
	return gin.H{"field": field, "reason": reason}
}
