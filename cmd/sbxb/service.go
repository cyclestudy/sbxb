package sbxb

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"

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
		serviceAction("start")
	},
}

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop sbxb service",
	Run: func(cmd *cobra.Command, args []string) {
		serviceAction("stop")
	},
}

var restartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart sbxb service",
	Run: func(cmd *cobra.Command, args []string) {
		serviceAction("restart")
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show sbxb service status",
	Run: func(cmd *cobra.Command, args []string) {
		serviceAction("status")
	},
}

var enableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable sbxb auto-start on boot",
	Run: func(cmd *cobra.Command, args []string) {
		serviceEnable()
	},
}

var disableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable sbxb auto-start on boot",
	Run: func(cmd *cobra.Command, args []string) {
		serviceDisable()
	},
}

var logCmd = &cobra.Command{
	Use:   "log",
	Short: "View sbxb service logs",
	Run: func(cmd *cobra.Command, args []string) {
		viewLog()
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
			serviceAction("restart")
		}
	},
}

var installServiceCmd = &cobra.Command{
	Use:   "install",
	Short: "Install sbxb as a system service",
	Run: func(cmd *cobra.Command, args []string) {
		serviceInstall()
	},
}

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Uninstall sbxb service and binary",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Print("This will stop and remove sbxb service. Continue? [y/N] ")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("Cancelled.")
			return
		}
		serviceUninstall()
	},
}

// viewLog tails service logs using the appropriate tool for the init system.
func viewLog() {
	var c *exec.Cmd
	switch detectInit() {
	case initSystemd:
		c = exec.Command("journalctl", "-u", serviceName+".service", "-e", "--no-pager", "-f")
	case initProcd:
		// OpenWrt uses logread.
		c = exec.Command("logread", "-f", "-e", serviceName)
	case initSysVinit:
		c = exec.Command("tail", "-f", sysvinitLog)
	default:
		fmt.Println("No supported init system found.")
		return
	}

	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	setPdeathsig(c)

	if err := c.Start(); err != nil {
		fmt.Printf("Failed to view logs: %v\n", err)
		return
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	<-sigCh
	signal.Stop(sigCh)
	_ = c.Process.Kill()
	_ = c.Wait()
}
