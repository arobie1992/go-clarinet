package config

import (
	"encoding/json"
	"log"
	"os"
)

type libp2pConfig struct {
	Port     int
	CertPath string
	DbPath   string
}

type adminConfig struct {
	Port int
}

type Config struct {
	Libp2p libp2pConfig `json:"libp2p"`
	Admin  adminConfig  `json:"admin"`
}

func LoadConfig(configPath string) (*Config, error) {
	if configPath == "" {
		configPath = "./config.json"
		log.Printf("No parameter file provided. Will attempt to load default '%s' file.\n", configPath)
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
