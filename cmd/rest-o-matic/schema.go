package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/drewlsvern/rest-o-matic/internal/config"
)

var schemaCmd = &cobra.Command{
	Use:   "schema",
	Short: "Print a JSON Schema of the config file, for editors",
	Long: `schema prints a JSON Schema (draft-07) describing the config file this
version of rest-o-matic reads: every key, what it is for, and the values it
allows. Give it to an editor's YAML support for completion and checking as
you type.

The editor must also be told about the !locked and !plain tags, which the
schema sees as plain strings. In VS Code's YAML extension, add them to
"yaml.customTags" as "!locked scalar" and "!plain scalar".`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := os.Stdout.Write(config.Schema)
		return err
	},
}
