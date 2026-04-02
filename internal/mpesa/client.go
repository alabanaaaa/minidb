package mpesa

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Config struct {
	ConsumerKey    string
	ConsumerSecret string
	ShortCode      string
	Passkey        string
	Environment    string // "sandbox" or "production"
	CallbackURL    string
	APIVersion     string // "v2" or "v3" (default: v2)
}

func (c *Config) BaseURL() string {
	if c.Environment == "production" {
		return "https://api.safaricom.co.ke"
	}
	return "https://sandbox.safaricom.co.ke"
}

func (c *Config) TokenURL() string {
	if c.APIVersion == "v3" {
		return c.BaseURL() + "/oauth/v3/token"
	}
	return c.BaseURL() + "/oauth/v1/generate?grant_type=client_credentials"
}

func (c *Config) STKPushURL() string {
	if c.APIVersion == "v3" {
		return c.BaseURL() + "/mpesa/stkpush/v2/processrequest"
	}
	return c.BaseURL() + "/mpesa/stkpush/v1/processrequest"
}

type Client struct {
	cfg         Config
	httpClient  *http.Client
	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time
}

func NewClient(cfg Config) *Client {
	if cfg.APIVersion == "" {
		cfg.APIVersion = "v2"
	}
	return &Client{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

type STKPushRequest struct {
	PhoneNumber string
	Amount      int64
	AccountRef  string
	Description string
}

type STKPushResponse struct {
	CheckoutRequestID string `json:"CheckoutRequestID"`
	ResponseCode      string `json:"ResponseCode"`
	ResponseDesc      string `json:"ResponseDescription"`
	MerchantRequestID string `json:"MerchantRequestID"`
}

func (c *Client) STKPush(req STKPushRequest) (*STKPushResponse, error) {
	token, err := c.getAccessToken()
	if err != nil {
		return nil, fmt.Errorf("get access token: %w", err)
	}

	timestamp := time.Now().Format("20060102150405")
	password := base64.StdEncoding.EncodeToString([]byte(c.cfg.ShortCode + c.cfg.Passkey + timestamp))

	phone := normalizePhone(req.PhoneNumber)

	payload := map[string]interface{}{
		"BusinessShortCode": c.cfg.ShortCode,
		"Password":          password,
		"Timestamp":         timestamp,
		"TransactionType":   "CustomerPayBillOnline",
		"Amount":            req.Amount,
		"PartyA":            phone,
		"PartyB":            c.cfg.ShortCode,
		"PhoneNumber":       phone,
		"CallBackURL":       c.cfg.CallbackURL,
		"AccountReference":  req.AccountRef,
		"TransactionDesc":   req.Description,
	}

	body, _ := json.Marshal(payload)
	httpReq, _ := http.NewRequest("POST", c.cfg.STKPushURL(), bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("stk push request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		slog.Error("stk push failed", "status", resp.StatusCode, "body", string(respBody))
		return nil, fmt.Errorf("stk push failed: %s", string(respBody))
	}

	var stkResp STKPushResponse
	if err := json.Unmarshal(respBody, &stkResp); err != nil {
		return nil, fmt.Errorf("parse stk push response: %w", err)
	}

	slog.Info("stk push initiated",
		"checkout_request_id", stkResp.CheckoutRequestID,
		"phone", phone,
		"amount", req.Amount,
	)

	return &stkResp, nil
}

type CallbackRequest struct {
	Body struct {
		STKCallback struct {
			MerchantRequestID string `json:"MerchantRequestID"`
			CheckoutRequestID string `json:"CheckoutRequestID"`
			ResultCode        int    `json:"ResultCode"`
			ResultDesc        string `json:"ResultDesc"`
			CallbackMetadata  struct {
				Item []struct {
					Name  string      `json:"Name"`
					Value interface{} `json:"Value"`
				} `json:"Item"`
			} `json:"CallbackMetadata"`
		} `json:"stkCallback"`
	} `json:"Body"`
}

type CallbackResult struct {
	MpesaReceipt  string
	PhoneNumber   string
	Amount        float64
	TransactionID string
	Success       bool
	ResultCode    int
	ResultDesc    string
}

func ParseCallback(raw []byte) (*CallbackResult, error) {
	var cb CallbackRequest
	if err := json.Unmarshal(raw, &cb); err != nil {
		return nil, fmt.Errorf("parse callback: %w", err)
	}

	result := &CallbackResult{
		Success:    cb.Body.STKCallback.ResultCode == 0,
		ResultCode: cb.Body.STKCallback.ResultCode,
		ResultDesc: cb.Body.STKCallback.ResultDesc,
	}

	if !result.Success {
		return result, nil
	}

	for _, item := range cb.Body.STKCallback.CallbackMetadata.Item {
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

	return result, nil
}

func (c *Client) getAccessToken() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.accessToken != "" && time.Now().Before(c.tokenExpiry) {
		return c.accessToken, nil
	}

	// Try v3 first, fall back to v2
	token, err := c.fetchToken(c.cfg.TokenURL())
	if err != nil && c.cfg.APIVersion != "v3" {
		// Fall back to v2
		token, err = c.fetchToken(c.cfg.BaseURL() + "/oauth/v1/generate?grant_type=client_credentials")
	}
	if err != nil {
		return "", err
	}

	c.accessToken = token
	c.tokenExpiry = time.Now().Add(55 * time.Minute)

	return c.accessToken, nil
}

func (c *Client) fetchToken(url string) (string, error) {
	req, _ := http.NewRequest("GET", url, nil)
	req.SetBasicAuth(c.cfg.ConsumerKey, c.cfg.ConsumerSecret)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token request failed: %d %s", resp.StatusCode, string(respBody))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   string `json:"expires_in"`
	}
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return "", fmt.Errorf("parse token response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("empty access token from %s", url)
	}

	return tokenResp.AccessToken, nil
}

func normalizePhone(phone string) string {
	phone = strings.TrimSpace(phone)
	phone = strings.ReplaceAll(phone, " ", "")
	phone = strings.ReplaceAll(phone, "-", "")

	if strings.HasPrefix(phone, "+") {
		phone = phone[1:]
	}
	if strings.HasPrefix(phone, "0") {
		phone = "254" + phone[1:]
	}
	if strings.HasPrefix(phone, "7") {
		phone = "254" + phone
	}

	return phone
}

func GenerateAccountRef() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "MD" + strings.ToUpper(base64.RawURLEncoding.EncodeToString(b))[:10]
}
