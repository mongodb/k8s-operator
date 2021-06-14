package wait

import "time"

type Configuration func(*Options)

// Options holds values which can be configured when waiting for specific confitions.
type Options struct {
	RetryInterval time.Duration
	Timeout       time.Duration
}

// RetryInterval specifies the RetryInterval
func RetryInterval(retryInterval time.Duration) Configuration {
	return func(options *Options) {
		options.RetryInterval = retryInterval
	}
}

// Timeout specifies the Timeout
func Timeout(timeout time.Duration) Configuration {
	return func(options *Options) {
		options.Timeout = timeout
	}
}

// newOptions returns an Options that has been configured with default values.
func newOptions(fns ...Configuration) Options {
	defaults := defaultStatefulSetReadinessOptions()
	for _, fn := range fns {
		fn(&defaults)
	}
	return defaults
}

func defaultStatefulSetReadinessOptions() Options {
	return Options{
		RetryInterval: time.Second * 15,
		Timeout:       time.Minute * 12,
	}
}
