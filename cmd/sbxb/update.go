package sbxb

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(updateCmd)
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update sbxb to the latest version",
	Run: func(cmd *cobra.Command, args []string) {
		doUpdate()
	},
}

func doUpdate() {
	const repo = "cyclestudy/sbxb"

	// 1. Get latest release tag.
	fmt.Println("Checking for updates...")
	resp, err := http.Get("https://api.github.com/repos/" + repo + "/releases/latest")
	if err != nil {
		fmt.Printf("Failed to check updates: %v\n", err)
		return
	}
	defer resp.Body.Close()

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		fmt.Printf("Failed to parse release info: %v\n", err)
		return
	}

	if release.TagName == "" {
		fmt.Println("No release found.")
		return
	}

	if release.TagName == version {
		fmt.Printf("Already up to date: %s\n", version)
		return
	}

	fmt.Printf("Current: %s -> Latest: %s\n", version, release.TagName)

	// 2. Build download URL.
	name := "sbxb-" + getPlatformName()
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	url := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", repo, release.TagName, name)

	fmt.Printf("Downloading %s...\n", url)

	// 3. Download to temp file.
	dlResp, err := http.Get(url)
	if err != nil {
		fmt.Printf("Download failed: %v\n", err)
		return
	}
	defer dlResp.Body.Close()

	if dlResp.StatusCode != 200 {
		fmt.Printf("Download failed: HTTP %d\n", dlResp.StatusCode)
		return
	}

	tmpFile, err := os.CreateTemp("", "sbxb-update-*")
	if err != nil {
		fmt.Printf("Failed to create temp file: %v\n", err)
		return
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	written, err := io.Copy(tmpFile, dlResp.Body)
	tmpFile.Close()
	if err != nil {
		fmt.Printf("Download failed: %v\n", err)
		return
	}
	fmt.Printf("Downloaded %.1f MB\n", float64(written)/1024/1024)

	// 4. Replace binary.
	binPath, err := os.Executable()
	if err != nil {
		fmt.Printf("Failed to locate current binary: %v\n", err)
		return
	}
	binPath, _ = filepath.EvalSymlinks(binPath)

	os.Chmod(tmpPath, 0755)

	if err := os.Rename(tmpPath, binPath); err != nil {
		// Cross-device rename; fallback to copy.
		if err := copyFile(tmpPath, binPath); err != nil {
			fmt.Printf("Failed to replace binary: %v\n", err)
			return
		}
	}

	fmt.Printf("Updated to %s. Run 'sbxb restart' to apply.\n", release.TagName)
}

// getPlatformName returns the platform name matching CI release artifacts.
// If platformName was injected via ldflags at build time, use it directly.
// Otherwise, construct from runtime info (won't distinguish ARM sub-versions).
func getPlatformName() string {
	if platformName != "" {
		return platformName
	}
	os := runtime.GOOS
	if os == "darwin" {
		os = "macos"
	}
	arch := runtime.GOARCH
	switch arch {
	case "amd64":
		arch = "64"
	case "386":
		arch = "32"
	case "arm64":
		arch = "arm64-v8a"
	case "arm":
		arch = "arm32-v7a"
	case "mips":
		arch = "mips32"
	case "mipsle":
		arch = "mips32le"
	}
	return os + "-" + arch
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
