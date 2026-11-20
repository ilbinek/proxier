// Package install sets proxier up as a systemd service on Linux.
package install

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	binPath  = "/usr/local/bin/proxier"
	unitPath = "/etc/systemd/system/proxier.service"
	envDir   = "/etc/proxier"
	envPath  = "/etc/proxier/proxier.env"
	dataDir  = "/var/lib/proxier"
	svcUser  = "proxier"
)

// Unit is the systemd unit file. It's also shipped as deploy/proxier.service.
const Unit = `[Unit]
Description=proxier - TCP/UDP port forwarder with web UI
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=proxier
Group=proxier
EnvironmentFile=-/etc/proxier/proxier.env
ExecStart=/usr/local/bin/proxier serve
Restart=on-failure
RestartSec=3
LimitNOFILE=65535

# Allow binding ports < 1024 without running as root.
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE

# Hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
PrivateDevices=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictSUIDSGID=true
LockPersonality=true
RestrictNamespaces=true
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX AF_NETLINK
ReadWritePaths=/var/lib/proxier

[Install]
WantedBy=multi-user.target
`

func envFile(listen, tlsMode, domain, email string) string {
	return fmt.Sprintf(`# proxier configuration (see: proxier serve -h)
PROXIER_DATA_DIR=%s
PROXIER_LISTEN=%s
# off | self-signed | acme | files
PROXIER_TLS=%s
PROXIER_ACME_DOMAIN=%s
PROXIER_ACME_EMAIL=%s
#PROXIER_TLS_CERT=/etc/proxier/cert.pem
#PROXIER_TLS_KEY=/etc/proxier/key.pem
# Set to true only when running behind your own reverse proxy:
PROXIER_TRUST_PROXY=false
PROXIER_LOG_LEVEL=info
`, dataDir, listen, tlsMode, domain, email)
}

func preflight() error {
	if runtime.GOOS != "linux" {
		return errors.New("install is only supported on Linux with systemd")
	}
	if os.Geteuid() != 0 {
		return errors.New("install must run as root (try: sudo proxier install)")
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return errors.New("systemctl not found; is this a systemd system?")
	}
	return nil
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

// Install runs `proxier install [flags]`.
func Install(args []string) error {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	listen := fs.String("listen", ":8080", "web UI address written to the env file")
	tlsMode := fs.String("tls", "self-signed", "TLS mode written to the env file")
	domain := fs.String("acme-domain", "", "domain for --tls=acme")
	email := fs.String("acme-email", "", "email for --tls=acme")
	noStart := fs.Bool("no-start", false, "install but don't enable/start the service")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := preflight(); err != nil {
		return err
	}

	if _, err := user.Lookup(svcUser); err != nil {
		fmt.Println("• creating system user", svcUser)
		if err := run("useradd", "--system", "--no-create-home", "--home-dir", dataDir, "--shell", "/usr/sbin/nologin", svcUser); err != nil {
			return err
		}
	}
	fmt.Println("• data directory", dataDir)
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return err
	}
	if err := run("chown", svcUser+":"+svcUser, dataDir); err != nil {
		return err
	}

	self, err := os.Executable()
	if err != nil {
		return err
	}
	self, _ = filepath.EvalSymlinks(self)
	if self != binPath {
		fmt.Println("• installing binary to", binPath)
		if err := copyFile(self, binPath, 0o755); err != nil {
			return err
		}
	}

	if err := os.MkdirAll(envDir, 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(envPath); errors.Is(err, os.ErrNotExist) {
		fmt.Println("• writing", envPath)
		if err := os.WriteFile(envPath, []byte(envFile(*listen, *tlsMode, *domain, *email)), 0o640); err != nil {
			return err
		}
		_ = run("chgrp", svcUser, envPath)
	} else {
		fmt.Println("• keeping existing", envPath)
	}

	fmt.Println("• writing", unitPath)
	if err := os.WriteFile(unitPath, []byte(Unit), 0o644); err != nil {
		return err
	}
	if err := run("systemctl", "daemon-reload"); err != nil {
		return err
	}
	if *noStart {
		fmt.Println("\nInstalled. Start with: systemctl enable --now proxier")
		return nil
	}
	if err := run("systemctl", "enable", "--now", "proxier"); err != nil {
		return err
	}
	_ = run("systemctl", "restart", "proxier")
	fmt.Print(`
proxier is running.
  • Status:       systemctl status proxier
  • Logs:         journalctl -u proxier -f
  • Setup token:  cat /var/lib/proxier/setup-token.txt
  • Config:       /etc/proxier/proxier.env  (then: systemctl restart proxier)

Open the web UI and finish setup with the setup token.
Remember to open the UI port and your forwarded ports in the firewall (ufw/nftables/cloud panel).
`)
	return nil
}

// Uninstall runs `proxier uninstall [--purge]`.
func Uninstall(args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	purge := fs.Bool("purge", false, "also delete /var/lib/proxier, /etc/proxier and the proxier user")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := preflight(); err != nil {
		return err
	}
	_ = run("systemctl", "disable", "--now", "proxier")
	_ = os.Remove(unitPath)
	_ = run("systemctl", "daemon-reload")
	_ = os.Remove(binPath)
	if *purge {
		_ = os.RemoveAll(dataDir)
		_ = os.RemoveAll(envDir)
		_ = run("userdel", svcUser)
		fmt.Println("proxier removed, including data.")
	} else {
		fmt.Println("proxier removed. Data kept in", dataDir, "and config in", envDir, "(use --purge to delete).")
	}
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}
