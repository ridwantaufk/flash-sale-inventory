package inventory

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ridwantaufk/flash-sale-inventory/internal/transport/httpx"
)

type Handler struct {
	svc *Service
	log *slog.Logger
}

func NewHandler(svc *Service, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

type reserveRequest struct {
	UserID   string `json:"user_id"`
	ItemID   string `json:"item_id"`
	Quantity int    `json:"quantity"`
}

type confirmRequest struct {
	ReservationID string `json:"reservation_id"`
}

func (h *Handler) Reserve(c *gin.Context) {
	var in reserveRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		httpx.Error(c, http.StatusBadRequest, "INVALID_JSON", "request body must be a JSON object", nil)
		return
	}

	res, err := h.svc.Reserve(c.Request.Context(), in.ItemID, in.UserID, in.Quantity)
	if err != nil {
		h.fail(c, err)
		return
	}

	h.log.Info("stock reserved", "reservation_id", res.ID, "item_id", res.ItemID, "quantity", res.Quantity)
	c.JSON(http.StatusCreated, gin.H{
		"status":         "success",
		"reservation_id": res.ID,
		"item_id":        res.ItemID,
		"quantity":       res.Quantity,
		"expires_at":     res.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

func (h *Handler) Confirm(c *gin.Context) {
	var in confirmRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		httpx.Error(c, http.StatusBadRequest, "INVALID_JSON", "request body must be a JSON object", nil)
		return
	}

	res, err := h.svc.Confirm(c.Request.Context(), in.ReservationID)
	if err != nil {
		h.fail(c, err)
		return
	}

	h.log.Info("reservation confirmed", "reservation_id", res.ID, "item_id", res.ItemID)
	c.JSON(http.StatusOK, gin.H{
		"status":         "success",
		"reservation_id": res.ID,
		"confirmed_at":   res.ConfirmedAt.UTC().Format(time.RFC3339),
	})
}

func (h *Handler) Stock(c *gin.Context) {
	itemID := c.Query("item_id")
	stock, err := h.svc.Stock(c.Request.Context(), itemID)
	if err != nil {
		h.fail(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"item_id":         stock.ItemID,
		"total_stock":     stock.Total,
		"reserved_stock":  stock.Reserved,
		"available_stock": stock.Available(),
	})
}

func (h *Handler) fail(c *gin.Context, err error) {
	var invalid *InvalidField
	if errors.As(err, &invalid) {
		httpx.Error(c, http.StatusUnprocessableEntity, "INVALID_INPUT", invalid.Reason,
			httpx.ErrorDetails(invalid.Field, invalid.Reason))
		return
	}

	switch {
	case errors.Is(err, ErrItemNotFound):
		httpx.Error(c, http.StatusNotFound, "ITEM_NOT_FOUND", "no inventory item matches that id", nil)
	case errors.Is(err, ErrInsufficientStock):
		httpx.Error(c, http.StatusConflict, "INSUFFICIENT_STOCK", "requested quantity exceeds available stock", nil)
	case errors.Is(err, ErrReservationNotFound):
		httpx.Error(c, http.StatusNotFound, "RESERVATION_NOT_FOUND", "no reservation matches that id", nil)
	case errors.Is(err, ErrAlreadyConfirmed):
		httpx.Error(c, http.StatusConflict, "ALREADY_CONFIRMED", "reservation is already confirmed", nil)
	case errors.Is(err, ErrReservationExpired):
		httpx.Error(c, http.StatusConflict, "RESERVATION_EXPIRED", "reservation is no longer active", nil)
	case errors.Is(err, context.DeadlineExceeded):
		httpx.Error(c, http.StatusGatewayTimeout, "TIMEOUT", "the database did not answer in time", nil)
	case errors.Is(err, ErrStockInvariant):
		_ = c.Error(err)
		httpx.Error(c, http.StatusInternalServerError, "STOCK_INVARIANT",
			"inventory counters disagree with the reservations on file", nil)
	default:
		httpx.Internal(c, err)
	}
}
