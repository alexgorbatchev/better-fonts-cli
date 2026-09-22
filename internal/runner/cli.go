package runner

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/alexgorbatchev/better-fonts/internal/app"
	"github.com/alexgorbatchev/better-fonts/internal/config"
	"github.com/alexgorbatchev/better-fonts/internal/selfupdate"
	"github.com/alexgorbatchev/better-fonts/internal/sysutil"
	cobrahelptree "github.com/alexgorbatchev/cobra-help-tree/v2"
	"github.com/spf13/cobra"
)

// GlobalFlags represents root persistent flags that apply across all commands.
type GlobalFlags struct {
	ConfigFile string
	Verbose    bool
}

// PatchFlags represents flags specific to the patch command.
type PatchFlags struct {
	Font      string
	Driver    string
	Restart   bool
	NoRestart bool
	DryRun    bool
}

// UnpatchFlags represents flags specific to the unpatch command.
type UnpatchFlags struct {
	Driver    string
	Restart   bool
	NoRestart bool
	DryRun    bool
}

// NewRootCommand constructs the Cobra command tree for better-fonts.
func NewRootCommand(version string) *cobra.Command {
	flags := &GlobalFlags{}

	rootCmd := &cobra.Command{
		Use:   "better-fonts",
		Short: "Patch and unpatch macOS Electron and Native applications with custom fonts",
		Long: `better-fonts is a modular CLI tool for patching and unpatching macOS applications
(Electron apps like Paseo, Signal, Slack, and Native apps like Rekordbox, Engine DJ, Telegram,
or any arbitrary .app bundle) with custom fonts.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Name() == "init" {
				return nil
			}
			configPath := flags.ConfigFile
			if configPath == "" {
				var err error
				configPath, err = config.GetConfigFilePath()
				if err != nil {
					return err
				}
			}
			_, err := config.EnsureConfigFile(configPath)
			return err
		},
	}

	rootCmd.Version = version
	rootCmd.SetVersionTemplate("{{.Version}}\n")

	pflags := rootCmd.PersistentFlags()
	pflags.StringVarP(&flags.ConfigFile, "config", "c", "", "path to config.toml (default $XDG_CONFIG_HOME/better-fonts/config.toml)")
	pflags.BoolVarP(&flags.Verbose, "verbose", "v", false, "enable verbose output")

	rootCmd.AddCommand(newPatchCommand(flags))
	rootCmd.AddCommand(newUnpatchCommand(flags))
	rootCmd.AddCommand(newStatusCommand(flags))
	rootCmd.AddCommand(newListCommand(flags))
	rootCmd.AddCommand(newConfigCommand(flags))
	rootCmd.AddCommand(newUpgradeCommand(version))

	if err := cobrahelptree.SetupWithOptions(rootCmd, cobrahelptree.HelpOptions{
		Catalog: BuildTechCatalog(),
		Tree: cobrahelptree.TreeOptions{
			HideGeneratedCommands: true,
		},
	}); err != nil {
		panic(fmt.Sprintf("setting up help tree: %v", err))
	}

	return rootCmd
}

// BuildTechCatalog constructs the command metadata and argument specifications for cobra-help-tree.
func BuildTechCatalog() cobrahelptree.TechCatalog {
	return cobrahelptree.TechCatalog{
		"better-fonts": {
			Summary:     "Patch and unpatch macOS Electron and Native applications with custom fonts",
			Description: "better-fonts is a modular CLI tool for patching and unpatching macOS applications (Electron apps like Paseo, Signal, Slack, and Native apps like Rekordbox, Engine DJ, Telegram, or any arbitrary .app bundle) with custom fonts.",
			Metadata: map[string]string{
				"author":  "Alex Gorbatchev",
				"license": "MIT",
			},
		},
		"better-fonts patch": {
			Summary:     "Patch applications to use the configured font",
			Description: "Modifies applications to use your chosen font by patching preload scripts in Electron apps or injecting CoreText dynamic libraries in native apps.",
			Args: []cobrahelptree.ArgSpec{
				{
					Name:        "[apps...]",
					Description: "Application name(s), bundle ID(s), or .app bundle paths to patch. If omitted, all configured apps are patched.",
				},
			},
			AutoBackup: true,
			Metadata: map[string]string{
				"driver":     "electron|native-hook",
				"restart":    "true|false",
				"reversible": "true",
			},
		},
		"better-fonts unpatch": {
			Summary:     "Remove font patch from applications",
			Description: "Restores original preload scripts or executables for applications, reverting them to their unpatched state.",
			Args: []cobrahelptree.ArgSpec{
				{
					Name:        "[apps...]",
					Description: "Application name(s), bundle ID(s), or .app bundle paths to unpatch. If omitted, all configured apps are unpatched.",
				},
			},
			AutoBackup: true,
			Metadata: map[string]string{
				"driver":  "electron|native-hook",
				"restart": "true|false",
			},
		},
		"better-fonts status": {
			Summary:     "Show installation and patch status of supported applications",
			Description: "Displays a table showing driver, installation status, current patch state, and active font for each supported application.",
		},
		"better-fonts list": {
			Summary:     "List all supported applications",
			Description: "Prints a summary of all built-in and custom-configured applications along with their default installation paths and patching drivers.",
		},
		"better-fonts config": {
			Summary:     "Manage better-fonts configuration",
			Description: "Inspect, display, or initialize the user configuration file.",
		},
		"better-fonts config path": {
			Summary:     "Print the path to config.toml",
			Description: "Resolves and outputs the active configuration file path according to XDG base directory rules.",
		},
		"better-fonts config show": {
			Summary:     "Display the current configuration file contents",
			Description: "Outputs the complete contents of the active config.toml configuration file.",
		},
		"better-fonts config init": {
			Summary:     "Create a default configuration file if not already present",
			Description: "Generates an annotated default config.toml file at the XDG configuration path.",
		},
		"better-fonts upgrade": {
			Summary:     "Check for and install the latest release of better-fonts",
			Description: "Queries GitHub for newer releases and performs an in-place self-upgrade of the executable.",
			Metadata: map[string]string{
				"source": "github.com/alexgorbatchev/better-fonts-cli",
			},
		},
	}
}

func loadAppConfig(flags *GlobalFlags) (*config.Config, error) {
	configPath := flags.ConfigFile
	if configPath == "" {
		var err error
		configPath, err = config.GetConfigFilePath()
		if err != nil {
			return nil, err
		}
	}

	cfg, err := config.EnsureConfigFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("loading configuration: %w", err)
	}

	return cfg, nil
}

func detectAppDriver(appPath string, forcedDriver string) app.DriverType {
	if strings.ToLower(forcedDriver) == string(app.DriverElectron) {
		return app.DriverElectron
	}
	if strings.ToLower(forcedDriver) == string(app.DriverNativeHook) {
		return app.DriverNativeHook
	}

	// Check if app bundle has an asar file in Contents/Resources
	resDir := filepath.Join(appPath, "Contents", "Resources")
	if entries, err := os.ReadDir(resDir); err == nil {
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".asar") {
				return app.DriverElectron
			}
		}
	}

	return app.DriverNativeHook
}

func selectTargetApps(all []app.App, cfg *config.Config, explicitArgs []string, forcedDriver string) []app.App {
	if len(explicitArgs) > 0 {
		var targets []app.App
		for _, arg := range explicitArgs {
			norm := strings.ToLower(strings.TrimSpace(arg))
			if norm == "*" || norm == "all" {
				return all
			}

			// 1. Match against known registry
			if matched, ok := app.FindApp(all, arg); ok {
				appCopy := *matched
				if forcedDriver != "" {
					appCopy.Driver = app.DriverType(forcedDriver)
				}
				targets = append(targets, appCopy)
				continue
			}

			// 2. Check if arg is an application bundle path or name in /Applications
			appPath := arg
			if !filepath.IsAbs(appPath) && !strings.HasSuffix(appPath, ".app") {
				appPath = filepath.Join("/Applications", arg+".app")
			} else if !filepath.IsAbs(appPath) {
				if abs, err := filepath.Abs(appPath); err == nil {
					appPath = abs
				}
			}

			if _, err := os.Stat(appPath); err == nil {
				baseName := strings.TrimSuffix(filepath.Base(appPath), ".app")
				driver := detectAppDriver(appPath, forcedDriver)

				targets = append(targets, app.App{
					ID:             strings.ToLower(baseName),
					Name:           baseName,
					AppPath:        appPath,
					ProcessName:    baseName,
					Driver:         driver,
					PatchMarker:    fmt.Sprintf("fonted-%s-patch", strings.ToLower(baseName)),
					PreloadRelPath: "preload.js",
					ResolveAsarPath: func(p string) (string, error) {
						return filepath.Join(p, "Contents", "Resources", "app.asar"), nil
					},
					DisableFuses:  true,
					NeedsCodesign: true,
				})
			}
		}
		return targets
	}

	var targets []app.App
	for _, a := range all {
		if cfg.MatchesApp(a.ID) {
			appCopy := a
			if forcedDriver != "" {
				appCopy.Driver = app.DriverType(forcedDriver)
			}
			targets = append(targets, appCopy)
		}
	}
	return targets
}

func newPatchCommand(global *GlobalFlags) *cobra.Command {
	flags := &PatchFlags{}
	cmd := &cobra.Command{
		Use:   "patch [apps...]",
		Short: "Patch applications to use the configured font",
		Long: `patch modifies applications to use your chosen font.
Supports built-in apps (e.g. 'better-fonts patch slack rekordbox telegram') or any
arbitrary macOS application path (e.g. 'better-fonts patch /Applications/SomeApp.app').`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadAppConfig(global)
			if err != nil {
				return err
			}

			if flags.Font != "" {
				cfg.Font = flags.Font
			}
			if flags.NoRestart {
				cfg.Restart = false
			} else if flags.Restart {
				cfg.Restart = true
			}

			allApps := app.GetAllApps(cfg)
			targets := selectTargetApps(allApps, cfg, args, flags.Driver)

			if len(targets) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No matching applications found to patch.")
				return nil
			}

			for _, target := range targets {
				st := target.Status()
				if !st.Installed && !flags.DryRun {
					fmt.Fprintf(cmd.OutOrStdout(), "%s (%s) is not installed.\n", target.Name, target.AppPath)
					continue
				}

				font := cfg.EffectiveFont(target.ID)
				restart := cfg.EffectiveRestart(target.ID)

				if target.Driver == app.DriverNativeHook {
					fmt.Fprintf(cmd.OutOrStdout(), "⚠️  Patching %s [%s] (%s) with font %q (Native CoreText hook)...\n", target.Name, target.Driver, target.AppPath, font)
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "Patching %s [%s] (%s) with font %q...\n", target.Name, target.Driver, target.AppPath, font)
				}

				opts := app.PatchOptions{
					Font:    font,
					Restart: restart,
					DryRun:  flags.DryRun,
					Runner:  sysutil.DefaultRunner,
				}

				if err := target.Patch(opts); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "Error patching %s: %v\n", target.Name, err)
					continue
				}

				fmt.Fprintf(cmd.OutOrStdout(), "Successfully patched %s!\n", target.Name)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&flags.Font, "font", "f", "", "override font name")
	cmd.Flags().StringVar(&flags.Driver, "driver", "", "override driver ('electron' or 'native-hook')")
	cmd.Flags().BoolVar(&flags.Restart, "restart", true, "restart application after patching")
	cmd.Flags().BoolVar(&flags.NoRestart, "no-restart", false, "do not restart application after patching")
	cmd.Flags().BoolVar(&flags.DryRun, "dry-run", false, "simulate actions without modifying application files")

	return cmd
}

func newUnpatchCommand(global *GlobalFlags) *cobra.Command {
	flags := &UnpatchFlags{}
	cmd := &cobra.Command{
		Use:   "unpatch [apps...]",
		Short: "Remove font patch from applications",
		Long: `unpatch restores original preload scripts or executables for applications.
Supports built-in apps or any arbitrary macOS application path.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadAppConfig(global)
			if err != nil {
				return err
			}

			if flags.NoRestart {
				cfg.Restart = false
			} else if flags.Restart {
				cfg.Restart = true
			}

			allApps := app.GetAllApps(cfg)
			targets := selectTargetApps(allApps, cfg, args, flags.Driver)

			if len(targets) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No matching applications found to unpatch.")
				return nil
			}

			for _, target := range targets {
				st := target.Status()
				if !st.Installed && !flags.DryRun {
					fmt.Fprintf(cmd.OutOrStdout(), "%s (%s) is not installed.\n", target.Name, target.AppPath)
					continue
				}
				if !st.Patched && !flags.DryRun {
					fmt.Fprintf(cmd.OutOrStdout(), "%s (%s) is already in its clean unpatched state.\n", target.Name, target.AppPath)
					continue
				}

				restart := cfg.EffectiveRestart(target.ID)

				fmt.Fprintf(cmd.OutOrStdout(), "Unpatching %s [%s] (%s)...\n", target.Name, target.Driver, target.AppPath)

				opts := app.PatchOptions{
					Restart: restart,
					DryRun:  flags.DryRun,
					Runner:  sysutil.DefaultRunner,
				}

				if err := target.Unpatch(opts); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "Error unpatching %s: %v\n", target.Name, err)
					continue
				}

				fmt.Fprintf(cmd.OutOrStdout(), "Successfully unpatched %s!\n", target.Name)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&flags.Driver, "driver", "", "override driver ('electron' or 'native-hook')")
	cmd.Flags().BoolVar(&flags.Restart, "restart", true, "restart application after unpatching")
	cmd.Flags().BoolVar(&flags.NoRestart, "no-restart", false, "do not restart application after unpatching")
	cmd.Flags().BoolVar(&flags.DryRun, "dry-run", false, "simulate actions without modifying application files")

	return cmd
}

