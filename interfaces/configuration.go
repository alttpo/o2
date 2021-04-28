package interfaces

import "encoding/json"

type ConfigurationSystem interface {
	LoadConfiguration() bool
	SaveConfiguration() bool
}

type Configurable interface {
	// ProvideConfigurationSystem provides this configurable instance with the system that can load/save its configuration
	ProvideConfigurationSystem(configurationSystem ConfigurationSystem)

	// LoadConfiguration calls json.Unmarshal on the json.RawMessage into its configuration model
	LoadConfiguration(config json.RawMessage)

	// ConfigurationModel returns a json.Marshal interface{} that will be stored by the ConfigurationSystem
	ConfigurationModel() interface{}
}
