package config

import (
	"encoding/json"
	"os"
)

type Config struct {
	Port     int
	CertPath string
}

func LoadConfig(configPath string) (*Config, error) {
	if configPath == "" {
		configPath = "./config.json"
	}
	contents, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	var config Config
	err = json.Unmarshal(contents, &config)
	if err != nil {
		return nil, err
	}
	return &config, nil
}
