package settings

import "fmt"

const CmdName = "xcover"

// Version is the xcover release recorded in coverage reports. It defaults to
// "dev" and is overridden at release time via
// -ldflags "-X github.com/maxgio92/xcover/internal/settings.Version=<v>".
var Version = "dev"

var (
	PidFile             = fmt.Sprintf("/tmp/%s.pid", CmdName)
	LogFile             = fmt.Sprintf("/tmp/%s.log", CmdName)
	HealthCheckSockPath = fmt.Sprintf("/tmp/%s.sock", CmdName)
)
