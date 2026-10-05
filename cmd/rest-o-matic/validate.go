package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/drewlsvern/rest-o-matic/internal/color"
	"github.com/drewlsvern/rest-o-matic/internal/config"
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate the config file without running anything",
	Long: `validate checks the config file by the same rules every other command
applies, and reports every problem it finds, with its line in the file.
Errors make the config unusable; warnings point at something that is
probably, but not certainly, wrong.

validate needs nothing from the machine it runs on: no host key, state
directory or source paths, so a config gives the same result anywhere. The
one exception is read_as: podman-unshare, which is checked against this
machine's operating system and whether podman is installed.

With --json it prints one JSON document instead, whatever the outcome, with
every problem's severity, message, field path, line and column. The format
is described in contract/validate/v1 in the source repository.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		_, problems := config.Check(configPath)
		if validateJSON {
			report := validateReport(problems)
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(report); err != nil {
				return err
			}
			if !report.Valid {
				// The document says why; nothing more goes to stderr.
				os.Exit(1)
			}
			return nil
		}
		if n := printProblems(problems); n > 0 {
			return fmt.Errorf("%d config validation error(s)", n)
		}
		fmt.Println(color.Stdout.Success("config is valid"))
		return nil
	},
}

var validateJSON bool

func init() {
	validateCmd.Flags().BoolVar(&validateJSON, "json", false, "print one JSON document instead of text")
}

// validateFormatVersion identifies the shape of the validate --json
// document. It changes only for an incompatible change.
const validateFormatVersion = 1

type validateDoc struct {
	FormatVersion     int           `json:"format_version"`
	RestOMaticVersion string        `json:"rest_o_matic_version"`
	Valid             bool          `json:"valid"`
	Problems          []problemJSON `json:"problems"`
}

// problemJSON is one problem. Every key is always present; a value that
// doesn't apply or isn't known is null.
type problemJSON struct {
	Severity   string  `json:"severity"`
	Message    string  `json:"message"`
	Job        *string `json:"job"`
	Repository *string `json:"repository"`
	Path       *string `json:"path"`
	Line       *int    `json:"line"`
	Column     *int    `json:"column"`
}

func validateReport(problems []config.Problem) validateDoc {
	doc := validateDoc{
		FormatVersion:     validateFormatVersion,
		RestOMaticVersion: readBuildInfo().Version,
		Valid:             !config.HasErrors(problems),
		Problems:          []problemJSON{},
	}
	intPtr := func(n int) *int {
		if n == 0 {
			return nil
		}
		return &n
	}
	for _, p := range problems {
		doc.Problems = append(doc.Problems, problemJSON{
			Severity: p.Severity, Message: p.Message,
			Job: strPtr(p.Job), Repository: strPtr(p.Repository), Path: strPtr(p.Path),
			Line: intPtr(p.Line), Column: intPtr(p.Column),
		})
	}
	return doc
}
