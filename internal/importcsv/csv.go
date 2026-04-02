package importcsv

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"mini-database/core"
	"mini-database/engine"
)

type InventoryRow struct {
	ProductID string
	Quantity  int64
	Cost      int64
}

func ImportInventoryCSV(path string, eng *engine.Engine) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)

	var imported int
	headerSkipped := false

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return imported, fmt.Errorf("read csv: %w", err)
		}

		if !headerSkipped {
			if len(record) > 0 && (record[0] == "product_id" || record[0] == "ProductID" || record[0] == "product") {
				headerSkipped = true
				continue
			}
			headerSkipped = true
		}

		if len(record) < 3 {
			continue
		}

		productID := record[0]
		quantity, err := strconv.ParseInt(record[1], 10, 64)
		if err != nil {
			return imported, fmt.Errorf("parse quantity for %s: %w", productID, err)
		}
		cost, err := strconv.ParseInt(record[2], 10, 64)
		if err != nil {
			return imported, fmt.Errorf("parse cost for %s: %w", productID, err)
		}

		stock := core.StockItem{
			ProductID: productID,
			Quantity:  quantity,
			Cost:      cost,
		}

		if err := eng.ApplyStock(stock); err != nil {
			return imported, fmt.Errorf("apply stock for %s: %w", productID, err)
		}

		imported++
	}

	return imported, nil
}

type WorkerRow struct {
	WorkerID   string
	Name       string
	Pin        string
	Role       string
	Commission int64
}

func ImportWorkersCSV(path string) ([]WorkerRow, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)

	var workers []WorkerRow
	headerSkipped := false

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read csv: %w", err)
		}

		if !headerSkipped {
			if len(record) > 0 && (record[0] == "worker_id" || record[0] == "WorkerID") {
				headerSkipped = true
				continue
			}
			headerSkipped = true
		}

		if len(record) < 2 {
			continue
		}

		worker := WorkerRow{
			WorkerID: record[0],
			Name:     record[1],
		}

		if len(record) > 2 {
			worker.Pin = record[2]
		}
		if len(record) > 3 {
			worker.Role = record[3]
		}
		if len(record) > 4 {
			if c, err := strconv.ParseInt(record[4], 10, 64); err == nil {
				worker.Commission = c
			}
		}

		workers = append(workers, worker)
	}

	return workers, nil
}

type SaleRow struct {
	ProductID string
	Quantity  int64
	Price     int64
	WorkerID  string
	Payment   string
	Timestamp time.Time
}

func ImportSalesCSV(path string, eng *engine.Engine) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)

	var imported int
	headerSkipped := false

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return imported, fmt.Errorf("read csv: %w", err)
		}

		if !headerSkipped {
			if len(record) > 0 && (record[0] == "product_id" || record[0] == "ProductID") {
				headerSkipped = true
				continue
			}
			headerSkipped = true
		}

		if len(record) < 4 {
			continue
		}

		productID := record[0]
		quantity, err := strconv.ParseInt(record[1], 10, 64)
		if err != nil {
			continue
		}
		price, err := strconv.ParseInt(record[2], 10, 64)
		if err != nil {
			continue
		}
		workerID := record[3]

		var payment core.PaymentMethod
		if len(record) > 4 {
			if record[4] == "mpesa" || record[4] == "M-Pesa" {
				payment = core.PaymentMpesa
			} else {
				payment = core.PaymentCash
			}
		} else {
			payment = core.PaymentCash
		}

		sale := core.Sale{
			ProductID: productID,
			Quantity:  quantity,
			Price:     price,
			WorkerID:  workerID,
			Payment:   payment,
		}

		if err := eng.ApplySale(sale); err != nil {
			continue
		}

		imported++
	}

	return imported, nil
}

func ExportInventoryCSV(path string, inventory map[string]int64) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	writer.Write([]string{"product_id", "quantity", "cost"})

	for productID, quantity := range inventory {
		writer.Write([]string{productID, fmt.Sprintf("%d", quantity), "0"})
	}

	return writer.Error()
}
