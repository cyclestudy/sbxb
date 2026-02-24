package sbxb

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

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
	arch := runtime.GOARCH
	if arch == "arm" {
		arch = "armv7"
	}
	name := fmt.Sprintf("sbxb-%s-%s", runtime.GOOS, arch)
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}
	url := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s%s", repo, release.TagName, name, ext)

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

	tmpDir, err := os.MkdirTemp("", "sbxb-update-*")
	if err != nil {
		fmt.Printf("Failed to create temp dir: %v\n", err)
		return
	}
	defer os.RemoveAll(tmpDir)

	archivePath := filepath.Join(tmpDir, name+ext)
	f, err := os.Create(archivePath)
	if err != nil {
		fmt.Printf("Failed to create temp file: %v\n", err)
		return
	}
	written, err := io.Copy(f, dlResp.Body)
	f.Close()
	if err != nil {
		fmt.Printf("Download failed: %v\n", err)
		return
	}
	fmt.Printf("Downloaded %.1f MB\n", float64(written)/1024/1024)

	// 4. Extract.
	if strings.HasSuffix(ext, ".tar.gz") {
		if err := extractTarGz(archivePath, tmpDir); err != nil {
			fmt.Printf("Extract failed: %v\n", err)
			return
		}
	} else {
		fmt.Println("ZIP extraction not supported on this platform. Please update manually.")
		return
	}

	// 5. Replace binary.
	binPath, err := os.Executable()
	if err != nil {
		fmt.Printf("Failed to locate current binary: %v\n", err)
		return
	}
	binPath, _ = filepath.EvalSymlinks(binPath)

	newBin := filepath.Join(tmpDir, name)
	if _, err := os.Stat(newBin); err != nil {
		// Try without arch suffix.
		newBin = filepath.Join(tmpDir, "sbxb")
		if _, err := os.Stat(newBin); err != nil {
			fmt.Println("Binary not found in archive.")
			return
		}
	}

	if err := os.Rename(newBin, binPath); err != nil {
		// Cross-device rename; fallback to copy.
		if err := copyFile(newBin, binPath); err != nil {
			fmt.Printf("Failed to replace binary: %v\n", err)
			return
		}
	}
	os.Chmod(binPath, 0755)

	fmt.Printf("Updated to %s. Run 'sbxb restart' to apply.\n", release.TagName)
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
