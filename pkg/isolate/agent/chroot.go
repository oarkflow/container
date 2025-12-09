package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
)

// ChrootExecutor wraps command execution with chroot isolation on supported platforms
type ChrootExecutor struct {
	rootDir string
}

// NewChrootExecutor creates a new chroot executor
func NewChrootExecutor(rootDir string) (*ChrootExecutor, error) {
	if rootDir == "" {
		return nil, fmt.Errorf("root directory is required")
	}

	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("invalid root directory: %w", err)
	}

	// Verify root directory exists
	if _, err := os.Stat(absRoot); err != nil {
		return nil, fmt.Errorf("root directory does not exist: %w", err)
	}

	return &ChrootExecutor{rootDir: absRoot}, nil
}

// PrepareCommand prepares a command for chroot execution
func (ce *ChrootExecutor) PrepareCommand(cmd *exec.Cmd, workDir string) error {
	if runtime.GOOS == "windows" {
		// Windows doesn't support chroot - use path validation only
		return fmt.Errorf("chroot isolation not supported on Windows - consider using WSL2 or containers")
	}

	// Set up chroot on Unix systems
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}

	// Set chroot directory
	cmd.SysProcAttr.Chroot = ce.rootDir

	// Adjust working directory to be relative to chroot
	if workDir != "" {
		relPath, err := filepath.Rel(ce.rootDir, workDir)
		if err != nil {
			cmd.Dir = "/"
		} else {
			cmd.Dir = "/" + relPath
		}
	} else {
		cmd.Dir = "/"
	}

	// Set up credential to run as current user (required for chroot)
	// Note: chroot typically requires root privileges or specific capabilities
	currentUID := uint32(os.Getuid())
	currentGID := uint32(os.Getgid())

	cmd.SysProcAttr.Credential = &syscall.Credential{
		Uid: currentUID,
		Gid: currentGID,
	}

	return nil
}

// IsSupported returns whether chroot is supported on the current platform
func (ce *ChrootExecutor) IsSupported() bool {
	return runtime.GOOS != "windows"
}

// RequiresRoot returns whether chroot requires root privileges
func (ce *ChrootExecutor) RequiresRoot() bool {
	// On most Unix systems, chroot requires root or CAP_SYS_CHROOT capability
	return os.Getuid() != 0
}
