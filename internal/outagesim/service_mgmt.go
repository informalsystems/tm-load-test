package outagesim

import "os/exec"

// IsTendermintRunning attempts to check whether there is a process called
// "tendermint" currently running on the system.
func IsTendermintRunning() bool {
	cmd := exec.Command("sudo", "pidof", "tendermint")
	return cmd.Run() == nil
}

// ExecuteServiceCmd will execute the specified service-related command for the
// Tendermint service on this machine. This requires the `outage-sim-server`
// process to be running as root, and requires the OS to have a `service`
// command (such as CentOS, Ubuntu or Debian).
func ExecuteServiceCmd(status string) error {
	cmd := exec.Command("sudo", "/bin/systemctl", status, "tendermint")
	return cmd.Run()
}