func newStatusCommand(flags *GlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show installation and patch status of supported applications",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadAppConfig(flags)
			if err != nil {
				return err
			}

			allApps := app.GetAllApps(cfg)
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
			fmt.Fprintln(w, "APP\tDRIVER\tINSTALLED\tPATCHED\tCURRENT FONT\tPATH")
			fmt.Fprintln(w, "---\t------\t---------\t-------\t------------\t----")

			for _, a := range allApps {
				st := a.Status()
				installedStr := "No"
				if st.Installed {
					installedStr = "Yes"
				}

				patchedStr := "No"
				fontStr := "-"
				if st.Patched {
					patchedStr = "Yes"
					fontStr = st.CurrentFont
				}

				if st.Error != nil {
					patchedStr = fmt.Sprintf("Error: %v", st.Error)
				}

				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", a.Name, a.Driver, installedStr, patchedStr, fontStr, a.AppPath)
			}

			return w.Flush()
		},
	}
	return cmd
}

func newListCommand(flags *GlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all supported applications",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadAppConfig(flags)
			if err != nil {
				return err
			}

			allApps := app.GetAllApps(cfg)
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
			fmt.Fprintln(w, "ID\tNAME\tDRIVER\tDEFAULT PATH")
			fmt.Fprintln(w, "--\t----\t------\t------------")

			for _, a := range allApps {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", a.ID, a.Name, a.Driver, a.AppPath)
			}

			return w.Flush()
		},
	}
	return cmd
}

