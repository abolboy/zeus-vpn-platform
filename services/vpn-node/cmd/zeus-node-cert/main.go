package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/abolboy/zeus-vpn-platform/services/vpn-node/internal/nodecert"
)

const (
	defaultCAValidity   = 10 * 365 * 24 * time.Hour
	defaultNodeValidity = 90 * 24 * time.Hour
	maxInputBytes       = 1 << 20
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "zeus-node-cert:", err)
		os.Exit(2)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printUsage(stderr)
		return errors.New("a command is required")
	}
	switch args[0] {
	case "init-ca":
		flags := flag.NewFlagSet("init-ca", flag.ContinueOnError)
		flags.SetOutput(stderr)
		certPath := flags.String("cert", "", "output path for the public CA certificate")
		keyPath := flags.String("key", "", "output path for the offline CA private key")
		validityText := flags.String("validity", defaultCAValidity.String(), "CA validity, 1y to 20y (Go duration, e.g. 87600h)")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || *certPath == "" || *keyPath == "" {
			return errors.New("init-ca requires --cert and --key and accepts no positional arguments")
		}
		validity, err := time.ParseDuration(*validityText)
		if err != nil {
			return fmt.Errorf("invalid CA validity: %w", err)
		}
		certPEM, keyPEM, err := nodecert.NewCA(validity)
		if err != nil {
			return err
		}
		if err := writePairExclusive(*certPath, certPEM, 0o644, *keyPath, keyPEM, 0o600); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Created ZEUS node CA certificate: %s\n", *certPath)
		fmt.Fprintf(stdout, "Created offline CA private key: %s (mode 0600; never copy to a VPN node or repository)\n", *keyPath)
		fmt.Fprintln(stdout, "Back up the CA private key only to encrypted offline storage.")
		return nil

	case "issue":
		flags := flag.NewFlagSet("issue", flag.ContinueOnError)
		flags.SetOutput(stderr)
		caCertPath := flags.String("ca-cert", "", "trusted node CA certificate path")
		caKeyPath := flags.String("ca-key", "", "offline node CA private key path")
		nodeID := flags.String("node-id", "", "existing vpn_nodes UUID")
		outputDir := flags.String("out-dir", "", "existing private output directory")
		validityText := flags.String("validity", defaultNodeValidity.String(), "node certificate validity, 1h to 365d (default 2160h)")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || *caCertPath == "" || *caKeyPath == "" || *nodeID == "" || *outputDir == "" {
			return errors.New("issue requires --ca-cert, --ca-key, --node-id, and --out-dir")
		}
		if err := validatePrivateDirectory(*outputDir); err != nil {
			return err
		}
		validity, err := time.ParseDuration(*validityText)
		if err != nil {
			return fmt.Errorf("invalid node certificate validity: %w", err)
		}
		caCertPEM, err := readRegularFile(*caCertPath, false)
		if err != nil {
			return fmt.Errorf("read CA certificate: %w", err)
		}
		caKeyPEM, err := readRegularFile(*caKeyPath, true)
		if err != nil {
			return fmt.Errorf("read CA private key: %w", err)
		}
		certPEM, keyPEM, info, err := nodecert.IssueNodeCertificate(caCertPEM, caKeyPEM, *nodeID, validity)
		if err != nil {
			return err
		}
		certPath := filepath.Join(*outputDir, "client.crt")
		keyPath := filepath.Join(*outputDir, "client.key")
		if err := writePairExclusive(certPath, certPEM, 0o644, keyPath, keyPEM, 0o600); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Issued ClientAuth certificate for node %s\n", info.NodeID)
		fmt.Fprintf(stdout, "Certificate: %s\nPrivate key: %s (mode 0600)\n", certPath, keyPath)
		fmt.Fprintf(stdout, "SHA-256 fingerprint: %s\n", info.Fingerprint)
		fmt.Fprintf(stdout, "Not before: %s\nNot after: %s\n", info.NotBefore.Format(time.RFC3339), info.NotAfter.Format(time.RFC3339))
		fmt.Fprintln(stdout, "\nRegister this certificate fingerprint only after verifying the node's inventory and address pools:")
		fmt.Fprintf(stdout, "INSERT INTO vpn_node_agent_certificates (node_id, fingerprint_sha256, not_before, not_after)\nVALUES ('%s', decode('%s', 'hex'), '%s', '%s');\n",
			info.NodeID, info.Fingerprint, info.NotBefore.Format(time.RFC3339), info.NotAfter.Format(time.RFC3339))
		fmt.Fprintln(stdout, "\nThe CA private key was not copied. Transfer only client.crt, client.key, and the control-plane server CA to the intended node.")
		return nil

	case "fingerprint":
		flags := flag.NewFlagSet("fingerprint", flag.ContinueOnError)
		flags.SetOutput(stderr)
		certPath := flags.String("cert", "", "certificate PEM path")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || *certPath == "" {
			return errors.New("fingerprint requires --cert")
		}
		certPEM, err := readRegularFile(*certPath, false)
		if err != nil {
			return err
		}
		fingerprint, err := nodecert.CertificateFingerprint(certPEM)
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, fingerprint)
		return nil

	default:
		printUsage(stderr)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func readRegularFile(path string, private bool) ([]byte, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = syscall.Close(fd)
		return nil, errors.New("could not open file")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("key and certificate inputs must be regular files")
	}
	if private && info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("CA private key must be owner-only (0600 or stricter)")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxInputBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxInputBytes {
		return nil, errors.New("certificate input exceeds 1 MiB")
	}
	return data, nil
}

func validatePrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("output path must be an existing real directory, not a symlink")
	}
	if info.Mode().Perm()&0o022 != 0 {
		return errors.New("output directory must not be group- or world-writable")
	}
	return nil
}

func writePairExclusive(firstPath string, firstData []byte, firstMode os.FileMode, secondPath string, secondData []byte, secondMode os.FileMode) error {
	firstAbs, err := filepath.Abs(firstPath)
	if err != nil {
		return err
	}
	secondAbs, err := filepath.Abs(secondPath)
	if err != nil {
		return err
	}
	if filepath.Clean(firstAbs) == filepath.Clean(secondAbs) {
		return errors.New("certificate and private key paths must be different")
	}
	if err := validatePrivateDirectory(filepath.Dir(firstAbs)); err != nil {
		return fmt.Errorf("validate certificate output directory: %w", err)
	}
	if err := validatePrivateDirectory(filepath.Dir(secondAbs)); err != nil {
		return fmt.Errorf("validate private-key output directory: %w", err)
	}
	if err := writeExclusive(firstAbs, firstData, firstMode); err != nil {
		return fmt.Errorf("create %s: %w", firstAbs, err)
	}
	if err := writeExclusive(secondAbs, secondData, secondMode); err != nil {
		_ = os.Remove(firstAbs)
		return fmt.Errorf("create %s: %w", secondAbs, err)
	}
	return nil
}

func writeExclusive(path string, data []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	removeOnFailure := true
	defer func() {
		if removeOnFailure {
			_ = os.Remove(path)
		}
	}()
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	removeOnFailure = false
	return nil
}

func printUsage(writer io.Writer) {
	fmt.Fprintln(writer, "ZEUS VPN node certificate utility")
	fmt.Fprintln(writer, "  zeus-node-cert init-ca --cert <ca.crt> --key <ca.key> [--validity 87600h]")
	fmt.Fprintln(writer, "  zeus-node-cert issue --ca-cert <ca.crt> --ca-key <ca.key> --node-id <uuid> --out-dir <private-dir> [--validity 2160h]")
	fmt.Fprintln(writer, "  zeus-node-cert fingerprint --cert <certificate.crt>")
}
