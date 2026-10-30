package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/luisdvss/docty/internal/auth"
	"github.com/luisdvss/docty/internal/config"
	"github.com/luisdvss/docty/internal/llm"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type authStatusJSON struct {
	SchemaVersion int    `json:"schema_version"`
	Configured    bool   `json:"configured"`
	Provider      string `json:"provider,omitempty"`
	Model         string `json:"model,omitempty"`
	BaseURL       string `json:"base_url,omitempty"`
	KeySource     string `json:"key_source"`
	KeyringError  string `json:"keyring_error,omitempty"`
	Config        string `json:"config"`
	Online        string `json:"online,omitempty"`
	OnlineDetail  string `json:"online_detail,omitempty"`
}

type authLoginJSON struct {
	SchemaVersion int    `json:"schema_version"`
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	BaseURL       string `json:"base_url,omitempty"`
	KeySource     string `json:"key_source"`
	Config        string `json:"config"`
	EnvOverride   bool   `json:"env_override,omitempty"`
}

type authLogoutJSON struct {
	SchemaVersion int    `json:"schema_version"`
	Provider      string `json:"provider,omitempty"`
	Removed       bool   `json:"removed"`
}

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Gestiona la API key del proveedor LLM",
		Long: `Guarda la API key en el keyring del sistema y configura el proveedor.

Sin subcomando muestra el estado actual. La precedencia de la clave es:
DOCTY_API_KEY, llm.api_key y, por último, el keyring.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthStatus(cmd, false)
		},
	}
	cmd.AddCommand(newAuthLoginCmd())
	cmd.AddCommand(newAuthStatusCmd())
	cmd.AddCommand(newAuthLogoutCmd())
	return cmd
}

func newAuthLoginCmd() *cobra.Command {
	var opts authLoginOptions
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Guarda la API key en el keyring y configura el proveedor",
		Long: `Guarda la API key en el keyring del sistema.

Si no se indica --provider se usa el proveedor configurado o groq. La clave
se toma de --api-key-stdin, DOCTY_API_KEY, stdin o se pide de forma interactiva.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfigAllowMissing()
			if err != nil {
				return err
			}
			return runAuthLogin(cmd, cfg, opts)
		},
	}
	cmd.Flags().StringVar(&opts.provider, "provider", "", "proveedor: groq, openrouter u openai-compatible")
	cmd.Flags().StringVar(&opts.model, "model", "", "modelo a usar")
	cmd.Flags().StringVar(&opts.baseURL, "base-url", "", "URL base para openai-compatible")
	cmd.Flags().BoolVar(&opts.keyStdin, "api-key-stdin", false, "lee la API key de stdin")
	cmd.Flags().BoolVar(&opts.noValidate, "no-validate", false, "no comprobar la clave contra el proveedor")
	return cmd
}

type authLoginOptions struct {
	provider   string
	model      string
	baseURL    string
	keyStdin   bool
	noValidate bool
}

func newAuthStatusCmd() *cobra.Command {
	var online bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Muestra el proveedor y el origen de la API key",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthStatus(cmd, online)
		},
	}
	cmd.Flags().BoolVar(&online, "online", false, "comprueba también la conectividad")
	return cmd
}

func newAuthLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Borra la API key del keyring",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthLogout(cmd)
		},
	}
}

