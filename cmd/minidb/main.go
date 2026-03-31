package main

import (
	"encoding/json"
	"log"
	"mini-database/core"
	"mini-database/engine"
	"net/http"
)

func main() {

	e := engine.NewEngine()

	http.HandleFunc("/stock", func(w http.ResponseWriter, r *http.Request) {

		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var stock core.StockItem

		err := json.NewDecoder(r.Body).Decode(&stock)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		err = e.ApplyStock(stock)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusOK)
	})

	http.HandleFunc("/sale", func(w http.ResponseWriter, r *http.Request) {

		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var sale core.Sale

		err := json.NewDecoder(r.Body).Decode(&sale)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		err = e.ApplySale(sale)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusOK)
	})

	http.HandleFunc("/inventory", func(w http.ResponseWriter, r *http.Request) {

		inventory := e.InventorySnapshot()

		json.NewEncoder(w).Encode(inventory)
	})

	http.HandleFunc("/sales", func(w http.ResponseWriter, r *http.Request) {

		summary := e.SalesSummary()

		json.NewEncoder(w).Encode(summary)
	})

	http.HandleFunc("/verify-ledger", func(w http.ResponseWriter, r *http.Request) {

		err := e.VerifyLedger()

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Write([]byte("ledger verified"))
	})

	log.Println("MiniDB server running on :8080")

	log.Fatal(http.ListenAndServe(":8080", nil))
}
