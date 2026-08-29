package settings

import "fmt"

const CmdName = "xcover"

var (
	PidFile             = fmt.Sprintf("/tmp/%s.pid", CmdName)
	LogFile             = fmt.Sprintf("/tmp/%s.log", CmdName)
	HealthCheckSockPath = fmt.Sprintf("/tmp/%s.sock", CmdName)
)
