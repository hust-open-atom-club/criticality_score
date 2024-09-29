/*
 * @Date: 2024-08-31 03:29:29
 * @LastEditTime: 2024-09-27 22:02:28
 * @Description:
 */
package workerpool

const (
	defaultScalaThreshold = 1
)

// Config is used to config pool.
type Config struct {
	// threshold for scale.
	// new goroutine is created if len(task chan) > ScaleThreshold.
	// defaults to defaultScalaThreshold.
	ScaleThreshold int32
}

// NewConfig creates a default Config.
func NewConfig() *Config {
	c := &Config{
		ScaleThreshold: defaultScalaThreshold,
	}
	return c
}
