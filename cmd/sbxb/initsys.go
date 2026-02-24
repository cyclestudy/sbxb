package sbxb

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// initKind represents the detected init system.
type initKind int

const (
	initSystemd initKind = iota
	initProcd          // OpenWrt procd
	initSysVinit       // traditional SysVinit / update-rc.d / chkconfig
	initUnknown
)

const (
	initdScript  = "/etc/init.d/" + serviceName
	pidFile      = "/var/run/" + serviceName + ".pid"
	sysvinitLog  = "/var/log/" + serviceName + ".log"
)

// detectInit returns the init system in use.
func detectInit() initKind {
	if _, err := exec.LookPath("systemctl"); err == nil {
		// Verify systemd is actually running (PID 1).
		if target, err := os.Readlink("/proc/1/exe"); err == nil {
			if strings.Contains(target, "systemd") {
				return initSystemd
			}
		}
		// Fallback: if systemctl exists, assume systemd.
		return initSystemd
	}
	if _, err := os.Stat("/etc/openwrt_release"); err == nil {
		return initProcd
	}
	// Check if procd is PID 1.
	if target, err := os.Readlink("/proc/1/exe"); err == nil {
		if strings.Contains(target, "procd") {
			return initProcd
		}
	}
	if _, err := os.Stat("/etc/init.d"); err == nil {
		return initSysVinit
	}
	return initUnknown
}

func (k initKind) String() string {
	switch k {
	case initSystemd:
		return "systemd"
	case initProcd:
		return "OpenWrt procd"
	case initSysVinit:
		return "SysVinit"
	default:
		return "unknown"
	}
}

// ---------------------------------------------------------------------------
// Service control dispatcher
// ---------------------------------------------------------------------------

func serviceAction(action string) {
	switch detectInit() {
	case initSystemd:
		runSystemctl(action)
	case initProcd:
		runInitd(action)
	case initSysVinit:
		runInitd(action)
	default:
		fmt.Println("No supported init system found (systemd/procd/SysVinit).")
		fmt.Println("You can run sbxb manually: sbxb server -c " + configFile)
	}
}

func serviceEnable() {
	switch detectInit() {
	case initSystemd:
		runSystemctl("enable")
	case initProcd:
		runInitd("enable")
	case initSysVinit:
		sysvinitEnable(true)
	default:
		fmt.Println("No supported init system found.")
	}
}

func serviceDisable() {
	switch detectInit() {
	case initSystemd:
		runSystemctl("disable")
	case initProcd:
		runInitd("disable")
	case initSysVinit:
		sysvinitEnable(false)
	default:
		fmt.Println("No supported init system found.")
	}
}

func serviceInstall() {
	kind := detectInit()
	switch kind {
	case initSystemd:
		installSystemd()
	case initProcd:
		installProcd()
	case initSysVinit:
		installSysVinit()
	default:
		fmt.Println("No supported init system found.")
		fmt.Println("Supported: systemd, OpenWrt procd, SysVinit")
		return
	}
	fmt.Printf("Service installed (%s). Use 'sbxb start' to start.\n", kind)
}

func serviceUninstall() {
	switch detectInit() {
	case initSystemd:
		uninstallSystemd()
	case initProcd, initSysVinit:
		uninstallInitd()
	default:
		// Just remove binary.
		os.Remove("/usr/local/bin/sbxb")
		os.RemoveAll("/usr/local/sbxb")
		fmt.Println("sbxb removed. Config preserved at " + configFile)
	}
}

// ---------------------------------------------------------------------------
// Status / enabled queries
// ---------------------------------------------------------------------------

func getServiceStatus() string {
	switch detectInit() {
	case initSystemd:
		out, err := exec.Command("systemctl", "is-active", serviceName+".service").Output()
		if err != nil {
			return "not installed"
		}
		return strings.TrimSpace(string(out))
	case initProcd, initSysVinit:
		if isRunningByPid() {
			return "active"
		}
		if _, err := os.Stat(initdScript); err == nil {
			return "inactive"
		}
		return "not installed"
	default:
		return "unknown"
	}
}

func isEnabled() bool {
	switch detectInit() {
	case initSystemd:
		out, err := exec.Command("systemctl", "is-enabled", serviceName+".service").Output()
		if err != nil {
			return false
		}
		return strings.TrimSpace(string(out)) == "enabled"
	case initProcd:
		// OpenWrt: enabled if symlink exists in /etc/rc.d/
		matches, _ := filepath.Glob("/etc/rc.d/S*" + serviceName)
		return len(matches) > 0
	case initSysVinit:
		matches, _ := filepath.Glob("/etc/rc2.d/S*" + serviceName)
		return len(matches) > 0
	default:
		return false
	}
}

func isRunningByPid() bool {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return false
	}
	pid := strings.TrimSpace(string(data))
	if pid == "" {
		return false
	}
	_, err = os.Stat("/proc/" + pid)
	return err == nil
}

// ---------------------------------------------------------------------------
// systemd
// ---------------------------------------------------------------------------

