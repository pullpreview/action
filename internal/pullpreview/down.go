package pullpreview

func RunDown(opts DownOptions, provider Provider, logger *Logger) error {
	instance := NewInstance(opts.Name, CommonOptions{}, provider, logger)
	if logger != nil {
		logger.Infof("Destroying instance name=%s", instance.Name)
	}
	return instance.Terminate()
}
