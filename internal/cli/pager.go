package cli

import (
	"bytes"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var cfgPager = "auto"
var cfgBootstrap = true

// writeOutput sends rendered output to the terminal, using less as a pager
// when the output is long and stdout is a TTY. Piped output is never paged.
func writeOutput(cmd *cobra.Command, data []byte) error {
	out := cmd.OutOrStdout()

	if outputMode() == formatTTY && !pagerDisabled() && isTerminalWriter(out) && len(data) > 0 {
		if less, err := exec.LookPath("less"); err == nil {
			c := exec.Command(less, "-R", "-F", "-X")
			c.Stdin = bytes.NewReader(data)
			c.Stdout = out
			// c.Stderr = os.Stderr
			c.Stderr = cmd.ErrOrStderr()
			return c.Run()
		}
	}
	// _, err := cmd.OutOrStdout().Write(data)
	_, err := out.Write(data)
	return err
}

func pagerDisabled() bool {
	if flagNoPager {
		return true
	}
	switch strings.ToLower(cfgPager) {
	case "never", "off", "false":
		return true
	}
	if v, ok := os.LookupEnv("DOCTY_NO_PAGER"); ok {
		if b, err := strconv.ParseBool(v); err == nil && b {
			return true
		}
	}
	return false
}

// func isTTY() bool {
// 	st, err := os.Stdout.Stat()
// 	if err != nil {
// 		return false
// 	}
// 	return st.Mode()&os.ModeCharDevice != 0
// }