func newConfigCommand(flags *GlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage better-fonts configuration",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Print the path to config.toml",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := flags.ConfigFile
			if path == "" {
				var err error
				path, err = config.GetConfigFilePath()
				if err != nil {
					return err
				}
			}
			fmt.Fprintln(cmd.OutOrStdout(), path)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Display the current configuration file contents",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := flags.ConfigFile
			if path == "" {
				var err error
				path, err = config.GetConfigFilePath()
				if err != nil {
					return err
				}
			}

			cfg, err := config.EnsureConfigFile(path)
			if err != nil {
				return err
			}

			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("reading %s: %w", path, err)
			}

			_ = cfg
			_, err = io.WriteString(cmd.OutOrStdout(), string(data))
			return err
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "init",
		Short: "Create a default configuration file if not already present",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := flags.ConfigFile
			if path == "" {
				var err error
				path, err = config.GetConfigFilePath()
				if err != nil {
					return err
				}
			}

			if _, err := os.Stat(path); err == nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Config file already exists at %s\n", path)
				return nil
			}

			cfg := config.NewDefaultConfig()
			if err := config.SaveConfig(path, cfg); err != nil {
				return fmt.Errorf("saving default config to %s: %w", path, err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Created default config file at %s\n", path)
			return nil
		},
	})

	return cmd
}

func newUpgradeCommand(version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Check for and install the latest release of better-fonts",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "Checking for latest release...")
			updated, latestVer, err := selfupdate.UpgradeSelf(cmd.Context(), version)
			if err != nil {
				return fmt.Errorf("upgrading better-fonts: %w", err)
			}

			if updated {
				fmt.Fprintf(cmd.OutOrStdout(), "Successfully upgraded better-fonts to v%s!\n", latestVer)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "better-fonts is already up to date (v%s).\n", latestVer)
			}
			return nil
		},
	}
	return cmd
}