func runAuthLogin(cmd *cobra.Command, cfg config.Config, opts authLoginOptions) error {
	in := cmd.InOrStdin()
	interactive := isTerminalReader(in)
	reader := bufio.NewReader(in)

	provider := strings.ToLower(strings.TrimSpace(opts.provider))
	if provider == "" {
		if interactive {
			def := strings.ToLower(strings.TrimSpace(cfg.LLM.Provider))
			if def == "" || def == "none" {
				def = "groq"
			}
			for {
				v, err := promptLine(cmd, reader, fmt.Sprintf("Proveedor [groq/openrouter/openai-compatible] (%s): ", def))
				if err != nil {
					return exitWith(2, "leyendo el proveedor: %v", err)
				}
				if v == "" {
					v = def
				}
				v = strings.ToLower(v)
				if validProvider(v) {
					provider = v
					break
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "proveedor inválido %q\n", v)
			}
		} else {
			provider = strings.ToLower(strings.TrimSpace(cfg.LLM.Provider))
			if provider == "" || provider == "none" {
				provider = "groq"
			}
		}
	}
	if !validProvider(provider) {
		return exitWith(2, "proveedor inválido %q (usa groq, openrouter u openai-compatible)", provider)
	}

	sameProvider := strings.EqualFold(strings.TrimSpace(cfg.LLM.Provider), provider)
	baseURL := strings.TrimSpace(opts.baseURL)
	model := strings.TrimSpace(opts.model)
	baseURLExplicit := baseURL != ""
	if baseURL == "" && sameProvider {
		baseURL = strings.TrimSpace(cfg.LLM.BaseURL)
	}
	if model == "" && sameProvider {
		model = strings.TrimSpace(cfg.LLM.Model)
	}

	if provider == "openai-compatible" {
		if baseURL == "" {
			if !interactive {
				return exitWith(2, "openai-compatible requiere --base-url")
			}
			var err error
			baseURL, err = promptRequired(cmd, reader, "Base URL: ")
			if err != nil {
				return exitWith(2, "leyendo la base URL: %v", err)
			}
		}
		if model == "" {
			if !interactive {
				return exitWith(2, "openai-compatible requiere --model")
			}
			var err error
			model, err = promptRequired(cmd, reader, "Modelo: ")
			if err != nil {
				return exitWith(2, "leyendo el modelo: %v", err)
			}
		}
	}

	candidate := cfg.LLM
	candidate.Provider = provider
	candidate.Model = model
	candidate.BaseURL = baseURL
	settings, err := llm.ResolveProvider(candidate)
	if err != nil {
		return exitWith(2, "%v", err)
	}

	key, err := resolveLoginKey(cmd, interactive, opts.keyStdin)
	if err != nil {
		return exitWith(2, "leyendo la API key: %v", err)
	}
	if key == "" {
		return exitWith(2, "la API key está vacía")
	}
	envOverride := strings.TrimSpace(os.Getenv("DOCTY_API_KEY")) != ""

	if !opts.noValidate {
		settings.APIKey = key
		status, _, probeErr := probeProvider(settings)
		switch {
		case probeErr != nil:
			fmt.Fprintf(cmd.ErrOrStderr(), "aviso: no se pudo validar la clave: %v\n", probeErr)
		case status == 401 || status == 403:
			return exitWith(3, "el proveedor rechazó la API key (HTTP %d)", status)
		case status == 404:
			fmt.Fprintln(cmd.ErrOrStderr(), "aviso: el proveedor no expone /models; no se pudo validar la clave")
		case status >= 400:
			fmt.Fprintf(cmd.ErrOrStderr(), "aviso: la validación devolvió HTTP %d; se guarda la clave igualmente\n", status)
		}
	}

	oldKey, oldErr := auth.GetKey(settings.Provider, settings.BaseURL)
	hadOldKey := oldErr == nil && oldKey != ""
	if err := auth.SetKey(settings.Provider, settings.BaseURL, key); err != nil {
		return exitWith(3,
			"no se pudo guardar la clave en el keyring: %v\n"+
				"       la configuración no se modificó; alternativas: exporta DOCTY_API_KEY y configura provider/model en %s",
			err, config.Path())
	}

	savedBaseURL := ""
	if settings.Provider == "openai-compatible" || baseURLExplicit || (sameProvider && cfg.LLM.BaseURL != "") {
		savedBaseURL = settings.BaseURL
	}
	if err := config.SaveLLM(settings.Provider, settings.Model, savedBaseURL); err != nil {
		if hadOldKey {
			_ = auth.SetKey(settings.Provider, settings.BaseURL, oldKey)
		} else {
			_ = auth.DeleteKey(settings.Provider, settings.BaseURL)
		}
		return exitWith(3, "no se pudo actualizar %s: %v", config.Path(), err)
	}

	if envOverride {
		fmt.Fprintln(cmd.ErrOrStderr(), "aviso: DOCTY_API_KEY sigue teniendo prioridad; desactívala para usar el keyring")
	}

	if flagJSON {
		return writeJSON(cmd, authLoginJSON{
			SchemaVersion: 1,
			Provider:      settings.Provider,
			Model:         settings.Model,
			BaseURL:       savedBaseURL,
			KeySource:     string(llm.KeySourceKeyring),
			Config:        config.Path(),
			EnvOverride:   envOverride,
		})
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "✓ clave guardada en el keyring para %s\n", settings.Provider)
	fmt.Fprintf(out, "✓ configuración actualizada en %s\n", config.Path())
	fmt.Fprintf(out, "  modelo: %s\n", settings.Model)
	if savedBaseURL != "" {
		fmt.Fprintf(out, "  base URL: %s\n", savedBaseURL)
	}
	return nil
}

