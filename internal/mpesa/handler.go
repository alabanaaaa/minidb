package mpesa

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"mini-database/engine"
)

type Handler struct {
	client *Client
	engine *engine.Engine
}

func NewHandler(cfg Config, eng *engine.Engine) *Handler {
	return &Handler{
		client: NewClient(cfg),
		engine: eng,
	}
}

type PaymentRequest struct {
	PhoneNumber string `json:"phone_number"`
	Amount      int64  `json:"amount"`
	ProductID   string `json:"product_id"`
	Quantity    int64  `json:"quantity"`
	WorkerID    string `json:"worker_id"`
}

type PaymentResponse struct {
	Success    bool   `json:"success"`
	CheckoutID string `json:"checkout_id,omitempty"`
	Message    string `json:"message"`
	ReceiptID  string `json:"receipt_id,omitempty"`
}

func (h *Handler) InitiatePayment(w http.ResponseWriter, r *http.Request) {
	var req PaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if req.PhoneNumber == "" || req.Amount <= 0 {
		http.Error(w, "invalid phone or amount", http.StatusBadRequest)
		return
	}

	accountRef := GenerateAccountRef()

	resp, err := h.client.STKPush(STKPushRequest{
		PhoneNumber: req.PhoneNumber,
		Amount:      req.Amount,
		AccountRef:  accountRef,
		Description: fmt.Sprintf("POS Purchase - %s x%d", req.ProductID, req.Quantity),
	})

	if err != nil {
		slog.Error("STK push failed", "error", err)
		w.WriteHeader(http.StatusPaymentRequired)
		json.NewEncoder(w).Encode(PaymentResponse{
			Success: false,
			Message: "Payment initiation failed. Please try again.",
		})
		return
	}

	if resp.ResponseCode != "0" {
		slog.Error("STK push returned error", "code", resp.ResponseCode, "desc", resp.ResponseDesc)
		w.WriteHeader(http.StatusPaymentRequired)
		json.NewEncoder(w).Encode(PaymentResponse{
			Success: false,
			Message: resp.ResponseDesc,
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(PaymentResponse{
		Success:    true,
		CheckoutID: resp.CheckoutRequestID,
		Message:    "Payment initiated. Check your phone for STK push.",
	})
}

func (h *Handler) HandleCallback(w http.ResponseWriter, r *http.Request) {
	var callback struct {
		Body struct {
			STKCallback struct {
				ResultCode       int    `json:"ResultCode"`
				ResultDesc       string `json:"ResultDesc"`
				CallbackMetadata struct {
					Item []struct {
						Name  string      `json:"Name"`
						Value interface{} `json:"Value"`
					} `json:"Item"`
				} `json:"CallbackMetadata"`
			} `json:"stkCallback"`
		} `json:"Body"`
	}

	if err := json.NewDecoder(r.Body).Decode(&callback); err != nil {
		slog.Error("parse callback failed", "error", err)
		http.Error(w, "invalid callback", http.StatusBadRequest)
		return
	}

	result := &CallbackResult{
		Success:    callback.Body.STKCallback.ResultCode == 0,
		ResultCode: callback.Body.STKCallback.ResultCode,
		ResultDesc: callback.Body.STKCallback.ResultDesc,
	}

	if !result.Success {
		slog.Info("payment cancelled or failed", "code", result.ResultCode, "desc", result.ResultDesc)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "failed"})
		return
	}

	for _, item := range callback.Body.STKCallback.CallbackMetadata.Item {
		switch item.Name {
		case "MpesaReceiptNumber":
			if v, ok := item.Value.(string); ok {
				result.MpesaReceipt = v
			}
		case "PhoneNumber":
			if v, ok := item.Value.(float64); ok {
				result.PhoneNumber = fmt.Sprintf("%.0f", v)
			}
		case "Amount":
			if v, ok := item.Value.(float64); ok {
				result.Amount = v
			}
		case "TransactionId":
			if v, ok := item.Value.(string); ok {
				result.TransactionID = v
			}
		}
	}

	slog.Info("payment successful",
		"receipt", result.MpesaReceipt,
		"amount", result.Amount,
		"phone", result.PhoneNumber,
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":         "success",
		"receipt":        result.MpesaReceipt,
		"transaction_id": result.TransactionID,
	})
}

type MpesaConfig struct {
	ConsumerKey    string
	ConsumerSecret string
	ShortCode      string
	Passkey        string
	Environment    string
	CallbackURL    string
}

func LoadMpesaConfig() MpesaConfig {
	return MpesaConfig{
		ConsumerKey:    getEnvOrDefault("MPESA_CONSUMER_KEY", ""),
		ConsumerSecret: getEnvOrDefault("MPESA_CONSUMER_SECRET", ""),
		ShortCode:      getEnvOrDefault("MPESA_SHORT_CODE", ""),
		Passkey:        getEnvOrDefault("MPESA_PASSKEY", ""),
		Environment:    getEnvOrDefault("MPESA_ENVIRONMENT", "sandbox"),
		CallbackURL:    getEnvOrDefault("MPESA_CALLBACK_URL", ""),
	}
}

func getEnvOrDefault(key, defaultValue string) string {
	val := os.Getenv(key)
	if val == "" {
		return defaultValue
	}
	return val
}

func (c *MpesaConfig) IsConfigured() bool {
	return c.ConsumerKey != "" && c.ConsumerSecret != "" && c.ShortCode != ""
}

func (h *Handler) CheckPaymentStatus(checkoutID string) (*CallbackResult, error) {
	time.Sleep(2 * time.Second)

	return &CallbackResult{
		Success:    true,
		ResultCode: 0,
		ResultDesc: "Success",
	}, nil
}
