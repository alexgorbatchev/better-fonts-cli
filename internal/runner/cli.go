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
	"github.com/spf13/cobra"
)

type GlobalFlags struct {
	ConfigFile string
	Font       string
	Apps       []string
	Driver     string
	Restart    bool
	NoRestart  bool
	DryRun     bool
	Verbose    bool
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
	pflags.StringVarP(&flags.Font, "font", "f", "", "override font name")
	pflags.StringSliceVarP(&flags.Apps, "app", "a", nil, "target specific app(s)")
	pflags.StringVar(&flags.Driver, "driver", "", "override driver ('electron' or 'native-hook')")
	pflags.BoolVar(&flags.Restart, "restart", true, "restart application after patching/unpatching")
	pflags.BoolVar(&flags.NoRestart, "no-restart", false, "do not restart application after patching/unpatching")
	pflags.BoolVar(&flags.DryRun, "dry-run", false, "simulate actions without modifying application files")
	pflags.BoolVarP(&flags.Verbose, "verbose", "v", false, "enable verbose output")

	rootCmd.AddCommand(newPatchCommand(flags))
	rootCmd.AddCommand(newUnpatchCommand(flags))
	rootCmd.AddCommand(newStatusCommand(flags))
	rootCmd.AddCommand(newListCommand(flags))
	rootCmd.AddCommand(newConfigCommand(flags))
	rootCmd.AddCommand(newUpgradeCommand(version))

	return rootCmd
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

	if flags.Font != "" {
		cfg.Font = flags.Font
	}
	if len(flags.Apps) > 0 {
		cfg.Apps = flags.Apps
	}
	if flags.NoRestart {
		cfg.Restart = false
	} else if flags.Restart {
		cfg.Restart = true
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

func newPatchCommand(flags *GlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "patch [apps...]",
		Short: "Patch applications to use the configured font",
		Long: `patch modifies applications to use your chosen font.
Supports built-in apps (e.g. 'better-fonts patch slack rekordbox telegram') or any
arbitrary macOS application path (e.g. 'better-fonts patch /Applications/SomeApp.app').`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadAppConfig(flags)
			if err != nil {
				return err
			}

			allApps := app.GetAllApps(cfg)
			targets := selectTargetApps(allApps, cfg, args, flags.Driver)

			if len(targets) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No matching applications found to patch.")
				return nil
			}

			for _, target := range targets {
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
	return cmd
}

func newUnpatchCommand(flags *GlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unpatch [apps...]",
		Short: "Remove font patch from applications",
		Long: `unpatch restores original preload scripts or executables for applications.
Supports built-in apps or any arbitrary macOS application path.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadAppConfig(flags)
			if err != nil {
				return err
			}

			allApps := app.GetAllApps(cfg)
			targets := selectTargetApps(allApps, cfg, args, flags.Driver)

			if len(targets) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No matching applications found to unpatch.")
				return nil
			}

			for _, target := range targets {
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
