package cli

import (
	"fmt"
	"mini-database/internal/mpesa"

	"github.com/spf13/cobra"
)

var mpesaCmd = &cobra.Command{
	Use:   "mpesa",
	Short: "M-Pesa payment integration",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Usage: pos mpesa pay|status|config")
	},
}

var mpesaPayCmd = &cobra.Command{
	Use:   "pay [phone] [amount]",
	Short: "Initiate M-Pesa STK push payment",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		phone := args[0]
		var amount int64
		fmt.Sscanf(args[1], "%d", &amount)

		cfg := mpesa.LoadMpesaConfig()
		if !cfg.IsConfigured() {
			fmt.Println("M-Pesa not configured. Set environment variables:")
			fmt.Println("  MPESA_CONSUMER_KEY, MPESA_CONSUMER_SECRET")
			fmt.Println("  MPESA_SHORT_CODE, MPESA_PASSKEY")
			fmt.Println("  MPESA_ENVIRONMENT, MPESA_CALLBACK_URL")
			return
		}

		client := mpesa.NewClient(mpesa.Config{
			ConsumerKey:    cfg.ConsumerKey,
			ConsumerSecret: cfg.ConsumerSecret,
			ShortCode:      cfg.ShortCode,
			Passkey:        cfg.Passkey,
			Environment:    cfg.Environment,
			CallbackURL:    cfg.CallbackURL,
		})

		resp, err := client.STKPush(mpesa.STKPushRequest{
			PhoneNumber: phone,
			Amount:      amount,
			AccountRef:  mpesa.GenerateAccountRef(),
			Description: "POS Payment",
		})

		if err != nil {
			fmt.Printf("Payment failed: %v\n", err)
			return
		}

		if resp.ResponseCode != "0" {
			fmt.Printf("Payment failed: %s\n", resp.ResponseDesc)
			return
		}

		fmt.Printf("✓ STK Push sent to %s\n", phone)
		fmt.Printf("  Checkout ID: %s\n", resp.CheckoutRequestID)
		fmt.Println("  Check your phone for payment prompt")
	},
}

var mpesaConfigCmd = &cobra.Command{
	Use:   "config",
	Short: "Show M-Pesa configuration status",
	Run: func(cmd *cobra.Command, args []string) {
		cfg := mpesa.LoadMpesaConfig()

		if cfg.IsConfigured() {
			fmt.Println("✓ M-Pesa configured")
			fmt.Printf("  Environment: %s\n", cfg.Environment)
			fmt.Printf("  Short Code: %s\n", cfg.ShortCode)
		} else {
			fmt.Println("✗ M-Pesa not configured")
			fmt.Println("\nConfigure with environment variables:")
			fmt.Println("  MPESA_CONSUMER_KEY    - Your M-Pesa API consumer key")
			fmt.Println("  MPESA_CONSUMER_SECRET - Your M-Pesa API consumer secret")
			fmt.Println("  MPESA_SHORT_CODE      - Business short code")
			fmt.Println("  MPESA_PASSKEY         - M-Pesa passkey")
			fmt.Println("  MPESA_ENVIRONMENT     - sandbox or production")
			fmt.Println("  MPESA_CALLBACK_URL    - Callback URL for payment notifications")
		}
	},
}

func init() {
	mpesaCmd.AddCommand(mpesaPayCmd)
	mpesaCmd.AddCommand(mpesaConfigCmd)
	rootCmd.AddCommand(mpesaCmd)
}
