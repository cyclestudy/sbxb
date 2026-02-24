package sbxb

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strings"
)

func showMenu() {
	green := "\033[32m"
	yellow := "\033[33m"
	red := "\033[31m"
	cyan := "\033[36m"
	reset := "\033[0m"
	bold := "\033[1m"

	reader := bufio.NewReader(os.Stdin)

	for {
		// Get current status.
		status := getServiceStatus()
		enabled := isEnabled()

		statusText := red + "not running" + reset
		switch status {
		case "active":
			statusText = green + "running" + reset
		case "not installed":
			statusText = yellow + "not installed" + reset
		}

		enabledText := yellow + "disabled" + reset
		if enabled {
			enabledText = green + "enabled" + reset
		}

		fmt.Printf("\n%s%s  sbxb Management  %s\n", bold, cyan, reset)
		fmt.Printf("  Version: %s (%s)  Go %s %s/%s\n", version, commit, runtime.Version(), runtime.GOOS, runtime.GOARCH)
		fmt.Printf("  Status: %s  Auto-start: %s\n", statusText, enabledText)
		fmt.Printf("%s—————————————————————————————%s\n", cyan, reset)
		fmt.Println("  0. Edit config")
		fmt.Println("  1. Start sbxb")
		fmt.Println("  2. Stop sbxb")
		fmt.Println("  3. Restart sbxb")
		fmt.Println("  4. View status")
		fmt.Println("  5. View logs")
		fmt.Printf("%s—————————————————————————————%s\n", cyan, reset)
		fmt.Println("  6. Enable auto-start")
		fmt.Println("  7. Disable auto-start")
		fmt.Printf("%s—————————————————————————————%s\n", cyan, reset)
		fmt.Println("  8. Update sbxb")
		fmt.Println("  9. Install service")
		fmt.Println(" 10. Uninstall sbxb")
		fmt.Println(" 11. Show version")
		fmt.Printf("%s—————————————————————————————%s\n", cyan, reset)
		fmt.Println(" 12. Exit")
		fmt.Println()
		fmt.Print("Select [0-12]: ")

		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		fmt.Println()

		switch input {
		case "0":
			configCmd.Run(nil, nil)
		case "1":
			serviceAction("start")
		case "2":
			serviceAction("stop")
		case "3":
			serviceAction("restart")
		case "4":
			serviceAction("status")
		case "5":
			logCmd.Run(nil, nil)
		case "6":
			serviceEnable()
		case "7":
			serviceDisable()
		case "8":
			doUpdate()
		case "9":
			serviceInstall()
		case "10":
			serviceUninstall()
			return
		case "11":
			fmt.Printf("sbxb %s (%s)\n", version, commit)
			fmt.Printf("Go %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
		case "12", "q", "quit", "exit":
			return
		default:
			fmt.Println("Invalid option.")
		}

		if input != "5" && input != "12" && input != "q" {
			fmt.Print("\nPress Enter to continue...")
			reader.ReadString('\n')
		}
	}
}
