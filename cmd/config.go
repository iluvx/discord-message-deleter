package cmd

import (
	"bufio"
	"os"
	"strings"

	"github.com/iluvx/discord-message-deleter/internal/config"
	"github.com/iluvx/discord-message-deleter/internal/ui"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage stored configuration (your Discord token)",
	Long:  `Manage the configuration file, which lives in your OS's standard config directory.`,
}

var configSetTokenCmd = &cobra.Command{
	Use:   "set-token [token]",
	Short: "Store your Discord token",
	Long:  `Store your Discord token in the config file.`,
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		var token string
		if len(args) == 1 {
			token = strings.TrimSpace(args[0])
		} else {
			ui.Step("Paste your Discord token (input is read from stdin):")
			reader := bufio.NewReader(os.Stdin)
			line, err := reader.ReadString('\n')
			if err != nil && line == "" {
				return err
			}
			token = strings.TrimSpace(line)
		}

		if token == "" {
			ui.Error("No token provided.")
			return errSilent
		}

		cfg.Token = token
		if err := cfg.Save(); err != nil {
			return err
		}

		path, _ := config.Path()
		ui.Success("Token saved to %s", ui.Dim(path))
		return nil
	},
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show the current configuration",
	Long:  `Show the stored configuration. The token is masked unless --reveal is set.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		path, _ := config.Path()

		ui.Field("path", "%s", ui.Dim(path))
		if cfg.Token == "" {
			ui.Field("token", "%s", ui.Dim("(not set — run 'config set-token')"))
			return nil
		}

		reveal, _ := cmd.Flags().GetBool("reveal")
		if reveal {
			ui.Field("token", "%s", cfg.Token)
		} else {
			ui.Field("token", "%s", maskToken(cfg.Token))
		}
		return nil
	},
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print the config file path",
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := config.Path()
		if err != nil {
			return err
		}
		cmd.Println(path)
		return nil
	},
}

// maskToken shows only the first few characters of a token.
func maskToken(token string) string {
	if len(token) <= 6 {
		return strings.Repeat("•", len(token))
	}
	return token[:6] + strings.Repeat("•", len(token)-6)
}

func init() {
	configShowCmd.Flags().Bool("reveal", false, "Show the token in full instead of masking it")

	configCmd.AddCommand(configSetTokenCmd)
	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configPathCmd)
	rootCmd.AddCommand(configCmd)
}
