package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/drewlsvern/rest-o-matic/internal/config"
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate the config file without running anything",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(configPath)
		if err != nil {
			return err
		}

		errs := config.Validate(cfg)
		if len(errs) == 0 {
			fmt.Println("config is valid")
			return nil
		}

		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "config error:", e)
		}
		return fmt.Errorf("%d config validation error(s)", len(errs))
	},
}
