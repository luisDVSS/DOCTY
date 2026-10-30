package cli

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

var (
	version = "0.1.0-dev"
	commit  = "none"
	date    = "unknown"
)

func newVersionCmd() *cobra.Command {
	var short bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Muestra la versión de Docty",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if short {
				fmt.Fprintln(cmd.OutOrStdout(), version)
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "docty %s\n", version)
			fmt.Fprintf(cmd.OutOrStdout(), "  commit:  %s\n", commit)
			fmt.Fprintf(cmd.OutOrStdout(), "  built:   %s\n", date)
			fmt.Fprintf(cmd.OutOrStdout(), "  go:      %s\n", runtime.Version())
			fmt.Fprintf(cmd.OutOrStdout(), "  platform:%s/%s\n", runtime.GOOS, runtime.GOARCH)
			return nil
		},
	}
	cmd.Flags().BoolVar(&short, "short", false, "solo el número de versión")
	return cmd
}