func runAuthStatus(cmd *cobra.Command, online bool) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	result := authStatusJSON{
		SchemaVersion: 1,
		KeySource:     string(llm.KeySourceNone),
		Config:        config.Path(),
	}
	out := cmd.OutOrStdout()

	provider := strings.ToLower(strings.TrimSpace(cfg.LLM.Provider))
	if provider == "" || provider == "none" {
		if flagJSON {
			return writeJSON(cmd, result)
		}
		fmt.Fprintln(out, "Proveedor   no configurado")
		fmt.Fprintf(out, "Config      %s\n", config.Path())
		fmt.Fprintln(out, "Usa: docty auth login")
		return nil
	}

	settings, err := llm.ResolveProvider(cfg.LLM)
	if err != nil {
		return exitWith(2, "%v", err)
	}
	key, source, keyErr := llm.ResolveKey(cfg.LLM)

	result.Configured = true
	result.Provider = settings.Provider
	result.Model = settings.Model
	result.BaseURL = settings.BaseURL
	result.KeySource = string(source)
	if keyErr != nil {
		result.KeySource = string(llm.KeySourceNone)
		result.KeyringError = keyErr.Error()
	}

	var connStatus, connDetail string
	if online {
		connStatus, connDetail = authConnectivity(settings, key)
		result.Online = onlineCode(connStatus)
		result.OnlineDetail = connDetail
	}

	if flagJSON {
		return writeJSON(cmd, result)
	}

	fmt.Fprintf(out, "Proveedor   %s\n", settings.Provider)
	fmt.Fprintf(out, "Modelo      %s\n", settings.Model)
	fmt.Fprintf(out, "Base URL    %s\n", settings.BaseURL)
	switch source {
	case llm.KeySourceEnv:
		fmt.Fprintln(out, "API key     env DOCTY_API_KEY")
	case llm.KeySourceConfig:
		fmt.Fprintf(out, "API key     config llm.api_key (%s)\n", config.Path())
	case llm.KeySourceKeyring:
		fmt.Fprintln(out, "API key     keyring del sistema")
	default:
		if keyErr != nil {
			fmt.Fprintf(out, "API key     no disponible · %v\n", keyErr)
		} else {
			fmt.Fprintln(out, "API key     ninguna · usa: docty auth login")
		}
	}
	fmt.Fprintf(out, "Config      %s\n", config.Path())
	if online {
		fmt.Fprintf(out, "Conectividad %s %s\n", connStatus, connDetail)
	}
	return nil
}

