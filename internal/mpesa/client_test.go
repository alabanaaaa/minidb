package mpesa

import (
	"encoding/json"
	"testing"
)

func TestNormalizePhone(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"0712345678", "254712345678"},
		{"712345678", "254712345678"},
		{"+254712345678", "254712345678"},
		{"254712345678", "254712345678"},
		{"0712 345 678", "254712345678"},
		{"0712-345-678", "254712345678"},
		{" 0712345678 ", "254712345678"},
	}

	for _, tc := range tests {
		result := normalizePhone(tc.input)
		if result != tc.expected {
			t.Errorf("normalizePhone(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

func TestParseCallbackSuccess(t *testing.T) {
	raw := []byte(`{
		"Body": {
			"stkCallback": {
				"MerchantRequestID": "12345",
				"CheckoutRequestID": "ws_CO_12345",
				"ResultCode": 0,
				"ResultDesc": "The service request is processed successfully.",
				"CallbackMetadata": {
					"Item": [
						{"Name": "Amount", "Value": 100},
						{"Name": "MpesaReceiptNumber", "Value": "QKL123456"},
						{"Name": "PhoneNumber", "Value": 254712345678},
						{"Name": "TransactionId", "Value": "ABC123"},
						{"Name": "TransactionDate", "Value": 20260331120000}
					]
				}
			}
		}
	}`)

	result, err := ParseCallback(raw)
	if err != nil {
		t.Fatalf("ParseCallback failed: %v", err)
	}

	if !result.Success {
		t.Fatal("expected success")
	}
	if result.MpesaReceipt != "QKL123456" {
		t.Errorf("expected QKL123456, got %s", result.MpesaReceipt)
	}
	if result.Amount != 100 {
		t.Errorf("expected 100, got %f", result.Amount)
	}
	if result.PhoneNumber != "254712345678" {
		t.Errorf("expected 254712345678, got %s", result.PhoneNumber)
	}
	if result.TransactionID != "ABC123" {
		t.Errorf("expected ABC123, got %s", result.TransactionID)
	}
}

func TestParseCallbackFailure(t *testing.T) {
	raw := []byte(`{
		"Body": {
			"stkCallback": {
				"MerchantRequestID": "12345",
				"CheckoutRequestID": "ws_CO_12345",
				"ResultCode": 1,
				"ResultDesc": "Cancelled by user"
			}
		}
	}`)

	result, err := ParseCallback(raw)
	if err != nil {
		t.Fatalf("ParseCallback failed: %v", err)
	}

	if result.Success {
		t.Fatal("expected failure")
	}
	if result.ResultCode != 1 {
		t.Errorf("expected result code 1, got %d", result.ResultCode)
	}
	if result.ResultDesc != "Cancelled by user" {
		t.Errorf("expected 'Cancelled by user', got %s", result.ResultDesc)
	}
}

func TestParseCallbackInvalidJSON(t *testing.T) {
	_, err := ParseCallback([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestGenerateAccountRef(t *testing.T) {
	ref1 := GenerateAccountRef()
	ref2 := GenerateAccountRef()

	if len(ref1) == 0 {
		t.Fatal("account ref is empty")
	}
	if ref1 == ref2 {
		t.Fatal("account refs should be unique")
	}
	if ref1[:2] != "MD" {
		t.Errorf("expected prefix MD, got %s", ref1[:2])
	}
}

func TestConfigBaseURL(t *testing.T) {
	sandbox := Config{Environment: "sandbox"}
	if sandbox.BaseURL() != "https://sandbox.safaricom.co.ke" {
		t.Errorf("wrong sandbox URL: %s", sandbox.BaseURL())
	}

	prod := Config{Environment: "production"}
	if prod.BaseURL() != "https://api.safaricom.co.ke" {
		t.Errorf("wrong production URL: %s", prod.BaseURL())
	}
}

func TestSTKPushPayload(t *testing.T) {
	cfg := Config{
		ShortCode:   "174379",
		Passkey:     "bfb279f9aa9bdbcf158e97dd71a467cd2e0c893059b10f78e6b72ada1ed2c919",
		CallbackURL: "https://example.com/callback",
	}

	req := STKPushRequest{
		PhoneNumber: "0712345678",
		Amount:      100,
		AccountRef:  "TEST001",
		Description: "Test payment",
	}

	_ = cfg
	_ = req

	payload := map[string]interface{}{
		"BusinessShortCode": cfg.ShortCode,
		"TransactionType":   "CustomerPayBillOnline",
		"Amount":            req.Amount,
		"PartyA":            normalizePhone(req.PhoneNumber),
		"PartyB":            cfg.ShortCode,
		"PhoneNumber":       normalizePhone(req.PhoneNumber),
		"CallBackURL":       cfg.CallbackURL,
		"AccountReference":  req.AccountRef,
		"TransactionDesc":   req.Description,
	}

	data, _ := json.Marshal(payload)

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	if parsed["PartyA"] != "254712345678" {
		t.Errorf("expected normalized phone, got %v", parsed["PartyA"])
	}
	if parsed["Amount"] != float64(100) {
		t.Errorf("expected amount 100, got %v", parsed["Amount"])
	}
}
