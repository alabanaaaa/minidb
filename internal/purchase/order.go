package purchase

import (
	"errors"
	"fmt"
	"math/rand"
	"time"
)

var (
	ErrInvalidPOStatus  = errors.New("invalid purchase order status")
	ErrItemNotFound     = errors.New("item not found in order")
	ErrSupplierNotFound = errors.New("supplier not found")
)

type Status string

const (
	StatusDraft     Status = "draft"
	StatusPending   Status = "pending"
	StatusApproved  Status = "approved"
	StatusOrdered   Status = "ordered"
	StatusPartial   Status = "partial"
	StatusReceived  Status = "received"
	StatusCancelled Status = "cancelled"
)

type PurchaseOrder struct {
	ID           string     `json:"id"`
	SupplierID   string     `json:"supplier_id"`
	SupplierName string     `json:"supplier_name"`
	Status       Status     `json:"status"`
	Items        []POItem   `json:"items"`
	TotalCost    int64      `json:"total_cost"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	ExpectedDate *time.Time `json:"expected_date,omitempty"`
	ReceivedDate *time.Time `json:"received_date,omitempty"`
	CreatedBy    string     `json:"created_by"`
	Notes        string     `json:"notes"`
}

type POItem struct {
	ProductID   string `json:"product_id"`
	ProductName string `json:"product_name"`
	Quantity    int64  `json:"quantity"`
	UnitCost    int64  `json:"unit_cost"`
	ReceivedQty int64  `json:"received_qty"`
	TotalCost   int64  `json:"total_cost"`
}

type Supplier struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Contact      string   `json:"contact"`
	Phone        string   `json:"phone"`
	Email        string   `json:"email"`
	Address      string   `json:"address"`
	LeadTimeDays int      `json:"lead_time_days"`
	POHistory    []string `json:"po_history"`
	TotalOrders  int      `json:"total_orders"`
	TotalSpent   int64    `json:"total_spent"`
}

type OrderManager struct {
	orders    map[string]*PurchaseOrder
	suppliers map[string]*Supplier
}

func NewOrderManager() *OrderManager {
	return &OrderManager{
		orders:    make(map[string]*PurchaseOrder),
		suppliers: make(map[string]*Supplier),
	}
}

func (om *OrderManager) CreatePO(supplierID string, items []POItem, createdBy, notes string) (*PurchaseOrder, error) {
	supplier, ok := om.suppliers[supplierID]
	if !ok {
		return nil, ErrSupplierNotFound
	}

	var totalCost int64
	for i := range items {
		items[i].TotalCost = items[i].Quantity * items[i].UnitCost
		items[i].ReceivedQty = 0
		totalCost += items[i].TotalCost
	}

	po := &PurchaseOrder{
		ID:           generatePOID(),
		SupplierID:   supplierID,
		SupplierName: supplier.Name,
		Status:       StatusDraft,
		Items:        items,
		TotalCost:    totalCost,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		CreatedBy:    createdBy,
		Notes:        notes,
	}

	om.orders[po.ID] = po
	return po, nil
}

func (om *OrderManager) AddSupplier(supplier Supplier) error {
	if supplier.ID == "" {
		return errors.New("supplier ID required")
	}
	supplier.TotalOrders = len(supplier.POHistory)
	om.suppliers[supplier.ID] = &supplier
	return nil
}

func (om *OrderManager) SubmitPO(poID string) error {
	po, ok := om.orders[poID]
	if !ok {
		return errors.New("purchase order not found")
	}

	if po.Status != StatusDraft {
		return ErrInvalidPOStatus
	}

	po.Status = StatusPending
	po.UpdatedAt = time.Now()
	return nil
}

func (om *OrderManager) ApprovePO(poID string) error {
	po, ok := om.orders[poID]
	if !ok {
		return errors.New("purchase order not found")
	}

	if po.Status != StatusPending {
		return ErrInvalidPOStatus
	}

	po.Status = StatusApproved
	po.UpdatedAt = time.Now()
	return nil
}

func (om *OrderManager) MarkOrdered(poID string, expectedDate *time.Time) error {
	po, ok := om.orders[poID]
	if !ok {
		return errors.New("purchase order not found")
	}

	if po.Status != StatusApproved {
		return ErrInvalidPOStatus
	}

	po.Status = StatusOrdered
	po.ExpectedDate = expectedDate
	po.UpdatedAt = time.Now()
	return nil
}

func (om *OrderManager) ReceiveItem(poID, productID string, qty int64) error {
	po, ok := om.orders[poID]
	if !ok {
		return errors.New("purchase order not found")
	}

	found := false
	for i := range po.Items {
		if po.Items[i].ProductID == productID {
			po.Items[i].ReceivedQty += qty
			if po.Items[i].ReceivedQty > po.Items[i].Quantity {
				return errors.New("received qty exceeds ordered qty")
			}
			found = true
			break
		}
	}

	if !found {
		return ErrItemNotFound
	}

	po.UpdatedAt = time.Now()
	po.checkCompletion()

	return nil
}

func (om *OrderManager) ReceiveFullPO(poID string, receivedDate time.Time) error {
	po, ok := om.orders[poID]
	if !ok {
		return errors.New("purchase order not found")
	}

	if po.Status != StatusOrdered && po.Status != StatusPartial {
		return ErrInvalidPOStatus
	}

	for i := range po.Items {
		po.Items[i].ReceivedQty = po.Items[i].Quantity
	}

	po.Status = StatusReceived
	po.ReceivedDate = &receivedDate
	po.UpdatedAt = time.Now()

	return nil
}

func (om *OrderManager) CancelPO(poID string) error {
	po, ok := om.orders[poID]
	if !ok {
		return errors.New("purchase order not found")
	}

	if po.Status == StatusReceived || po.Status == StatusCancelled {
		return ErrInvalidPOStatus
	}

	po.Status = StatusCancelled
	po.UpdatedAt = time.Now()
	return nil
}

func (om *OrderManager) GetPO(poID string) (*PurchaseOrder, error) {
	po, ok := om.orders[poID]
	if !ok {
		return nil, errors.New("purchase order not found")
	}
	return po, nil
}

func (om *OrderManager) GetAllPOs() []*PurchaseOrder {
	var pos []*PurchaseOrder
	for _, po := range om.orders {
		pos = append(pos, po)
	}
	return pos
}

func (om *OrderManager) GetPOsByStatus(status Status) []*PurchaseOrder {
	var pos []*PurchaseOrder
	for _, po := range om.orders {
		if po.Status == status {
			pos = append(pos, po)
		}
	}
	return pos
}

func (om *OrderManager) GetSupplier(supplierID string) (*Supplier, error) {
	supplier, ok := om.suppliers[supplierID]
	if !ok {
		return nil, ErrSupplierNotFound
	}
	return supplier, nil
}

func (om *OrderManager) GetAllSuppliers() []*Supplier {
	var suppliers []*Supplier
	for _, s := range om.suppliers {
		suppliers = append(suppliers, s)
	}
	return suppliers
}

func (om *OrderManager) GetPendingOrders() []*PurchaseOrder {
	var pos []*PurchaseOrder
	for _, po := range om.orders {
		if po.Status == StatusPending || po.Status == StatusApproved || po.Status == StatusOrdered {
			pos = append(pos, po)
		}
	}
	return pos
}

func (om *OrderManager) GetOverdueOrders() []string {
	var overdue []string
	now := time.Now()

	for _, po := range om.orders {
		if po.Status != StatusOrdered {
			continue
		}
		if po.ExpectedDate != nil && po.ExpectedDate.Before(now) {
			overdue = append(overdue, po.ID)
		}
	}
	return overdue
}

type ReorderSuggestion struct {
	ProductID      string `json:"product_id"`
	CurrentStock   int64  `json:"current_stock"`
	AvgDailySales  int64  `json:"avg_daily_sales"`
	DaysUntilStock int    `json:"days_until_stock"`
	SupplierID     string `json:"supplier_id"`
	SupplierName   string `json:"supplier_name"`
	SuggestedQty   int64  `json:"suggested_qty"`
	Urgency        string `json:"urgency"`
}

func (om *OrderManager) GenerateReorderSuggestions(stock map[string]int64, avgSales map[string]int64) []ReorderSuggestion {
	var suggestions []ReorderSuggestion

	for productID, currentStock := range stock {
		avgDaily, ok := avgSales[productID]
		if !ok {
			avgDaily = 1
		}

		daysUntilStock := int(currentStock / avgDaily)
		if daysUntilStock < 7 {
			suggestedQty := avgDaily * 14

			suggestion := ReorderSuggestion{
				ProductID:      productID,
				CurrentStock:   currentStock,
				AvgDailySales:  avgDaily,
				DaysUntilStock: daysUntilStock,
				SuggestedQty:   suggestedQty,
			}

			if daysUntilStock <= 2 {
				suggestion.Urgency = "critical"
			} else if daysUntilStock <= 5 {
				suggestion.Urgency = "high"
			} else {
				suggestion.Urgency = "medium"
			}

			suggestions = append(suggestions, suggestion)
		}
	}

	return suggestions
}

func (om *OrderManager) CalculateOrderValue(poID string) (int64, error) {
	po, ok := om.orders[poID]
	if !ok {
		return 0, errors.New("purchase order not found")
	}
	return po.TotalCost, nil
}

func (om *OrderManager) GetTotalSpendBySupplier(supplierID string) (int64, error) {
	supplier, ok := om.suppliers[supplierID]
	if !ok {
		return 0, ErrSupplierNotFound
	}
	return supplier.TotalSpent, nil
}

func generatePOID() string {
	const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 8)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return "PO" + string(b) + time.Now().Format("060102")
}

func (po *PurchaseOrder) checkCompletion() {
	var allReceived bool
	var partialReceived bool

	for _, item := range po.Items {
		if item.ReceivedQty == 0 {
			allReceived = false
		} else if item.ReceivedQty < item.Quantity {
			partialReceived = true
		}
	}

	if allReceived {
		po.Status = StatusReceived
		now := time.Now()
		po.ReceivedDate = &now
	} else if partialReceived {
		po.Status = StatusPartial
	}
}

func ValidatePO(po *PurchaseOrder) error {
	if po.SupplierID == "" {
		return errors.New("supplier required")
	}
	if len(po.Items) == 0 {
		return errors.New("at least one item required")
	}
	for _, item := range po.Items {
		if item.Quantity <= 0 {
			return fmt.Errorf("invalid quantity for %s", item.ProductID)
		}
		if item.UnitCost <= 0 {
			return fmt.Errorf("invalid unit cost for %s", item.ProductID)
		}
	}
	return nil
}

type POReport struct {
	TotalOrders   int             `json:"total_orders"`
	TotalValue    int64           `json:"total_value"`
	PendingValue  int64           `json:"pending_value"`
	ReceivedValue int64           `json:"received_value"`
	AvgOrderValue int64           `json:"avg_order_value"`
	ByStatus      map[string]int  `json:"by_status"`
	TopSuppliers  []SupplierSpend `json:"top_suppliers"`
}

type SupplierSpend struct {
	SupplierID   string `json:"supplier_id"`
	SupplierName string `json:"supplier_name"`
	TotalSpent   int64  `json:"total_spent"`
	OrderCount   int    `json:"order_count"`
}

func (om *OrderManager) GenerateReport() POReport {
	report := POReport{
		ByStatus: make(map[string]int),
	}

	supplierSpend := make(map[string]SupplierSpend)

	for _, po := range om.orders {
		report.TotalOrders++
		report.TotalValue += po.TotalCost

		report.ByStatus[string(po.Status)]++

		switch po.Status {
		case StatusPending, StatusApproved, StatusOrdered:
			report.PendingValue += po.TotalCost
		case StatusReceived:
			report.ReceivedValue += po.TotalCost
		}

		if _, ok := supplierSpend[po.SupplierID]; !ok {
			supplierSpend[po.SupplierID] = SupplierSpend{
				SupplierID:   po.SupplierID,
				SupplierName: po.SupplierName,
			}
		}
		ss := supplierSpend[po.SupplierID]
		ss.TotalSpent += po.TotalCost
		ss.OrderCount++
		supplierSpend[po.SupplierID] = ss
	}

	if report.TotalOrders > 0 {
		report.AvgOrderValue = report.TotalValue / int64(report.TotalOrders)
	}

	var topSuppliers []SupplierSpend
	for _, ss := range supplierSpend {
		topSuppliers = append(topSuppliers, ss)
	}

	for i := 0; i < len(topSuppliers)-1; i++ {
		for j := i + 1; j < len(topSuppliers); j++ {
			if topSuppliers[j].TotalSpent > topSuppliers[i].TotalSpent {
				topSuppliers[i], topSuppliers[j] = topSuppliers[j], topSuppliers[i]
			}
		}
	}

	if len(topSuppliers) > 5 {
		topSuppliers = topSuppliers[:5]
	}
	report.TopSuppliers = topSuppliers

	return report
}
