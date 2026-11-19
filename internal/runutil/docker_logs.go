package runutil

// Docker's default json-file driver keeps a container's stdout/stderr for as long as the
// container exists, with no size limit, so one noisy app could fill the disk. Every container
// NextDeploy creates rotates its log instead: at most LogMaxFiles files of LogMaxSize each.
const (
	LogMaxSize  = "8m"
	LogMaxFiles = "3"
)

// DockerLogArgs are the `docker run` flags for the rotated log.
func DockerLogArgs() []string {
	return []string{
		"--log-driver", "json-file",
		"--log-opt", "max-size=" + LogMaxSize,
		"--log-opt", "max-file=" + LogMaxFiles,
	}
}

// ComposeLogging is the compose `logging:` block for the rotated log.
func ComposeLogging() map[string]any {
	return map[string]any{
		"driver": "json-file",
		"options": map[string]any{
			"max-size": LogMaxSize,
			"max-file": LogMaxFiles,
		},
	}
}

// SetDefaultComposeLogging adds the rotated log to a compose service that has no `logging:`
// block of its own; a block the user wrote is kept as it is.
func SetDefaultComposeLogging(svc map[string]any) {
	if svc == nil {
		return
	}
	if _, ok := svc["logging"]; ok {
		return
	}
	svc["logging"] = ComposeLogging()
}