func runSystemctl(action string) {
	c := exec.Command("systemctl", action, serviceName+".service")
	if action == "status" {
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

func installSystemd() {
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
}

func uninstallSystemd() {
	exec.Command("systemctl", "stop", serviceName+".service").Run()
	exec.Command("systemctl", "disable", serviceName+".service").Run()
	os.Remove("/etc/systemd/system/" + serviceName + ".service")
	exec.Command("systemctl", "daemon-reload").Run()
	os.Remove("/usr/local/bin/sbxb")
	os.RemoveAll("/usr/local/sbxb")
	fmt.Println("sbxb service removed. Config preserved at " + configFile)
}

// ---------------------------------------------------------------------------
// OpenWrt procd
// ---------------------------------------------------------------------------

func installProcd() {
	binPath, err := os.Executable()
	if err != nil {
		fmt.Printf("Failed to get executable path: %v\n", err)
		return
	}

	script := fmt.Sprintf(`#!/bin/sh /etc/rc.common

START=99
STOP=10
USE_PROCD=1

start_service() {
    procd_open_instance
    procd_set_param command %s server -c %s
    procd_set_param respawn
    procd_set_param stdout 1
    procd_set_param stderr 1
    procd_set_param pidfile %s
    procd_set_param limits nofile="1048576 1048576"
    procd_close_instance
}
`, binPath, configFile, pidFile)

	if err := os.WriteFile(initdScript, []byte(script), 0755); err != nil {
		fmt.Printf("Failed to write init script: %v\n", err)
		return
	}
}

// ---------------------------------------------------------------------------
// SysVinit
// ---------------------------------------------------------------------------

func installSysVinit() {
	binPath, err := os.Executable()
	if err != nil {
		fmt.Printf("Failed to get executable path: %v\n", err)
		return
	}

	script := fmt.Sprintf(`#!/bin/sh
### BEGIN INIT INFO
# Provides:          %s
# Required-Start:    $network $remote_fs
# Required-Stop:     $network $remote_fs
# Default-Start:     2 3 4 5
# Default-Stop:      0 1 6
# Short-Description: sbxb - sing-box node backend for XBoard
### END INIT INFO

DAEMON="%s"
PIDFILE="%s"
CONFIG="%s"
LOGFILE="%s"

do_start() {
    if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
        echo "sbxb is already running"
        return 1
    fi
    echo "Starting sbxb..."
    ulimit -n 1048576 2>/dev/null
    nohup "$DAEMON" server -c "$CONFIG" >> "$LOGFILE" 2>&1 &
    echo $! > "$PIDFILE"
    echo "sbxb started (PID: $!)"
}

do_stop() {
    if [ ! -f "$PIDFILE" ]; then
        echo "sbxb is not running"
        return 1
    fi
    PID=$(cat "$PIDFILE")
    if kill -0 "$PID" 2>/dev/null; then
        echo "Stopping sbxb (PID: $PID)..."
        kill "$PID"
        rm -f "$PIDFILE"
        echo "sbxb stopped"
    else
        echo "sbxb is not running (stale PID file)"
        rm -f "$PIDFILE"
    fi
}

case "$1" in
    start)   do_start ;;
    stop)    do_stop ;;
    restart)
        do_stop
        sleep 1
        do_start
        ;;
    status)
        if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
            echo "sbxb is running (PID: $(cat "$PIDFILE"))"
        else
            echo "sbxb is not running"
        fi
        ;;
    *)
        echo "Usage: $0 {start|stop|restart|status}"
        exit 1
        ;;
esac
`, serviceName, binPath, pidFile, configFile, sysvinitLog)

	if err := os.WriteFile(initdScript, []byte(script), 0755); err != nil {
		fmt.Printf("Failed to write init script: %v\n", err)
		return
	}
}

// ---------------------------------------------------------------------------
// Common /etc/init.d/ operations (procd + SysVinit)
// ---------------------------------------------------------------------------

func runInitd(action string) {
	if _, err := os.Stat(initdScript); err != nil {
		fmt.Printf("Init script not found: %s\n", initdScript)
		fmt.Println("Run 'sbxb install' first.")
		return
	}

	c := exec.Command(initdScript, action)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil && action != "status" {
		fmt.Printf("Failed to %s service: %v\n", action, err)
	}
}

func sysvinitEnable(enable bool) {
	if _, err := os.Stat(initdScript); err != nil {
		fmt.Printf("Init script not found: %s\n", initdScript)
		return
	}
	if _, err := exec.LookPath("update-rc.d"); err == nil {
		if enable {
			exec.Command("update-rc.d", serviceName, "defaults").Run()
			fmt.Println("sbxb auto-start enabled.")
		} else {
			exec.Command("update-rc.d", "-f", serviceName, "remove").Run()
			fmt.Println("sbxb auto-start disabled.")
		}
		return
	}
	if _, err := exec.LookPath("chkconfig"); err == nil {
		if enable {
			exec.Command("chkconfig", serviceName, "on").Run()
			fmt.Println("sbxb auto-start enabled.")
		} else {
			exec.Command("chkconfig", serviceName, "off").Run()
			fmt.Println("sbxb auto-start disabled.")
		}
		return
	}
	fmt.Println("Neither update-rc.d nor chkconfig found. Enable manually.")
}

func uninstallInitd() {
	runInitd("stop")
	if detectInit() == initProcd {
		runInitd("disable")
	} else {
		sysvinitEnable(false)
	}
	os.Remove(initdScript)
	os.Remove(pidFile)
	os.Remove("/usr/local/bin/sbxb")
	os.RemoveAll("/usr/local/sbxb")
	fmt.Println("sbxb service removed. Config preserved at " + configFile)
}