func runAuthLogout(cmd *cobra.Command) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	provider := strings.ToLower(strings.TrimSpace(cfg.LLM.Provider))
	if provider == "" || provider == "none" {
		if flagJSON {
			return writeJSON(cmd, authLogoutJSON{SchemaVersion: 1})
		}
		fmt.Fprintln(cmd.OutOrStdout(), "No hay proveedor configurado.")
		return nil
	}

	settings, err := llm.ResolveProvider(cfg.LLM)
	if err != nil {
		return exitWith(2, "%v", err)
	}

	err = auth.DeleteKey(settings.Provider, settings.BaseURL)
	removed := err == nil
	if err != nil && !errors.Is(err, auth.ErrNotFound) {
		return exitWith(3, "no se pudo borrar la clave del keyring: %v", err)
	}

	if flagJSON {
		return writeJSON(cmd, authLogoutJSON{
			SchemaVersion: 1,
			Provider:      settings.Provider,
			Removed:       removed,
		})
	}

	out := cmd.OutOrStdout()
	if removed {
		fmt.Fprintf(out, "✓ clave eliminada del keyring para %s\n", settings.Provider)
	} else {
		fmt.Fprintf(out, "· no había clave guardada para %s\n", settings.Provider)
	}
	_, source, _ := llm.ResolveKey(cfg.LLM)
	switch source {
	case llm.KeySourceConfig:
		fmt.Fprintf(cmd.ErrOrStderr(), "aviso: llm.api_key sigue en %s; bórralo para dejar de usarla\n", config.Path())
	case llm.KeySourceEnv:
		fmt.Fprintln(cmd.ErrOrStderr(), "aviso: DOCTY_API_KEY sigue teniendo prioridad")
	}
	return nil
}

func authConnectivity(settings llm.Settings, key string) (string, string) {
	if key == "" && settings.Provider != "openai-compatible" {
		return "·", "sin API key"
	}
	settings.APIKey = key
	return checkProvider(settings)
}

func onlineCode(status string) string {
	switch status {
	case "✓":
		return "ok"
	case "✗":
		return "error"
	default:
		return "skipped"
	}
}

func validProvider(p string) bool {
	switch p {
	case "groq", "openrouter", "openai-compatible":
		return true
	}
	return false
}

func resolveLoginKey(cmd *cobra.Command, interactive, forceStdin bool) (string, error) {
	if forceStdin {
		data, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), 64*1024))
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(data)), nil
	}
	if v := strings.TrimSpace(os.Getenv("DOCTY_API_KEY")); v != "" {
		return v, nil
	}
	if !interactive {
		data, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), 64*1024))
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(data)), nil
	}
	for {
		key, err := promptSecret(cmd, "API key: ")
		if err != nil {
			return "", err
		}
		if key != "" {
			return key, nil
		}
		fmt.Fprintln(cmd.ErrOrStderr(), "la API key no puede estar vacía")
	}
}

func promptLine(cmd *cobra.Command, reader *bufio.Reader, prompt string) (string, error) {
	fmt.Fprint(cmd.ErrOrStderr(), prompt)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func promptRequired(cmd *cobra.Command, reader *bufio.Reader, prompt string) (string, error) {
	for {
		v, err := promptLine(cmd, reader, prompt)
		if err != nil {
			return "", err
		}
		if v != "" {
			return v, nil
		}
		fmt.Fprintln(cmd.ErrOrStderr(), "el valor no puede estar vacío")
	}
}

func promptSecret(cmd *cobra.Command, prompt string) (string, error) {
	f, ok := cmd.InOrStdin().(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		return promptLine(cmd, bufio.NewReader(cmd.InOrStdin()), prompt)
	}
	fmt.Fprint(cmd.ErrOrStderr(), prompt)
	b, err := term.ReadPassword(int(f.Fd()))
	fmt.Fprintln(cmd.ErrOrStderr())
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func isTerminalReader(in io.Reader) bool {
	f, ok := in.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

func writeJSON(cmd *cobra.Command, v any) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
