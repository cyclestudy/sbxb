package sbxb

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
)

const (
	serviceName = "sbxb"
	configFile  = "/etc/sbxb/config.json"
)

func init() {
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(restartCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(enableCmd)
	rootCmd.AddCommand(disableCmd)
	rootCmd.AddCommand(logCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(installServiceCmd)
	rootCmd.AddCommand(uninstallCmd)
}

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start sbxb service",
	Run: func(cmd *cobra.Command, args []string) {
		runSystemctl("start")
	},
}

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop sbxb service",
	Run: func(cmd *cobra.Command, args []string) {
		runSystemctl("stop")
	},
}

var restartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart sbxb service",
	Run: func(cmd *cobra.Command, args []string) {
		runSystemctl("restart")
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show sbxb service status",
	Run: func(cmd *cobra.Command, args []string) {
		runSystemctl("status")
	},
}

var enableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable sbxb auto-start on boot",
	Run: func(cmd *cobra.Command, args []string) {
		runSystemctl("enable")
	},
}

var disableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable sbxb auto-start on boot",
	Run: func(cmd *cobra.Command, args []string) {
		runSystemctl("disable")
	},
}

var logCmd = &cobra.Command{
	Use:   "log",
	Short: "View sbxb service logs",
	Run: func(cmd *cobra.Command, args []string) {
		c := exec.Command("journalctl", "-u", serviceName+".service", "-e", "--no-pager", "-f")
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		// Ensure journalctl is killed when the parent process dies (e.g. SSH disconnect).
		c.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}

		if err := c.Start(); err != nil {
			fmt.Printf("Failed to start journalctl: %v\n", err)
			return
		}

		// Forward SIGINT/SIGTERM to journalctl so Ctrl+C cleanly stops both.
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigCh
			_ = c.Process.Signal(syscall.SIGTERM)
		}()

		_ = c.Wait()
		signal.Stop(sigCh)
	},
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Edit sbxb config file",
	Run: func(cmd *cobra.Command, args []string) {
		editor := os.Getenv("EDITOR")
		if editor == "" {
			for _, e := range []string{"nano", "vi", "vim"} {
				if _, err := exec.LookPath(e); err == nil {
					editor = e
					break
				}
			}
		}
		if editor == "" {
			fmt.Println("No editor found. Please set EDITOR environment variable.")
			return
		}

		c := exec.Command(editor, configFile)
		c.Stdin = os.Stdin
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		if err := c.Run(); err != nil {
			fmt.Printf("Editor exited with error: %v\n", err)
			return
		}

		fmt.Print("Restart sbxb service? [Y/n] ")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer == "" || answer == "y" || answer == "yes" {
			runSystemctl("restart")
		}
	},
}

var installServiceCmd = &cobra.Command{
	Use:   "install",
	Short: "Install sbxb systemd service",
	Run: func(cmd *cobra.Command, args []string) {
		installService()
	},
}

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Uninstall sbxb service and binary",
	Run: func(cmd *cobra.Command, args []string) {
		uninstall()
	},
}

func runSystemctl(action string) {
	if _, err := exec.LookPath("systemctl"); err != nil {
		fmt.Println("systemctl not found. Only systemd is supported.")
		return
	}
	c := exec.Command("systemctl", action, serviceName+".service")
	if action == "status" {
		// Only show output for status queries.
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
	}
	if err := c.Run(); err != nil && action != "status" {
		fmt.Printf("Failed to %s service: %v\n", action, err)
		return
	}
	if action != "status" {
		fmt.Printf("sbxb service %sed.\n", action)
	}
}

func getServiceStatus() string {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return "unknown"
	}
	out, err := exec.Command("systemctl", "is-active", serviceName+".service").Output()
	if err != nil {
		return "not installed"
	}
	return strings.TrimSpace(string(out))
}

func isEnabled() bool {
	out, err := exec.Command("systemctl", "is-enabled", serviceName+".service").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "enabled"
}

func installService() {
	binPath, err := os.Executable()
	if err != nil {
		fmt.Printf("Failed to get executable path: %v\n", err)
		return
	}

	unit := fmt.Sprintf(`[Unit]
Description=sbxb - sing-box node backend for XBoard
After=network.target

[Service]
Type=simple
ExecStart=%s server -c %s
Restart=on-failure
RestartSec=5
LimitNOFILE=1048576
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
`, binPath, configFile)

	if err := os.WriteFile("/etc/systemd/system/"+serviceName+".service", []byte(unit), 0644); err != nil {
		fmt.Printf("Failed to write service file: %v\n", err)
		return
	}

	exec.Command("systemctl", "daemon-reload").Run()
	fmt.Println("Service installed. Use 'sbxb start' to start.")
}

func uninstall() {
	fmt.Print("This will stop and remove sbxb service. Continue? [y/N] ")
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer != "y" && answer != "yes" {
		fmt.Println("Cancelled.")
		return
	}

	exec.Command("systemctl", "stop", serviceName+".service").Run()
	exec.Command("systemctl", "disable", serviceName+".service").Run()
	os.Remove("/etc/systemd/system/" + serviceName + ".service")
	exec.Command("systemctl", "daemon-reload").Run()
	os.Remove("/usr/local/bin/sbxb")
	os.RemoveAll("/usr/local/sbxb")

	fmt.Println("sbxb service removed. Config preserved at " + configFile)
}
